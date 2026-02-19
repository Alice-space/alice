package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"gitee.com/alicespace/alice/internal/codex"
	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/connector"
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

	processor := connector.NewProcessor(codexRunner, connector.NewLarkSender(botClient), cfg.FailureMessage)
	app := connector.NewApp(cfg, processor)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("feishu-codex connector started (long connection mode)")
	if err := app.Run(ctx); err != nil {
		log.Fatalf("connector stopped with error: %v", err)
	}

	log.Printf("connector stopped")
}
