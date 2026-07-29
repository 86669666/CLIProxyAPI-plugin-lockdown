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

// safeOpenBeneath opens name only when it resolves to a non-symlink path below baseDir.
func safeOpenBeneath(baseDir, name string) (*os.File, error) {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.Contains(name, "\\") {
		return nil, fmt.Errorf("%w: invalid file name", errUnsafeFilePath)
	}

	for _, component := range strings.Split(name, string(os.PathSeparator)) {
		if component == ".." {
			return nil, fmt.Errorf("%w: path traversal", errUnsafeFilePath)
		}
	}

	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: path traversal", errUnsafeFilePath)
	}

	baseAbs, errAbs := filepath.Abs(baseDir)
	if errAbs != nil {
		return nil, fmt.Errorf("resolve base directory: %w", errAbs)
	}
	targetAbs := filepath.Join(baseAbs, cleanName)
	rel, errRel := filepath.Rel(baseAbs, targetAbs)
	if errRel != nil {
		return nil, fmt.Errorf("resolve relative file path: %w", errRel)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%w: file is outside base directory", errUnsafeFilePath)
	}

	path := baseAbs
	for _, component := range strings.Split(rel, string(os.PathSeparator)) {
		path = filepath.Join(path, component)
		info, errLstat := os.Lstat(path)
		if errLstat != nil {
			return nil, errLstat
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic links are not allowed", errUnsafeFilePath)
		}
	}

	file, errOpen := os.OpenInRoot(baseAbs, rel)
	if errOpen != nil {
		return nil, errOpen
	}

	info, errLstat := os.Lstat(targetAbs)
	if errLstat != nil {
		_ = file.Close()
		return nil, errLstat
	}
	if info.Mode()&os.ModeSymlink != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: symbolic links are not allowed", errUnsafeFilePath)
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

	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), file)
	return nil
}
