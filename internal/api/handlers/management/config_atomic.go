package management

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type configFileReplaceFunc func(string, string) error
type configDirectorySecurityFunc func(string) error
type configFileSecurityFunc func(*os.File) error

func writeConfigAtomic(path string, data []byte, replace configFileReplaceFunc) error {
	return writeConfigAtomicWithSecurity(path, data, replace, secureConfigDirectory, secureConfigFile)
}

func writeConfigAtomicWithSecurity(path string, data []byte, replace configFileReplaceFunc, secureDirectory configDirectorySecurityFunc, secureFile configFileSecurityFunc) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("config path is empty")
	}
	if replace == nil {
		return fmt.Errorf("config replace function is unavailable")
	}
	if secureDirectory == nil {
		return fmt.Errorf("config directory security function is unavailable")
	}
	if secureFile == nil {
		return fmt.Errorf("config file security function is unavailable")
	}
	path = filepath.Clean(path)
	dir := filepath.Clean(filepath.Dir(path))
	if !isControlledConfigDirectory(dir) {
		return fmt.Errorf("config path %q has no dedicated parent directory; refusing to modify a shared directory", path)
	}
	if errSecureDir := secureDirectory(dir); errSecureDir != nil {
		return fmt.Errorf("secure config directory: %w", errSecureDir)
	}

	tmpFile, errCreate := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if errCreate != nil {
		return fmt.Errorf("create config temp file: %w", errCreate)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if errSecureFile := secureFile(tmpFile); errSecureFile != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("secure config temp file: %w", errSecureFile)
	}
	if _, errWrite := tmpFile.Write(data); errWrite != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write config temp file: %w", errWrite)
	}
	if errSync := tmpFile.Sync(); errSync != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync config temp file: %w", errSync)
	}
	if errClose := tmpFile.Close(); errClose != nil {
		return fmt.Errorf("close config temp file: %w", errClose)
	}
	if errReplace := replace(tmpPath, path); errReplace != nil {
		return fmt.Errorf("replace config file: %w", errReplace)
	}
	tmpPath = ""
	if errSyncDir := syncConfigDirectory(dir); errSyncDir != nil {
		return fmt.Errorf("sync config directory: %w", errSyncDir)
	}
	return nil
}

func isControlledConfigDirectory(dir string) bool {
	if dir == "" || dir == "." || dir == ".." {
		return false
	}
	volume := filepath.VolumeName(dir)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + root
	}
	return !strings.EqualFold(filepath.Clean(dir), filepath.Clean(root))
}
