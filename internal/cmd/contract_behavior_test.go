package cmd_test

import (
	"bufio"
	"fmt"
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

func TestBinaryContract(t *testing.T) {
	bin := buildBinary(t)

	t.Run("cli filters", func(t *testing.T) {
		assertToolListed(t, bin, true, "tools", "list")
		assertToolListed(t, bin, true, "tools", "list", "--enabled-tools", "")
		assertToolListed(t, bin, true, "tools", "list", "--disabled-tools", " ")
		assertToolListed(t, bin, true, "tools", "list", "--disable-domains", " ")
		assertToolListed(t, bin, true, "tools", "list", "--enabled-tools", "keycloak_users_get_users")
		assertToolListed(t, bin, false, "tools", "list", "--enabled-tools", " ")
		assertToolListed(t, bin, false, "tools", "list", "--enabled-tools", ",")
		assertToolListed(t, bin, false, "tools", "list", "--enable-domains", " ")

		dir := t.TempDir()
		blankPath := filepath.Join(dir, "blank.yaml")
		if err := os.WriteFile(blankPath, []byte("enabled_tools:\n  - \"\"\n  - \"   \"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		emptyPath := filepath.Join(dir, "empty.yaml")
		if err := os.WriteFile(emptyPath, []byte("enabled_tools: []\ndisabled_tools:\n  - \" \"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		namedPath := filepath.Join(dir, "named.yaml")
		if err := os.WriteFile(namedPath, []byte("enabled_tools:\n  - keycloak_users_get_users\nread_only: false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertToolListed(t, bin, false, "tools", "list", "--config", blankPath)
		assertToolListed(t, bin, true, "tools", "list", "--config", emptyPath)
		assertToolListed(t, bin, true, "tools", "list", "--config", namedPath)
		assertWriteHidden(t, bin, "tools", "list", "--config", namedPath, "--read-only=true")
		assertToolListed(t, bin, false, "tools", "list", "--read-only=false", "--enabled-tools", "keycloak_users_post_users")
		post := exec.Command(bin, "tools", "list", "--read-only=false", "--enabled-tools", "keycloak_users_post_users")
		out, err := post.CombinedOutput()
		if err != nil {
			t.Fatalf("write tool list: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "keycloak_users_post_users") {
			t.Fatalf("read-only false flag hid the selected write tool:\n%s", out)
		}
	})

	t.Run("stdio", func(t *testing.T) {
		proveStdioContract(t, bin)
	})
}

func assertToolListed(t *testing.T, bin string, want bool, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	got := strings.Contains(string(out), "keycloak_users_get_users")
	if got != want {
		t.Fatalf("%s listed users=%v, want %v\n%s", strings.Join(args, " "), got, want, out)
	}
}

func assertWriteHidden(t *testing.T, bin string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}
	if strings.Contains(string(out), "keycloak_users_post_users") {
		t.Fatalf("%s exposed a write tool:\n%s", strings.Join(args, " "), out)
	}
}

func proveStdioContract(t *testing.T, bin string) {
	t.Helper()
	var mu sync.Mutex
	var hits []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/protocol/openid-connect/token") {
			_, _ = io.WriteString(w, `{"access_token":"fixture-token","expires_in":300,"token_type":"Bearer"}`)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(fake.Close)

	cmd := exec.Command(bin, "mcp")
	cmd.Env = append(os.Environ(),
		"MCP_KEYCLOAK_URL="+fake.URL,
		"MCP_KEYCLOAK_REALM=master",
		"MCP_KEYCLOAK_CLIENT_ID=fixture-client",
		"MCP_KEYCLOAK_CLIENT_SECRET=fixture-secret",
		"MCP_READ_ONLY=true",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	reader := bufio.NewReader(stdout)
	send := func(payload string) {
		t.Helper()
		if _, err := io.WriteString(stdin, payload+"\n"); err != nil {
			t.Fatalf("write: %v\nstderr=%s", err, stderr.String())
		}
	}
	read := func() string {
		t.Helper()
		if file, ok := stdout.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = file.SetReadDeadline(time.Now().Add(20 * time.Second))
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v\nstderr=%s", err, stderr.String())
		}
		return strings.TrimSpace(line)
	}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hits...)
	}
	adminCount := func(paths []string) int {
		count := 0
		for _, path := range paths {
			if strings.Contains(path, "/admin/") {
				count++
			}
		}
		return count
	}

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"contract","version":"0"}}}`)
	init := read()
	if !strings.Contains(init, `"protocolVersion":"2024-11-05"`) || adminCount(snapshot()) != 0 {
		t.Fatalf("initialize = %s hits=%v", init, snapshot())
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	list := read()
	if !strings.Contains(list, "keycloak_users_get_users") || strings.Contains(list, "keycloak_users_post_users") || adminCount(snapshot()) != 0 {
		t.Fatalf("tools/list = %s hits=%v", trim(list), snapshot())
	}

	call := func(id int, arguments string, wantAdmin bool, wantError bool) {
		t.Helper()
		before := adminCount(snapshot())
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"keycloak_users_get_users","arguments":%s}}`, id, arguments)
		if arguments == "<omit>" {
			body = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"keycloak_users_get_users"}}`, id)
		}
		send(body)
		response := read()
		gotAdmin := adminCount(snapshot()) - before
		if wantAdmin && gotAdmin != 1 {
			t.Fatalf("arguments %s admin hits = %d, want 1; response=%s", arguments, gotAdmin, trim(response))
		}
		if !wantAdmin && gotAdmin != 0 {
			t.Fatalf("arguments %s admin hits = %d, want 0; response=%s", arguments, gotAdmin, trim(response))
		}
		if wantError != strings.Contains(response, `"isError":true`) {
			t.Fatalf("arguments %s isError=%v, want %v; response=%s", arguments, strings.Contains(response, `"isError":true`), wantError, trim(response))
		}
	}

	call(3, `{"max":1}`, true, false)
	call(4, `[]`, false, true)
	call(5, `"text"`, false, true)
	call(6, `1`, false, true)
	call(7, `true`, false, true)
	call(8, `null`, true, false)
	call(9, "<omit>", true, false)
	call(10, `{"nope":true}`, false, true)

	send(`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"missing_tool","arguments":{}}}`)
	unknown := read()
	if !strings.Contains(unknown, "missing_tool") || !strings.Contains(unknown, `"error"`) {
		t.Fatalf("unknown tool response = %s", trim(unknown))
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "keycloak")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mcp-server")
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func trim(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 400 {
		return text[:400] + "..."
	}
	return text
}
