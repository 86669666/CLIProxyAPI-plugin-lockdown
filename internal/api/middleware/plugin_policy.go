package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

const (
	invalidPluginPolicyErrorCode = "invalid_plugin_policy"
	pluginCapabilityErrorCode    = "plugin_capability_disabled"
)

// PluginCapabilityMiddleware rejects requests that require plugin capability
// when the process-wide plugin lockdown policy disables it.
func PluginCapabilityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !RequirePluginCapability(c) {
			return
		}
		c.Next()
	}
}

// PluginCapabilityDisabledByPolicy evaluates the plugin lockdown policy and
// writes a standardized error response when the policy is invalid.
func PluginCapabilityDisabledByPolicy(c *gin.Context) (disabled bool, proceed bool) {
	disabled, err := config.PluginsDisabledByPolicy()
	if err == nil {
		return disabled, true
	}

	log.WithError(err).Error("invalid plugin lockdown policy")
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error":   invalidPluginPolicyErrorCode,
		"message": "plugin lockdown policy is invalid",
	})
	return false, false
}

// RequirePluginCapability evaluates plugin policy and rejects disabled plugin capability.
func RequirePluginCapability(c *gin.Context) bool {
	disabled, proceed := PluginCapabilityDisabledByPolicy(c)
	if !proceed {
		return false
	}
	if !disabled {
		return true
	}

	RejectPluginCapability(c)
	return false
}

// RejectPluginCapability writes the standardized disabled-plugin response.
func RejectPluginCapability(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error":   pluginCapabilityErrorCode,
		"message": "plugin capability is disabled by server policy",
	})
}
