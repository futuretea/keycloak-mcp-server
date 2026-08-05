package keycloakclient

import "fmt"

// APIError identifies a non-success Keycloak HTTP response without exposing raw payloads.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("keycloak API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("keycloak API returned HTTP %d: %s", e.StatusCode, e.Message)
}

func newAPIError(statusCode int, _ []byte) error {
	return &APIError{StatusCode: statusCode, Message: remediationFor(statusCode)}
}

func remediationFor(statusCode int) string {
	switch statusCode {
	case 400:
		return "check required parameters and request body values"
	case 401:
		return "verify the Keycloak service-account credentials"
	case 403:
		return "grant the service account the required Keycloak admin role"
	case 404:
		return "verify the configured realm and resource identifiers"
	case 409:
		return "reload the resource and resolve the conflicting change"
	case 429:
		return "retry after Keycloak accepts additional requests"
	}
	if statusCode >= 500 && statusCode < 600 {
		return "Keycloak is unavailable; retry the request"
	}
	return "request failed without exposing the remote response payload"
}
