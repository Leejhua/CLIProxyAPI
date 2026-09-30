#!/bin/sh
set -eu

umask 077
: "${CLI_PROXY_PORT:=8318}"

if [ -z "${MANAGEMENT_PASSWORD:-}" ]; then
  echo 'Set MANAGEMENT_PASSWORD in Dokploy to enable the management page.' >&2
  exit 1
fi

if [ -n "${CLI_PROXY_CONFIG_B64:-}" ]; then
  if ! printf '%s' "$CLI_PROXY_CONFIG_B64" | base64 -d > /CLIProxyAPI/config.yaml; then
    echo 'CLI_PROXY_CONFIG_B64 is not valid Base64.' >&2
    exit 1
  fi
elif [ -n "${CLI_PROXY_CONFIG_YAML:-}" ]; then
  printf '%s\n' "$CLI_PROXY_CONFIG_YAML" > /CLIProxyAPI/config.yaml
else
  if [ -z "${CLI_PROXY_API_KEYS_JSON:-}" ]; then
    echo 'Set CLI_PROXY_CONFIG_B64, CLI_PROXY_CONFIG_YAML, or CLI_PROXY_API_KEYS_JSON in Dokploy.' >&2
    exit 1
  fi
  printf '{"config-version":8,"server":{"port":%s},"management":{"allow-remote":true},"access":{"api-keys":%s},"oauth":{"auth-dir":"/root/.cli-proxy-api"}}\n' \
    "$CLI_PROXY_PORT" "$CLI_PROXY_API_KEYS_JSON" > /CLIProxyAPI/config.yaml
fi

export CLI_PROXY_ENV_CONFIG_PATH=/CLIProxyAPI/config.yaml
exec /CLIProxyAPI/CLIProxyAPI "$@"
