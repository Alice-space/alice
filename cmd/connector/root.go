package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

func newRootCmd() *cobra.Command {
	configPath := config.DefaultConfigPath
	root := &cobra.Command{
		Use:           "alice-connector",
		Short:         "Run the Alice Feishu connector",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnector(configPath)
		},
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", config.DefaultConfigPath, "path to config yaml")
	root.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Run the connector process",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnector(configPath)
		},
	})
	return root
}

func runConnector(configPath string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return err
	}
	if err := logging.Configure(logging.Options{
		Level:      cfg.LogLevel,
		FilePath:   cfg.LogFile,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAgeDays: cfg.LogMaxAgeDays,
		Compress:   cfg.LogCompress,
	}); err != nil {
		return err
	}
	logging.Debugf("debug logging enabled log_level=%s config=%s", cfg.LogLevel, configPath)

	llmProvider, err := bootstrap.NewLLMProvider(cfg)
	if err != nil {
		return err
	}

	skillReport, err := bootstrap.EnsureBundledSkillsLinked(cfg.WorkspaceDir)
	if err != nil {
		logging.Warnf("sync bundled skills failed: %v", err)
	} else if skillReport.Discovered > 0 {
		logging.Infof(
			"bundled skills synced codex_home=%s discovered=%d linked=%d updated=%d backed_up=%d unchanged=%d failed=%d",
			skillReport.CodexHome,
			skillReport.Discovered,
			skillReport.Linked,
			skillReport.Updated,
			skillReport.BackedUp,
			skillReport.Unchanged,
			skillReport.Failed,
		)
	}

	if cfg.CodexMCPAutoRegister {
		mcpRegisterCtx, cancelRegister := context.WithTimeout(context.Background(), 20*time.Second)
		err = bootstrap.RegisterMCPServer(mcpRegisterCtx, llmProvider, cfg, configPath)
		cancelRegister()
		if err != nil {
			if cfg.CodexMCPRegisterStrict {
				return err
			}
			logging.Warnf("register llm mcp server failed but ignored: %v", err)
		} else {
			logging.Infof("llm mcp server ready name=%s", cfg.CodexMCPServerName)
		}
	}

	runtime, err := bootstrap.BuildConnectorRuntime(cfg, llmProvider)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logging.Infof("feishu-codex connector started (long connection mode)")
	logging.Infof("memory module enabled dir=%s", runtime.MemoryDir)
	logging.Infof("automation engine enabled state_file=%s", runtime.AutomationStatePath)
	if runtime.RuntimeAPI != nil {
		logging.Infof("runtime http api enabled addr=%s", runtime.RuntimeAPIBaseURL)
	}
	if err := runtime.Run(ctx); err != nil {
		return err
	}

	logging.Infof("connector stopped")
	return nil
}
