package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/safemode"
	"gopkg.in/yaml.v3"
)

func TestReadEnvironmentConfig(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		expectedPort string
		wantPort     int
		wantError    string
	}{
		{name: "legacy config on deployed port", config: "port: 8318\napi-keys: [client-key]\n", expectedPort: "8318", wantPort: 8318},
		{name: "legacy port overridden", config: "port: 8317\napi-keys: [client-key]\n", expectedPort: "8318", wantPort: 8318},
		{name: "v8 port overridden", config: "config-version: 8\nserver:\n  port: 8317\naccess:\n  api-keys: [client-key]\n", expectedPort: "8318", wantPort: 8318},
		{name: "missing port supplied by deployment", config: "api-keys: [client-key]\n", expectedPort: "8318", wantPort: 8318},
		{name: "no override", config: "port: 8317\n", wantPort: 8317},
		{name: "whitespace override", config: "port: 8317\n", expectedPort: " 8318 ", wantPort: 8318},
		{name: "aliased server", config: "defaults: &defaults\n  port: 8317\n  host: 0.0.0.0\nserver: *defaults\n", expectedPort: "8318", wantPort: 8318},
		{name: "invalid yaml", config: "port: [\n", expectedPort: "8318", wantError: "validate environment configuration"},
		{name: "comments only", config: "# no configuration\n", expectedPort: "8318", wantError: "must be a YAML mapping"},
		{name: "missing port and override", config: "api-keys: [client-key]\n", wantError: "must set a server port"},
		{name: "invalid override", config: "port: 8317\n", expectedPort: "abc", wantError: "invalid CLI_PROXY_PORT"},
		{name: "zero override", config: "port: 8317\n", expectedPort: "0", wantError: "invalid CLI_PROXY_PORT"},
		{name: "out of range override", config: "port: 8317\n", expectedPort: "65536", wantError: "invalid CLI_PROXY_PORT"},
		{name: "invalid configured port", config: "port: 65536\n", wantError: "must set a server port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := readEnvironmentConfig(path, tt.expectedPort, "")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("readEnvironmentConfig() error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := config.ParseConfigBytes(data)
			if err != nil {
				t.Fatal(err)
			}
			if got.Port != tt.wantPort {
				t.Fatalf("port = %d, want %d", got.Port, tt.wantPort)
			}
			before, err := config.ParseConfigBytes([]byte(tt.config))
			if err != nil {
				t.Fatal(err)
			}
			originalPort := before.Port
			before.Port = got.Port
			if !reflect.DeepEqual(before, got) {
				t.Fatal("port override changed other effective settings")
			}
			if originalPort == got.Port && string(data) != tt.config {
				t.Fatal("unchanged port should preserve the original payload")
			}
		})
	}
}

func TestEnvironmentAPIKeyOverride(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
	}{
		{"legacy", "port: 8317\napi-keys: [your-api-key-1, your-api-key-2, your-api-key-3]\n"},
		{"v8", "config-version: 8\nserver: {port: 8317}\naccess: {api-keys: [your-api-key-1]}\n"},
		{"mixed", "port: 8317\napi-keys: [your-api-key-1]\naccess: {api-keys: [your-api-key-2]}\n"},
		{"missing", "port: 8317\n"},
		{"aliased access", "port: 8317\ndefaults: &access {api-keys: [your-api-key-1]}\naccess: *access\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.payload), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := readEnvironmentConfig(path, "8318", `["client-secret-a", "client-secret-b"]`)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.ParseConfigBytes(data)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Port != 8318 || !reflect.DeepEqual(cfg.APIKeys, []string{"client-secret-a", "client-secret-b"}) {
				t.Fatal("deployment port or API keys did not take precedence")
			}
			if safemode.HasExampleAPIKeys(cfg.APIKeys) {
				t.Fatal("template client keys still enable safe mode after override")
			}
		})
	}
}

func TestEnvironmentAPIKeyOverridePreservesUpstreamCredentials(t *testing.T) {
	payload := `config-version: 8
server: {port: 8318}
access: {api-keys: [your-api-key-1]}
api-keys:
  openai-compatibility:
    - name: provider
      base-url: https://example.invalid/v1
      keys:
        - api-key: upstream-secret
      models:
        - name: test-model
          alias: friendly-model
oauth:
  model-alias:
    codex:
      - name: original-model
        alias: renamed-model
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readEnvironmentConfig(path, "8318", `["client-secret"]`)
	if err != nil {
		t.Fatal(err)
	}
	before, err := config.ParseConfigBytes([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	after, err := config.ParseConfigBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	before.APIKeys = []string{"client-secret"}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("client key override changed upstream credentials or other settings")
	}
	again, err := applyEnvironmentConfig(data, "8318", `["client-secret"]`)
	if err != nil || string(again) != string(data) {
		t.Fatal("repeated startup rewrote an unchanged configuration")
	}
}

func TestEnvironmentAPIKeyValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8318\napi-keys: [your-api-key-1]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`client-secret`, `"client-secret"`, `[]`, `null`, `["client-secret", 123]`, `[""]`, `[" "]`, `{"key":"client-secret"}`} {
		_, err := readEnvironmentConfig(path, "8318", value)
		if err == nil || !strings.Contains(err.Error(), "CLI_PROXY_API_KEYS_JSON") {
			t.Fatal("invalid API key override should produce an actionable error")
		}
		if strings.Contains(err.Error(), "client-secret") {
			t.Fatal("validation error exposed an API key")
		}
	}
	for _, value := range []string{"", `["your-api-key-1"]`} {
		data, err := readEnvironmentConfig(path, "8318", value)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.ParseConfigBytes(data)
		if err != nil {
			t.Fatal(err)
		}
		if !safemode.HasExampleAPIKeys(cfg.APIKeys) {
			t.Fatal("template keys must remain protected when no real replacement is provided")
		}
	}
}

func TestEnvironmentPortOverridePreservesProviderConfig(t *testing.T) {
	payload := `# Keep custom settings and comments.
port: 8317
api-keys: [client-key]
openai-compatibility:
  - name: provider
    base-url: https://example.invalid/v1
    api-key-entries:
      - api-key: provider-key
    models:
      - name: test-model
        alias: friendly-model
custom-setting:
  port: 8317
  value: "00123"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readEnvironmentConfig(path, "8318", "")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err = yaml.Unmarshal([]byte(payload), &before); err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	delete(after, "server")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("port override changed the original configuration values")
	}
	if !strings.Contains(string(data), "# Keep custom settings and comments.") {
		t.Fatal("port override discarded comments")
	}
}
