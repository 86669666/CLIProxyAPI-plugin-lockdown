package pluginhost

import "github.com/router-for-me/CLIProxyAPI/v7/internal/config"

// SupportPluginHeaderValue reports whether the current binary exposes plugin capability.
func SupportPluginHeaderValue() string {
	if config.PluginsDisabledByPolicy() {
		return "0"
	}
	return supportPluginValue
}
