package management

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteConfigAtomicallyReplacesExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not allow replacing a file while it is open")
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	original := []byte("debug: false\n")
	replacement := []byte("debug: true\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("create existing config: %v", err)
	}

	oldFile, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("open existing config: %v", err)
	}
	defer func() {
		if errClose := oldFile.Close(); errClose != nil {
			t.Errorf("close existing config: %v", errClose)
		}
	}()

	if errWrite := WriteConfig(configPath, replacement); errWrite != nil {
		t.Fatalf("WriteConfig() error = %v", errWrite)
	}

	oldData, err := io.ReadAll(oldFile)
	if err != nil {
		t.Fatalf("read original file handle: %v", err)
	}
	if string(oldData) != string(original) {
		t.Fatalf("original file handle data = %q, want %q", oldData, original)
	}

	newData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read replaced config: %v", err)
	}
	if string(newData) != string(replacement) {
		t.Fatalf("replaced config data = %q, want %q", newData, replacement)
	}
	assertNoConfigTempFiles(t, configDir)
}

func TestWriteConfigCleansTemporaryFileWhenRenameFails(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatalf("create conflicting config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "keep"), []byte("unchanged"), 0o600); err != nil {
		t.Fatalf("create conflicting config content: %v", err)
	}

	if err := WriteConfig(configPath, []byte("debug: true\n")); err == nil {
		t.Fatal("WriteConfig() error = nil, want rename failure")
	}

	if _, err := os.Stat(filepath.Join(configPath, "keep")); err != nil {
		t.Fatalf("conflicting destination changed after failure: %v", err)
	}
	assertNoConfigTempFiles(t, configDir)
}

func TestWriteConfigUsesPrivateFileAndDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix file permission semantics")
	}

	configDir := filepath.Join(t.TempDir(), "config")
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}

	if err := WriteConfig(configPath, []byte("debug: true\n")); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}

	assertFilePermissions(t, configDir, 0o700)
	assertFilePermissions(t, configPath, 0o600)
}

func TestWriteConfigRepairsExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix file permission semantics")
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.Chmod(configDir, 0o755); err != nil {
		t.Fatalf("set initial config directory permissions: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("debug: false\n"), 0o644); err != nil {
		t.Fatalf("create existing config: %v", err)
	}

	if err := WriteConfig(configPath, []byte("debug: true\n")); err != nil {
		t.Fatalf("WriteConfig() error = %v", err)
	}

	assertFilePermissions(t, configDir, 0o700)
	assertFilePermissions(t, configPath, 0o600)
}

func assertFilePermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s = %o, want %o", path, got, want)
	}
}

func assertNoConfigTempFiles(t *testing.T, configDir string) {
	t.Helper()

	tempFiles, err := filepath.Glob(filepath.Join(configDir, ".config.yaml.tmp-*"))
	if err != nil {
		t.Fatalf("find temporary config files: %v", err)
	}
	if len(tempFiles) != 0 {
		t.Fatalf("temporary config files were not cleaned up: %v", tempFiles)
	}
}
