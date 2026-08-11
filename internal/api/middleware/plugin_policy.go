package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func PluginCapabilityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !RequirePluginCapability(c) {
			return
		}
		c.Next()
	}
}

func RequirePluginCapability(c *gin.Context) bool {
	if !config.PluginsDisabledByPolicy() {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error":   "plugin_capability_disabled",
		"message": "plugin capability is disabled by server policy",
	})
	return false
}
