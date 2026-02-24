package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitee.com/alicespace/alice/internal/bootstrap"
	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/logging"
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

	if cfg.CodexMCPAutoRegister {
		mcpRegisterCtx, cancelRegister := context.WithTimeout(context.Background(), 20*time.Second)
		err = bootstrap.RegisterCodexMCPServer(mcpRegisterCtx, cfg, configPath)
		cancelRegister()
		if err != nil {
			if cfg.CodexMCPRegisterStrict {
				log.Fatalf("register codex mcp server failed: %v", err)
			}
			log.Printf("register codex mcp server failed but ignored: %v", err)
		} else {
			log.Printf("codex mcp server ready name=%s", cfg.CodexMCPServerName)
		}
	}

	runtime, err := bootstrap.BuildConnectorRuntime(cfg)
	if err != nil {
		log.Fatalf("init connector runtime failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("feishu-codex connector started (long connection mode)")
	log.Printf("memory module enabled dir=%s", runtime.MemoryDir)
	log.Printf("automation engine enabled state_file=%s", runtime.AutomationStatePath)
	if err := runtime.App.Run(ctx); err != nil {
		log.Fatalf("connector stopped with error: %v", err)
	}

	log.Printf("connector stopped")
}
