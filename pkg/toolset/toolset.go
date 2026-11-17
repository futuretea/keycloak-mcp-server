package toolset

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Toolset defines a group of MCP tools.
type Toolset interface {
	GetName() string
	GetDescription() string
	GetTools() []ServerTool
}

// ServerTool combines an MCP tool definition with its handler.
type ServerTool struct {
	Tool        mcp.Tool
	Handler     ToolHandler
	Domain      string
	IsWrite     bool
	IsSensitive bool
	IsGlobal    bool
}

// ToolHandler handles a tool call.
type ToolHandler func(ctx context.Context, params map[string]any) (string, error)

// FilterOptions controls which tools are exposed.
type FilterOptions struct {
	ReadOnly        bool
	EnabledTools    []string
	DisabledTools   []string
	EnabledDomains  []string
	DisabledDomains []string
}

// FilterTools applies enable/disable lists for tools and domains.
func FilterTools(tools []ServerTool, opts FilterOptions) []ServerTool {
	enabledTools, restrictTools := allowNames(opts.EnabledTools)
	disabledTools := nameSet(opts.DisabledTools)
	enabledDomains, restrictDomains := allowNames(opts.EnabledDomains)
	disabledDomains := nameSet(opts.DisabledDomains)

	filtered := make([]ServerTool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Tool.Name
		domain := tool.Domain

		if opts.ReadOnly && (tool.IsWrite || tool.IsSensitive || tool.IsGlobal) {
			continue
		}
		if restrictTools {
			if _, ok := enabledTools[name]; !ok {
				continue
			}
		}
		if _, ok := disabledTools[name]; ok {
			continue
		}
		if restrictDomains {
			if _, ok := enabledDomains[domain]; !ok {
				continue
			}
		}
		if _, ok := disabledDomains[domain]; ok {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func allowNames(values []string) (map[string]struct{}, bool) {
	if len(values) == 0 {
		return nil, false
	}
	return nameSet(values), true
}

func nameSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}
