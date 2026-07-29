package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizePluginsConfigDisablePolicyForcesRuntimeOff(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")
	cfg, err := ParseConfigBytes([]byte("plugins:\n  enabled: true\n  configs:\n    sample:\n      enabled: true\n      priority: 7\n      mode: safe\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.Plugins.Enabled {
		t.Fatal("Plugins.Enabled = true, want false under policy")
	}
	item := cfg.Plugins.Configs["sample"]
	if item.Enabled == nil || *item.Enabled {
		t.Fatalf("sample enabled = %#v, want false under policy", item.Enabled)
	}
	raw, err := yaml.Marshal(&item.Raw)
	if err != nil {
		t.Fatalf("yaml.Marshal(Raw) error = %v", err)
	}
	for _, want := range []string{"enabled: false", "priority: 7", "mode: safe"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("raw YAML missing %q:\n%s", want, raw)
		}
	}
}

func TestPluginsDisabledByPolicyInvalidValueReturnsError(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "not-a-bool")
	disabled, err := PluginsDisabledByPolicy()
	if err == nil {
		t.Fatal("PluginsDisabledByPolicy() error = nil, want error")
	}
	if disabled {
		t.Fatal("PluginsDisabledByPolicy() = true for invalid value")
	}
}
