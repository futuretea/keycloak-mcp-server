package toolset_test

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
)

func TestFilterTools(t *testing.T) {
	tools := []toolset.ServerTool{
		{Tool: mcp.NewTool("echo"), Domain: "example"},
		{Tool: mcp.NewTool("ping"), Domain: "example"},
		{Tool: mcp.NewTool("other"), Domain: "other"},
		{Tool: mcp.NewTool("write"), Domain: "other", IsWrite: true},
		{Tool: mcp.NewTool("read-secret"), Domain: "other", IsSensitive: true},
		{Tool: mcp.NewTool("global-read"), Domain: "other", IsGlobal: true},
	}

	tests := []struct {
		name string
		opts toolset.FilterOptions
		want []string
	}{
		{
			name: "enabled domain and disabled tool",
			opts: toolset.FilterOptions{
				EnabledDomains: []string{"example"},
				DisabledTools:  []string{"ping"},
			},
			want: []string{"echo"},
		},
		{
			name: "enabled tools only",
			opts: toolset.FilterOptions{EnabledTools: []string{"ping"}},
			want: []string{"ping"},
		},
		{
			name: "disabled domain",
			opts: toolset.FilterOptions{DisabledDomains: []string{"example"}},
			want: []string{"other", "write", "read-secret", "global-read"},
		},
		{
			name: "filters everything",
			opts: toolset.FilterOptions{EnabledTools: []string{"missing"}},
			want: nil,
		},
		{
			name: "read-only cannot be overridden by enabled tool",
			opts: toolset.FilterOptions{ReadOnly: true, EnabledTools: []string{"write", "read-secret", "global-read", "other"}},
			want: []string{"other"},
		},
		{
			name: "read-write explicitly allows sensitive tools",
			opts: toolset.FilterOptions{EnabledTools: []string{"read-secret", "global-read"}},
			want: []string{"read-secret", "global-read"},
		},
		{
			name: "empty allow lists stay open",
			opts: toolset.FilterOptions{EnabledTools: []string{}, EnabledDomains: []string{}},
			want: []string{"echo", "ping", "other", "write", "read-secret", "global-read"},
		},
		{
			name: "blank tool allow list matches nothing",
			opts: toolset.FilterOptions{EnabledTools: []string{"", "  "}},
			want: nil,
		},
		{
			name: "blank domain allow list matches nothing",
			opts: toolset.FilterOptions{EnabledDomains: []string{" "}},
			want: nil,
		},
		{
			name: "blank deny lists stay open",
			opts: toolset.FilterOptions{
				DisabledTools:   []string{"", "   "},
				DisabledDomains: []string{" ", ""},
			},
			want: []string{"echo", "ping", "other", "write", "read-secret", "global-read"},
		},
		{
			name: "blank entries do not erase a real tool name",
			opts: toolset.FilterOptions{EnabledTools: []string{"", " ping "}},
			want: []string{"ping"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := toolset.FilterTools(tools, tt.opts)
			if len(filtered) != len(tt.want) {
				t.Fatalf("got %d tools, want %d (%+v)", len(filtered), len(tt.want), filtered)
			}
			for i, name := range tt.want {
				if filtered[i].Tool.Name != name {
					t.Fatalf("tool[%d]=%q, want %q", i, filtered[i].Tool.Name, name)
				}
			}
		})
	}
}
