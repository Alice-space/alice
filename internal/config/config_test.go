package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFile_WithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "feishu_app_id: cli_xxx\nfeishu_app_secret: sss\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.FeishuBaseURL != "https://open.feishu.cn" {
		t.Fatalf("unexpected feishu_base_url: %s", cfg.FeishuBaseURL)
	}
	if cfg.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected llm_provider: %s", cfg.LLMProvider)
	}
	if cfg.CodexCommand != "codex" {
		t.Fatalf("unexpected codex_command: %s", cfg.CodexCommand)
	}
	if cfg.CodexTimeout != 120*time.Second {
		t.Fatalf("unexpected codex_timeout: %s", cfg.CodexTimeout)
	}
	if cfg.QueueCapacity != 256 {
		t.Fatalf("unexpected queue_capacity: %d", cfg.QueueCapacity)
	}
	if cfg.ThinkingMessage != "正在思考中..." {
		t.Fatalf("unexpected thinking_message: %s", cfg.ThinkingMessage)
	}
	if cfg.IdleSummaryHours != 8 {
		t.Fatalf("unexpected idle_summary_hours: %d", cfg.IdleSummaryHours)
	}
	if cfg.IdleSummaryIdle != 8*time.Hour {
		t.Fatalf("unexpected idle_summary_idle: %s", cfg.IdleSummaryIdle)
	}
	if cfg.GroupContextWindowMinutes != 5 {
		t.Fatalf("unexpected group_context_window_minutes: %d", cfg.GroupContextWindowMinutes)
	}
	if cfg.GroupContextWindowTTL != 5*time.Minute {
		t.Fatalf("unexpected group_context_window_ttl: %s", cfg.GroupContextWindowTTL)
	}
	if cfg.MemoryDir != ".memory" {
		t.Fatalf("unexpected memory_dir: %s", cfg.MemoryDir)
	}
	if cfg.CodexPromptPrefix != "" {
		t.Fatalf("unexpected codex_prompt_prefix: %q", cfg.CodexPromptPrefix)
	}
	if !cfg.CodexMCPAutoRegister {
		t.Fatal("codex_mcp_auto_register should default to true")
	}
	if cfg.CodexMCPRegisterStrict {
		t.Fatal("codex_mcp_register_strict should default to false")
	}
	if cfg.CodexMCPServerName != "alice-feishu" {
		t.Fatalf("unexpected codex_mcp_server_name: %q", cfg.CodexMCPServerName)
	}
	if len(cfg.CodexEnv) != 0 {
		t.Fatalf("unexpected codex_env: %#v", cfg.CodexEnv)
	}
}

func TestLoadFromFile_RequiredKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "feishu_app_id: cli_xxx\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "feishu_app_secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_Env(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
env:
  HTTPS_PROXY: "  http://127.0.0.1:7890  "
  ALL_PROXY: "socks5://127.0.0.1:7891"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.CodexEnv["HTTPS_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected HTTPS_PROXY: %q", cfg.CodexEnv["HTTPS_PROXY"])
	}
	if cfg.CodexEnv["ALL_PROXY"] != "socks5://127.0.0.1:7891" {
		t.Fatalf("unexpected ALL_PROXY: %q", cfg.CodexEnv["ALL_PROXY"])
	}
}

func TestLoadFromFile_EnvInvalidKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
env:
  "BAD=KEY": "v"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must not contain '='") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_IdleSummaryHoursInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
idle_summary_hours: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "idle_summary_hours must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_GroupContextWindowMinutesInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
group_context_window_minutes: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "group_context_window_minutes must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_FeishuBotIDsTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
feishu_bot_open_id: "  ou_bot  "
feishu_bot_user_id: "  123456  "
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.FeishuBotOpenID != "ou_bot" {
		t.Fatalf("unexpected feishu_bot_open_id: %q", cfg.FeishuBotOpenID)
	}
	if cfg.FeishuBotUserID != "123456" {
		t.Fatalf("unexpected feishu_bot_user_id: %q", cfg.FeishuBotUserID)
	}
}

func TestLoadFromFile_LLMProviderInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: openai
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LLMProviderTrimmedLowercase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: "  CoDeX  "
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected llm_provider: %q", cfg.LLMProvider)
	}
}
