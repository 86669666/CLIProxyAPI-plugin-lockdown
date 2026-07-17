package pluginhost

import "testing"

func TestSupportPluginHeaderValueDisablePolicy(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	if got := SupportPluginHeaderValue(); got != "0" {
		t.Fatalf("SupportPluginHeaderValue() = %q, want 0", got)
	}
}
