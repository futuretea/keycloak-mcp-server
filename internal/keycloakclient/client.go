// Package keycloakclient executes Keycloak Admin REST requests with a service account.
package keycloakclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/futuretea/keycloak-mcp-server/pkg/core/config"
)

const tokenRefreshSkew = 5 * time.Second

var pathParameterPattern = regexp.MustCompile(`\{([^{}]+)\}`)

// Request describes an Admin REST operation from the pinned Keycloak catalog.
type Request struct {
	Method      string
	Path        string
	PathParams  map[string]string
	Query       url.Values
	Body        any
	ContentType string
	Accept      string
}

// Client is a service-account authenticated Keycloak Admin REST client.
type Client struct {
	baseURL      *url.URL
	realm        string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	now          func() time.Time

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// New creates a Keycloak client from validated static configuration.
func New(staticConfig *config.StaticConfig, httpClient *http.Client) (*Client, error) {
	if staticConfig == nil {
		return nil, fmt.Errorf("static config is required")
	}
	if err := staticConfig.ValidateKeycloak(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(staticConfig.RequestTimeoutSeconds) * time.Second}
	}
	baseURL, err := url.Parse(staticConfig.KeycloakURL)
	if err != nil {
		return nil, fmt.Errorf("parse keycloak_url: %w", err)
	}
	client := *httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		baseURL:      baseURL,
		realm:        staticConfig.KeycloakRealm,
		clientID:     staticConfig.KeycloakClientID,
		clientSecret: staticConfig.KeycloakClientSecret,
		httpClient:   &client,
		now:          time.Now,
	}, nil
}

// Execute sends one Keycloak Admin REST request and returns its response body.
func (c *Client) Execute(ctx context.Context, request Request) (string, error) {
	if c == nil {
		return "", fmt.Errorf("keycloak client is required")
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		return "", fmt.Errorf("keycloak request method is required")
	}
	endpoint, err := c.adminURL(request.Path, request.PathParams)
	if err != nil {
		return "", err
	}
	if len(request.Query) > 0 {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("parse keycloak Admin URL: %w", err)
		}
		parsed.RawQuery = request.Query.Encode()
		endpoint = parsed.String()
	}

	body, contentType, err := encodeBody(request.Body, request.ContentType)
	if err != nil {
		return "", err
	}
	token, err := c.token(ctx)
	if err != nil {
		return "", err
	}

	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return "", fmt.Errorf("create keycloak Admin request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	accept := request.Accept
	if accept == "" {
		accept = "application/json"
	}
	httpRequest.Header.Set("Accept", accept)
	if body != nil {
		httpRequest.Header.Set("Content-Type", contentType)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("call keycloak Admin API: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read keycloak Admin response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", newAPIError(response.StatusCode, payload)
	}
	return formatResponse(payload, response.Header.Get("Content-Type")), nil
}

func (c *Client) adminURL(pathTemplate string, pathParams map[string]string) (string, error) {
	if !strings.HasPrefix(pathTemplate, "/admin/") {
		return "", fmt.Errorf("keycloak Admin path must start with /admin/")
	}

	missing := ""
	invalid := ""
	path := pathParameterPattern.ReplaceAllStringFunc(pathTemplate, func(match string) string {
		name := strings.Trim(match, "{}")
		value := c.realm
		if name != "realm" {
			value = pathParams[name]
		}
		if value == "" {
			missing = name
			return match
		}
		if value == "." || value == ".." {
			invalid = name
			return match
		}
		return url.PathEscape(value)
	})
	if missing != "" {
		return "", fmt.Errorf("keycloak Admin path parameter %q is required", missing)
	}
	if invalid != "" {
		return "", fmt.Errorf("keycloak Admin path parameter %q cannot be a dot segment", invalid)
	}
	if strings.Contains(path, "{") || strings.Contains(path, "}") {
		return "", fmt.Errorf("keycloak Admin path has an invalid parameter placeholder or dot segment")
	}
	canonicalPath, err := url.PathUnescape(path)
	if err != nil {
		return "", fmt.Errorf("decode keycloak Admin path: %w", err)
	}
	if !strings.HasPrefix(canonicalPath, "/admin/") || hasDotSegment(canonicalPath) {
		return "", fmt.Errorf("keycloak Admin path must be normalized under /admin/")
	}
	if err := c.validateRealmPath(canonicalPath); err != nil {
		return "", err
	}
	return c.endpointURL(path)
}

func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && c.now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	endpoint, err := c.endpointURL("/realms/" + url.PathEscape(c.realm) + "/protocol/openid-connect/token")
	if err != nil {
		return "", fmt.Errorf("build keycloak token URL: %w", err)
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create keycloak token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request keycloak access token: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read keycloak token response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", newAPIError(response.StatusCode, payload)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(payload, &tokenResponse); err != nil {
		return "", fmt.Errorf("decode keycloak token response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("keycloak token response did not contain access_token")
	}

	c.accessToken = tokenResponse.AccessToken
	c.tokenExpiry = c.now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second).Add(-tokenRefreshSkew)
	return c.accessToken, nil
}

func encodeBody(value any, contentType string) (io.Reader, string, error) {
	if value == nil {
		return nil, "", nil
	}
	if contentType == "" {
		contentType = "application/json"
	}
	switch contentType {
	case "application/json":
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, "", fmt.Errorf("encode keycloak Admin request body: %w", err)
		}
		return bytes.NewReader(payload), contentType, nil
	case "application/x-www-form-urlencoded":
		form, err := formBody(value)
		if err != nil {
			return nil, "", err
		}
		return strings.NewReader(form.Encode()), contentType, nil
	case "text/plain", "application/xml", "application/yaml":
		text, ok := value.(string)
		if !ok {
			return nil, "", fmt.Errorf("keycloak request body for %s must be a string", contentType)
		}
		return strings.NewReader(text), contentType, nil
	default:
		return nil, "", fmt.Errorf("unsupported keycloak request content type %q", contentType)
	}
}

func formBody(value any) (url.Values, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("keycloak form request body must be an object")
	}
	form := make(url.Values, len(fields))
	for key, field := range fields {
		switch typed := field.(type) {
		case []any:
			for _, item := range typed {
				form.Add(key, fmt.Sprint(item))
			}
		case nil:
			continue
		default:
			form.Add(key, fmt.Sprint(typed))
		}
	}
	return form, nil
}

func (c *Client) endpointURL(escapedPath string) (string, error) {
	baseURL := *c.baseURL
	rawPath := strings.TrimRight(baseURL.EscapedPath(), "/") + escapedPath
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return "", fmt.Errorf("build keycloak URL: %w", err)
	}
	baseURL.Path = decodedPath
	baseURL.RawPath = rawPath
	return baseURL.String(), nil
}

func (c *Client) validateRealmPath(path string) error {
	const realmPrefix = "/admin/realms/"
	if !strings.HasPrefix(path, realmPrefix) {
		return nil
	}
	realmSegment := strings.Split(strings.TrimPrefix(path, realmPrefix), "/")[0]
	if realmSegment != c.realm {
		return fmt.Errorf("keycloak Admin path must target configured realm")
	}
	return nil
}

func hasDotSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func prettyJSON(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, payload, "", "  "); err == nil {
		return formatted.String()
	}
	return string(payload)
}

func formatResponse(payload []byte, contentType string) string {
	if len(payload) == 0 {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = ""
	}
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		return prettyJSON(payload)
	}
	if strings.HasPrefix(mediaType, "text/") || mediaType == "application/xml" || mediaType == "application/yaml" {
		return string(payload)
	}
	if mediaType == "" && json.Valid(payload) {
		return prettyJSON(payload)
	}
	encoded, _ := json.Marshal(struct {
		ContentType string `json:"content_type"`
		Encoding    string `json:"encoding"`
		Data        string `json:"data"`
	}{
		ContentType: mediaType,
		Encoding:    "base64",
		Data:        base64.StdEncoding.EncodeToString(payload),
	})
	return string(encoded)
}
