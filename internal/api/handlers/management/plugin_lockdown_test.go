package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/api/middleware"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func runPluginPolicyHandler(c *gin.Context, handler gin.HandlerFunc) {
	middleware.PluginCapabilityMiddleware()(c)
	if !c.IsAborted() {
		handler(c)
	}
}

func TestPluginStoreHandlersRejectDisablePolicyBeforeAccess(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	h := &Handler{}
	for _, tt := range []struct {
		method string
		path   string
		call   func(*gin.Context)
	}{
		{http.MethodGet, "/v0/management/plugin-store", h.ListPluginStore},
		{http.MethodPost, "/v0/management/plugin-store/sample/install", h.InstallPluginFromStore},
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Params = gin.Params{{Key: "id", Value: "sample"}}
		c.Request = httptest.NewRequest(tt.method, tt.path, nil)
		runPluginPolicyHandler(c, tt.call)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "plugin_capability_disabled") {
			t.Fatalf("status/body = %d %s, want policy rejection", rec.Code, rec.Body.String())
		}
	}
}

func TestPluginMutationHandlersRejectDisablePolicy(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	for _, tt := range []struct {
		method string
		path   string
		body   string
		call   func(*gin.Context)
	}{
		{http.MethodPatch, "/v0/management/plugins/sample/enabled", `{"enabled":true}`, h.PatchPluginEnabled},
		{http.MethodPut, "/v0/management/plugins/sample/config", `{"enabled":true}`, h.PutPluginConfig},
		{http.MethodPatch, "/v0/management/plugins/sample/config", `{"mode":"fast"}`, h.PatchPluginConfig},
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Params = gin.Params{{Key: "id", Value: "sample"}}
		c.Request = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		c.Request.Header.Set("Content-Type", "application/json")
		runPluginPolicyHandler(c, tt.call)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "plugin_capability_disabled") {
			t.Fatalf("status/body = %d %s, want policy rejection", rec.Code, rec.Body.String())
		}
	}
}

func TestPutConfigYAMLRejectsPluginEnableUnderPolicy(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	for _, body := range []string{
		"plugins:\n  enabled: true\n",
		"plugins:\n  enabled: false\n  configs:\n    sample:\n      enabled: true\n",
	} {
		configPath := writeTestConfigFile(t)
		h := &Handler{cfg: &config.Config{}, configFilePath: configPath}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader(body))
		h.PutConfigYAML(c)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "plugin_capability_disabled") {
			t.Fatalf("status/body = %d %s, want policy rejection", rec.Code, rec.Body.String())
		}
		data, err := os.ReadFile(configPath)
		if err != nil || string(data) != "{}\n" {
			t.Fatalf("config changed after rejection: %q err=%v", data, err)
		}
	}
}

func TestPutConfigYAMLAllowsUnrelatedDisabledConfigUnderPolicy(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	configPath := writeTestConfigFile(t)
	h := &Handler{cfg: &config.Config{}, configFilePath: configPath}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader("debug: true\nplugins:\n  enabled: false\n"))
	h.PutConfigYAML(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPutConfigYAMLValidationTempFileUsesSystemTempDir(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	validationTempDir := filepath.Join(t.TempDir(), "validation-temp")
	if err := os.MkdirAll(validationTempDir, 0o700); err != nil {
		t.Fatalf("create validation temp dir: %v", err)
	}
	t.Setenv("TMPDIR", validationTempDir)

	configPath := writeTestConfigFile(t)
	h := &Handler{cfg: &config.Config{}, configFilePath: configPath}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", strings.NewReader("debug: true\nplugins:\n  enabled: false\n"))
	h.PutConfigYAML(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "config-validate-") || strings.HasPrefix(entry.Name(), "cliproxyapi-config-validate-") {
			t.Fatalf("validation temp file was created beside config: %s", entry.Name())
		}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !strings.Contains(string(data), "debug: true") {
		t.Fatalf("config was not updated: %q", data)
	}
}
