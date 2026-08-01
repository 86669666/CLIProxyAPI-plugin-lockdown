package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginpolicy"
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

func TestPluginsDisabledByPolicyInvalidValueFailsClosed(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "not-a-bool")
	if !PluginsDisabledByPolicy() {
		t.Fatal("PluginsDisabledByPolicy() = false for invalid value")
	}
	disabled, errPolicy := ParsePluginsDisabledPolicy("not-a-bool")
	if !disabled || !errors.Is(errPolicy, pluginpolicy.ErrInvalidValue) {
		t.Fatalf("ParsePluginsDisabledPolicy() = (%v, %v), want true and ErrInvalidValue", disabled, errPolicy)
	}
}

func TestParsePluginsDisabledPolicyBoolValues(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "", want: false},
		{raw: "false", want: false},
		{raw: "0", want: false},
		{raw: "true", want: true},
		{raw: "1", want: true},
	}
	for _, tt := range tests {
		disabled, errPolicy := ParsePluginsDisabledPolicy(tt.raw)
		if errPolicy != nil || disabled != tt.want {
			t.Fatalf("ParsePluginsDisabledPolicy(%q) = (%v, %v), want (%v, nil)", tt.raw, disabled, errPolicy, tt.want)
		}
	}
}
