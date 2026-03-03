package main

import (
	"flag"
	"log"
	"path/filepath"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/mark3labs/mcp-go/server"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/bootstrap"
	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/connector"
	"gitee.com/alicespace/alice/internal/mcpserver"
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

	memoryDir := bootstrap.ResolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	resourceDir := filepath.Join(memoryDir, "resources")
	automationStatePath := filepath.Join(memoryDir, "automation_state.json")
	codeArmyStateDir := filepath.Join(memoryDir, "code_army")
	sender := connector.NewLarkSender(botClient, resourceDir)
	mcpSrv, err := mcpserver.New(sender, nil, automation.NewStore(automationStatePath), codeArmyStateDir)
	if err != nil {
		log.Fatalf("init mcp server failed: %v", err)
	}

	if err := server.ServeStdio(mcpSrv); err != nil {
		log.Fatalf("mcp server stopped with error: %v", err)
	}
}
