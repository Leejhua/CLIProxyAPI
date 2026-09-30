#!/bin/sh
set -eu

umask 077

if [ -z "${MANAGEMENT_PASSWORD:-}" ]; then
  echo 'Set MANAGEMENT_PASSWORD in Dokploy to enable the management page.' >&2
  exit 1
fi

if [ -n "${CLI_PROXY_CONFIG_YAML:-}" ]; then
  printf '%s\n' "$CLI_PROXY_CONFIG_YAML" > /CLIProxyAPI/config.yaml
else
  if [ -z "${CLI_PROXY_API_KEYS_JSON:-}" ]; then
    echo 'Set CLI_PROXY_API_KEYS_JSON or CLI_PROXY_CONFIG_YAML in Dokploy.' >&2
    exit 1
  fi
  printf '{"config-version":8,"server":{"port":8317},"management":{"allow-remote":true},"access":{"api-keys":%s},"oauth":{"auth-dir":"/root/.cli-proxy-api"}}\n' \
    "$CLI_PROXY_API_KEYS_JSON" > /CLIProxyAPI/config.yaml
fi

exec /CLIProxyAPI/CLIProxyAPI "$@"
