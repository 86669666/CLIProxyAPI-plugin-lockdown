package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

const startupMainProcessEnv = "CLIPROXY_STARTUP_MAIN_PROCESS"

func TestMainProcessForStartupTests(t *testing.T) {
	if os.Getenv(startupMainProcessEnv) != "1" {
		return
	}
	os.Args = []string{
		"cli-proxy-api-startup-test",
		"-config",
		os.Getenv("CLIPROXY_STARTUP_CONFIG_PATH"),
	}
	main()
}

func TestStartupLoadsDotEnvBeforePolicyEvaluation(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte("CLIPROXY_DISABLE_PLUGINS=true\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	unsetEnvironment(t, "CLIPROXY_DISABLE_PLUGINS")

	loadStartupDotEnv(workDir)
	disabled, err := config.PluginsDisabledByPolicy()
	if err != nil {
		t.Fatalf("PluginsDisabledByPolicy() error = %v", err)
	}
	if !disabled {
		t.Fatal("PluginsDisabledByPolicy() = false, want true from .env")
	}
}

func TestStartupPolicyLogsEvaluatedState(t *testing.T) {
	t.Setenv("CLIPROXY_DISABLE_PLUGINS", "true")

	logger := log.StandardLogger()
	originalOutput := logger.Out
	originalLevel := logger.Level
	originalFormatter := logger.Formatter
	var output bytes.Buffer
	logger.SetOutput(&output)
	logger.SetLevel(log.InfoLevel)
	logger.SetFormatter(&log.TextFormatter{DisableTimestamp: true})
	t.Cleanup(func() {
		logger.SetOutput(originalOutput)
		logger.SetLevel(originalLevel)
		logger.SetFormatter(originalFormatter)
	})

	disabled, err := evaluateStartupPluginPolicy()
	if err != nil {
		t.Fatalf("evaluateStartupPluginPolicy() error = %v", err)
	}
	if !disabled {
		t.Fatal("evaluateStartupPluginPolicy() = false, want true")
	}
	if got := output.String(); !strings.Contains(got, "plugin lockdown policy evaluated") || !strings.Contains(got, "disabled=true") {
		t.Fatalf("startup policy log = %q, want evaluated message and disabled=true", got)
	}
}

func TestStartupRejectsInvalidPolicyBeforePluginBootstrap(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte("CLIPROXY_DISABLE_PLUGINS=not-a-bool\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	configPath := filepath.Join(workDir, "bootstrap-config")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}

	cmd, output := startupMainCommand(t, workDir, configPath)
	err := cmd.Run()
	if err == nil {
		t.Fatal("startup process exited successfully, want policy rejection")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("startup process error = %v, want exit code 1", err)
	}
	if !strings.Contains(output.String(), "invalid plugin lockdown policy; refusing to start") {
		t.Fatalf("startup output = %q, missing invalid-policy log", output.String())
	}
	if strings.Contains(output.String(), "failed to read plugin bootstrap config") {
		t.Fatalf("startup output = %q, plugin bootstrap ran before policy rejection", output.String())
	}
}

func TestStartupEvaluatesPolicyBeforePluginBootstrap(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, ".env"), []byte("CLIPROXY_DISABLE_PLUGINS=true\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	configPath := filepath.Join(workDir, "bootstrap-config")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}

	cmd, output := startupMainCommand(t, workDir, configPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("startup process error = %v; output: %s", err, output.String())
	}
	policyIndex := strings.Index(output.String(), "plugin lockdown policy evaluated")
	bootstrapIndex := strings.Index(output.String(), "failed to read plugin bootstrap config")
	if policyIndex < 0 || bootstrapIndex < 0 {
		t.Fatalf("startup output = %q, missing policy or bootstrap log", output.String())
	}
	if policyIndex > bootstrapIndex {
		t.Fatalf("startup output = %q, policy log occurred after bootstrap log", output.String())
	}
}

func startupMainCommand(t *testing.T, workDir, configPath string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestMainProcessForStartupTests")
	cmd.Dir = workDir
	cmd.Env = withoutEnvironment(os.Environ(), "CLIPROXY_DISABLE_PLUGINS")
	cmd.Env = append(cmd.Env,
		startupMainProcessEnv+"=1",
		"CLIPROXY_STARTUP_CONFIG_PATH="+configPath,
	)
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd, output
}

func unsetEnvironment(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("os.Unsetenv(%q) error = %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func withoutEnvironment(environment []string, key string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(environment))
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func TestShouldEnableExampleAPIKeySafeMode(t *testing.T) {
	cfgWithExampleKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key", " your-api-key-1 "},
		},
	}
	cfgWithRealKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key"},
		},
	}

	tests := []struct {
		name               string
		cfg                *config.Config
		commandMode        bool
		tuiMode            bool
		standalone         bool
		cloudConfigMissing bool
		homeMode           bool
		want               bool
	}{
		{
			name: "normal server with example key",
			cfg:  cfgWithExampleKey,
			want: true,
		},
		{
			name:       "standalone tui with example key",
			cfg:        cfgWithExampleKey,
			tuiMode:    true,
			standalone: true,
			want:       true,
		},
		{
			name:        "pure tui client is not blocked",
			cfg:         cfgWithExampleKey,
			tuiMode:     true,
			standalone:  false,
			commandMode: false,
			want:        false,
		},
		{
			name:        "one-shot command is not blocked",
			cfg:         cfgWithExampleKey,
			commandMode: true,
			want:        false,
		},
		{
			name:     "home mode is not blocked",
			cfg:      cfgWithExampleKey,
			homeMode: true,
			want:     false,
		},
		{
			name:               "cloud standby without config is not blocked",
			cfg:                cfgWithExampleKey,
			cloudConfigMissing: true,
			want:               false,
		},
		{
			name: "normal server with real key",
			cfg:  cfgWithRealKey,
			want: false,
		},
		{
			name: "nil config",
			cfg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableExampleAPIKeySafeMode(tt.cfg, tt.commandMode, tt.tuiMode, tt.standalone, tt.cloudConfigMissing, tt.homeMode)
			if got != tt.want {
				t.Fatalf("shouldEnableExampleAPIKeySafeMode() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestModelCatalogUpdaterPlan(t *testing.T) {
	tests := []struct {
		name            string
		localModel      bool
		homeEnabled     bool
		wantModels      bool
		wantCodexClient bool
	}{
		{
			name:            "normal CPA refreshes both catalogs",
			localModel:      false,
			homeEnabled:     false,
			wantModels:      true,
			wantCodexClient: true,
		},
		{
			name:            "home mode keeps models.json local and refreshes codex templates",
			localModel:      false,
			homeEnabled:     true,
			wantModels:      false,
			wantCodexClient: true,
		},
		{
			name:            "local-model disables both remote catalogs",
			localModel:      true,
			homeEnabled:     false,
			wantModels:      false,
			wantCodexClient: false,
		},
		{
			name:            "local-model disables both remote catalogs even under home",
			localModel:      true,
			homeEnabled:     true,
			wantModels:      false,
			wantCodexClient: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModels, gotCodex := modelCatalogUpdaterPlan(tt.localModel, tt.homeEnabled)
			if gotModels != tt.wantModels || gotCodex != tt.wantCodexClient {
				t.Fatalf("modelCatalogUpdaterPlan(%v, %v) = (%v, %v), want (%v, %v)",
					tt.localModel, tt.homeEnabled, gotModels, gotCodex, tt.wantModels, tt.wantCodexClient)
			}
		})
	}
}
