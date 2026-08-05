//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const keycloakImage = "quay.io/keycloak/keycloak:26.6.4"

type keycloakFixture struct {
	BaseURL      string
	Realm        string
	ClientID     string
	ClientSecret string
}

func startKeycloakFixture(t *testing.T) keycloakFixture {
	t.Helper()
	assertDockerAvailable(t)
	adminUser := "e2e-" + randomHex(t, 8)
	adminPassword := randomHex(t, 24)
	containerName := "keycloak-mcp-e2e-" + randomHex(t, 6)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := dockerRun(ctx,
		"run", "--detach", "--rm", "--name", containerName,
		"--publish", "127.0.0.1::8080",
		"--env", "KC_BOOTSTRAP_ADMIN_USERNAME="+adminUser,
		"--env", "KC_BOOTSTRAP_ADMIN_PASSWORD="+adminPassword,
		keycloakImage, "start-dev", "--http-port=8080"); err != nil {
		t.Fatalf("start Keycloak %s container: %v", keycloakImage, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := dockerRun(cleanupCtx, "rm", "--force", containerName); err != nil {
			t.Errorf("remove Keycloak E2E container: %v", err)
		}
	})

	baseURL, err := dockerPublishedURL(ctx, containerName)
	if err != nil {
		t.Fatalf("find Keycloak published port: %v", err)
	}
	waitForKeycloak(t, baseURL, 90*time.Second)
	adminToken := keycloakPasswordAccessToken(t, baseURL, "master", adminUser, adminPassword)

	realm := "e2e-" + randomHex(t, 8)
	clientID := "mcp-e2e-" + randomHex(t, 8)
	keycloakAdminCall(t, baseURL, adminToken, http.MethodPost, "/admin/realms", map[string]any{
		"realm": realm, "enabled": true,
	}, nil)
	keycloakAdminCall(t, baseURL, adminToken, http.MethodPost, "/admin/realms/"+realm+"/clients", map[string]any{
		"clientId":                  clientID,
		"serviceAccountsEnabled":    true,
		"standardFlowEnabled":       false,
		"directAccessGrantsEnabled": false,
		"publicClient":              false,
	}, nil)

	clientUUID := keycloakClientUUID(t, baseURL, adminToken, realm, clientID)
	var secretResponse struct {
		Value string `json:"value"`
	}
	keycloakAdminCall(t, baseURL, adminToken, http.MethodPost,
		"/admin/realms/"+realm+"/clients/"+clientUUID+"/client-secret", nil, &secretResponse)
	if secretResponse.Value == "" {
		t.Fatal("Keycloak did not return a service-account client secret")
	}
	grantServiceAccountUserQueryRoles(t, baseURL, adminToken, realm, clientUUID)

	return keycloakFixture{
		BaseURL:      baseURL,
		Realm:        realm,
		ClientID:     clientID,
		ClientSecret: secretResponse.Value,
	}
}

func assertDockerAvailable(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := dockerRun(ctx, "version", "--format", "{{.Server.Version}}"); err != nil {
		t.Fatalf("Docker is required for E2E tests: %v", err)
	}
}

func dockerPublishedURL(ctx context.Context, containerName string) (string, error) {
	command := exec.CommandContext(ctx, "docker", "port", containerName, "8080/tcp")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("query Docker published port: %w", err)
	}
	address := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse Docker published port: %w", err)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func waitForKeycloak(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/realms/master/.well-known/openid-configuration")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Keycloak %s did not become ready within %s", keycloakImage, timeout)
}

func keycloakPasswordAccessToken(t *testing.T, baseURL, realm, username, password string) string {
	t.Helper()
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {"admin-cli"},
		"username":   {username},
		"password":   {password},
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		baseURL+"/realms/"+url.PathEscape(realm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("create Keycloak token request: %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("request Keycloak token: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Keycloak token response status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode Keycloak token response: %v", err)
	}
	if payload.AccessToken == "" {
		t.Fatal("Keycloak token response did not contain access_token")
	}
	return payload.AccessToken
}

func keycloakClientUUID(t *testing.T, baseURL, token, realm, clientID string) string {
	t.Helper()
	var clients []struct {
		ID string `json:"id"`
	}
	keycloakAdminCall(t, baseURL, token, http.MethodGet,
		"/admin/realms/"+realm+"/clients?clientId="+url.QueryEscape(clientID), nil, &clients)
	if len(clients) != 1 || clients[0].ID == "" {
		t.Fatalf("Keycloak client lookup returned %d clients, want one", len(clients))
	}
	return clients[0].ID
}

func grantServiceAccountUserQueryRoles(t *testing.T, baseURL, token, realm, clientUUID string) {
	t.Helper()
	var serviceAccount struct {
		ID string `json:"id"`
	}
	keycloakAdminCall(t, baseURL, token, http.MethodGet,
		"/admin/realms/"+realm+"/clients/"+clientUUID+"/service-account-user", nil, &serviceAccount)
	if serviceAccount.ID == "" {
		t.Fatal("Keycloak did not return the service-account user")
	}
	realmManagementID := keycloakClientUUID(t, baseURL, token, realm, "realm-management")
	roles := make([]map[string]any, 0, 2)
	for _, roleName := range []string{"query-users", "view-users"} {
		var role map[string]any
		keycloakAdminCall(t, baseURL, token, http.MethodGet,
			"/admin/realms/"+realm+"/clients/"+realmManagementID+"/roles/"+roleName, nil, &role)
		roles = append(roles, role)
	}
	keycloakAdminCall(t, baseURL, token, http.MethodPost,
		"/admin/realms/"+realm+"/users/"+serviceAccount.ID+"/role-mappings/clients/"+realmManagementID, roles, nil)
}

func keycloakAdminCall(t *testing.T, baseURL, token, method, path string, input, output any) {
	t.Helper()
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("encode Keycloak admin request: %v", err)
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(t.Context(), method, baseURL+path, body)
	if err != nil {
		t.Fatalf("create Keycloak admin request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("call Keycloak admin API: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("Keycloak admin %s %s status = %d", method, path, response.StatusCode)
	}
	if output != nil {
		if err := json.NewDecoder(response.Body).Decode(output); err != nil {
			t.Fatalf("decode Keycloak admin %s %s response: %v", method, path, err)
		}
	}
}

func dockerRun(ctx context.Context, args ...string) error {
	if err := exec.CommandContext(ctx, "docker", args...).Run(); err != nil {
		return fmt.Errorf("run Docker command: %w", err)
	}
	return nil
}

func randomHex(t *testing.T, byteLength int) string {
	t.Helper()
	payload := make([]byte, byteLength)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("generate E2E random value: %v", err)
	}
	return hex.EncodeToString(payload)
}
