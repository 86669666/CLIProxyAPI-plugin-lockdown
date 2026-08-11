//go:build !windows

package management

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteConfigAcceptsSafeExistingDirectoryWithoutChangingPermissions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "managed")
	if errMkdir := os.Mkdir(dir, 0o755); errMkdir != nil {
		t.Fatalf("create config directory: %v", errMkdir)
	}
	if errChmod := os.Chmod(dir, 0o755); errChmod != nil {
		t.Fatalf("set config directory permissions: %v", errChmod)
	}
	path := filepath.Join(dir, "config.yaml")
	if errWrite := os.WriteFile(path, []byte("debug: false\n"), 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}

	if errWrite := WriteConfig(path, []byte("debug: true\n")); errWrite != nil {
		t.Fatalf("WriteConfig() error = %v", errWrite)
	}
	dirInfo, errStat := os.Stat(dir)
	if errStat != nil {
		t.Fatalf("stat config directory: %v", errStat)
	}
	if got := dirInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("existing config directory permissions = %04o, want unchanged 0755", got)
	}
}

func TestWriteConfigRejectsUnsafeExistingDirectoryWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "managed")
	if errMkdir := os.Mkdir(dir, 0o777); errMkdir != nil {
		t.Fatalf("create config directory: %v", errMkdir)
	}
	if errChmod := os.Chmod(dir, 0o777); errChmod != nil {
		t.Fatalf("set unsafe config directory permissions: %v", errChmod)
	}
	path := filepath.Join(dir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}

	if errWrite := WriteConfig(path, []byte("debug: true\n")); errWrite == nil {
		t.Fatal("WriteConfig() succeeded with a group/other-writable config directory")
	}
	dirInfo, errStat := os.Stat(dir)
	if errStat != nil {
		t.Fatalf("stat config directory: %v", errStat)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Fatalf("unsafe config directory permissions = %04o, want unchanged 0777", got)
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

func TestWriteConfigRejectsSymlinkDirectoryAndPreservesOriginal(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if errMkdir := os.Mkdir(realDir, 0o700); errMkdir != nil {
		t.Fatalf("create real config directory: %v", errMkdir)
	}
	path := filepath.Join(realDir, "config.yaml")
	original := []byte("debug: false\n")
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write original config: %v", errWrite)
	}
	linkedDir := filepath.Join(root, "linked")
	if errSymlink := os.Symlink(realDir, linkedDir); errSymlink != nil {
		t.Fatalf("create config directory symlink: %v", errSymlink)
	}

	errWrite := WriteConfig(filepath.Join(linkedDir, "config.yaml"), []byte("debug: true\n"))
	if errWrite == nil {
		t.Fatal("WriteConfig() succeeded through a symlink config directory")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read original config: %v", errRead)
	}
	if string(data) != string(original) {
		t.Fatalf("original config = %q, want %q", data, original)
	}
	residual, errGlob := filepath.Glob(filepath.Join(realDir, ".config.yaml-*.tmp"))
	if errGlob != nil {
		t.Fatalf("glob config temp files: %v", errGlob)
	}
	if len(residual) != 0 {
		t.Fatalf("residual config temp files = %#v, want none", residual)
	}
}

func TestWriteConfigRejectsSymlinkPathComponent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real-parent")
	if errMkdir := os.Mkdir(realParent, 0o700); errMkdir != nil {
		t.Fatalf("create real parent: %v", errMkdir)
	}
	linkedParent := filepath.Join(root, "linked-parent")
	if errSymlink := os.Symlink(realParent, linkedParent); errSymlink != nil {
		t.Fatalf("create parent symlink: %v", errSymlink)
	}

	errWrite := WriteConfig(filepath.Join(linkedParent, "managed", "config.yaml"), []byte("debug: true\n"))
	if errWrite == nil {
		t.Fatal("WriteConfig() succeeded through a symlink path component")
	}
	if _, errStat := os.Stat(filepath.Join(realParent, "managed")); !os.IsNotExist(errStat) {
		t.Fatalf("managed directory stat error = %v, want not exist", errStat)
	}
}
