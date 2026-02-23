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

	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/connector"
	"gitee.com/alicespace/alice/internal/llm"
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

	backend, err := llm.NewBackend(llm.FactoryConfig{
		Provider: cfg.LLMProvider,
		Codex: llm.CodexConfig{
			Command:      cfg.CodexCommand,
			Timeout:      cfg.CodexTimeout,
			Env:          cfg.CodexEnv,
			PromptPrefix: cfg.CodexPromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
	})
	if err != nil {
		log.Fatalf("init llm backend failed: %v", err)
	}

	memoryDir := resolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	memoryManager := memory.NewManager(memoryDir)
	memoryManager.SetProjectDir(cfg.WorkspaceDir)
	if err := memoryManager.Init(); err != nil {
		log.Fatalf("init memory module failed: %v", err)
	}
	resourceDir := filepath.Join(memoryDir, "resources")

	processor := connector.NewProcessorWithMemory(
		backend,
		connector.NewLarkSender(botClient, resourceDir),
		cfg.FailureMessage,
		cfg.ThinkingMessage,
		memoryManager,
	)
	sessionStatePath := filepath.Join(memoryDir, "session_state.json")
	if err := processor.LoadSessionState(sessionStatePath); err != nil {
		log.Printf("load session state failed file=%s err=%v", sessionStatePath, err)
	} else {
		log.Printf("session state enabled file=%s", sessionStatePath)
	}
	app := connector.NewApp(cfg, processor)
	runtimeStatePath := filepath.Join(memoryDir, "runtime_state.json")
	if err := app.LoadRuntimeState(runtimeStatePath); err != nil {
		log.Printf("load runtime state failed file=%s err=%v", runtimeStatePath, err)
	} else {
		log.Printf("runtime state enabled file=%s", runtimeStatePath)
	}

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
