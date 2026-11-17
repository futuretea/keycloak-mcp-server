package config_test

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/futuretea/keycloak-mcp-server/pkg/core/config"
	"github.com/futuretea/keycloak-mcp-server/pkg/toolset"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 0 {
		t.Fatalf("expected port 0, got %d", cfg.Port)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("expected listen 127.0.0.1, got %q", cfg.Listen)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected log_level info, got %q", cfg.LogLevel)
	}
	if !cfg.ReadOnly {
		t.Fatal("read_only must default to true")
	}
	if cfg.RequestTimeoutSeconds != 30 {
		t.Fatalf("expected request_timeout_seconds 30, got %d", cfg.RequestTimeoutSeconds)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.StaticConfig
		wantSub string
	}{
		{
			name:    "bad port",
			cfg:     config.StaticConfig{Port: 70000, LogLevel: "info", Listen: "127.0.0.1"},
			wantSub: "port must be between",
		},
		{
			name:    "empty listen",
			cfg:     config.StaticConfig{Port: 8080, LogLevel: "info", Listen: " "},
			wantSub: "listen must be set",
		},
		{
			name:    "bad log level",
			cfg:     config.StaticConfig{Port: 0, LogLevel: "nope", Listen: "127.0.0.1"},
			wantSub: "invalid log_level",
		},
		{
			name:    "bad timeout",
			cfg:     config.StaticConfig{Port: 0, LogLevel: "info", Listen: "127.0.0.1"},
			wantSub: "request_timeout_seconds must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestValidateKeycloak(t *testing.T) {
	clientID := testOpaqueValue(t)
	clientSecret := testOpaqueValue(t)
	valid := config.StaticConfig{
		KeycloakURL:          "https://keycloak.example.test/auth",
		KeycloakRealm:        "master",
		KeycloakClientID:     clientID,
		KeycloakClientSecret: clientSecret,
	}
	if err := valid.ValidateKeycloak(); err != nil {
		t.Fatalf("ValidateKeycloak: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*config.StaticConfig)
		wantSub string
	}{
		{"missing URL", func(c *config.StaticConfig) { c.KeycloakURL = "" }, "keycloak_url is required"},
		{"bad URL", func(c *config.StaticConfig) { c.KeycloakURL = "file:///tmp/keycloak" }, "must be an absolute URL"},
		{"URL credentials", func(c *config.StaticConfig) { c.KeycloakURL = "https://user:pass@example.test" }, "must not contain credentials"},
		{"missing realm", func(c *config.StaticConfig) { c.KeycloakRealm = "" }, "keycloak_realm is required"},
		{"missing client ID", func(c *config.StaticConfig) { c.KeycloakClientID = "" }, "keycloak_client_id is required"},
		{"missing secret", func(c *config.StaticConfig) { c.KeycloakClientSecret = "" }, "keycloak_client_secret is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			err := cfg.ValidateKeycloak()
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("ValidateKeycloak error = %v, want %q", err, tt.wantSub)
			}
		})
	}
}

func TestLoadConfigReadsKeycloakEnvironment(t *testing.T) {
	clientID := testOpaqueValue(t)
	clientSecret := testOpaqueValue(t)
	t.Setenv("MCP_KEYCLOAK_URL", "https://keycloak.example.test")
	t.Setenv("MCP_KEYCLOAK_REALM", "master")
	t.Setenv("MCP_KEYCLOAK_CLIENT_ID", clientID)
	t.Setenv("MCP_KEYCLOAK_CLIENT_SECRET", clientSecret)

	cfg, err := config.LoadConfig("", viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.KeycloakURL != "https://keycloak.example.test" || cfg.KeycloakRealm != "master" || cfg.KeycloakClientID != clientID || cfg.KeycloakClientSecret != clientSecret {
		t.Fatal("Keycloak environment fields were not loaded")
	}
}

func TestLoadConfigPrioritizesFlagOverEnvironmentAndFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("keycloak_url: https://file.example.test\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_KEYCLOAK_URL", "https://environment.example.test")

	flags := pflag.NewFlagSet("config", pflag.ContinueOnError)
	flags.String("keycloak-url", "", "")
	if err := flags.Set("keycloak-url", "https://flag.example.test"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	v := viper.New()
	if err := v.BindPFlag("keycloak_url", flags.Lookup("keycloak-url")); err != nil {
		t.Fatalf("bind flag: %v", err)
	}

	cfg, err := config.LoadConfig(configPath, v)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.KeycloakURL != "https://flag.example.test" {
		t.Fatalf("keycloak_url = %q, want flag value", cfg.KeycloakURL)
	}
}

func TestLoadConfigPrioritizesEnvironmentOverFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("keycloak_url: https://file.example.test\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_KEYCLOAK_URL", "https://environment.example.test")

	cfg, err := config.LoadConfig(configPath, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.KeycloakURL != "https://environment.example.test" {
		t.Fatalf("keycloak_url = %q, want environment value", cfg.KeycloakURL)
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

func TestLoadConfigBlankAllowListDoesNotWiden(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	body := "enabled_tools:\n  - \"\"\n  - \"   \"\nenabled_domains:\n  - \" \"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadConfig(configPath, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := filteredNames(cfg); len(got) != 0 {
		t.Fatalf("blank allow list exposed %v", got)
	}
}

func TestLoadConfigEmptyAllowListStaysOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	body := "enabled_tools: []\nenabled_domains: []\ndisabled_tools: []\ndisabled_domains: []\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadConfig(configPath, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got, want := filteredNames(cfg), []string{"read", "other"}; !sameNames(got, want) {
		t.Fatalf("empty allow list = %v, want %v", got, want)
	}
}

func TestLoadConfigWhitespaceEnvAllowListDoesNotWiden(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - read\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_ENABLED_TOOLS", "   ")
	cfg, err := config.LoadConfig(configPath, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := filteredNames(cfg); len(got) != 0 {
		t.Fatalf("whitespace environment allow list exposed %v", got)
	}
}

func TestLoadConfigEmptyEnvKeepsFileAllowList(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - read\nread_only: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_ENABLED_TOOLS", "")
	t.Setenv("MCP_READ_ONLY", "")
	cfg, err := config.LoadConfig(configPath, viper.New())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ReadOnly {
		t.Fatal("empty MCP_READ_ONLY overrode file read_only false")
	}
	if got, want := filteredNames(cfg), []string{"read"}; !sameNames(got, want) {
		t.Fatalf("empty environment allow list = %v, want %v", got, want)
	}
}

func TestLoadConfigUnchangedFlagsKeepFileFilters(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - read\nread_only: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := loadConfigWithFlags(t, configPath, nil)
	if cfg.ReadOnly {
		t.Fatal("unchanged read-only flag overrode file read_only false")
	}
	if got, want := filteredNames(cfg), []string{"read"}; !sameNames(got, want) {
		t.Fatalf("unchanged enabled-tools flag = %v, want %v", got, want)
	}
}

func TestLoadConfigChangedFlagsBeatEnvironment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - other\nread_only: false\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_ENABLED_TOOLS", "read")
	t.Setenv("MCP_READ_ONLY", "false")
	cfg := loadConfigWithFlags(t, configPath, map[string]string{
		"enabled-tools": "other",
		"read-only":     "true",
	})
	if !cfg.ReadOnly {
		t.Fatal("changed read-only flag did not override the environment")
	}
	if got, want := filteredNames(cfg), []string{"other"}; !sameNames(got, want) {
		t.Fatalf("changed enabled-tools flag = %v, want %v", got, want)
	}
}

func TestLoadConfigEmptyChangedFlagStaysOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - read\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := loadConfigWithFlags(t, configPath, map[string]string{"enabled-tools": ""})
	if got, want := filteredNames(cfg), []string{"read", "other"}; !sameNames(got, want) {
		t.Fatalf("empty changed flag = %v, want %v", got, want)
	}
}

func TestLoadConfigBlankChangedFlagDoesNotWiden(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("enabled_tools:\n  - read\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := loadConfigWithFlags(t, configPath, map[string]string{"enabled-tools": " "})
	if got := filteredNames(cfg); len(got) != 0 {
		t.Fatalf("blank changed flag exposed %v", got)
	}
}

func filteredNames(cfg *config.StaticConfig) []string {
	filtered := toolset.FilterTools([]toolset.ServerTool{
		{Tool: mcp.NewTool("read"), Domain: "users"},
		{Tool: mcp.NewTool("other"), Domain: "clients"},
	}, toolset.FilterOptions{
		ReadOnly:        cfg.ReadOnly,
		EnabledTools:    cfg.EnabledTools,
		DisabledTools:   cfg.DisabledTools,
		EnabledDomains:  cfg.EnabledDomains,
		DisabledDomains: cfg.DisabledDomains,
	})
	names := make([]string, 0, len(filtered))
	for _, tool := range filtered {
		names = append(names, tool.Tool.Name)
	}
	return names
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func loadConfigWithFlags(t *testing.T, configPath string, values map[string]string) *config.StaticConfig {
	t.Helper()
	flags := pflag.NewFlagSet("config", pflag.ContinueOnError)
	flags.StringSlice("enabled-tools", []string{}, "")
	flags.StringSlice("disabled-tools", []string{}, "")
	flags.StringSlice("enable-domains", []string{}, "")
	flags.StringSlice("disable-domains", []string{}, "")
	flags.Bool("read-only", true, "")
	for name, value := range values {
		if err := flags.Set(name, value); err != nil {
			t.Fatalf("set flag %s: %v", name, err)
		}
	}
	v := viper.New()
	bindings := map[string]string{
		"enabled_tools":    "enabled-tools",
		"disabled_tools":   "disabled-tools",
		"enabled_domains":  "enable-domains",
		"disabled_domains": "disable-domains",
		"read_only":        "read-only",
	}
	for key, name := range bindings {
		if err := v.BindPFlag(key, flags.Lookup(name)); err != nil {
			t.Fatalf("bind flag %s: %v", name, err)
		}
	}
	cfg, err := config.LoadConfig(configPath, v)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

func TestGetListenAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StaticConfig
		want string
	}{
		{name: "stdio", cfg: config.StaticConfig{Port: 0, Listen: "127.0.0.1"}, want: ""},
		{name: "IPv4", cfg: config.StaticConfig{Port: 8080, Listen: "127.0.0.1"}, want: "127.0.0.1:8080"},
		{name: "hostname", cfg: config.StaticConfig{Port: 8080, Listen: "localhost"}, want: "localhost:8080"},
		{name: "IPv6", cfg: config.StaticConfig{Port: 8080, Listen: "::1"}, want: "[::1]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetListenAddress(); got != tt.want {
				t.Fatalf("GetListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
