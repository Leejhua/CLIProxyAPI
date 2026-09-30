package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/store"
)

func readEnvironmentConfig(path, expectedPortValue string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read environment configuration: %w", err)
	}
	cfg, err := config.ParseConfigBytes(data)
	if err != nil {
		return nil, fmt.Errorf("validate environment configuration: %w", err)
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("environment configuration must set a server port")
	}
	if value := strings.TrimSpace(expectedPortValue); value != "" {
		expectedPort, errParse := strconv.Atoi(value)
		if errParse != nil || expectedPort <= 0 {
			return nil, fmt.Errorf("invalid CLI_PROXY_PORT %q", value)
		}
		if cfg.Port != expectedPort {
			return nil, fmt.Errorf("environment configuration port %d does not match CLI_PROXY_PORT %d", cfg.Port, expectedPort)
		}
	}
	return data, nil
}

func persistEnvironmentConfig(ctx context.Context, pgStore *store.PostgresStore, data []byte) error {
	path := pgStore.ConfigPath()
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read postgres configuration mirror: %w", err)
	}
	if bytes.Equal(current, data) {
		return nil
	}
	if err = os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write postgres configuration mirror: %w", err)
	}
	if err = pgStore.PersistConfig(ctx); err != nil {
		return fmt.Errorf("persist environment configuration to postgres: %w", err)
	}
	return nil
}
