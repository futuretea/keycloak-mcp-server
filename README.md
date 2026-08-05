# Keycloak MCP Server

[中文文档](README.zh.md)

An MCP (Model Context Protocol) server for the complete Keycloak **26.6.4 Admin REST API**. It exposes 401 version-pinned operations as organized MCP tools and authenticates to Keycloak with a confidential-client service account.

The primary transport is unauthenticated Streamable HTTP at `/mcp`. Keep it on a trusted network or place an authenticated reverse proxy in front of it.

## Quick start

Build the binary with Go 1.25.10 or newer:

```bash
make build

export MCP_KEYCLOAK_URL=http://127.0.0.1:8081
export MCP_KEYCLOAK_REALM=master
export MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp
export MCP_KEYCLOAK_CLIENT_SECRET='set-this-in-your-shell-or-secret-store'

# Streamable HTTP, health check at /healthz
./bin/keycloak mcp --port 8080 --listen 127.0.0.1
```

Connect an MCP client without an MCP-side token:

```json
{
  "mcpServers": {
    "keycloak": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

For stdio, omit `--port` and configure the client to run `keycloak mcp` with the same Keycloak environment variables.

## Keycloak setup

Create a confidential client in the selected realm, enable **Service Accounts**, and grant its service-account user the `realm-management` roles required by the tools you expose. A read-only user directory setup commonly starts with `query-users` and `view-users`.

The MCP process sends the client credentials only to the configured Keycloak base URL. The realm in every realm-scoped tool path is always the configured realm and cannot be overridden by a caller. The two global `/admin/realms` operations are only available after explicit full-API opt-in.

## Tools and safety

The embedded Keycloak 26.6.4 OpenAPI document generates 401 tool definitions. Tool names use this predictable shape:

```text
keycloak_<domain>_<method>_<path>
```

Examples: `keycloak_users_get_users`, `keycloak_clients_get_clients`, and `keycloak_authorization_get_clients_by_client_uuid_authz_resource_server_resource`.

Tools are grouped by the OpenAPI resource domain. Request path and query parameters are direct MCP inputs; the configured `realm` is deliberately omitted. JSON, form, text, XML, and YAML request bodies follow the operation's declared content type. JSON responses are formatted, textual responses are returned as text, and binary responses are returned losslessly as a Base64 JSON envelope.

`read_only` defaults to `true`, so state-changing tools, the two confidential-client secret retrieval tools, and global realm operations are not registered. `enabled_tools` and `enable_domains` cannot bypass this setting. To expose the complete API, make a deliberate configuration change:

```bash
./bin/keycloak mcp --read-only=false --port 8080
```

Use least-privilege service-account roles even when writes are enabled.

Inspect the current catalog without Keycloak credentials:

```bash
./bin/keycloak tools list
./bin/keycloak tools describe keycloak_users_get_users --json
```

Calling a tool requires the Keycloak configuration above:

```bash
./bin/keycloak tools call keycloak_users_get_users --params '{"max": 10}'
```

## HTTP and Docker

The HTTP process provides these endpoints:

| Path | Purpose |
|---|---|
| `/healthz` | Health check (`GET` / `HEAD`) |
| `/mcp` | Streamable HTTP MCP |
| `/sse` | SSE connection |
| `/message` | SSE messages |

All MCP HTTP and SSE endpoints have no built-in authentication or TLS. The default listener is loopback. For a non-loopback deployment, use a reverse proxy that provides TLS and your chosen access control.

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

## Configuration

Precedence is **flags > environment variables > YAML config > defaults**. See [config.example.yaml](config.example.yaml).

| Key / environment variable | Default | Meaning |
|---|---:|---|
| `keycloak_url` / `MCP_KEYCLOAK_URL` | — | Absolute Keycloak base URL |
| `keycloak_realm` / `MCP_KEYCLOAK_REALM` | — | Realm used for service-account authentication and all realm-scoped tools |
| `keycloak_client_id` / `MCP_KEYCLOAK_CLIENT_ID` | — | Confidential client ID |
| `keycloak_client_secret` / `MCP_KEYCLOAK_CLIENT_SECRET` | — | Confidential client secret; keep it out of config files and source control |
| `read_only` / `MCP_READ_ONLY` | `true` | Hide state-changing, client-secret retrieval, and global realm tools |
| `request_timeout_seconds` / `MCP_REQUEST_TIMEOUT_SECONDS` | `30` | Keycloak API request timeout |
| `port` / `MCP_PORT` | `0` | HTTP port; `0` uses stdio |
| `listen` / `MCP_LISTEN` | `127.0.0.1` | HTTP bind host |
| `sse_base_url` / `MCP_SSE_BASE_URL` | empty | Public base URL for the SSE message endpoint |
| `enabled_tools`, `disabled_tools` | empty | Tool-name filters |
| `enabled_domains`, `disabled_domains` | empty | Resource-domain filters |

## Verification and releases

```bash
make test       # unit tests
make e2e        # requires Docker; starts Keycloak 26.6.4 and exercises MCP HTTP
make ci         # lint, unit tests, Docker E2E, template check, build
```

The build workflow and both NPM and Docker release validations run the Docker E2E suite before publication. Releases publish `@futuretea/keycloak-mcp-server` and `ghcr.io/futuretea/keycloak-mcp-server` from stable `vX.Y.Z` tags.

The API source and SHA-256 provenance are recorded in [internal/keycloakapi/spec/README.md](internal/keycloakapi/spec/README.md). A local Keycloak source checkout may live in ignored `third-party-projects/keycloak`.

## License

[MIT](LICENSE)
