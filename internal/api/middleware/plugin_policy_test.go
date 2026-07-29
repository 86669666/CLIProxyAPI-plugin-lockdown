package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPluginCapabilityMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		policy     string
		wantStatus int
		wantError  string
		wantCalled bool
	}{
		{
			name:       "disabled policy rejects request",
			policy:     "true",
			wantStatus: http.StatusForbidden,
			wantError:  pluginCapabilityErrorCode,
		},
		{
			name:       "invalid policy rejects request",
			policy:     "invalid",
			wantStatus: http.StatusInternalServerError,
			wantError:  invalidPluginPolicyErrorCode,
		},
		{
			name:       "enabled policy allows request",
			wantStatus: http.StatusNoContent,
			wantCalled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CLIPROXY_DISABLE_PLUGINS", test.policy)
			called := false
			router := gin.New()
			router.GET("/plugin", PluginCapabilityMiddleware(), func(c *gin.Context) {
				called = true
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/plugin", nil))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if called != test.wantCalled {
				t.Fatalf("handler called = %t, want %t", called, test.wantCalled)
			}
			if test.wantError == "" {
				return
			}

			var response struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
			}
			if response.Error != test.wantError {
				t.Fatalf("error = %q, want %q", response.Error, test.wantError)
			}
			if response.Message == "" {
				t.Fatal("expected standardized error message")
			}
		})
	}
}

func TestPluginCapabilityDisabledByPolicyUsesStandardError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "invalid")

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	if _, proceed := PluginCapabilityDisabledByPolicy(context); proceed {
		t.Fatal("proceed = true, want false")
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if !context.IsAborted() {
		t.Fatal("expected context to be aborted")
	}
}
