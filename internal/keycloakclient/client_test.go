package keycloakclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/futuretea/keycloak-mcp-server/pkg/core/config"
)

func TestClientUsesBearerTokenAndCachesIt(t *testing.T) {
	tokenRequests := 0
	adminRequests := 0
	token := testOpaqueValue(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/realms/master/protocol/openid-connect/token":
			tokenRequests++
			assertTokenRequest(t, r)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": 60})
		case "/auth/admin/realms/master/users/user/one":
			adminRequests++
			if got := r.URL.EscapedPath(); got != "/auth/admin/realms/master/users/user%2Fone" {
				t.Fatalf("escaped path = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+token {
				t.Fatalf("Authorization = %q", got)
			}
			if got := r.URL.Query().Get("briefRepresentation"); got != "true" {
				t.Fatalf("query parameter = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"user/one"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL+"/auth")
	request := Request{
		Method:     http.MethodGet,
		Path:       "/admin/realms/{realm}/users/{user-id}",
		PathParams: map[string]string{"user-id": "user/one"},
		Query:      url.Values{"briefRepresentation": {"true"}},
	}
	for range 2 {
		result, err := client.Execute(t.Context(), request)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result != "{\n  \"id\": \"user/one\"\n}" {
			t.Fatalf("result = %q", result)
		}
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
	if adminRequests != 2 {
		t.Fatalf("admin requests = %d, want 2", adminRequests)
	}
}

func TestClientRefreshesExpiredToken(t *testing.T) {
	tokens := []string{testOpaqueValue(t), testOpaqueValue(t)}
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tokens[tokenRequests], "expires_in": 5})
			tokenRequests++
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+tokens[tokenRequests-1] {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := mustNewClient(t, server.URL)
	clock := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return clock }
	request := Request{Method: http.MethodGet, Path: "/admin/realms/{realm}/users"}
	if _, err := client.Execute(t.Context(), request); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	clock = clock.Add(time.Second)
	if _, err := client.Execute(t.Context(), request); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if tokenRequests != 2 {
		t.Fatalf("token requests = %d, want 2", tokenRequests)
	}
}

func TestClientRedactsTokenAndSecretFromErrors(t *testing.T) {
	secret := testOpaqueValue(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error_description": secret})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := mustNewClientWithSecret(t, server.URL, secret)
	_, err := client.Execute(t.Context(), Request{Method: http.MethodGet, Path: "/admin/realms/{realm}/users"})
	if err == nil {
		t.Fatal("Execute should fail")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "access_token") {
		t.Fatal("error leaked credentials")
	}
	if !strings.Contains(err.Error(), "verify the Keycloak service-account credentials") {
		t.Fatalf("error = %q, want actionable credential guidance", err)
	}
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("error = %T %v, want unauthorized APIError", err, err)
	}
}

func TestClientNegotiatesAndFormatsResponseContent(t *testing.T) {
	testCases := []struct {
		name         string
		accept       string
		responseType string
		responseBody []byte
		wantText     string
		wantBinary   bool
	}{
		{name: "JSON", accept: "application/json", responseType: "application/json; charset=utf-8", responseBody: []byte(`{"id":"user"}`), wantText: "{\n  \"id\": \"user\"\n}"},
		{name: "text", accept: "text/plain", responseType: "text/plain; charset=utf-8", responseBody: []byte("Welcome"), wantText: "Welcome"},
		{name: "YAML", accept: "application/yaml", responseType: "application/yaml", responseBody: []byte("enabled: true\n"), wantText: "enabled: true\n"},
		{name: "binary", accept: "application/octet-stream", responseType: "application/octet-stream", responseBody: []byte{0, 1, 2, 255}, wantBinary: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			token := testOpaqueValue(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": 60})
					return
				}
				if got := r.Header.Get("Accept"); got != testCase.accept {
					t.Fatalf("Accept = %q, want %q", got, testCase.accept)
				}
				w.Header().Set("Content-Type", testCase.responseType)
				_, _ = w.Write(testCase.responseBody)
			}))
			defer server.Close()

			client := mustNewClient(t, server.URL)
			result, err := client.Execute(t.Context(), Request{
				Method: http.MethodGet,
				Path:   "/admin/realms/{realm}/resource",
				Accept: testCase.accept,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !testCase.wantBinary {
				if result != testCase.wantText {
					t.Fatalf("result = %q, want %q", result, testCase.wantText)
				}
				return
			}
			var encoded struct {
				ContentType string `json:"content_type"`
				Encoding    string `json:"encoding"`
				Data        string `json:"data"`
			}
			if err := json.Unmarshal([]byte(result), &encoded); err != nil {
				t.Fatalf("decode binary envelope: %v", err)
			}
			if encoded.ContentType != "application/octet-stream" || encoded.Encoding != "base64" {
				t.Fatalf("binary envelope = %#v", encoded)
			}
			payload, err := decodeBase64(encoded.Data)
			if err != nil || string(payload) != string(testCase.responseBody) {
				t.Fatalf("decoded binary payload = %v, error = %v", payload, err)
			}
		})
	}
}

func TestClientRefusesRedirectsBeforeCredentialReplay(t *testing.T) {
	receiverCalls := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receiverCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer receiver.Close()

	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			http.Redirect(w, r, receiver.URL, http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
	}))
	defer issuer.Close()

	client := mustNewClient(t, issuer.URL)
	_, err := client.Execute(t.Context(), Request{Method: http.MethodGet, Path: "/admin/realms/{realm}/users"})
	if err == nil {
		t.Fatal("Execute should reject a token redirect")
	}
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %T %v, want HTTP %d APIError", err, err, http.StatusTemporaryRedirect)
	}
	if receiverCalls != 0 {
		t.Fatalf("redirect receiver calls = %d, want 0", receiverCalls)
	}
}

func TestClientRejectsInvalidAdminRequest(t *testing.T) {
	client := mustNewClient(t, "https://keycloak.example.test")
	_, err := client.Execute(context.Background(), Request{Method: http.MethodGet, Path: "/realms/{realm}"})
	if err == nil || !strings.Contains(err.Error(), "must start with /admin/") {
		t.Fatalf("invalid path error = %v", err)
	}

	_, err = client.Execute(context.Background(), Request{Method: http.MethodGet, Path: "/admin/realms/{realm}/users/{user-id}"})
	if err == nil || !strings.Contains(err.Error(), `parameter "user-id" is required`) {
		t.Fatalf("missing parameter error = %v", err)
	}

	for _, path := range []string{
		"/admin/../realms/master/users",
		"/admin/realms/other/users",
		"/admin/realms%2Fother/users",
		"/admin/%2e%2e/realms/master/users",
	} {
		_, err = client.Execute(context.Background(), Request{Method: http.MethodGet, Path: path})
		if err == nil {
			t.Fatalf("path %q should be rejected", path)
		}
	}

	endpoint, err := client.adminURL("/admin/realms/{realm}/users", map[string]string{"realm": "other"})
	if err != nil {
		t.Fatalf("adminURL: %v", err)
	}
	if !strings.Contains(endpoint, "/admin/realms/master/users") {
		t.Fatalf("realm override endpoint = %q", endpoint)
	}
}

func TestClientSupportsEscapedConfiguredRealm(t *testing.T) {
	realm := "sales west"
	client, err := New(&config.StaticConfig{
		KeycloakURL:           "https://keycloak.example.test",
		KeycloakRealm:         realm,
		KeycloakClientID:      testOpaqueValue(t),
		KeycloakClientSecret:  testOpaqueValue(t),
		RequestTimeoutSeconds: 5,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	endpoint, err := client.adminURL("/admin/realms/{realm}/users", nil)
	if err != nil {
		t.Fatalf("adminURL: %v", err)
	}
	if !strings.Contains(endpoint, "/admin/realms/sales%20west/users") {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestClientEncodesDocumentedBodyTypes(t *testing.T) {
	testCases := []struct {
		name        string
		contentType string
		body        any
		wantBody    string
	}{
		{name: "JSON", contentType: "application/json", body: map[string]any{"enabled": true}, wantBody: `{"enabled":true}`},
		{name: "form", contentType: "application/x-www-form-urlencoded", body: map[string]any{"id": "user-id"}, wantBody: "id=user-id"},
		{name: "text", contentType: "text/plain", body: "label", wantBody: "label"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			token := testOpaqueValue(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/token") {
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": 60})
					return
				}
				if got := r.Header.Get("Content-Type"); got != testCase.contentType {
					t.Fatalf("Content-Type = %q", got)
				}
				payload, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if got := string(payload); got != testCase.wantBody {
					t.Fatalf("body = %q", got)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			client := mustNewClient(t, server.URL)
			_, err := client.Execute(t.Context(), Request{
				Method:      http.MethodPut,
				Path:        "/admin/realms/{realm}/resource",
				Body:        testCase.body,
				ContentType: testCase.contentType,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	}
}

func mustNewClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	return mustNewClientWithSecret(t, baseURL, testOpaqueValue(t))
}

func mustNewClientWithSecret(t *testing.T, baseURL, secret string) *Client {
	t.Helper()
	client, err := New(&config.StaticConfig{
		KeycloakURL:           baseURL,
		KeycloakRealm:         "master",
		KeycloakClientID:      testOpaqueValue(t),
		KeycloakClientSecret:  secret,
		RequestTimeoutSeconds: 5,
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func assertTokenRequest(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Fatalf("token method = %s", request.Method)
	}
	if got := request.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("token content type = %q", got)
	}
	if err := request.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	if got := request.Form.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type = %q", got)
	}
	if request.Form.Get("client_id") == "" {
		t.Fatal("client_id must be sent")
	}
	if request.Form.Get("client_secret") == "" {
		t.Fatal("client_secret must be sent")
	}
}

func testOpaqueValue(t *testing.T) string {
	t.Helper()
	payload := make([]byte, 24)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("generate test-only opaque value: %v", err)
	}
	return hex.EncodeToString(payload)
}

func decodeBase64(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}
