# Keycloak MCP Server

[English](README.md)

面向 Keycloak **26.6.4 Admin REST API** 的 MCP（Model Context Protocol）服务。它将版本锁定的 401 个 API 按资源域组织为 MCP 工具，并通过机密客户端的服务账户访问 Keycloak。

主要对外方式是无 MCP 侧认证的 Streamable HTTP `/mcp`。请只在受信网络使用，或在前面部署带认证的反向代理。

## 快速开始

需要 Go 1.25.10 或更高版本：

```bash
make build

export MCP_KEYCLOAK_URL=http://127.0.0.1:8081
export MCP_KEYCLOAK_REALM=master
export MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp
export MCP_KEYCLOAK_CLIENT_SECRET='在 shell 或密钥管理系统中设置'

# Streamable HTTP；健康检查为 /healthz
./bin/keycloak mcp --port 8080 --listen 127.0.0.1
```

MCP 客户端无需提供 MCP 侧 token：

```json
{
  "mcpServers": {
    "keycloak": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

使用 stdio 时不传 `--port`，并向 `keycloak mcp` 进程提供同一组 Keycloak 环境变量。

## Keycloak 配置

在目标 realm 创建机密客户端，开启 **Service Accounts**，再为该服务账户用户授予已暴露工具需要的 `realm-management` 角色。只读用户目录通常从 `query-users` 与 `view-users` 开始。

MCP 进程只会把客户端凭据发送到配置的 Keycloak 基础地址。所有 realm 范围工具路径中的 `realm` 始终采用配置值，调用者无法覆写。两个全局 `/admin/realms` 操作只有在明确选择完整 API 后才会出现。

## 工具与安全策略

内嵌的 Keycloak 26.6.4 OpenAPI 文档会生成 401 个工具，命名规则固定：

```text
keycloak_<domain>_<method>_<path>
```

例如 `keycloak_users_get_users`、`keycloak_clients_get_clients` 与 `keycloak_authorization_get_clients_by_client_uuid_authz_resource_server_resource`。

工具按 OpenAPI 资源域组织。路径和 query 参数直接作为 MCP 入参；已配置的 `realm` 故意不暴露。JSON、表单、纯文本、XML、YAML 请求体遵循对应 API 的 content type。JSON 响应会格式化；文本响应原样返回；二进制响应会以包含 MIME 类型的 Base64 JSON 返回，避免内容损坏。

`read_only` 默认是 `true`，因此变更状态的工具、两条机密客户端 secret 读取工具和全局 realm 操作都不会注册。`enabled_tools` 与 `enable_domains` 不能绕过它。只有明确接受完整 API 暴露风险时才开启：

```bash
./bin/keycloak mcp --read-only=false --port 8080
```

即使开启写操作，也应为服务账户授予最小权限。

无需 Keycloak 凭据即可查看工具目录：

```bash
./bin/keycloak tools list
./bin/keycloak tools describe keycloak_users_get_users --json
```

实际调用需要前面的 Keycloak 配置：

```bash
./bin/keycloak tools call keycloak_users_get_users --params '{"max": 10}'
```

## HTTP 与 Docker

HTTP 进程提供以下端点：

| 路径 | 用途 |
|---|---|
| `/healthz` | 健康检查（`GET` / `HEAD`） |
| `/mcp` | Streamable HTTP MCP |
| `/sse` | SSE 连接 |
| `/message` | SSE 消息 |

MCP HTTP 与 SSE 不内置认证和 TLS。默认仅监听 loopback。部署到非 loopback 地址时，请使用反向代理提供 TLS 与访问控制。

```bash
make docker

docker run --rm -p 8080:8080 \
  -e MCP_KEYCLOAK_URL=http://host.docker.internal:8081 \
  -e MCP_KEYCLOAK_REALM=master \
  -e MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp \
  -e MCP_KEYCLOAK_CLIENT_SECRET \
  ghcr.io/futuretea/keycloak-mcp-server:dev \
  --port 8080 --listen 0.0.0.0
```

## 配置

优先级：**flags > 环境变量 > YAML 配置 > 默认值**。完整示例见 [config.example.yaml](config.example.yaml)。

| 配置 / 环境变量 | 默认值 | 说明 |
|---|---:|---|
| `keycloak_url` / `MCP_KEYCLOAK_URL` | — | Keycloak 绝对基础 URL |
| `keycloak_realm` / `MCP_KEYCLOAK_REALM` | — | 服务账户认证和所有 realm 范围工具使用的 realm |
| `keycloak_client_id` / `MCP_KEYCLOAK_CLIENT_ID` | — | 机密客户端 ID |
| `keycloak_client_secret` / `MCP_KEYCLOAK_CLIENT_SECRET` | — | 机密客户端 secret；不要写入配置文件或版本库 |
| `read_only` / `MCP_READ_ONLY` | `true` | 隐藏写操作、客户端 secret 读取和全局 realm 工具 |
| `request_timeout_seconds` / `MCP_REQUEST_TIMEOUT_SECONDS` | `30` | Keycloak API 请求超时秒数 |
| `port` / `MCP_PORT` | `0` | HTTP 端口；`0` 使用 stdio |
| `listen` / `MCP_LISTEN` | `127.0.0.1` | HTTP 监听地址 |
| `sse_base_url` / `MCP_SSE_BASE_URL` | 空 | SSE message endpoint 对外 base URL |
| `enabled_tools`、`disabled_tools` | 空 | 工具名过滤 |
| `enabled_domains`、`disabled_domains` | 空 | 资源域过滤 |

## 验证与发布

```bash
make test       # 单元测试
make e2e        # 需要 Docker；启动 Keycloak 26.6.4 并验证 MCP HTTP
make ci         # lint、单元测试、Docker E2E、模板检查、构建
```

构建工作流及 NPM、Docker 两条发布前验证都会运行 Docker E2E。稳定 `vX.Y.Z` 标签发布 `@futuretea/keycloak-mcp-server` 和 `ghcr.io/futuretea/keycloak-mcp-server`。

API 来源及 SHA-256 记录在 [internal/keycloakapi/spec/README.md](internal/keycloakapi/spec/README.md)。可把本地 Keycloak 源码 checkout 放在已忽略的 `third-party-projects/keycloak`。

## License

[MIT](LICENSE)
