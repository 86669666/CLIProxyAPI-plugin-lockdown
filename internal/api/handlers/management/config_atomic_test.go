package management

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteConfigUsesPrivatePermissions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "managed", "config")
	path := filepath.Join(dir, "config.yaml")
	if errWrite := WriteConfig(path, []byte("debug: true\n")); errWrite != nil {
		t.Fatalf("WriteConfig() error = %v", errWrite)
	}
	if runtime.GOOS == "windows" {
		return
	}
	fileInfo, errStat := os.Stat(path)
	if errStat != nil {
		t.Fatalf("stat config file: %v", errStat)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions = %04o, want 0600", got)
	}
	dirInfo, errStatDir := os.Stat(dir)
	if errStatDir != nil {
		t.Fatalf("stat config directory: %v", errStatDir)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory permissions = %04o, want 0700", got)
	}
}

func TestWriteConfigAtomicallyReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if errWrite := os.WriteFile(path, []byte("debug: false\n"), 0o644); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	before, errStat := os.Stat(path)
	if errStat != nil {
		t.Fatalf("stat original config: %v", errStat)
	}

	if errWrite := WriteConfig(path, []byte("debug: true\n")); errWrite != nil {
		t.Fatalf("WriteConfig() error = %v", errWrite)
	}
	after, errStatAfter := os.Stat(path)
	if errStatAfter != nil {
		t.Fatalf("stat replaced config: %v", errStatAfter)
	}
	if os.SameFile(before, after) {
		t.Fatal("WriteConfig() reused the original file instead of atomically replacing it")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read replaced config: %v", errRead)
	}
	if string(data) != "debug: true\n" {
		t.Fatalf("config contents = %q, want updated config", data)
	}
	if runtime.GOOS != "windows" && after.Mode().Perm() != 0o600 {
		t.Fatalf("replaced config permissions = %04o, want 0600", after.Mode().Perm())
	}
}

func TestWriteConfigReplaceFailurePreservesOriginalAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	replaceErr := errors.New("injected replace failure")
	errWrite := writeConfigAtomic(path, []byte("debug: true\n"), func(string, string) error {
		return replaceErr
	})
	if !errors.Is(errWrite, replaceErr) {
		t.Fatalf("writeConfigAtomic() error = %v, want injected replace failure", errWrite)
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
	residual, errGlob := filepath.Glob(filepath.Join(dir, ".config.yaml-*.tmp"))
	if errGlob != nil {
		t.Fatalf("glob config temp files: %v", errGlob)
	}
	if len(residual) != 0 {
		t.Fatalf("residual config temp files = %#v, want none", residual)
	}
}

func TestWriteConfigAtomicSecuresExistingDirectoryBeforeTempCreation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	securityErr := errors.New("injected directory security failure")
	directorySecured := false
	replaced := false

	errWrite := writeConfigAtomicWithSecurity(
		path,
		[]byte("debug: true\n"),
		func(string, string) error {
			replaced = true
			return nil
		},
		func(securedPath string) error {
			directorySecured = true
			if securedPath != dir {
				t.Fatalf("secured directory = %q, want %q", securedPath, dir)
			}
			return securityErr
		},
		func(*os.File) error {
			t.Fatal("file security called after directory security failure")
			return nil
		},
	)
	if !errors.Is(errWrite, securityErr) {
		t.Fatalf("writeConfigAtomicWithSecurity() error = %v, want directory security failure", errWrite)
	}
	if !directorySecured {
		t.Fatal("existing config directory was not secured")
	}
	if replaced {
		t.Fatal("replace called after directory security failure")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
	residual, errGlob := filepath.Glob(filepath.Join(dir, ".config.yaml-*.tmp"))
	if errGlob != nil {
		t.Fatalf("glob config temp files: %v", errGlob)
	}
	if len(residual) != 0 {
		t.Fatalf("residual config temp files = %#v, want none", residual)
	}
}

func TestWriteConfigAtomicRejectsSharedDirectoryPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "current directory", path: "config.yaml"},
		{name: "parent directory", path: filepath.Join("..", "config.yaml")},
		{name: "filesystem root", path: filepath.Join(filepath.VolumeName(string(filepath.Separator))+string(filepath.Separator), "config.yaml")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			securityCalled := false
			errWrite := writeConfigAtomicWithSecurity(
				test.path,
				[]byte("debug: true\n"),
				func(string, string) error { return nil },
				func(string) error {
					securityCalled = true
					return nil
				},
				func(*os.File) error { return nil },
			)
			if errWrite == nil || !strings.Contains(errWrite.Error(), "no dedicated parent directory") {
				t.Fatalf("writeConfigAtomicWithSecurity() error = %v, want shared directory rejection", errWrite)
			}
			if securityCalled {
				t.Fatal("directory security called for shared directory path")
			}
		})
	}
}
