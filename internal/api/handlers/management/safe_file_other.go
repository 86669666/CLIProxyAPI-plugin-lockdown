//go:build !linux

package management

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func safeOpenFileBeneath(baseDir, name string, flags int, perm os.FileMode) (*os.File, error) {
	cleanName, errValidate := validateSafeRelativePath(name)
	if errValidate != nil {
		return nil, errValidate
	}
	baseAbs, errAbs := filepath.Abs(baseDir)
	if errAbs != nil {
		return nil, fmt.Errorf("resolve base directory: %w", errAbs)
	}
	currentPath := baseAbs
	for _, component := range strings.Split(cleanName, string(os.PathSeparator)) {
		currentPath = filepath.Join(currentPath, component)
		info, errLstat := os.Lstat(currentPath)
		if errLstat != nil {
			return nil, errLstat
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic links are not allowed", errUnsafeFilePath)
		}
	}
	root, errRoot := os.OpenRoot(baseAbs)
	if errRoot != nil {
		return nil, errRoot
	}
	defer func() {
		_ = root.Close()
	}()
	info, errLstat := root.Lstat(cleanName)
	if errLstat != nil {
		return nil, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: symbolic links are not allowed", errUnsafeFilePath)
	}
	file, errOpen := root.OpenFile(cleanName, flags, perm)
	if errOpen != nil {
		return nil, errOpen
	}
	if errRegular := validateOpenedRegularFile(file); errRegular != nil {
		_ = file.Close()
		return nil, errRegular
	}
	return file, nil
}

func safeRemoveRegularBeneath(baseDir, name string) (bool, error) {
	cleanName, errValidate := validateSafeRelativePath(name)
	if errValidate != nil {
		return false, errValidate
	}
	baseAbs, errAbs := filepath.Abs(baseDir)
	if errAbs != nil {
		return false, fmt.Errorf("resolve base directory: %w", errAbs)
	}
	root, errRoot := os.OpenRoot(baseAbs)
	if errRoot != nil {
		return false, errRoot
	}
	defer func() {
		_ = root.Close()
	}()
	info, errLstat := root.Lstat(cleanName)
	if errLstat != nil {
		return false, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, nil
	}
	if errRemove := root.Remove(cleanName); errRemove != nil {
		return false, errRemove
	}
	return true, nil
}
