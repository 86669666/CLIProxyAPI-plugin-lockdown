package management

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	configYAMLMaxBodyBytes int64 = 4 * 1024 * 1024
	uploadedFileMaxBytes   int64 = 1 * 1024 * 1024
	uploadRequestMaxBytes  int64 = 2 * 1024 * 1024
)

var errUploadedFileTooLarge = errors.New("uploaded file exceeds 1 MB limit")

func isRequestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

// ManagementBodyLimitMiddleware enforces a hard cap before JSON handlers decode request bodies.
func ManagementBodyLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		data, errRead := io.ReadAll(io.LimitReader(c.Request.Body, configYAMLMaxBodyBytes+1))
		if errRead != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		if int64(len(data)) > configYAMLMaxBodyBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request entity too large"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(data))
		c.Request.ContentLength = int64(len(data))
		c.Next()
	}
}
