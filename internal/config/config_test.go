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
	if cfg.TriggerMode != TriggerModeAt {
		t.Fatalf("unexpected trigger_mode: %s", cfg.TriggerMode)
	}
	if cfg.TriggerPrefix != "" {
		t.Fatalf("unexpected trigger_prefix: %q", cfg.TriggerPrefix)
	}
	if cfg.ImmediateFeedbackMode != ImmediateFeedbackModeReply {
		t.Fatalf("unexpected immediate_feedback_mode: %q", cfg.ImmediateFeedbackMode)
	}
	if cfg.ImmediateFeedbackReaction != DefaultImmediateFeedbackReaction {
		t.Fatalf("unexpected immediate_feedback_reaction: %q", cfg.ImmediateFeedbackReaction)
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
	if cfg.ClaudeCommand != "claude" {
		t.Fatalf("unexpected claude_command: %s", cfg.ClaudeCommand)
	}
	if cfg.ClaudeTimeout != 120*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", cfg.ClaudeTimeout)
	}
	if cfg.ClaudePromptPrefix != "" {
		t.Fatalf("unexpected claude_prompt_prefix: %q", cfg.ClaudePromptPrefix)
	}
	if cfg.QueueCapacity != 256 {
		t.Fatalf("unexpected queue_capacity: %d", cfg.QueueCapacity)
	}
	if cfg.AutomationTaskTimeoutSecs != 600 {
		t.Fatalf("unexpected automation_task_timeout_secs: %d", cfg.AutomationTaskTimeoutSecs)
	}
	if cfg.AutomationTaskTimeout != 10*time.Minute {
		t.Fatalf("unexpected automation_task_timeout: %s", cfg.AutomationTaskTimeout)
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
	if cfg.LogLevel != "info" {
		t.Fatalf("unexpected log_level: %q", cfg.LogLevel)
	}
	if cfg.LogFile != "" {
		t.Fatalf("unexpected log_file: %q", cfg.LogFile)
	}
	if cfg.LogMaxSizeMB != 20 {
		t.Fatalf("unexpected log_max_size_mb: %d", cfg.LogMaxSizeMB)
	}
	if cfg.LogMaxBackups != 5 {
		t.Fatalf("unexpected log_max_backups: %d", cfg.LogMaxBackups)
	}
	if cfg.LogMaxAgeDays != 7 {
		t.Fatalf("unexpected log_max_age_days: %d", cfg.LogMaxAgeDays)
	}
	if cfg.LogCompress {
		t.Fatal("log_compress should default to false")
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

func TestLoadFromFile_LogConfigTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
log_level: "  DEBUG  "
log_file: "  logs/alice.log  "
log_max_size_mb: 0
log_max_backups: -1
log_max_age_days: 0
log_compress: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("unexpected log_level: %q", cfg.LogLevel)
	}
	if cfg.LogFile != "logs/alice.log" {
		t.Fatalf("unexpected log_file: %q", cfg.LogFile)
	}
	if cfg.LogMaxSizeMB != 20 {
		t.Fatalf("unexpected log_max_size_mb fallback: %d", cfg.LogMaxSizeMB)
	}
	if cfg.LogMaxBackups != 5 {
		t.Fatalf("unexpected log_max_backups fallback: %d", cfg.LogMaxBackups)
	}
	if cfg.LogMaxAgeDays != 7 {
		t.Fatalf("unexpected log_max_age_days fallback: %d", cfg.LogMaxAgeDays)
	}
	if !cfg.LogCompress {
		t.Fatal("expected log_compress to be true")
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

func TestLoadFromFile_AutomationTaskTimeoutSecsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
automation_task_timeout_secs: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "automation_task_timeout_secs must be > 0") {
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

func TestLoadFromFile_TriggerModeTrimmedLowercase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "  PrEfIx  "
trigger_prefix: "  !alice  "
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.TriggerMode != TriggerModePrefix {
		t.Fatalf("unexpected trigger_mode: %q", cfg.TriggerMode)
	}
	if cfg.TriggerPrefix != "!alice" {
		t.Fatalf("unexpected trigger_prefix: %q", cfg.TriggerPrefix)
	}
}

func TestLoadFromFile_TriggerModeInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "all"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported trigger_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_ImmediateFeedbackModeTrimmedLowercase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
immediate_feedback_mode: "  ReAcTiOn  "
immediate_feedback_reaction: "  smile  "
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.ImmediateFeedbackMode != ImmediateFeedbackModeReaction {
		t.Fatalf("unexpected immediate_feedback_mode: %q", cfg.ImmediateFeedbackMode)
	}
	if cfg.ImmediateFeedbackReaction != "SMILE" {
		t.Fatalf("unexpected immediate_feedback_reaction: %q", cfg.ImmediateFeedbackReaction)
	}
}

func TestLoadFromFile_ImmediateFeedbackModeInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
immediate_feedback_mode: "wave"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported immediate_feedback_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_TriggerModePrefixRequiresPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "prefix"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "trigger_prefix is required") {
		t.Fatalf("unexpected error: %v", err)
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

func TestLoadFromFile_LLMProviderClaude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: "  ClAuDe  "
claude_command: "  claude-custom  "
claude_timeout_secs: 233
claude_prompt_prefix: "  你是Claude助手  "
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.LLMProvider != LLMProviderClaude {
		t.Fatalf("unexpected llm_provider: %q", cfg.LLMProvider)
	}
	if cfg.ClaudeCommand != "claude-custom" {
		t.Fatalf("unexpected claude_command: %q", cfg.ClaudeCommand)
	}
	if cfg.ClaudeTimeout != 233*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", cfg.ClaudeTimeout)
	}
	if cfg.ClaudePromptPrefix != "你是Claude助手" {
		t.Fatalf("unexpected claude_prompt_prefix: %q", cfg.ClaudePromptPrefix)
	}
}

func TestLoadFromFile_ClaudeTimeoutInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: claude
claude_timeout_secs: 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "claude_timeout_secs must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_CodexTimeoutIgnoredWhenClaudeProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: claude
codex_timeout_secs: 0
claude_timeout_secs: 60
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.CodexTimeout != 120*time.Second {
		t.Fatalf("unexpected codex_timeout fallback: %s", cfg.CodexTimeout)
	}
	if cfg.ClaudeTimeout != 60*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", cfg.ClaudeTimeout)
	}
}
