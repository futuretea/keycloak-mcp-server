package keycloakapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	toolNamePattern              = regexp.MustCompile(`^keycloak_[a-z0-9_]+$`)
	authorizationToolNamePattern = regexp.MustCompile(`^keycloak_authorization_`)
)

func TestCatalogCoversPinnedAdminAPIDocument(t *testing.T) {
	operations, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	if got, want := len(operations), 401; got != want {
		t.Fatalf("catalog length = %d, want %d", got, want)
	}

	counts := map[string]int{}
	sensitiveOperations := 0
	globalOperations := 0
	seenNames := map[string]struct{}{}
	seenOperations := map[string]struct{}{}
	for index, operation := range operations {
		if operation.ToolName == "" || operation.Domain == "" {
			t.Fatalf("operation has empty identity: %#v", operation)
		}
		if !toolNamePattern.MatchString(operation.ToolName) {
			t.Fatalf("tool name %q is not MCP-safe", operation.ToolName)
		}
		if index > 0 && operations[index-1].ToolName >= operation.ToolName {
			t.Fatalf("catalog order %q then %q is not ascending", operations[index-1].ToolName, operation.ToolName)
		}
		if _, exists := seenNames[operation.ToolName]; exists {
			t.Fatalf("duplicate tool name %q", operation.ToolName)
		}
		seenNames[operation.ToolName] = struct{}{}

		operationKey := operation.Method + " " + operation.Path
		if _, exists := seenOperations[operationKey]; exists {
			t.Fatalf("duplicate operation %q", operationKey)
		}
		seenOperations[operationKey] = struct{}{}
		counts[operation.Method]++

		if operation.Method == "GET" && operation.IsWrite {
			t.Fatalf("GET operation %q is classified as write", operation.ToolName)
		}
		if operation.Method != "GET" && !operation.IsWrite {
			t.Fatalf("non-GET operation %q is classified as read-only", operation.ToolName)
		}
		if operation.IsSensitive {
			sensitiveOperations++
			if operation.IsWrite || operation.Method != "GET" ||
				(operation.Path != "/admin/realms/{realm}/clients/{client-uuid}/client-secret" &&
					operation.Path != "/admin/realms/{realm}/clients/{client-uuid}/client-secret/rotated") {
				t.Fatalf("operation %q has an invalid sensitive classification", operation.ToolName)
			}
		}
		if operation.IsGlobal {
			globalOperations++
			if operation.Path != "/admin/realms" || (operation.Method != "GET" && operation.Method != "POST") {
				t.Fatalf("operation %q has an invalid global classification", operation.ToolName)
			}
		}
		seenParameters := map[string]struct{}{}
		for _, parameter := range operation.Parameters {
			if parameter.Name == "" || (parameter.In != "path" && parameter.In != "query") || parameter.Type == "" {
				t.Fatalf("operation %q has invalid parameter: %#v", operation.ToolName, parameter)
			}
			key := parameter.In + "\x00" + parameter.Name
			if _, exists := seenParameters[key]; exists {
				t.Fatalf("operation %q has duplicate parameter %q", operation.ToolName, key)
			}
			seenParameters[key] = struct{}{}
		}
	}

	wantCounts := map[string]int{"DELETE": 61, "GET": 202, "POST": 86, "PUT": 52}
	if got := fmt.Sprint(counts); got != fmt.Sprint(wantCounts) {
		t.Fatalf("method counts = %v, want %v", counts, wantCounts)
	}
	if got, want := sensitiveOperations, 2; got != want {
		t.Fatalf("sensitive operations = %d, want %d", got, want)
	}
	if got, want := globalOperations, 2; got != want {
		t.Fatalf("global operations = %d, want %d", got, want)
	}
}

func TestCatalogIncludesDirectParametersAndRequestBodies(t *testing.T) {
	operations, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	users, ok := findOperation(operations, "GET", "/admin/realms/{realm}/users")
	if !ok {
		t.Fatal("users list operation is missing")
	}
	if !hasParameter(users.Parameters, "path", "realm", "string", true) {
		t.Fatalf("users list is missing realm parameter: %#v", users.Parameters)
	}
	if !hasParameter(users.Parameters, "query", "briefRepresentation", "boolean", false) {
		t.Fatalf("users list is missing boolean query parameter: %#v", users.Parameters)
	}

	createUser, ok := findOperation(operations, "POST", "/admin/realms/{realm}/users")
	if !ok || len(createUser.ContentTypes) != 1 || createUser.ContentTypes[0] != "application/json" {
		t.Fatalf("create user request body = %#v", createUser)
	}

	inviteUser, ok := findOperation(operations, "POST", "/admin/realms/{realm}/organizations/{org-id}/members/invite-user")
	if !ok || len(inviteUser.ContentTypes) != 1 || inviteUser.ContentTypes[0] != "application/x-www-form-urlencoded" {
		t.Fatalf("invite user request body = %#v", inviteUser)
	}

	workflow, ok := findOperation(operations, "POST", "/admin/realms/{realm}/workflows/{id}/activate/{type}/{resourceId}")
	if !ok || !hasParameter(workflow.Parameters, "path", "type", "string", true) {
		t.Fatalf("workflow path parameters = %#v", workflow.Parameters)
	}
}

func TestCatalogClassifiesUntaggedAuthorizationOperations(t *testing.T) {
	operations, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	untagged := 0
	for _, operation := range operations {
		if operation.SourceTag != "" {
			continue
		}
		untagged++
		if operation.Domain != "authorization" {
			t.Fatalf("untagged operation %s %s domain = %q, want authorization", operation.Method, operation.Path, operation.Domain)
		}
		if !authorizationToolNamePattern.MatchString(operation.ToolName) {
			t.Fatalf("untagged operation %s %s tool name = %q, want authorization prefix", operation.Method, operation.Path, operation.ToolName)
		}
	}
	if got, want := untagged, 31; got != want {
		t.Fatalf("untagged operations = %d, want %d", got, want)
	}
}

func TestEmbeddedOpenAPIDocumentHash(t *testing.T) {
	got := fmt.Sprintf("%x", sha256.Sum256(openAPISpec))
	if got != OpenAPISHA256 {
		t.Fatalf("embedded OpenAPI SHA-256 = %s, want %s", got, OpenAPISHA256)
	}
}

func TestCatalogMatchesRawOpenAPIContract(t *testing.T) {
	var document contractDocument
	if err := json.Unmarshal(openAPISpec, &document); err != nil {
		t.Fatalf("decode raw OpenAPI contract: %v", err)
	}

	expected := make(map[string]contractOperation)
	directParameters := 0
	bodyOperations := 0
	multiContentTypeBodies := 0
	for path, pathItem := range document.Paths {
		pathParameters := contractParameters(pathItem.Parameters)
		for method, source := range pathItem.Operations {
			if !contractMethods[method] {
				continue
			}
			operation := contractOperation{
				Parameters:           mergeContractParameters(pathParameters, contractParameters(source.Parameters)),
				RequestBodyRequired:  source.RequestBody != nil && source.RequestBody.Required,
				ContentTypes:         contractContentTypes(source.RequestBody),
				ResponseContentTypes: contractSuccessContentTypes(source.Responses),
			}
			for _, parameter := range operation.Parameters {
				if parameter.In != "path" || parameter.Name != "realm" {
					directParameters++
				}
			}
			if source.RequestBody != nil {
				bodyOperations++
				if len(operation.ContentTypes) > 1 {
					multiContentTypeBodies++
				}
			}
			expected[contractKey(method, path)] = operation
		}
	}

	operations, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if got, want := len(expected), len(operations); got != want {
		t.Fatalf("raw OpenAPI operations = %d, catalog operations = %d", got, want)
	}
	for _, operation := range operations {
		want, exists := expected[contractKey(operation.Method, operation.Path)]
		if !exists {
			t.Fatalf("catalog operation %s %s is absent from raw OpenAPI", operation.Method, operation.Path)
		}
		if fmt.Sprint(operation.Parameters) != fmt.Sprint(want.Parameters) ||
			operation.RequestBodyRequired != want.RequestBodyRequired ||
			fmt.Sprint(operation.ContentTypes) != fmt.Sprint(want.ContentTypes) ||
			fmt.Sprint(operation.ResponseContentTypes) != fmt.Sprint(want.ResponseContentTypes) {
			t.Fatalf("catalog contract mismatch for %s %s: got %#v, want %#v", operation.Method, operation.Path, operation, want)
		}
		delete(expected, contractKey(operation.Method, operation.Path))
	}
	if len(expected) != 0 {
		t.Fatalf("raw OpenAPI operations absent from catalog: %d", len(expected))
	}
	if directParameters != 791 || bodyOperations != 120 || multiContentTypeBodies != 4 {
		t.Fatalf("raw OpenAPI contract totals = direct parameters %d, bodies %d, multi-content bodies %d", directParameters, bodyOperations, multiContentTypeBodies)
	}
}

var contractMethods = map[string]bool{"delete": true, "get": true, "post": true, "put": true}

type contractDocument struct {
	Paths map[string]contractPathItem `json:"paths"`
}

type contractPathItem struct {
	Parameters []contractParameter                `json:"parameters"`
	Operations map[string]contractSourceOperation `json:"-"`
}

func (p *contractPathItem) UnmarshalJSON(payload []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return err
	}
	if parameters, exists := raw["parameters"]; exists {
		if err := json.Unmarshal(parameters, &p.Parameters); err != nil {
			return err
		}
	}
	p.Operations = make(map[string]contractSourceOperation)
	for method := range contractMethods {
		payload, exists := raw[method]
		if !exists {
			continue
		}
		var operation contractSourceOperation
		if err := json.Unmarshal(payload, &operation); err != nil {
			return err
		}
		p.Operations[method] = operation
	}
	return nil
}

type contractSourceOperation struct {
	Parameters  []contractParameter         `json:"parameters"`
	RequestBody *contractRequestBody        `json:"requestBody"`
	Responses   map[string]contractResponse `json:"responses"`
}

type contractParameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Description string         `json:"description"`
	Required    bool           `json:"required"`
	Schema      contractSchema `json:"schema"`
}

type contractSchema struct {
	Type  string          `json:"type"`
	Items *contractSchema `json:"items"`
}

type contractRequestBody struct {
	Required bool                       `json:"required"`
	Content  map[string]json.RawMessage `json:"content"`
}

type contractResponse struct {
	Content map[string]json.RawMessage `json:"content"`
}

type contractOperation struct {
	Parameters           []Parameter
	RequestBodyRequired  bool
	ContentTypes         []string
	ResponseContentTypes []string
}

func contractParameters(parameters []contractParameter) []Parameter {
	result := make([]Parameter, 0, len(parameters))
	for _, parameter := range parameters {
		if parameter.In != "path" && parameter.In != "query" {
			continue
		}
		parameterType := parameter.Schema.Type
		if parameter.In == "path" || parameterType == "" {
			parameterType = "string"
		}
		converted := Parameter{
			Name: parameter.Name, In: parameter.In, Description: parameter.Description,
			Required: parameter.Required || parameter.In == "path", Type: parameterType,
		}
		if parameter.Schema.Items != nil {
			converted.ItemsType = parameter.Schema.Items.Type
		}
		result = append(result, converted)
	}
	return result
}

func mergeContractParameters(pathParameters, operationParameters []Parameter) []Parameter {
	merged := make(map[string]Parameter, len(pathParameters)+len(operationParameters))
	for _, parameter := range append(pathParameters, operationParameters...) {
		merged[parameter.In+"\x00"+parameter.Name] = parameter
	}
	result := make([]Parameter, 0, len(merged))
	for _, parameter := range merged {
		result = append(result, parameter)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].In == result[j].In {
			return result[i].Name < result[j].Name
		}
		return result[i].In < result[j].In
	})
	return result
}

func contractContentTypes(requestBody *contractRequestBody) []string {
	if requestBody == nil {
		return nil
	}
	result := make([]string, 0, len(requestBody.Content))
	for contentType := range requestBody.Content {
		result = append(result, contentType)
	}
	sort.Strings(result)
	return result
}

func contractSuccessContentTypes(responses map[string]contractResponse) []string {
	contentTypes := map[string]struct{}{}
	for statusCode, response := range responses {
		if len(statusCode) == 0 || statusCode[0] != '2' {
			continue
		}
		for contentType := range response.Content {
			contentTypes[contentType] = struct{}{}
		}
	}
	result := make([]string, 0, len(contentTypes))
	for contentType := range contentTypes {
		result = append(result, contentType)
	}
	sort.Strings(result)
	return result
}

func contractKey(method, path string) string {
	return strings.ToUpper(method) + "\x00" + path
}

func findOperation(operations []Operation, method, path string) (Operation, bool) {
	for _, operation := range operations {
		if operation.Method == method && operation.Path == path {
			return operation, true
		}
	}
	return Operation{}, false
}

func hasParameter(parameters []Parameter, location, name, parameterType string, required bool) bool {
	for _, parameter := range parameters {
		if parameter.In == location && parameter.Name == name && parameter.Type == parameterType && parameter.Required == required {
			return true
		}
	}
	return false
}
