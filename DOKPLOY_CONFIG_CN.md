# Dokploy 配置持久化

CPA 主体容器连接的 PostgreSQL 是配置和 OAuth 凭证的持久化来源：

- `config_store` 保存完整 YAML，包括 OpenAI 兼容接入的地址、上游 Key 和模型。YAML 是存储格式，并不表示配置只保存在容器文件内。
- `auth_store` 保存 OAuth 凭证；启动时镜像到本地认证目录。
- `config_store_history` 保存配置内容变更或删除前的完整版本。历史记录包含凭证，按生产密钥保护。

## 启动优先级

数据库已有配置时，直接读取数据库；环境变量中的完整 YAML/Base64 不再覆盖它。

数据库没有配置时，使用本地持久化镜像（如存在），否则使用入口脚本生成的种子配置。种子来源依次是 `CLI_PROXY_CONFIG_B64`、`CLI_PROXY_CONFIG_YAML`、`CLI_PROXY_API_KEYS_JSON` 生成的最小配置。种子和环境覆盖通过校验后才提交数据库；初始化不会覆盖其他实例刚创建的配置。

以下变量只覆盖指定字段：

| CPA 容器环境变量 | 行为 |
| --- | --- |
| `CLI_PROXY_API_KEYS_JSON` | 非空 JSON 字符串数组，仅替换客户端访问 Key；不替换上游 Key、模型或 OAuth 凭证。省略则保留数据库中的客户端 Key。 |
| `CLI_PROXY_PORT` | 覆盖监听端口；本仓库入口脚本默认 `8318`。 |
| `MANAGEMENT_PASSWORD` | 管理页面登录密码，沿用运行时环境变量机制。 |
| `PGSTORE_DSN` | PostgreSQL 连接地址。升级应继续指向原数据库。 |
| `PGSTORE_SCHEMA` | 数据库 schema；默认 `public`，升级时保持一致。 |

现有部署只配真实 `CLI_PROXY_API_KEYS_JSON`、`MANAGEMENT_PASSWORD` 和原来的 `PGSTORE_DSN` 即可；无需为了保留上游配置重新提供 Base64。真实 Key 会覆盖旧模板客户端 Key；模板 Key 的安全拦截仍保留。

修改上游接入请在管理页面保存；已有数据库时，修改 Base64/YAML 不会更新上游配置。环境变量指定的客户端 Key/端口会在每次启动时重新生效。

## 历史与恢复

首次运行此版本时自动安装配置历史表和数据库触发器。此后 `UPDATE` 改变内容、或 `DELETE` 删除配置前，原文会在同一事务中归档；归档失败则拒绝修改。内容不变的保存不会增加历史。回退旧程序后，数据库触发器仍能保护常规更新和删除。历史从安装触发器后开始，不会补回已经丢失的内容。

默认 schema 下，可先只查询历史元数据，避免把密钥输出到日志：

```sql
SELECT revision, config_id, source_updated_at, archived_at, operation,
       octet_length(content) AS bytes
FROM public.config_store_history
ORDER BY revision DESC;
```

需要恢复时，先停止 CPA 的写入并备份当前数据库，选择准确的 revision，在受控本地文件中导出相应 `content`，检查后通过管理页面恢复需要的接入配置。不要直接选择最新记录就覆盖生产配置，也不要把导出文件提交 Git。恢复旧配置后仍需留意环境变量中的 Key 覆盖。

历史暂不自动清理。它与主配置在同一个数据库中，无法抵御数据库/卷删除、`TRUNCATE` 或整库损坏；请保留原数据库卷，并在 Dokploy 配置独立数据库备份。

## 本地验证

Go 单元测试通过 `go test ./...` 运行。真实数据库回归测试需要显式设置 `CLIPROXY_TEST_PG_DSN`，只可指向专用测试数据库；测试会创建和删除独立 schema：

```sh
CLIPROXY_TEST_PG_DSN='postgres://user:password@localhost/testdb?sslmode=disable' \
  go test ./internal/store -run 'TestPostgres(Config|Bootstrap|Concurrent)' -count=1
```
