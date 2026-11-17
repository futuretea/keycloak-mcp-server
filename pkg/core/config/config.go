package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// StaticConfig contains configuration that is fixed when the server starts.
type StaticConfig struct {
	Port                  int      `mapstructure:"port"`
	Listen                string   `mapstructure:"listen"`
	SSEBaseURL            string   `mapstructure:"sse_base_url"`
	LogLevel              string   `mapstructure:"log_level"`
	KeycloakURL           string   `mapstructure:"keycloak_url"`
	KeycloakRealm         string   `mapstructure:"keycloak_realm"`
	KeycloakClientID      string   `mapstructure:"keycloak_client_id"`
	KeycloakClientSecret  string   `mapstructure:"keycloak_client_secret"`
	ReadOnly              bool     `mapstructure:"read_only"`
	RequestTimeoutSeconds int      `mapstructure:"request_timeout_seconds"`
	EnabledTools          []string `mapstructure:"enabled_tools"`
	DisabledTools         []string `mapstructure:"disabled_tools"`
	EnabledDomains        []string `mapstructure:"enabled_domains"`
	DisabledDomains       []string `mapstructure:"disabled_domains"`
}

// Validate checks whether the configuration can be used to start the server.
func (c *StaticConfig) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535, got %d", c.Port)
	}
	if c.Port != 0 && strings.TrimSpace(c.Listen) == "" {
		return fmt.Errorf("listen must be set when port is non-zero")
	}
	if _, err := zerolog.ParseLevel(c.LogLevel); err != nil {
		return fmt.Errorf("invalid log_level %q: %w", c.LogLevel, err)
	}
	if c.RequestTimeoutSeconds <= 0 {
		return fmt.Errorf("request_timeout_seconds must be positive, got %d", c.RequestTimeoutSeconds)
	}
	return nil
}

// ValidateKeycloak checks the credentials and endpoint required to call Keycloak.
func (c *StaticConfig) ValidateKeycloak() error {
	if strings.TrimSpace(c.KeycloakURL) == "" {
		return fmt.Errorf("keycloak_url is required to call Keycloak")
	}
	parsed, err := url.Parse(c.KeycloakURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("keycloak_url must be an absolute URL, got %q", c.KeycloakURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("keycloak_url scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("keycloak_url must not contain credentials, a query, or a fragment")
	}
	if strings.TrimSpace(c.KeycloakRealm) == "" {
		return fmt.Errorf("keycloak_realm is required to call Keycloak")
	}
	if strings.TrimSpace(c.KeycloakClientID) == "" {
		return fmt.Errorf("keycloak_client_id is required to call Keycloak")
	}
	if c.KeycloakClientSecret == "" {
		return fmt.Errorf("keycloak_client_secret is required to call Keycloak")
	}
	return nil
}

// GetListenAddress returns the configured HTTP listen address (host:port).
func (c *StaticConfig) GetListenAddress() string {
	if c.Port == 0 {
		return ""
	}
	return net.JoinHostPort(c.Listen, strconv.Itoa(c.Port))
}

// LoadConfig loads configuration from defaults, optional YAML, environment, and flags.
func LoadConfig(configPath string, v *viper.Viper) (*StaticConfig, error) {
	if v == nil {
		v = viper.New()
	}

	defaults := map[string]any{
		"port":                    0,
		"listen":                  "127.0.0.1",
		"sse_base_url":            "",
		"log_level":               "info",
		"keycloak_url":            "",
		"keycloak_realm":          "",
		"keycloak_client_id":      "",
		"keycloak_client_secret":  "",
		"read_only":               true,
		"request_timeout_seconds": 30,
		"enabled_tools":           []string{},
		"disabled_tools":          []string{},
		"enabled_domains":         []string{},
		"disabled_domains":        []string{},
	}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	if configPath != "" {
		v.SetConfigFile(configPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	v.SetEnvPrefix("MCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	if err := bindEnvironment(v); err != nil {
		return nil, err
	}

	cfg := &StaticConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.KeycloakURL = strings.TrimRight(strings.TrimSpace(cfg.KeycloakURL), "/")
	cfg.KeycloakRealm = strings.TrimSpace(cfg.KeycloakRealm)
	cfg.KeycloakClientID = strings.TrimSpace(cfg.KeycloakClientID)
	cfg.EnabledTools = normalizeStringSlice(cfg.EnabledTools)
	cfg.DisabledTools = normalizeStringSlice(cfg.DisabledTools)
	cfg.EnabledDomains = normalizeStringSlice(cfg.EnabledDomains)
	cfg.DisabledDomains = normalizeStringSlice(cfg.DisabledDomains)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func bindEnvironment(v *viper.Viper) error {
	for _, key := range []string{
		"keycloak_url",
		"keycloak_realm",
		"keycloak_client_id",
		"keycloak_client_secret",
	} {
		if err := v.BindEnv(key); err != nil {
			return fmt.Errorf("bind environment %s: %w", key, err)
		}
	}
	return nil
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 && len(values) > 0 {
		return []string{""}
	}
	return normalized
}
