package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	mcpprotocol "github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
)

func TestNewToolHandlerRejectsNonObjectArguments(t *testing.T) {
	tests := []struct {
		name     string
		raw      any
		wantCall bool
		wantArgs map[string]any
	}{
		{name: "omitted", raw: nil, wantCall: true, wantArgs: map[string]any{}},
		{name: "object", raw: map[string]any{"max": float64(1)}, wantCall: true, wantArgs: map[string]any{"max": float64(1)}},
		{name: "nil map", raw: map[string]any(nil), wantCall: true, wantArgs: map[string]any{}},
		{name: "array", raw: []any{}, wantCall: false},
		{name: "string", raw: "text", wantCall: false},
		{name: "number", raw: float64(1), wantCall: false},
		{name: "bool", raw: true, wantCall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			var got map[string]any
			handler := newToolHandler(toolset.ServerTool{
				Handler: func(_ context.Context, params map[string]any) (string, error) {
					called = true
					got = params
					return "ok", nil
				},
			})
			result, err := handler(t.Context(), mcpprotocol.CallToolRequest{
				Params: mcpprotocol.CallToolParams{Name: "read", Arguments: tt.raw},
			})
			if err != nil {
				t.Fatalf("handler transport error = %v", err)
			}
			if called != tt.wantCall {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCall)
			}
			if tt.wantCall {
				if result.IsError {
					t.Fatalf("result error = %#v, want success", result)
				}
				if !reflect.DeepEqual(got, tt.wantArgs) {
					t.Fatalf("handler args = %#v, want %#v", got, tt.wantArgs)
				}
				return
			}
			if !result.IsError {
				t.Fatal("non-object arguments returned a successful tool result")
			}
		})
	}
}

func TestNewToolHandlerReturnsErrorResult(t *testing.T) {
	wantErr := errors.New("boom")
	handler := newToolHandler(toolset.ServerTool{
		Handler: func(_ context.Context, params map[string]any) (string, error) {
			if params == nil {
				t.Fatal("handler params must be a non-nil map")
			}
			return "", wantErr
		},
	})

	result, err := handler(t.Context(), mcpprotocol.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler transport error = %v, want nil", err)
	}
	if !result.IsError {
		t.Fatal("handler result IsError = false, want true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("handler content count = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(mcpprotocol.TextContent)
	if !ok {
		t.Fatalf("handler content type = %T, want mcp.TextContent", result.Content[0])
	}
	if text.Type != "text" || text.Text != wantErr.Error() {
		t.Fatalf("handler text = {Type:%q Text:%q}, want {Type:%q Text:%q}", text.Type, text.Text, "text", wantErr)
	}
}
