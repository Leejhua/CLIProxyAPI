package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"gopkg.in/yaml.v3"
)

func readEnvironmentConfig(path, expectedPortValue, apiKeysJSON string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read environment configuration: %w", err)
	}
	return applyEnvironmentConfig(data, expectedPortValue, apiKeysJSON)
}

func applyEnvironmentConfig(data []byte, expectedPortValue, apiKeysJSON string) ([]byte, error) {
	cfg, err := config.ParseConfigBytes(data)
	if err != nil {
		return nil, fmt.Errorf("validate environment configuration: %w", err)
	}
	expectedPort := cfg.Port
	if value := strings.TrimSpace(expectedPortValue); value != "" {
		var errParse error
		expectedPort, errParse = strconv.Atoi(value)
		if errParse != nil || expectedPort <= 0 || expectedPort > 65535 {
			return nil, fmt.Errorf("invalid CLI_PROXY_PORT %q", value)
		}
	}
	var apiKeys []string
	if value := strings.TrimSpace(apiKeysJSON); value != "" {
		if err = json.Unmarshal([]byte(value), &apiKeys); err != nil || len(apiKeys) == 0 {
			return nil, fmt.Errorf("CLI_PROXY_API_KEYS_JSON must be a non-empty JSON array of API key strings")
		}
		for i, key := range apiKeys {
			apiKeys[i] = strings.TrimSpace(key)
			if apiKeys[i] == "" {
				return nil, fmt.Errorf("CLI_PROXY_API_KEYS_JSON must not contain empty API keys")
			}
		}
	}
	if cfg.Port != expectedPort || len(apiKeys) > 0 {
		// Override only deployment-owned fields. Round-tripping Config would
		// discard unrecognized fields and rewrite credential values.
		var doc yaml.Node
		if err = yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("decode environment configuration: %w", err)
		}
		if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
			return nil, fmt.Errorf("environment configuration must be a YAML mapping")
		}
		root := doc.Content[0]
		if cfg.Port != expectedPort {
			server := environmentMapping(root, "server")
			port := environmentMappingValue(server, "port")
			*port = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(expectedPort), HeadComment: port.HeadComment, LineComment: port.LineComment}
		}
		if len(apiKeys) > 0 {
			access := environmentMapping(root, "access")
			keys := environmentMappingValue(access, "api-keys")
			if err = keys.Encode(apiKeys); err != nil {
				return nil, fmt.Errorf("encode environment API keys")
			}
			// Legacy top-level lists contain client keys; v8 top-level maps
			// contain upstream credentials and must remain untouched.
			for i := 0; i < len(root.Content); i += 2 {
				if root.Content[i].Value == "api-keys" {
					legacy := root.Content[i+1]
					if legacy.Kind == yaml.AliasNode {
						legacy = legacy.Alias
					}
					if legacy.Kind == yaml.SequenceNode {
						root.Content = append(root.Content[:i], root.Content[i+2:]...)
					}
					break
				}
			}
		}
		data, err = yaml.Marshal(&doc)
		if err != nil {
			return nil, fmt.Errorf("encode environment configuration: %w", err)
		}
		cfg, err = config.ParseConfigBytes(data)
		if err != nil {
			return nil, fmt.Errorf("validate overridden environment configuration: %w", err)
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("environment configuration must set a server port between 1 and 65535")
	}
	return data, nil
}

func environmentMapping(node *yaml.Node, key string) *yaml.Node {
	value := environmentMappingValue(node, key)
	if value.Kind == yaml.AliasNode {
		alias := value.Alias
		*value = *alias
		value.Anchor = ""
		value.Content = append([]*yaml.Node(nil), alias.Content...)
	}
	if value.Kind != yaml.MappingNode {
		*value = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return value
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
