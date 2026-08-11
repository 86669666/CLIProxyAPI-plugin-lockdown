//go:build !windows

package management

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func secureConfigDirectory(path string) error {
	cleanPath := filepath.Clean(path)
	startPath := "."
	if filepath.IsAbs(cleanPath) {
		startPath = string(filepath.Separator)
		cleanPath = strings.TrimPrefix(cleanPath, startPath)
	}

	currentFD, errOpen := unix.Open(startPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if errOpen != nil {
		return fmt.Errorf("open config directory root: %w", errOpen)
	}
	defer func() {
		_ = unix.Close(currentFD)
	}()

	components := configDirectoryComponents(cleanPath)
	finalCreated := false
	for componentIndex, component := range components {
		nextFD, componentCreated, errComponent := openConfigDirectoryComponent(currentFD, component)
		if errComponent != nil {
			return errComponent
		}
		if componentIndex == len(components)-1 {
			finalCreated = componentCreated
		}
		if errClose := unix.Close(currentFD); errClose != nil {
			_ = unix.Close(nextFD)
			return fmt.Errorf("close config directory component: %w", errClose)
		}
		currentFD = nextFD
	}

	var stat unix.Stat_t
	if errStat := unix.Fstat(currentFD, &stat); errStat != nil {
		return fmt.Errorf("fstat config directory: %w", errStat)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("config directory is not a real directory")
	}
	if finalCreated {
		if errChmod := unix.Fchmod(currentFD, 0o700); errChmod != nil {
			return fmt.Errorf("chmod newly created config directory: %w", errChmod)
		}
		if errStat := unix.Fstat(currentFD, &stat); errStat != nil {
			return fmt.Errorf("fstat secured config directory: %w", errStat)
		}
		if stat.Mode&0o777 != 0o700 {
			return fmt.Errorf("newly created config directory permissions are %04o after chmod, want 0700", stat.Mode&0o777)
		}
	}
	if errValidate := validateExistingUnixConfigDirectory(&stat); errValidate != nil {
		return errValidate
	}
	return nil
}

func configDirectoryComponents(path string) []string {
	components := strings.Split(path, string(filepath.Separator))
	filtered := components[:0]
	for _, component := range components {
		if component != "" && component != "." {
			filtered = append(filtered, component)
		}
	}
	return filtered
}

func openConfigDirectoryComponent(parentFD int, component string) (int, bool, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	componentFD, errOpen := unix.Openat(parentFD, component, flags, 0)
	if errOpen == nil {
		return componentFD, false, nil
	}
	if !errors.Is(errOpen, unix.ENOENT) {
		return -1, false, fmt.Errorf("open config directory component %q without following symlinks: %w", component, errOpen)
	}
	created := false
	if errMkdir := unix.Mkdirat(parentFD, component, 0o700); errMkdir != nil {
		if !errors.Is(errMkdir, unix.EEXIST) {
			return -1, false, fmt.Errorf("create config directory component %q: %w", component, errMkdir)
		}
	} else {
		created = true
	}
	componentFD, errOpen = unix.Openat(parentFD, component, flags, 0)
	if errOpen != nil {
		return -1, false, fmt.Errorf("open config directory component %q after creation without following symlinks: %w", component, errOpen)
	}
	return componentFD, created, nil
}

func validateExistingUnixConfigDirectory(stat *unix.Stat_t) error {
	effectiveUID := uint32(os.Geteuid())
	if stat.Uid != effectiveUID && stat.Uid != 0 {
		return fmt.Errorf("config directory owner uid is %d, want current euid %d or root", stat.Uid, effectiveUID)
	}
	if stat.Mode&0o022 != 0 {
		return fmt.Errorf("config directory permissions are %04o; group or other write access is not allowed", stat.Mode&0o777)
	}
	return nil
}

func secureConfigFile(file *os.File) error {
	return file.Chmod(0o600)
}

func replaceConfigFile(source, destination string) error {
	return os.Rename(source, destination)
}

func syncConfigDirectory(path string) error {
	dir, errOpen := os.Open(path)
	if errOpen != nil {
		return errOpen
	}
	defer func() {
		_ = dir.Close()
	}()
	return dir.Sync()
}
