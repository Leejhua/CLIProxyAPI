package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
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
			data, err := readEnvironmentConfig(path, tt.expectedPort)
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
	data, err := readEnvironmentConfig(path, "8318")
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
