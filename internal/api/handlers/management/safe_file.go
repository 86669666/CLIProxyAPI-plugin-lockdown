package management

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

var errUnsafeFilePath = errors.New("unsafe file path")

func safeOpenBeneath(baseDir, name string) (*os.File, error) {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.Contains(name, "\\") {
		return nil, fmt.Errorf("%w: invalid file name", errUnsafeFilePath)
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: path traversal", errUnsafeFilePath)
	}
	for _, component := range strings.Split(cleanName, string(os.PathSeparator)) {
		if component == ".." {
			return nil, fmt.Errorf("%w: path traversal", errUnsafeFilePath)
		}
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
	file, errOpen := os.OpenInRoot(baseAbs, cleanName)
	if errOpen != nil {
		return nil, errOpen
	}
	info, errStat := file.Stat()
	if errStat != nil {
		_ = file.Close()
		return nil, errStat
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: file is not regular", errUnsafeFilePath)
	}
	return file, nil
}

func serveFileAttachment(c *gin.Context, file *os.File, name string) error {
	info, errStat := file.Stat()
	if errStat != nil {
		return fmt.Errorf("stat opened file: %w", errStat)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: file is not regular", errUnsafeFilePath)
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	http.ServeContent(c.Writer, c.Request, filepath.Base(name), info.ModTime(), file)
	return nil
}
