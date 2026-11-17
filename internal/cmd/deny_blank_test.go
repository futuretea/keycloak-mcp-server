package cmd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const usersTool = "keycloak_users_get_users"

func TestDenyListCatalogBaseline(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	toolFile := writeConfig(t, dir, "tools.yaml", "disabled_tools:\n  - keycloak_users_get_users\n")
	domainFile := writeConfig(t, dir, "domains.yaml", "disabled_domains:\n  - users\n")

	defaultTools := mustListTools(t, bin, nil, nil)
	if len(defaultTools) != 199 || !toolNamed(defaultTools, usersTool) {
		t.Fatalf("default catalog = %d tools, users=%v, want 199 and the users tool", len(defaultTools), toolNamed(defaultTools, usersTool))
	}
	deniedTool := mustListTools(t, bin, nil, []string{"--config", toolFile})
	if len(deniedTool) != 198 || toolNamed(deniedTool, usersTool) {
		t.Fatalf("disabled_tools users catalog = %d, users=%v, want 198 without the users tool", len(deniedTool), toolNamed(deniedTool, usersTool))
	}
	deniedDomain := mustListTools(t, bin, nil, []string{"--config", domainFile})
	if len(deniedDomain) != 185 || toolNamed(deniedDomain, usersTool) {
		t.Fatalf("disabled_domains users catalog = %d, users=%v, want 185 without the users tool", len(deniedDomain), toolNamed(deniedDomain, usersTool))
	}
}

func TestBlankDenyOverrideDoesNotRestoreCatalog(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	toolFile := writeConfig(t, dir, "tools.yaml", "disabled_tools:\n  - keycloak_users_get_users\n")
	domainFile := writeConfig(t, dir, "domains.yaml", "disabled_domains:\n  - users\n")
	blankTools := writeConfig(t, dir, "blank-tools.yaml", "disabled_tools:\n  - \"\"\n  - \"   \"\n")
	blankDomains := writeConfig(t, dir, "blank-domains.yaml", "disabled_domains:\n  - \" \"\n")

	cases := []struct {
		name string
		file string
		env  []string
		args []string
	}{
		{name: "yaml blank tools", file: blankTools},
		{name: "yaml blank domains", file: blankDomains},
		{name: "env spaces tools", file: toolFile, env: []string{"MCP_DISABLED_TOOLS=   "}},
		{name: "env comma tools", file: toolFile, env: []string{"MCP_DISABLED_TOOLS=,"}},
		{name: "flag space tools", file: toolFile, args: []string{"--disabled-tools", " "}},
		{name: "flag comma tools", file: toolFile, args: []string{"--disabled-tools", ","}},
		{name: "env spaces domains", file: domainFile, env: []string{"MCP_DISABLED_DOMAINS=   "}},
		{name: "flag space domains", file: domainFile, args: []string{"--disable-domains", " "}},
		{name: "flag comma domains", file: domainFile, args: []string{"--disable-domains", ","}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tools, out, err := listTools(bin, tt.env, append([]string{"--config", tt.file}, tt.args...))
			if err == nil {
				t.Fatalf("%s accepted a blank deny list and listed %d tools, users=%v", tt.name, len(tools), toolNamed(tools, usersTool))
			}
			if !strings.Contains(out, "blank") {
				t.Fatalf("%s error = %v, output = %s", tt.name, err, out)
			}
		})
	}
}

func TestDenyListLegitimateValues(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	toolFile := writeConfig(t, dir, "tools.yaml", "disabled_tools:\n  - keycloak_users_get_users\n")
	domainFile := writeConfig(t, dir, "domains.yaml", "disabled_domains:\n  - users\n")
	mixedTools := writeConfig(t, dir, "mixed-tools.yaml", "disabled_tools:\n  - \"\"\n  - keycloak_users_get_users\n")
	mixedDomains := writeConfig(t, dir, "mixed-domains.yaml", "disabled_domains:\n  - \" \"\n  - users\n")

	preserved := mustListTools(t, bin, []string{"MCP_DISABLED_TOOLS="}, []string{"--config", toolFile})
	if len(preserved) != 198 || toolNamed(preserved, usersTool) {
		t.Fatalf("empty MCP_DISABLED_TOOLS = %d, users=%v, want the YAML deny list", len(preserved), toolNamed(preserved, usersTool))
	}
	preservedDomain := mustListTools(t, bin, []string{"MCP_DISABLED_DOMAINS="}, []string{"--config", domainFile})
	if len(preservedDomain) != 185 || toolNamed(preservedDomain, usersTool) {
		t.Fatalf("empty MCP_DISABLED_DOMAINS = %d, users=%v, want the YAML domain deny list", len(preservedDomain), toolNamed(preservedDomain, usersTool))
	}

	cleared := mustListTools(t, bin, nil, []string{"--config", toolFile, "--disabled-tools", ""})
	if len(cleared) != 199 || !toolNamed(cleared, usersTool) {
		t.Fatalf("empty --disabled-tools = %d, users=%v, want the full catalog", len(cleared), toolNamed(cleared, usersTool))
	}
	clearedDomain := mustListTools(t, bin, nil, []string{"--config", domainFile, "--disable-domains", ""})
	if len(clearedDomain) != 199 || !toolNamed(clearedDomain, usersTool) {
		t.Fatalf("empty --disable-domains = %d, users=%v, want the full catalog", len(clearedDomain), toolNamed(clearedDomain, usersTool))
	}

	mixed := mustListTools(t, bin, nil, []string{"--config", mixedTools})
	if len(mixed) != 198 || toolNamed(mixed, usersTool) {
		t.Fatalf("mixed tool deny list = %d, users=%v, want 198 without the users tool", len(mixed), toolNamed(mixed, usersTool))
	}
	mixedDomain := mustListTools(t, bin, nil, []string{"--config", mixedDomains})
	if len(mixedDomain) != 185 || toolNamed(mixedDomain, usersTool) {
		t.Fatalf("mixed domain deny list = %d, users=%v, want 185 without the users tool", len(mixedDomain), toolNamed(mixedDomain, usersTool))
	}
	mixedEnv := mustListTools(t, bin, []string{"MCP_DISABLED_TOOLS= ,keycloak_users_get_users"}, nil)
	if len(mixedEnv) != 198 || toolNamed(mixedEnv, usersTool) {
		t.Fatalf("mixed env deny list = %d, users=%v, want 198 without the users tool", len(mixedEnv), toolNamed(mixedEnv, usersTool))
	}
	mixedFlag := mustListTools(t, bin, nil, []string{"--disable-domains", " ,users"})
	if len(mixedFlag) != 185 || toolNamed(mixedFlag, usersTool) {
		t.Fatalf("mixed domain flag = %d, users=%v, want 185 without the users tool", len(mixedFlag), toolNamed(mixedFlag, usersTool))
	}
}

func TestBlankDenyDoesNotCallAdmin(t *testing.T) {
	bin := buildBinary(t)
	configPath := writeConfig(t, t.TempDir(), "tools.yaml", "disabled_tools:\n  - keycloak_users_get_users\n")
	var mu sync.Mutex
	var hits []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fixture-token","expires_in":300}`)
	}))
	t.Cleanup(fake.Close)

	cmd := exec.Command(bin, "mcp", "--config", configPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = withFilterEnv([]string{
		"MCP_KEYCLOAK_URL=" + fake.URL,
		"MCP_KEYCLOAK_REALM=master",
		"MCP_KEYCLOAK_CLIENT_ID=fixture-client",
		"MCP_KEYCLOAK_CLIENT_SECRET=fixture-secret",
		"MCP_DISABLED_TOOLS=   ",
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		mu.Lock()
		got := append([]string(nil), hits...)
		mu.Unlock()
		if err == nil {
			t.Fatal("blank MCP_DISABLED_TOOLS started and exited successfully")
		}
		if !strings.Contains(stderr.String(), "blank") {
			t.Fatalf("stderr = %q, err = %v", stderr.String(), err)
		}
		if len(got) != 0 {
			t.Fatalf("blank deny list made HTTP calls: %v", got)
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		mu.Lock()
		got := append([]string(nil), hits...)
		mu.Unlock()
		t.Fatalf("blank deny list kept the server running, hits=%v", got)
	}
}

func mustListTools(t *testing.T, bin string, env []string, args []string) []listedTool {
	t.Helper()
	tools, out, err := listTools(bin, env, args)
	if err != nil {
		t.Fatalf("tools list %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return tools
}

type listedTool struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

func listTools(bin string, env []string, args []string) ([]listedTool, string, error) {
	cmd := exec.Command(bin, append([]string{"tools", "list", "--json"}, args...)...)
	cmd.Env = withFilterEnv(env)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, string(out), err
	}
	var tools []listedTool
	if decodeErr := json.Unmarshal(out, &tools); decodeErr != nil {
		return nil, string(out), decodeErr
	}
	return tools, string(out), nil
}

func withFilterEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && (strings.HasPrefix(key, "MCP_DISABLED_") || strings.HasPrefix(key, "MCP_ENABLED_")) {
			continue
		}
		env = append(env, item)
	}
	return append(env, extra...)
}

func toolNamed(tools []listedTool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func writeConfig(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
