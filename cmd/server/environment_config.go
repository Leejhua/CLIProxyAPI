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
	"gopkg.in/yaml.v3"
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
	if value := strings.TrimSpace(expectedPortValue); value != "" {
		expectedPort, errParse := strconv.Atoi(value)
		if errParse != nil || expectedPort <= 0 || expectedPort > 65535 {
			return nil, fmt.Errorf("invalid CLI_PROXY_PORT %q", value)
		}
		if cfg.Port != expectedPort {
			// Override the effective v8 port without round-tripping Config, which
			// would discard unrecognized fields and rewrite credential values.
			var doc yaml.Node
			if err = yaml.Unmarshal(data, &doc); err != nil {
				return nil, fmt.Errorf("decode environment configuration: %w", err)
			}
			if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
				return nil, fmt.Errorf("environment configuration must be a YAML mapping")
			}
			root := doc.Content[0]
			server := environmentMappingValue(root, "server")
			if server.Kind == yaml.AliasNode {
				alias := server.Alias
				*server = *alias
				server.Anchor = ""
				server.Content = append([]*yaml.Node(nil), alias.Content...)
			}
			if server.Kind != yaml.MappingNode {
				*server = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			port := environmentMappingValue(server, "port")
			*port = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(expectedPort), HeadComment: port.HeadComment, LineComment: port.LineComment}
			data, err = yaml.Marshal(&doc)
			if err != nil {
				return nil, fmt.Errorf("encode environment configuration: %w", err)
			}
			cfg, err = config.ParseConfigBytes(data)
			if err != nil {
				return nil, fmt.Errorf("validate overridden environment configuration: %w", err)
			}
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("environment configuration must set a server port between 1 and 65535")
	}
	return data, nil
}

func environmentMappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			// Replace the pointer so aliases elsewhere retain their original value.
			value := *node.Content[i+1]
			node.Content[i+1] = &value
			return &value
		}
	}
	value := &yaml.Node{}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	return value
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
