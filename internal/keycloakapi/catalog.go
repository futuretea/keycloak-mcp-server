// Package keycloakapi exposes the version-pinned Keycloak Admin REST catalog.
package keycloakapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const (
	// Version is the Keycloak version represented by the embedded API document.
	Version = "26.6.4"
	// OpenAPISHA256 identifies the reviewed OpenAPI source document.
	OpenAPISHA256 = "c7580b4a4a732d38e91b816a4759926c64c2167d6d65ea7ffdb96eb5d0e766aa"
)

//go:embed spec/keycloak-admin-26.6.4-openapi.json
var openAPISpec []byte

var methods = map[string]bool{
	"delete":  true,
	"get":     true,
	"head":    true,
	"options": true,
	"patch":   true,
	"post":    true,
	"put":     true,
	"trace":   true,
}

// Operation is one Keycloak Admin REST operation with an MCP-safe identity.
type Operation struct {
	ToolName             string
	Domain               string
	Method               string
	Path                 string
	Summary              string
	IsWrite              bool
	IsSensitive          bool
	IsGlobal             bool
	SourceTag            string
	Parameters           []Parameter
	RequestBodyRequired  bool
	ContentTypes         []string
	ResponseContentTypes []string
}

// Parameter is one direct path or query argument accepted by an operation.
type Parameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Type        string
	ItemsType   string
}

type openAPIDocument struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

type openAPIOperation struct {
	Summary     string                     `json:"summary"`
	Tags        []string                   `json:"tags"`
	Parameters  []openAPIParameter         `json:"parameters"`
	RequestBody *openAPIRequestBody        `json:"requestBody"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIParameter struct {
	Name        string        `json:"name"`
	In          string        `json:"in"`
	Description string        `json:"description"`
	Required    bool          `json:"required"`
	Schema      openAPISchema `json:"schema"`
}

type openAPISchema struct {
	Type  string         `json:"type"`
	Items *openAPISchema `json:"items"`
}

type openAPIRequestBody struct {
	Required bool                        `json:"required"`
	Content  map[string]openAPIMediaType `json:"content"`
}

type openAPIResponse struct {
	Content map[string]openAPIMediaType `json:"content"`
}

type openAPIMediaType struct {
	Schema openAPISchema `json:"schema"`
}

// Catalog returns every operation from the embedded Keycloak 26.6.4 Admin REST
// document in deterministic tool-name order.
func Catalog() ([]Operation, error) {
	var document openAPIDocument
	if err := json.Unmarshal(openAPISpec, &document); err != nil {
		return nil, fmt.Errorf("decode embedded Keycloak Admin REST document: %w", err)
	}

	operations := make([]Operation, 0, 401)
	seenNames := make(map[string]string)
	for path, pathItem := range document.Paths {
		pathParameters, err := pathItemParameters(pathItem)
		if err != nil {
			return nil, fmt.Errorf("decode parameters for %s: %w", path, err)
		}
		for method, rawOperation := range pathItem {
			if !methods[method] {
				continue
			}
			var source openAPIOperation
			if err := json.Unmarshal(rawOperation, &source); err != nil {
				return nil, fmt.Errorf("decode %s %s: %w", strings.ToUpper(method), path, err)
			}

			domain, err := domainFor(path, source.Tags)
			if err != nil {
				return nil, err
			}
			operation := Operation{
				ToolName:    toolName(domain, method, path),
				Domain:      domain,
				Method:      strings.ToUpper(method),
				Path:        path,
				Summary:     source.Summary,
				IsWrite:     isWriteMethod(method),
				IsSensitive: isSensitiveOperation(method, path),
				IsGlobal:    isGlobalOperation(path),
				Parameters:  combineParameters(pathParameters, source.Parameters),
			}
			if len(source.Tags) > 0 {
				operation.SourceTag = source.Tags[0]
			}
			if source.RequestBody != nil {
				operation.RequestBodyRequired = source.RequestBody.Required
				operation.ContentTypes = sortedContentTypes(source.RequestBody.Content)
			}
			operation.ResponseContentTypes = successContentTypes(source.Responses)

			if previousPath, exists := seenNames[operation.ToolName]; exists {
				return nil, fmt.Errorf("duplicate tool name %q for %s and %s", operation.ToolName, previousPath, path)
			}
			seenNames[operation.ToolName] = path
			operations = append(operations, operation)
		}
	}

	sort.Slice(operations, func(i, j int) bool {
		return operations[i].ToolName < operations[j].ToolName
	})
	return operations, nil
}

func pathItemParameters(pathItem map[string]json.RawMessage) ([]openAPIParameter, error) {
	raw, ok := pathItem["parameters"]
	if !ok {
		return nil, nil
	}
	var parameters []openAPIParameter
	if err := json.Unmarshal(raw, &parameters); err != nil {
		return nil, err
	}
	return parameters, nil
}

func combineParameters(pathParameters, operationParameters []openAPIParameter) []Parameter {
	combined := make(map[string]Parameter, len(pathParameters)+len(operationParameters))
	for _, source := range append(pathParameters, operationParameters...) {
		if source.In != "path" && source.In != "query" {
			continue
		}
		parameterType := source.Schema.Type
		if source.In == "path" || parameterType == "" {
			parameterType = "string"
		}
		parameter := Parameter{
			Name:        source.Name,
			In:          source.In,
			Description: source.Description,
			Required:    source.Required || source.In == "path",
			Type:        parameterType,
		}
		if source.Schema.Items != nil {
			parameter.ItemsType = source.Schema.Items.Type
		}
		combined[source.In+"\x00"+source.Name] = parameter
	}
	parameters := make([]Parameter, 0, len(combined))
	for _, parameter := range combined {
		parameters = append(parameters, parameter)
	}
	sort.Slice(parameters, func(i, j int) bool {
		if parameters[i].In == parameters[j].In {
			return parameters[i].Name < parameters[j].Name
		}
		return parameters[i].In < parameters[j].In
	})
	return parameters
}

func sortedContentTypes(content map[string]openAPIMediaType) []string {
	contentTypes := make([]string, 0, len(content))
	for contentType := range content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	return contentTypes
}

func successContentTypes(responses map[string]openAPIResponse) []string {
	content := make(map[string]openAPIMediaType)
	for statusCode, response := range responses {
		if !strings.HasPrefix(statusCode, "2") {
			continue
		}
		for contentType, mediaType := range response.Content {
			content[contentType] = mediaType
		}
	}
	return sortedContentTypes(content)
}

func domainFor(path string, tags []string) (string, error) {
	if len(tags) > 0 {
		return slug(tags[0]), nil
	}
	if strings.HasPrefix(path, "/admin/realms/{realm}/clients/{client-uuid}/authz/resource-server") {
		return "authorization", nil
	}
	return "", fmt.Errorf("cannot classify untagged Keycloak Admin REST path %q", path)
}

func toolName(domain, method, path string) string {
	return "keycloak_" + domain + "_" + method + "_" + pathSuffix(path)
}

func pathSuffix(path string) string {
	const realmPrefix = "/admin/realms/{realm}"

	path = strings.TrimPrefix(path, realmPrefix)
	if path == "" {
		return "realm"
	}
	if path == "/admin/realms" {
		return "realms"
	}
	path = strings.TrimPrefix(path, "/admin/")

	parts := strings.Split(strings.Trim(path, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			result = append(result, "by_"+slug(strings.Trim(part, "{}")))
			continue
		}
		result = append(result, slug(part))
	}
	return strings.Join(result, "_")
}

func isWriteMethod(method string) bool {
	return method != "get" && method != "head" && method != "options"
}

func isSensitiveOperation(method, path string) bool {
	if method != "get" {
		return false
	}
	return strings.HasSuffix(path, "/client-secret") || strings.HasSuffix(path, "/client-secret/rotated")
}

func isGlobalOperation(path string) bool {
	return path == "/admin/realms"
}

func slug(value string) string {
	var builder strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			previousSeparator = false
			continue
		}
		if builder.Len() > 0 && !previousSeparator {
			builder.WriteByte('_')
			previousSeparator = true
		}
	}
	return strings.Trim(builder.String(), "_")
}
