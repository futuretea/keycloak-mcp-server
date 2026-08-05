package keycloak

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/futuretea/keycloak-mcp-server/internal/keycloakapi"
	"github.com/futuretea/keycloak-mcp-server/internal/keycloakclient"
	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
)

func TestToolsetCoversEveryPinnedOperation(t *testing.T) {
	generated, err := New(&recordingExecutor{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	operations, err := keycloakapi.Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	tools := generated.GetTools()
	if len(tools) != len(operations) {
		t.Fatalf("tools = %d, operations = %d", len(tools), len(operations))
	}
	byName := map[string]toolset.ServerTool{}
	for _, tool := range tools {
		if _, exists := byName[tool.Tool.Name]; exists {
			t.Fatalf("duplicate tool %q", tool.Tool.Name)
		}
		byName[tool.Tool.Name] = tool
	}
	for _, operation := range operations {
		tool, exists := byName[operation.ToolName]
		if !exists {
			t.Fatalf("missing tool %q", operation.ToolName)
		}
		if tool.Domain != operation.Domain || tool.IsWrite != operation.IsWrite || tool.IsSensitive != operation.IsSensitive || tool.IsGlobal != operation.IsGlobal || tool.Handler == nil {
			t.Fatalf("tool %q does not match operation: %#v", operation.ToolName, tool)
		}
		schema := decodeSchema(t, tool)
		properties := schema["properties"].(map[string]any)
		required := schemaRequired(schema)
		for _, parameter := range operation.Parameters {
			if parameter.In == "path" && parameter.Name == "realm" {
				if _, exposed := properties[parameter.Name]; exposed {
					t.Fatalf("tool %q exposes configured realm", tool.Tool.Name)
				}
				continue
			}
			if _, exposed := properties[parameter.Name]; !exposed {
				t.Fatalf("tool %q is missing parameter %q", tool.Tool.Name, parameter.Name)
			}
			property := properties[parameter.Name].(map[string]any)
			if got, want := property["type"], schemaType(parameter.Type); got != want {
				t.Fatalf("tool %q parameter %q type = %v, want %s", tool.Tool.Name, parameter.Name, got, want)
			}
			if parameter.Required != required[parameter.Name] {
				t.Fatalf("tool %q parameter %q required = %t, want %t", tool.Tool.Name, parameter.Name, required[parameter.Name], parameter.Required)
			}
		}
		if len(operation.ContentTypes) > 0 {
			if _, exposed := properties["body"]; !exposed {
				t.Fatalf("tool %q is missing body", tool.Tool.Name)
			}
			if operation.RequestBodyRequired != required["body"] {
				t.Fatalf("tool %q body required = %t, want %t", tool.Tool.Name, required["body"], operation.RequestBodyRequired)
			}
			_, exposesContentType := properties["content_type"]
			if exposesContentType != (len(operation.ContentTypes) > 1) {
				t.Fatalf("tool %q content_type exposure = %t, want %t", tool.Tool.Name, exposesContentType, len(operation.ContentTypes) > 1)
			}
		}
	}
}

func TestToolHandlerBuildsDirectKeycloakRequest(t *testing.T) {
	executor := &recordingExecutor{result: `{"id":"user"}`}
	generated, err := New(executor)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tool := findTool(t, generated.GetTools(), "keycloak_users_get_users")
	result, err := tool.Handler(t.Context(), map[string]any{
		"briefRepresentation": true,
		"max":                 float64(10),
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result != `{"id":"user"}` {
		t.Fatalf("result = %q", result)
	}
	if executor.request.Method != "GET" || executor.request.Path != "/admin/realms/{realm}/users" {
		t.Fatalf("request = %#v", executor.request)
	}
	if got := executor.request.Query.Get("briefRepresentation"); got != "true" {
		t.Fatalf("briefRepresentation = %q", got)
	}
	if got := executor.request.Query.Get("max"); got != "10" {
		t.Fatalf("max = %q", got)
	}
	if _, exists := executor.request.PathParams["realm"]; exists {
		t.Fatal("handler must not accept a realm override")
	}
	if _, err := tool.Handler(t.Context(), map[string]any{"realm": "other"}); err == nil {
		t.Fatal("realm override should be rejected")
	}
	if executor.request.Accept != "application/json" {
		t.Fatalf("users list Accept = %q", executor.request.Accept)
	}
}

func TestToolHandlerSelectsNonJSONResponseTypes(t *testing.T) {
	executor := &recordingExecutor{}
	generated, err := New(executor)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	localization := findOperationTool(t, generated.GetTools(), "GET", "/admin/realms/{realm}/localization/{locale}/{key}")
	if _, err := localization.Handler(t.Context(), map[string]any{"locale": "en", "key": "welcome"}); err != nil {
		t.Fatalf("localization Handler: %v", err)
	}
	if executor.request.Accept != "text/plain" {
		t.Fatalf("localization Accept = %q, want text/plain", executor.request.Accept)
	}

	download := findOperationTool(t, generated.GetTools(), "POST", "/admin/realms/{realm}/clients/{client-uuid}/certificates/{attr}/download")
	if _, err := download.Handler(t.Context(), map[string]any{"client-uuid": "client", "attr": "jwt.credential"}); err != nil {
		t.Fatalf("certificate download Handler: %v", err)
	}
	if executor.request.Accept != "application/octet-stream" {
		t.Fatalf("certificate download Accept = %q, want application/octet-stream", executor.request.Accept)
	}
}

func TestToolHandlerPassesJSONAndFormBodies(t *testing.T) {
	executor := &recordingExecutor{}
	generated, err := New(executor)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	createUser := findTool(t, generated.GetTools(), "keycloak_users_post_users")
	body := map[string]any{"username": "alice"}
	if _, err := createUser.Handler(t.Context(), map[string]any{"body": body}); err != nil {
		t.Fatalf("create user Handler: %v", err)
	}
	if executor.request.ContentType != "application/json" || !reflect.DeepEqual(executor.request.Body, body) {
		t.Fatalf("JSON request = %#v", executor.request)
	}

	invite := findOperationTool(t, generated.GetTools(), "POST", "/admin/realms/{realm}/organizations/{org-id}/members/invite-user")
	form := map[string]any{"id": "user-id"}
	if _, err := invite.Handler(t.Context(), map[string]any{"org-id": "org-id", "body": form}); err != nil {
		t.Fatalf("invite Handler: %v", err)
	}
	if executor.request.ContentType != "application/x-www-form-urlencoded" || !reflect.DeepEqual(executor.request.Body, form) {
		t.Fatalf("form request = %#v", executor.request)
	}
}

func findTool(t *testing.T, tools []toolset.ServerTool, name string) toolset.ServerTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return toolset.ServerTool{}
}

func findOperationTool(t *testing.T, tools []toolset.ServerTool, method, path string) toolset.ServerTool {
	t.Helper()
	operations, err := keycloakapi.Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	for _, operation := range operations {
		if operation.Method == method && operation.Path == path {
			return findTool(t, tools, operation.ToolName)
		}
	}
	t.Fatalf("operation %s %s not found", method, path)
	return toolset.ServerTool{}
}

func decodeSchema(t *testing.T, tool toolset.ServerTool) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(tool.Tool.RawInputSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	return schema
}

func schemaRequired(schema map[string]any) map[string]bool {
	required := map[string]bool{}
	values, exists := schema["required"]
	if !exists {
		return required
	}
	for _, value := range values.([]any) {
		required[value.(string)] = true
	}
	return required
}

type recordingExecutor struct {
	request keycloakclient.Request
	result  string
	err     error
}

func (e *recordingExecutor) Execute(_ context.Context, request keycloakclient.Request) (string, error) {
	e.request = request
	return e.result, e.err
}
