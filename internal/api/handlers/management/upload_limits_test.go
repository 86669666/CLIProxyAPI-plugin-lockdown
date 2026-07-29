package management

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPutConfigYAMLRejectsOversizedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/v0/management/config.yaml",
		strings.NewReader(strings.Repeat("a", int(configYAMLMaxBodyBytes)+1)),
	)

	(&Handler{}).PutConfigYAML(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestUploadAuthFileRejectsOversizedRawFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandlerWithoutConfigFilePath(
		&config.Config{AuthDir: t.TempDir()},
		coreauth.NewManager(nil, nil, nil),
	)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v0/management/auth-files?name=large.json",
		strings.NewReader(strings.Repeat("a", int(uploadedFileMaxBytes)+1)),
	)

	handler.UploadAuthFile(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestUploadAuthFileRejectsOversizedMultipartFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body, contentType := multipartUploadBody(t, []multipartTestFile{
		{name: "large.json", size: uploadedFileMaxBytes + 1},
	})
	handler := NewHandlerWithoutConfigFilePath(
		&config.Config{AuthDir: t.TempDir()},
		coreauth.NewManager(nil, nil, nil),
	)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", body)
	ctx.Request.Header.Set("Content-Type", contentType)

	handler.UploadAuthFile(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestUploadAuthFileRejectsOversizedMultipartRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body, contentType := multipartUploadBody(t, []multipartTestFile{
		{name: "first.json", size: uploadedFileMaxBytes},
		{name: "second.json", size: uploadedFileMaxBytes},
	})
	if int64(body.Len()) <= uploadRequestMaxBytes {
		t.Fatalf("multipart body size = %d, want greater than %d", body.Len(), uploadRequestMaxBytes)
	}
	handler := NewHandlerWithoutConfigFilePath(
		&config.Config{AuthDir: t.TempDir()},
		coreauth.NewManager(nil, nil, nil),
	)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", body)
	ctx.Request.Header.Set("Content-Type", contentType)

	handler.UploadAuthFile(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestImportVertexCredentialRejectsOversizedFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body, contentType := multipartUploadBody(t, []multipartTestFile{
		{name: "service-account.json", size: uploadedFileMaxBytes + 1},
	})
	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/vertex/import", body)
	ctx.Request.Header.Set("Content-Type", contentType)

	handler.ImportVertexCredential(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

type multipartTestFile struct {
	name string
	size int64
}

func multipartUploadBody(t *testing.T, files []multipartTestFile) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatalf("create multipart file %q: %v", file.name, err)
		}
		if _, err = part.Write(bytes.Repeat([]byte("a"), int(file.size))); err != nil {
			t.Fatalf("write multipart file %q: %v", file.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
