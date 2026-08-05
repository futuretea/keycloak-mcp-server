# Keycloak MCP Server

[English](README.md)

让 MCP 客户端无需离开 IDE 就能使用 Keycloak：查找用户、查看客户端和角色；在你明确允许后，也能执行管理变更。服务将 Keycloak **26.6.4 Admin REST API** 封装为 MCP 工具，并通过机密客户端的服务账户登录 Keycloak。

建议先在本机通过 HTTP 使用。默认只监听 `127.0.0.1`，且 MCP 层没有内置认证；如果要给多人或远程客户端使用，请在前面加上提供 TLS 和访问控制的反向代理。

## 能做什么

直接用自然语言向 MCP 客户端提出目标。客户端会从已开放的工具中选择合适的调用并展示结果。

| 目标 | 示例提问 |
|---|---|
| 查找用户 | “列出用户名以 `alex` 开头的前 10 位用户。” |
| 查看权限 | “显示用户 `…` 拥有的 realm 角色。” |
| 了解客户端 | “找到 ID 为 `my-web-app` 的客户端，并显示它的重定向 URI。” |
| 审查 realm 设置 | “显示这个 realm 的登录和 token 设置。” |
| 有意识地变更 Keycloak | “用下列属性创建这个用户。”（需要写模式） |

完整模式提供 401 个 Keycloak 26.6.4 操作。服务默认以更安全的**只读**模式启动，开放 199 个非敏感、非全局的读取操作。调用者不能覆写已配置的 realm。

### 默认工具地图

| 工作范围 | 工具数 | 常见问题 |
|---|---:|---|
| 用户和群组 | 20 | 这个用户是谁？他属于哪些群组？ |
| 客户端、scope 和 mapper | 61 | 这个应用如何配置？能使用哪些 scope？ |
| 认证和身份提供商 | 27 | 用户如何登录？启用了哪些身份提供商？ |
| 角色和角色映射 | 29 | 有哪些角色？谁拥有这些角色？ |
| realm 设置和组织 | 36 | realm 策略和组织设置是什么？ |
| 授权、工作流和攻击检测 | 23 | 配置了哪些资源权限和工作流？ |
| 组件 | 3 | 安装了哪些 Keycloak 组件？ |

## 5 分钟启动

### 1. 创建 Keycloak 服务账户

在希望此服务管理的 realm 中：

1. 创建客户端，开启 **Client authentication**，并启用 **Service accounts roles**。
2. 在 **Service account roles** 中选择工作所需的 `realm-management` 客户端角色。
3. 如果先从只读用户目录开始，通常授予 `query-users` 与 `view-users` 即可。
4. 在客户端的 **Credentials** 页复制 client secret。把它存到 shell 或密钥管理系统中，绝不要写入本仓库或 YAML 文件。

服务账户才是 Keycloak 的权限边界。MCP 服务不会获得它没有的 Keycloak 权限。

### 2. 设置连接信息

```bash
export MCP_KEYCLOAK_URL=http://127.0.0.1:8081
export MCP_KEYCLOAK_REALM=master
export MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp
export MCP_KEYCLOAK_CLIENT_SECRET='在 shell 或密钥管理系统中设置'
```

### 3. 启动服务

#### npx（无需本地安装）

```bash
npx -y @futuretea/keycloak-mcp-server --port 8080
```

#### 从源码启动

需要 Go 1.25.10 或更高版本。

```bash
make build

./bin/keycloak mcp --port 8080
```

此时服务地址为 `http://127.0.0.1:8080/mcp`，健康检查为 `http://127.0.0.1:8080/healthz`。

### 4. 连接 MCP 客户端

将支持 Streamable HTTP 的 MCP 客户端指向这个端点。服务本身不要求 MCP token：

```json
{
  "mcpServers": {
    "keycloak": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

本地 stdio 客户端可直接在 MCP 配置中使用 `npx`：

```json
{
  "mcpServers": {
    "keycloak": {
      "command": "npx",
      "args": ["-y", "@futuretea/keycloak-mcp-server"],
      "env": {
        "MCP_KEYCLOAK_URL": "http://127.0.0.1:8081",
        "MCP_KEYCLOAK_REALM": "master",
        "MCP_KEYCLOAK_CLIENT_ID": "keycloak-mcp",
        "MCP_KEYCLOAK_CLIENT_SECRET": "<请改为 MCP 客户端的密钥存储引用>"
      }
    }
  }
}
```

如果从源码构建，请将 `command` 改成 `keycloak` 二进制的绝对路径，使用 `args: ["mcp"]`，并保留同一组环境变量。

### 5. 确认已就绪

```bash
curl -fsS http://127.0.0.1:8080/healthz
./bin/keycloak tools list
./bin/keycloak tools describe keycloak_users_get_users --json
```

`tools list` 和 `tools describe` 不需要 Keycloak 凭据。实际调用需要前面的 Keycloak 配置：

```bash
./bin/keycloak tools call keycloak_users_get_users --params '{"max": 10}'
```

## 选择访问级别

### 只读模式（默认）

面向回答问题、查找用户或审查配置的助手，请保留默认值。它不会注册任何变更状态的操作、读取客户端 secret 的操作，以及全局 realm 列表/创建操作。工具和域过滤也无法绕过这个限制。

```bash
./bin/keycloak mcp --port 8080
```

### 完整 API 模式（明确选择后开启）

只有当代理确实需要修改 Keycloak 时才使用。先只给服务账户授予该工作需要的 Keycloak 角色，再明确关闭只读模式：

```bash
./bin/keycloak mcp --port 8080 --read-only=false
```

完整模式会开放全部 401 个工具，其中包括用户、客户端、角色的变更操作，机密客户端 secret 操作，以及全局 realm 操作。这是访问策略的变更，不只是展示更多菜单。

## 限制助手看到的工具

可用过滤器把工具列表缩小到 MCP 客户端真正需要执行的任务。过滤器只能进一步收紧范围，不能突破只读模式。

```bash
# 只提供只读模式下安全的用户和群组操作。
./bin/keycloak mcp --port 8080 \
  --enable-domains users,groups

# 隐藏一个不希望助手调用的工具。
./bin/keycloak mcp --port 8080 \
  --disabled-tools keycloak_users_get_users_count
```

在配置过滤器或脚本调用前，先查询准确的名称和输入字段：

```bash
./bin/keycloak tools list --json
./bin/keycloak tools describe keycloak_clients_get_clients --json
```

工具名形式固定：

```text
keycloak_<domain>_<method>_<path>
```

已配置的 realm 有意不出现在工具入参中。路径和 query 参数可直接传入；请求体支持 Keycloak API 声明的 JSON、表单、纯文本、XML、YAML 等 content type。

## 使用 Docker

先在本机构建镜像：

```bash
make docker
```

然后运行。`host.docker.internal` 适用于 Docker Desktop；Linux 上请改用容器能够访问到的 Keycloak 地址。

```bash
docker run --rm -p 8080:8080 \
  -e MCP_KEYCLOAK_URL=http://host.docker.internal:8081 \
  -e MCP_KEYCLOAK_REALM=master \
  -e MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp \
  -e MCP_KEYCLOAK_CLIENT_SECRET \
  ghcr.io/futuretea/keycloak-mcp-server:dev \
  --port 8080 --listen 0.0.0.0
```

npm 包名为 `@futuretea/keycloak-mcp-server`，Docker 镜像名为 `ghcr.io/futuretea/keycloak-mcp-server`。

## 部署安全

HTTP、SSE 和 MCP 端点都**没有**内置 TLS 或认证。

| 场景 | 推荐做法 |
|---|---|
| 单个本地 MCP 客户端 | 保持默认的 `127.0.0.1` 监听。 |
| 团队或远程 MCP 客户端 | 在服务前部署带认证和 TLS 的反向代理。 |
| 自动化需要写权限 | 使用 `--read-only=false`，收紧 Keycloak 服务账户角色，并收紧工具/域过滤。 |
| 处理 secret | 从 shell、容器 secret 或密钥管理系统传入 `MCP_KEYCLOAK_CLIENT_SECRET`。 |

可用 HTTP 端点：

| 路径 | 用途 |
|---|---|
| `/healthz` | 健康检查（`GET` 或 `HEAD`） |
| `/mcp` | Streamable HTTP MCP |
| `/sse` | SSE 连接 |
| `/message` | SSE 消息 |

## 配置参考

一次性运行优先用 flags，容器优先用环境变量，非 secret 设置可用 YAML。优先级是 **flags > 环境变量 > YAML > 默认值**。从 [config.example.yaml](config.example.yaml) 开始。

| 设置 | 默认值 | 用途 |
|---|---:|---|
| `keycloak_url` / `MCP_KEYCLOAK_URL` | — | Keycloak 的绝对基础 URL。 |
| `keycloak_realm` / `MCP_KEYCLOAK_REALM` | — | 服务账户登录及所有 realm 范围请求使用的 realm。 |
| `keycloak_client_id` / `MCP_KEYCLOAK_CLIENT_ID` | — | 机密客户端 ID。 |
| `keycloak_client_secret` / `MCP_KEYCLOAK_CLIENT_SECRET` | — | 机密客户端 secret，不要写入文件。 |
| `read_only` / `MCP_READ_ONLY` | `true` | 是否只注册安全读取工具。 |
| `request_timeout_seconds` / `MCP_REQUEST_TIMEOUT_SECONDS` | `30` | 单个 Keycloak 请求超时秒数。 |
| `port` / `MCP_PORT` | `0` | HTTP 端口；`0` 表示 stdio。 |
| `listen` / `MCP_LISTEN` | `127.0.0.1` | HTTP 绑定地址。 |
| `enabled_tools`、`disabled_tools` | 空 | 允许或隐藏指定工具。 |
| `enabled_domains`、`disabled_domains` | 空 | 允许或隐藏资源域。 |

用 `--config` 传入 YAML 文件：

```bash
./bin/keycloak mcp --config /path/to/keycloak-mcp.yaml --port 8080
```

## 排查问题

| 看到的现象 | 检查方向 |
|---|---|
| Keycloak 返回 `401` | 检查机密客户端 ID、secret，以及是否启用了 service accounts。 |
| Keycloak 返回 `403` | 为服务账户授予所需的 `realm-management` 角色；不要盲目扩大权限。 |
| 缺少写工具 | 只读模式处于开启状态。只有确认服务账户应具备写权限后再使用 `--read-only=false`。 |
| 完整模式仍缺少工具 | 使用 `keycloak tools list` 检查 `enabled_*` 和 `disabled_*` 过滤器。 |
| MCP 客户端无法连接 | 确认服务正在运行、URL 以 `/mcp` 结尾，远程部署还需确认反向代理可达。 |
| Docker 中无法连接 Keycloak | 确认容器能解析并连接 `MCP_KEYCLOAK_URL` 中的主机。 |

## 兼容性与验证

工具目录从 Keycloak **26.6.4** 生成并锁定。规格来源及校验和记录在 [internal/keycloakapi/spec/README.md](internal/keycloakapi/spec/README.md)。

贡献者和运维人员可使用：

```bash
make test       # 单元测试
make e2e        # Docker Keycloak 26.6.4 与 MCP HTTP 端到端测试
make ci         # lint、单元测试、E2E、模板检查、构建
```

## License

[MIT](LICENSE)
