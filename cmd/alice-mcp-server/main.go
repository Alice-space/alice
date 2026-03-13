package main

import (
	"flag"
	"log"
	"path/filepath"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/mcpserver"
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
	if err := logging.Configure(logging.Options{
		Level:      cfg.LogLevel,
		FilePath:   cfg.LogFile,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAgeDays: cfg.LogMaxAgeDays,
		Compress:   cfg.LogCompress,
	}); err != nil {
		log.Fatalf("configure logging failed: %v", err)
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
		logging.Fatalf("init mcp server failed: %v", err)
	}

	if err := server.ServeStdio(mcpSrv); err != nil {
		logging.Fatalf("mcp server stopped with error: %v", err)
	}
}
