# Dokploy 部署

本仓库从上游 v8 构建镜像。`config.yaml` 不进入镜像，也不再由 Git 跟踪；容器启动时从 Dokploy 环境变量生成运行配置。

在 Dokploy 的 Compose 应用中使用仓库里的 `docker-compose.yml`，并设置：

| 变量 | 用途 |
| --- | --- |
| `MANAGEMENT_PASSWORD` | 管理页面登录密钥，必填。 |
| `CLI_PROXY_API_KEYS_JSON` | 客户端 API Key 的 JSON 数组，例如 `["key-1","key-2"]`。使用简易配置时必填。 |
| `CLI_PROXY_CONFIG_YAML` | 可选，完整的 v8 YAML 配置。设置后优先于 `CLI_PROXY_API_KEYS_JSON`。 |

简易配置会启用 8317 端口、远程管理页面，并将 OAuth 认证文件放在 `/root/.cli-proxy-api`。管理密钥由程序原生支持的 `MANAGEMENT_PASSWORD` 读取。`../files/auths`、`../files/logs` 和 `../files/plugins` 挂载为持久化目录，路径相对于 Compose 文件所在目录；如果 Dokploy 的工作目录不同，请按实际位置调整。

如需保留旧 `config.yaml` 中的其他设置，将其内容迁移到 `CLI_PROXY_CONFIG_YAML`，建议采用 [config.example.yaml](config.example.yaml) 的 v8 格式。至少包含以下配置，并按需补充其他设置：

```yaml
config-version: 8
server:
  port: 8317
management:
  allow-remote: true
access:
  api-keys:
    - replace-with-your-client-key
oauth:
  auth-dir: /root/.cli-proxy-api
```

重新部署后，用原管理地址登录并确认 `/v8/management` 可用。环境变量在容器启动时写入 `config.yaml`；在管理页面中改动配置后，如需跨重启保留，请同步更新 Dokploy 变量。旧版 fork 曾把 API Key 和管理密钥提交到 Git 历史，迁移后应轮换这些密钥。
