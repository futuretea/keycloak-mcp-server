//go:build e2e

package e2e_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/keycloak-mcp-server/internal/keycloakclient"
	"github.com/futuretea/keycloak-mcp-server/pkg/core/config"
	internalhttp "github.com/futuretea/keycloak-mcp-server/pkg/server/http"
	mcpserver "github.com/futuretea/keycloak-mcp-server/pkg/server/mcp"
	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
	keycloaktools "github.com/futuretea/keycloak-mcp-server/pkg/toolset/keycloak"
)

func TestKeycloakDockerMCPHTTP(t *testing.T) {
	fixture := startKeycloakFixture(t)
	serverURL := startMCPHTTPServer(t, fixture)

	response, err := (&http.Client{Timeout: 5 * time.Second}).Get(serverURL + internalhttp.HealthEndpoint)
	if err != nil {
		t.Fatalf("call unauthenticated health endpoint: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unauthenticated health status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	client, err := mcpclient.NewStreamableHttpClient(serverURL + internalhttp.MCPEndpoint)
	if err != nil {
		t.Fatalf("create unauthenticated MCP HTTP client: %v", err)
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start MCP HTTP client: %v", err)
	}
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "keycloak-mcp-e2e", Version: "test"},
		},
	}); err != nil {
		t.Fatalf("initialize unauthenticated MCP HTTP client: %v", err)
	}

	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	if !containsTool(tools.Tools, "keycloak_users_get_users") {
		t.Fatal("read-only MCP tool list is missing keycloak_users_get_users")
	}
	if containsTool(tools.Tools, "keycloak_users_post_users") {
		t.Fatal("read-only MCP tool list exposes keycloak_users_post_users")
	}
	if containsTool(tools.Tools, "keycloak_clients_get_clients_by_client_uuid_client_secret") {
		t.Fatal("read-only MCP tool list exposes a client-secret tool")
	}
	if containsTool(tools.Tools, "keycloak_realms_admin_get_realms") {
		t.Fatal("read-only MCP tool list exposes a global realm tool")
	}

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "keycloak_users_get_users",
			Arguments: map[string]any{"max": float64(1)},
		},
	})
	if err != nil {
		t.Fatalf("call Keycloak users tool through MCP HTTP: %v", err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("Keycloak users tool result is unsuccessful: %#v", result)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok || !strings.HasPrefix(text.Text, "[") {
		t.Fatalf("Keycloak users tool content is not a JSON list")
	}
}

func startMCPHTTPServer(t *testing.T, fixture keycloakFixture) string {
	t.Helper()
	staticConfig := &config.StaticConfig{
		Listen:                "127.0.0.1",
		LogLevel:              "disabled",
		KeycloakURL:           fixture.BaseURL,
		KeycloakRealm:         fixture.Realm,
		KeycloakClientID:      fixture.ClientID,
		KeycloakClientSecret:  fixture.ClientSecret,
		ReadOnly:              true,
		RequestTimeoutSeconds: 15,
	}
	keycloakClient, err := keycloakclient.New(staticConfig, nil)
	if err != nil {
		t.Fatalf("create Keycloak client: %v", err)
	}
	keycloakToolset, err := keycloaktools.New(keycloakClient)
	if err != nil {
		t.Fatalf("create Keycloak toolset: %v", err)
	}
	mcpService, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: staticConfig,
		Toolsets:     []toolset.Toolset{keycloakToolset},
	})
	if err != nil {
		t.Fatalf("create MCP server: %v", err)
	}
	t.Cleanup(mcpService.Close)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for MCP HTTP server: %v", err)
	}
	staticConfig.Port = listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- internalhttp.ServeListener(ctx, mcpService, staticConfig, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop MCP HTTP server: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("MCP HTTP server did not stop")
		}
	})

	serverURL := "http://" + listener.Addr().String()
	waitForHTTP(t, serverURL+internalhttp.HealthEndpoint, 15*time.Second)
	return serverURL
}

func containsTool(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func waitForHTTP(t *testing.T, endpoint string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("endpoint did not become healthy within %s", timeout)
}
