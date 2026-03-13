package main

import (
	"path/filepath"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/mcpserver"
)

func newRootCmd() *cobra.Command {
	configPath := config.DefaultConfigPath
	root := &cobra.Command{
		Use:           "alice-mcp-server",
		Short:         "Run the Alice MCP compatibility server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(configPath)
		},
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", config.DefaultConfigPath, "path to config yaml")
	root.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Serve MCP over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(configPath)
		},
	})
	return root
}

func runMCPServer(configPath string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}

	botClient := lark.NewClient(
		cfg.FeishuAppID,
		cfg.FeishuAppSecret,
		lark.WithOpenBaseUrl(cfg.FeishuBaseURL),
	)

	memoryDir := bootstrap.ResolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	resourceDir := filepath.Join(memoryDir, "resources")
	automationStatePath := filepath.Join(memoryDir, "automation_state.json")
	codeArmyStateDir := filepath.Join(memoryDir, "code_army")
	sender := connector.NewLarkSender(botClient, resourceDir)
	mcpSrv, err := mcpserver.New(sender, nil, automation.NewStore(automationStatePath), codeArmyStateDir)
	if err != nil {
		return err
	}

	if err := server.ServeStdio(mcpSrv); err != nil {
		return err
	}
	return nil
}
