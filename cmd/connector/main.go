package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
)

func main() {
	configPath := config.DefaultConfigPath
	flag.StringVar(&configPath, "config", config.DefaultConfigPath, "path to config yaml")
	flag.StringVar(&configPath, "c", config.DefaultConfigPath, "path to config yaml (short)")
	flag.Parse()

	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	logging.SetLevel(cfg.LogLevel)
	logging.Debugf("debug logging enabled log_level=%s config=%s", cfg.LogLevel, configPath)

	llmProvider, err := bootstrap.NewLLMProvider(cfg)
	if err != nil {
		log.Fatalf("init llm provider failed: %v", err)
	}

	skillReport, err := bootstrap.EnsureBundledSkillsLinked(cfg.WorkspaceDir)
	if err != nil {
		log.Printf("sync bundled skills failed: %v", err)
	} else if skillReport.Discovered > 0 {
		log.Printf(
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
				log.Fatalf("register llm mcp server failed: %v", err)
			}
			log.Printf("register llm mcp server failed but ignored: %v", err)
		} else {
			log.Printf("llm mcp server ready name=%s", cfg.CodexMCPServerName)
		}
	}

	runtime, err := bootstrap.BuildConnectorRuntime(cfg, llmProvider)
	if err != nil {
		log.Fatalf("init connector runtime failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("feishu-codex connector started (long connection mode)")
	log.Printf("memory module enabled dir=%s", runtime.MemoryDir)
	log.Printf("automation engine enabled state_file=%s", runtime.AutomationStatePath)
	if runtime.RuntimeAPI != nil {
		log.Printf("runtime http api enabled addr=%s", runtime.RuntimeAPIBaseURL)
	}
	if err := runtime.Run(ctx); err != nil {
		log.Fatalf("connector stopped with error: %v", err)
	}

	log.Printf("connector stopped")
}
