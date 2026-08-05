package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/futuretea/keycloak-mcp-server/internal/keycloakclient"
	"github.com/futuretea/keycloak-mcp-server/pkg/core/config"
	"github.com/futuretea/keycloak-mcp-server/pkg/core/logging"
	internalhttp "github.com/futuretea/keycloak-mcp-server/pkg/server/http"
	mcpserver "github.com/futuretea/keycloak-mcp-server/pkg/server/mcp"
)

func newMCPCommand(streams IOStreams, cfgFile *string, v *viper.Viper) *cobra.Command {
	command := &cobra.Command{
		Use:   "mcp",
		Short: "start the MCP server",
		Example: `  # stdio mode (default)
  keycloak mcp

  # HTTP mode on loopback
  keycloak mcp --port 8080

  # HTTP mode on a custom listen address
  keycloak mcp --port 8080 --listen 127.0.0.1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bindCommonFlags(v, cmd); err != nil {
				return err
			}
			if err := bindMCPFlags(v, cmd); err != nil {
				return err
			}
			return runServer(cmd.Context(), *cfgFile, streams, v)
		},
	}

	command.Flags().Int("port", 0, "port for HTTP mode; 0 runs stdio mode")
	command.Flags().String("listen", "127.0.0.1", "HTTP listen host; defaults to loopback (no auth on HTTP transports)")
	command.Flags().String("sse-base-url", "", "public base URL for SSE message endpoints")
	command.Flags().String("log-level", "info", "log level: trace, debug, info, warn, error, fatal, panic, disabled")
	return command
}

func bindMCPFlags(v *viper.Viper, cmd *cobra.Command) error {
	bindings := map[string]string{
		"port":         "port",
		"listen":       "listen",
		"sse_base_url": "sse-base-url",
		"log_level":    "log-level",
	}
	for key, flag := range bindings {
		if err := v.BindPFlag(key, cmd.Flags().Lookup(flag)); err != nil {
			return fmt.Errorf("bind flag %s: %w", flag, err)
		}
	}
	return nil
}

func runServer(ctx context.Context, cfgFile string, streams IOStreams, v *viper.Viper) error {
	cfg, err := config.LoadConfig(cfgFile, v)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Port == 0 {
		logging.Disable()
	} else if err := logging.Initialize(cfg.LogLevel, streams.ErrOut); err != nil {
		return fmt.Errorf("initialize logging: %w", err)
	}
	if err := cfg.ValidateKeycloak(); err != nil {
		return fmt.Errorf("validate Keycloak configuration: %w", err)
	}
	client, err := keycloakclient.New(cfg, nil)
	if err != nil {
		return fmt.Errorf("create Keycloak client: %w", err)
	}
	toolsets, err := defaultToolsets(client)
	if err != nil {
		return err
	}

	server, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: cfg,
		Toolsets:     toolsets,
	})
	if err != nil {
		return fmt.Errorf("create MCP server: %w", err)
	}
	defer server.Close()

	if cfg.Port != 0 {
		return internalhttp.Serve(ctx, server, cfg)
	}
	return server.ServeStdio()
}
