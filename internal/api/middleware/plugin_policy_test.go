package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginpolicy"
)

func TestPluginCapabilityMiddlewareRejectsDisabledPolicy(t *testing.T) {
	t.Setenv(pluginpolicy.DisablePluginsEnv, "true")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/plugins", PluginCapabilityMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plugins", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusForbidden)
	}
}
