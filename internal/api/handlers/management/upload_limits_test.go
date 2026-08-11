package management

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPutConfigYAMLRejectsOversizedBody(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	h.configFilePath = filepath.Join(t.TempDir(), "config.yaml")
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", bytes.NewReader(make([]byte, configYAMLMaxBodyBytes+1)))
	h.PutConfigYAML(ctx)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestUploadAuthFileRejectsOversizedRawBody(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files?name=large.json", bytes.NewReader(make([]byte, uploadedFileMaxBytes+1)))
	h.UploadAuthFile(ctx)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestManagementBodyLimitMiddlewareRejectsStreamingOverflow(t *testing.T) {
	engine := gin.New()
	engine.POST("/json", ManagementBodyLimitMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/json", bytes.NewReader(make([]byte, configYAMLMaxBodyBytes+1)))
	req.ContentLength = -1
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}
