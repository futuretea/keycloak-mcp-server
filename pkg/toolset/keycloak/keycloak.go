// Package keycloak exposes the version-pinned Keycloak Admin REST catalog as MCP tools.
package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/keycloak-mcp-server/internal/keycloakapi"
	"github.com/futuretea/keycloak-mcp-server/internal/keycloakclient"
	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
)

// Executor is the Keycloak client boundary used by generated tool handlers.
type Executor interface {
	Execute(context.Context, keycloakclient.Request) (string, error)
}

// Toolset exposes every Keycloak 26.6.4 Admin REST operation.
type Toolset struct {
	executor Executor
	tools    []toolset.ServerTool
}

// New creates a complete Keycloak Admin REST toolset.
func New(executor Executor) (*Toolset, error) {
	if executor == nil {
		return nil, fmt.Errorf("keycloak executor is required")
	}
	operations, err := keycloakapi.Catalog()
	if err != nil {
		return nil, fmt.Errorf("load keycloak API catalog: %w", err)
	}
	tools := make([]toolset.ServerTool, 0, len(operations))
	for _, operation := range operations {
		tools = append(tools, newServerTool(executor, operation))
	}
	return &Toolset{executor: executor, tools: tools}, nil
}

// GetName returns the toolset identity.
func (t *Toolset) GetName() string { return "keycloak" }

// GetDescription describes the grouped Admin REST toolset.
func (t *Toolset) GetDescription() string {
	return "Keycloak 26.6.4 Admin REST API grouped by resource domain"
}

// GetTools returns a copy of the generated tool catalog.
func (t *Toolset) GetTools() []toolset.ServerTool {
	return append([]toolset.ServerTool(nil), t.tools...)
}

func newServerTool(executor Executor, operation keycloakapi.Operation) toolset.ServerTool {
	schema, err := inputSchema(operation)
	if err != nil {
		panic(fmt.Sprintf("build input schema for %s: %v", operation.ToolName, err))
	}
	description := operation.Summary
	if description == "" {
		description = fmt.Sprintf("%s %s", operation.Method, operation.Path)
	}
	return toolset.ServerTool{
		Tool:        mcp.NewToolWithRawSchema(operation.ToolName, description, schema),
		Handler:     operationHandler(executor, operation),
		Domain:      operation.Domain,
		IsWrite:     operation.IsWrite,
		IsSensitive: operation.IsSensitive,
		IsGlobal:    operation.IsGlobal,
	}
}

func inputSchema(operation keycloakapi.Operation) (json.RawMessage, error) {
	properties := map[string]any{}
	required := make([]string, 0)
	for _, parameter := range operation.Parameters {
		if parameter.In == "path" && parameter.Name == "realm" {
			continue
		}
		property := map[string]any{"type": schemaType(parameter.Type)}
		if parameter.Description != "" {
			property["description"] = parameter.Description
		}
		if parameter.Type == "array" {
			property["items"] = map[string]any{"type": schemaType(parameter.ItemsType)}
		}
		properties[parameter.Name] = property
		if parameter.Required {
			required = append(required, parameter.Name)
		}
	}
	if len(operation.ContentTypes) > 0 {
		properties["body"] = map[string]any{
			"description": "Request body encoded with the operation content type",
		}
		if operation.RequestBodyRequired {
			required = append(required, "body")
		}
		if len(operation.ContentTypes) > 1 {
			properties["content_type"] = map[string]any{
				"type":        "string",
				"enum":        operation.ContentTypes,
				"description": "Content type used to encode body; defaults to application/json when available",
			}
		}
	}
	sort.Strings(required)
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode schema: %w", err)
	}
	return encoded, nil
}

func operationHandler(executor Executor, operation keycloakapi.Operation) toolset.ToolHandler {
	return func(ctx context.Context, params map[string]any) (string, error) {
		request, err := requestFor(operation, params)
		if err != nil {
			return "", err
		}
		return executor.Execute(ctx, request)
	}
}

func requestFor(operation keycloakapi.Operation, params map[string]any) (keycloakclient.Request, error) {
	known := map[string]struct{}{}
	pathParams := map[string]string{}
	query := url.Values{}
	for _, parameter := range operation.Parameters {
		if parameter.In == "path" && parameter.Name == "realm" {
			continue
		}
		known[parameter.Name] = struct{}{}
		value, exists := params[parameter.Name]
		if !exists || value == nil {
			if parameter.Required {
				return keycloakclient.Request{}, fmt.Errorf("%s is required", parameter.Name)
			}
			continue
		}
		values, err := parameterValues(parameter, value)
		if err != nil {
			return keycloakclient.Request{}, err
		}
		if parameter.In == "path" {
			pathParams[parameter.Name] = values[0]
			continue
		}
		query[parameter.Name] = values
	}

	request := keycloakclient.Request{
		Method:     operation.Method,
		Path:       operation.Path,
		PathParams: pathParams,
		Query:      query,
		Accept:     preferredResponseContentType(operation.ResponseContentTypes),
	}
	if len(operation.ContentTypes) > 0 {
		known["body"] = struct{}{}
		body, exists := params["body"]
		if !exists || body == nil {
			if operation.RequestBodyRequired {
				return keycloakclient.Request{}, fmt.Errorf("body is required")
			}
		} else {
			request.Body = body
		}
		contentType, err := contentTypeFor(operation, params, known)
		if err != nil {
			return keycloakclient.Request{}, err
		}
		request.ContentType = contentType
	}
	for name := range params {
		if _, ok := known[name]; !ok {
			return keycloakclient.Request{}, fmt.Errorf("unknown parameter %q", name)
		}
	}
	return request, nil
}

func parameterValues(parameter keycloakapi.Parameter, value any) ([]string, error) {
	if parameter.Type == "array" {
		values, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an array", parameter.Name)
		}
		converted := make([]string, 0, len(values))
		for _, item := range values {
			convertedValue, err := scalarValue(parameter.Name, parameter.ItemsType, item)
			if err != nil {
				return nil, err
			}
			converted = append(converted, convertedValue)
		}
		return converted, nil
	}
	converted, err := scalarValue(parameter.Name, parameter.Type, value)
	if err != nil {
		return nil, err
	}
	return []string{converted}, nil
}

func scalarValue(name, schemaType string, value any) (string, error) {
	switch schemaType {
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return "", fmt.Errorf("%s must be a boolean", name)
		}
		return strconv.FormatBool(boolean), nil
	case "integer":
		number, ok := value.(float64)
		if !ok || number != float64(int64(number)) {
			return "", fmt.Errorf("%s must be an integer", name)
		}
		return strconv.FormatInt(int64(number), 10), nil
	case "number":
		number, ok := value.(float64)
		if !ok {
			return "", fmt.Errorf("%s must be a number", name)
		}
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	default:
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", name)
		}
		return text, nil
	}
}

func contentTypeFor(operation keycloakapi.Operation, params map[string]any, known map[string]struct{}) (string, error) {
	contentType := preferredContentType(operation.ContentTypes)
	if len(operation.ContentTypes) == 1 {
		return contentType, nil
	}
	known["content_type"] = struct{}{}
	value, exists := params["content_type"]
	if !exists || value == nil {
		return contentType, nil
	}
	requested, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("content_type must be a string")
	}
	for _, supported := range operation.ContentTypes {
		if requested == supported {
			return requested, nil
		}
	}
	return "", fmt.Errorf("content_type %q is not supported", requested)
}

func preferredContentType(contentTypes []string) string {
	for _, contentType := range contentTypes {
		if contentType == "application/json" {
			return contentType
		}
	}
	return contentTypes[0]
}

func preferredResponseContentType(contentTypes []string) string {
	for _, contentType := range contentTypes {
		if contentType == "application/json" {
			return contentType
		}
	}
	for _, contentType := range contentTypes {
		if contentType == "text/plain" || contentType == "application/yaml" || contentType == "application/octet-stream" {
			return contentType
		}
	}
	if len(contentTypes) == 0 {
		return "application/json"
	}
	return contentTypes[0]
}

func schemaType(source string) string {
	switch source {
	case "boolean", "integer", "number", "array", "object", "string":
		return source
	default:
		return "string"
	}
}
