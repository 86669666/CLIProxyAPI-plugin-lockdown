package management

import (
	"errors"
	"net/http"
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
