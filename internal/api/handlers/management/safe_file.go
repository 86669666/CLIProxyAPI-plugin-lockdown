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
	return safeOpenFileBeneath(baseDir, name, os.O_RDONLY, 0)
}

func validateSafeRelativePath(name string) (string, error) {
	if strings.TrimSpace(name) == "" || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.Contains(name, "\\") {
		return "", fmt.Errorf("%w: invalid file name", errUnsafeFilePath)
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: path traversal", errUnsafeFilePath)
	}
	for _, component := range strings.Split(cleanName, string(os.PathSeparator)) {
		if component == ".." {
			return "", fmt.Errorf("%w: path traversal", errUnsafeFilePath)
		}
	}
	return cleanName, nil
}

func validateOpenedRegularFile(file *os.File) error {
	if file == nil {
		return fmt.Errorf("%w: file is unavailable", errUnsafeFilePath)
	}
	info, errStat := file.Stat()
	if errStat != nil {
		return errStat
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: file is not regular", errUnsafeFilePath)
	}
	return nil
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
