package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"gitee.com/alicespace/alice/internal/codex"
	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/connector"
	"gitee.com/alicespace/alice/internal/logging"
	"gitee.com/alicespace/alice/internal/memory"
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

	botClient := lark.NewClient(
		cfg.FeishuAppID,
		cfg.FeishuAppSecret,
		lark.WithOpenBaseUrl(cfg.FeishuBaseURL),
	)

	codexRunner := codex.Runner{
		Command:      cfg.CodexCommand,
		Timeout:      cfg.CodexTimeout,
		PromptPrefix: cfg.CodexPromptPrefix,
		WorkspaceDir: cfg.WorkspaceDir,
	}

	memoryDir := resolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	memoryManager := memory.NewManager(memoryDir)
	if err := memoryManager.Init(); err != nil {
		log.Fatalf("init memory module failed: %v", err)
	}

	processor := connector.NewProcessorWithMemory(
		codexRunner,
		connector.NewLarkSender(botClient),
		cfg.FailureMessage,
		cfg.ThinkingMessage,
		memoryManager,
	)
	app := connector.NewApp(cfg, processor)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("feishu-codex connector started (long connection mode)")
	log.Printf("memory module enabled dir=%s", memoryDir)
	if err := app.Run(ctx); err != nil {
		log.Fatalf("connector stopped with error: %v", err)
	}

	log.Printf("connector stopped")
}

func resolveMemoryDir(workspaceDir, memoryDir string) string {
	dir := strings.TrimSpace(memoryDir)
	if dir == "" {
		dir = ".memory"
	}
	if filepath.IsAbs(dir) {
		return dir
	}

	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, dir)
}
