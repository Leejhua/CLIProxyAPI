package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEnvironmentConfig(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		expectedPort string
		wantError    string
	}{
		{name: "legacy config on deployed port", config: "port: 8318\napi-keys: [client-key]\n", expectedPort: "8318"},
		{name: "wrong deployed port", config: "port: 8317\napi-keys: [client-key]\n", expectedPort: "8318", wantError: "does not match"},
		{name: "invalid yaml", config: "port: [\n", expectedPort: "8318", wantError: "validate environment configuration"},
		{name: "missing port", config: "api-keys: [client-key]\n", expectedPort: "8318", wantError: "must set a server port"},
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
			if string(data) != tt.config {
				t.Fatal("configuration payload changed")
			}
		})
	}
}
