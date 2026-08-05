# Keycloak MCP Server

[中文文档](README.zh.md)

Let your MCP client work with Keycloak without leaving your IDE: find users,
inspect clients and roles, or—when you deliberately allow it—make
administrative changes. This server exposes the Keycloak **26.6.4 Admin REST
API** as MCP tools and signs in to Keycloak with a confidential-client service
account.

Start with HTTP on your own machine. It listens on `127.0.0.1` by default and
does not add MCP-side authentication; for a shared endpoint, put TLS and access
control in a reverse proxy in front of it.

## What you can do

Ask your MCP client in plain language. It can choose from the available tools
and show you the result.

| Goal | Example prompt |
|---|---|
| Find people | “List the first 10 users whose username starts with `alex`.” |
| Inspect access | “Show the realm roles assigned to user `…`.” |
| Understand clients | “Find the client with ID `my-web-app` and show its redirect URIs.” |
| Review realm settings | “Show the login and token settings for this realm.” |
| Change Keycloak deliberately | “Create this user with the following attributes.” (requires write mode) |

The server has 401 Keycloak 26.6.4 operations available in full mode. It
starts in a safer **read-only** mode with 199 non-sensitive, non-global read
operations. No caller can override the configured realm.

### Default tool map

| Work area | Tools | Typical questions |
|---|---:|---|
| Users and groups | 20 | Who is this user? Which groups are they in? |
| Clients, scopes, and mappers | 61 | How is this application configured? Which scopes can it use? |
| Authentication and identity providers | 27 | How do users sign in? Which identity providers are enabled? |
| Roles and role mappings | 29 | Which roles exist and who has them? |
| Realm settings and organizations | 36 | What are the realm policies and organization settings? |
| Authorization, workflows, and attack detection | 23 | What resource permissions and workflows are configured? |
| Components | 3 | Which Keycloak components are installed? |

## Start in five minutes

### 1. Create a Keycloak service account

In the realm you want this server to manage:

1. Create a client, turn on **Client authentication**, and enable **Service
   accounts roles**.
2. In **Service account roles**, select the `realm-management` client roles
   required for the work you want to allow.
3. For a user-directory read-only start, grant `query-users` and `view-users`.
4. Copy the client secret from the client’s **Credentials** tab. Store it in
   your shell or a secret manager—never in this repository or a YAML file.

The service account is Keycloak’s permission boundary. The MCP server will not
invent permissions that the account does not have.

### 2. Set the connection values

```bash
export MCP_KEYCLOAK_URL=http://127.0.0.1:8081
export MCP_KEYCLOAK_REALM=master
export MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp
export MCP_KEYCLOAK_CLIENT_SECRET='set this in your shell or secret manager'
```

### 3. Start the server

#### npx (no local install)

```bash
npx -y @futuretea/keycloak-mcp-server --port 8080
```

#### From source

This requires Go 1.25.10 or newer.

```bash
make build

./bin/keycloak mcp --port 8080
```

The server is now available at `http://127.0.0.1:8080/mcp`. Its health check
is `http://127.0.0.1:8080/healthz`.

### 4. Connect your MCP client

Point a Streamable HTTP MCP client at the endpoint. The server itself does not
expect an MCP token:

```json
{
  "mcpServers": {
    "keycloak": {
      "url": "http://127.0.0.1:8080/mcp"
    }
  }
}
```

For a local stdio client, use `npx` directly in its MCP configuration:

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
        "MCP_KEYCLOAK_CLIENT_SECRET": "<store this in your MCP client's secret store>"
      }
    }
  }
}
```

If you built from source instead, set `command` to the absolute path of the
`keycloak` binary and use `args: ["mcp"]` with the same environment variables.

### 5. Check that it is ready

```bash
curl -fsS http://127.0.0.1:8080/healthz
./bin/keycloak tools list
./bin/keycloak tools describe keycloak_users_get_users --json
```

`tools list` and `tools describe` do not need Keycloak credentials. A real
tool call does:

```bash
./bin/keycloak tools call keycloak_users_get_users --params '{"max": 10}'
```

## Choose the access level

### Read-only mode (the default)

Keep the default for assistants that answer questions, search users, or audit
configuration. It omits every state-changing operation, client-secret
retrieval, and the global realm-list/create operations. Tool and domain filters
cannot bypass this restriction.

```bash
./bin/keycloak mcp --port 8080
```

### Full API mode (intentional opt-in)

Only use this for an agent that must change Keycloak. Grant the service account
only the Keycloak roles needed for that job, then explicitly disable read-only
mode:

```bash
./bin/keycloak mcp --port 8080 --read-only=false
```

Full mode exposes all 401 tools, including user/client/role mutations,
confidential-client secret operations, and global realm operations. This is an
access-policy change, not just a display setting.

## Limit what an assistant sees

Use filters to reduce the tool list to the tasks you want an MCP client to
perform. Filters work in addition to read-only mode.

```bash
# Only offer user and group operations that are safe in read-only mode.
./bin/keycloak mcp --port 8080 \
  --enable-domains users,groups

# Hide one tool you do not want an assistant to call.
./bin/keycloak mcp --port 8080 \
  --disabled-tools keycloak_users_get_users_count
```

Find exact names and input fields before building a filter or a scripted call:

```bash
./bin/keycloak tools list --json
./bin/keycloak tools describe keycloak_clients_get_clients --json
```

Tool names have the predictable form:

```text
keycloak_<domain>_<method>_<path>
```

The configured realm is intentionally absent from tool inputs. Path and query
parameters are direct inputs; request bodies support the content types declared
by the Keycloak API (JSON, form, text, XML, and YAML).

## Run with Docker

Build the image locally:

```bash
make docker
```

Then run it. `host.docker.internal` works with Docker Desktop; on Linux, use an
address that the container can reach for your Keycloak server.

```bash
docker run --rm -p 8080:8080 \
  -e MCP_KEYCLOAK_URL=http://host.docker.internal:8081 \
  -e MCP_KEYCLOAK_REALM=master \
  -e MCP_KEYCLOAK_CLIENT_ID=keycloak-mcp \
  -e MCP_KEYCLOAK_CLIENT_SECRET \
  ghcr.io/futuretea/keycloak-mcp-server:dev \
  --port 8080 --listen 0.0.0.0
```

The npm package is `@futuretea/keycloak-mcp-server`; the Docker image is
`ghcr.io/futuretea/keycloak-mcp-server`.

## Deployment safety

The HTTP, SSE, and MCP endpoints do **not** provide built-in TLS or
authentication.

| Situation | Recommended setup |
|---|---|
| One local MCP client | Keep the default `127.0.0.1` listener. |
| A team or remote MCP client | Place an authenticated TLS reverse proxy in front of the server. |
| Automation allowed to write | Use `--read-only=false`, narrow Keycloak service-account roles, and narrow tool/domain filters. |
| Secret handling | Pass `MCP_KEYCLOAK_CLIENT_SECRET` from a shell, container secret, or secret manager. |

Available HTTP endpoints:

| Path | Purpose |
|---|---|
| `/healthz` | Health check (`GET` or `HEAD`) |
| `/mcp` | Streamable HTTP MCP |
| `/sse` | SSE connection |
| `/message` | SSE messages |

## Configuration reference

Use flags for a one-off run, environment variables for containers, or a YAML
file for non-secret settings. Precedence is **flags > environment variables >
YAML > defaults**. Start from [config.example.yaml](config.example.yaml).

| Setting | Default | Use it for |
|---|---:|---|
| `keycloak_url` / `MCP_KEYCLOAK_URL` | — | Keycloak’s absolute base URL. |
| `keycloak_realm` / `MCP_KEYCLOAK_REALM` | — | Service-account login and every realm-scoped request. |
| `keycloak_client_id` / `MCP_KEYCLOAK_CLIENT_ID` | — | Your confidential client ID. |
| `keycloak_client_secret` / `MCP_KEYCLOAK_CLIENT_SECRET` | — | The confidential client secret. Keep it out of files. |
| `read_only` / `MCP_READ_ONLY` | `true` | Whether only safe read tools are registered. |
| `request_timeout_seconds` / `MCP_REQUEST_TIMEOUT_SECONDS` | `30` | Per-request Keycloak timeout. |
| `port` / `MCP_PORT` | `0` | HTTP port; `0` means stdio. |
| `listen` / `MCP_LISTEN` | `127.0.0.1` | HTTP bind address. |
| `enabled_tools`, `disabled_tools` | empty | Allow or hide named tools. |
| `enabled_domains`, `disabled_domains` | empty | Allow or hide resource domains. |

Pass a YAML file with `--config`:

```bash
./bin/keycloak mcp --config /path/to/keycloak-mcp.yaml --port 8080
```

## Troubleshooting

| What you see | What to check |
|---|---|
| `401` from Keycloak | Check the confidential client ID and secret, and that service accounts are enabled. |
| `403` from Keycloak | Grant the service-account user the required `realm-management` role; do not broaden it blindly. |
| A write tool is missing | Read-only mode is active. Use `--read-only=false` only after deciding the service account should have write access. |
| A tool is missing in full mode | Check `enabled_*` and `disabled_*` filters with `keycloak tools list`. |
| The MCP client cannot connect | Confirm the server is running, the URL ends in `/mcp`, and a remote deployment has a reachable reverse proxy. |
| Keycloak cannot be reached from Docker | Verify the container can resolve and connect to the `MCP_KEYCLOAK_URL` host. |

## Compatibility and verification

The tool catalog is generated from and pinned to Keycloak **26.6.4**. The
source specification and checksum are documented in
[internal/keycloakapi/spec/README.md](internal/keycloakapi/spec/README.md).

For contributors and operators validating a build:

```bash
make test       # unit tests
make e2e        # Docker Keycloak 26.6.4 and MCP HTTP end-to-end test
make ci         # lint, unit tests, E2E, template check, build
```

## License

[MIT](LICENSE)
