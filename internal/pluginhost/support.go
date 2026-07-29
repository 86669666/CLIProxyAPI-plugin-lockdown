package pluginhost

import "github.com/router-for-me/CLIProxyAPI/v7/internal/config"

// SupportPluginHeaderValue reports whether the current binary exposes plugin capability.
func SupportPluginHeaderValue() string {
	disabled, err := config.PluginsDisabledByPolicy()
	if err != nil || disabled {
		return "0"
	}
	return supportPluginValue
}
