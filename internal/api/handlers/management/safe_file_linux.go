//go:build linux

package management

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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
	dirFD, errOpenDir := unix.Open(baseAbs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errOpenDir != nil {
		return nil, errOpenDir
	}
	defer func() {
		_ = unix.Close(dirFD)
	}()

	components := strings.Split(cleanName, string(os.PathSeparator))
	currentFD := dirFD
	for _, component := range components[:len(components)-1] {
		nextFD, errOpen := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if currentFD != dirFD {
			_ = unix.Close(currentFD)
		}
		if errOpen != nil {
			return nil, errOpen
		}
		currentFD = nextFD
	}
	if currentFD != dirFD {
		defer func() {
			_ = unix.Close(currentFD)
		}()
	}

	fd, errOpen := unix.Openat(currentFD, components[len(components)-1], flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, uint32(perm.Perm()))
	if errOpen != nil {
		return nil, errOpen
	}
	file := os.NewFile(uintptr(fd), filepath.Join(baseAbs, cleanName))
	if errRegular := validateOpenedRegularFile(file); errRegular != nil {
		_ = file.Close()
		return nil, errRegular
	}
	return file, nil
}

func safeRemoveRegularBeneath(baseDir, name string) (bool, error) {
	file, errOpen := safeOpenFileBeneath(baseDir, name, os.O_RDONLY, 0)
	if errOpen != nil {
		if os.IsNotExist(errOpen) || errors.Is(errOpen, errUnsafeFilePath) || errors.Is(errOpen, unix.ELOOP) {
			return false, nil
		}
		return false, errOpen
	}
	defer func() {
		_ = file.Close()
	}()
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
