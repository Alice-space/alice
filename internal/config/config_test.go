package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFile_BotsAreRequired(t *testing.T) {
	path := writeConfigFile(t, "log_level: info\n")

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bots is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LegacyRootBotKeysRejected(t *testing.T) {
	path := writeConfigFile(t, "feishu_app_id: cli_old\nfeishu_app_secret: secret_old\n")

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "root bot keys are no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_WithDefaults(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvAliceHome, base)

	cfg, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
`)

	if cfg.FeishuBaseURL != "https://open.feishu.cn" {
		t.Fatalf("unexpected feishu_base_url: %s", cfg.FeishuBaseURL)
	}
	if runtime.FeishuBaseURL != "https://open.feishu.cn" {
		t.Fatalf("unexpected runtime feishu_base_url: %s", runtime.FeishuBaseURL)
	}
	if runtime.TriggerMode != TriggerModeAt {
		t.Fatalf("unexpected trigger_mode: %s", runtime.TriggerMode)
	}
	if runtime.TriggerPrefix != "" {
		t.Fatalf("unexpected trigger_prefix: %q", runtime.TriggerPrefix)
	}
	if runtime.ImmediateFeedbackMode != DefaultImmediateFeedbackMode {
		t.Fatalf("unexpected immediate_feedback_mode: %q", runtime.ImmediateFeedbackMode)
	}
	if runtime.ImmediateFeedbackReaction != DefaultImmediateFeedbackReaction {
		t.Fatalf("unexpected immediate_feedback_reaction: %q", runtime.ImmediateFeedbackReaction)
	}
	if runtime.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected llm_provider: %s", runtime.LLMProvider)
	}
	if runtime.CodexCommand != "codex" {
		t.Fatalf("unexpected codex_command: %s", runtime.CodexCommand)
	}
	if runtime.CodexTimeout != 172800*time.Second {
		t.Fatalf("unexpected codex_timeout: %s", runtime.CodexTimeout)
	}
	if runtime.ClaudeCommand != "claude" {
		t.Fatalf("unexpected claude_command: %s", runtime.ClaudeCommand)
	}
	if runtime.ClaudeTimeout != 172800*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", runtime.ClaudeTimeout)
	}
	if runtime.GeminiCommand != "gemini" {
		t.Fatalf("unexpected gemini_command: %s", runtime.GeminiCommand)
	}
	if runtime.GeminiTimeout != 172800*time.Second {
		t.Fatalf("unexpected gemini_timeout: %s", runtime.GeminiTimeout)
	}
	if runtime.QueueCapacity != 256 {
		t.Fatalf("unexpected queue_capacity: %d", runtime.QueueCapacity)
	}
	if runtime.WorkerConcurrency != DefaultWorkerConcurrency {
		t.Fatalf("unexpected worker_concurrency: %d", runtime.WorkerConcurrency)
	}
	if runtime.AutomationTaskTimeoutSecs != 6000 {
		t.Fatalf("unexpected automation_task_timeout_secs: %d", runtime.AutomationTaskTimeoutSecs)
	}
	if runtime.AutomationTaskTimeout != 100*time.Minute {
		t.Fatalf("unexpected automation_task_timeout: %s", runtime.AutomationTaskTimeout)
	}
	if runtime.ThinkingMessage != "正在思考中..." {
		t.Fatalf("unexpected thinking_message: %s", runtime.ThinkingMessage)
	}
	if len(runtime.LLMProfiles) != 0 {
		t.Fatalf("unexpected llm_profiles: %#v", runtime.LLMProfiles)
	}
	if runtime.CodexEnv["HTTPS_PROXY"] != DefaultHTTPSProxy {
		t.Fatalf("unexpected default HTTPS_PROXY: %q", runtime.CodexEnv["HTTPS_PROXY"])
	}
	if runtime.CodexEnv["ALL_PROXY"] != DefaultALLProxy {
		t.Fatalf("unexpected default ALL_PROXY: %q", runtime.CodexEnv["ALL_PROXY"])
	}
	if runtime.GroupScenes.Chat.Enabled {
		t.Fatal("chat scene should default to disabled")
	}
	if runtime.GroupScenes.Work.Enabled {
		t.Fatal("work scene should default to disabled")
	}
	wantAliceHome := filepath.Join(base, "bots", "main")
	if runtime.AliceHome != wantAliceHome {
		t.Fatalf("unexpected alice_home: %s", runtime.AliceHome)
	}
	if runtime.WorkspaceDir != filepath.Join(wantAliceHome, "workspace") {
		t.Fatalf("unexpected workspace_dir: %s", runtime.WorkspaceDir)
	}
	if runtime.PromptDir != filepath.Join(wantAliceHome, "prompts") {
		t.Fatalf("unexpected prompt_dir: %s", runtime.PromptDir)
	}
	if runtime.CodexHome != filepath.Join(wantAliceHome, ".codex") {
		t.Fatalf("unexpected codex_home: %s", runtime.CodexHome)
	}
	if runtime.SoulPath != filepath.Join(runtime.WorkspaceDir, "SOUL.md") {
		t.Fatalf("unexpected soul_path: %s", runtime.SoulPath)
	}
	if got, want := filepath.Dir(cfg.LogFile), LogDirForAliceHome(cfg.AliceHome); got != want {
		t.Fatalf("unexpected log_file dir: got=%q want=%q", got, want)
	}
	if _, err := time.ParseInLocation("2006-01-02.log", filepath.Base(cfg.LogFile), time.Local); err != nil {
		t.Fatalf("unexpected log_file basename: %q err=%v", filepath.Base(cfg.LogFile), err)
	}
}

func TestLoadFromFile_AliceHomeDrivesDefaultDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
alice_home: "~/.alice-custom"
`)

	wantAliceHome := filepath.Join(home, ".alice-custom")
	if runtime.AliceHome != wantAliceHome {
		t.Fatalf("unexpected alice_home got=%q want=%q", runtime.AliceHome, wantAliceHome)
	}
	if runtime.WorkspaceDir != filepath.Join(wantAliceHome, "workspace") {
		t.Fatalf("unexpected workspace_dir: %s", runtime.WorkspaceDir)
	}
	if runtime.PromptDir != filepath.Join(wantAliceHome, "prompts") {
		t.Fatalf("unexpected prompt_dir: %s", runtime.PromptDir)
	}
}

func TestLoadFromFile_RequiredKeys(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "feishu_app_secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_Env(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
env:
  HTTPS_PROXY: "  http://127.0.0.1:7890  "
  ALL_PROXY: "socks5://127.0.0.1:7891"
`)

	if runtime.CodexEnv["HTTPS_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected HTTPS_PROXY: %q", runtime.CodexEnv["HTTPS_PROXY"])
	}
	if runtime.CodexEnv["ALL_PROXY"] != "socks5://127.0.0.1:7891" {
		t.Fatalf("unexpected ALL_PROXY: %q", runtime.CodexEnv["ALL_PROXY"])
	}
}

func TestLoadFromFile_CodexModelConfigTrimmed(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
codex_model: "  gpt-5.4  "
codex_model_reasoning_effort: "  HIGH  "
`)

	if runtime.CodexModel != "gpt-5.4" {
		t.Fatalf("unexpected codex_model: %q", runtime.CodexModel)
	}
	if runtime.CodexReasoningEffort != "high" {
		t.Fatalf("unexpected codex_model_reasoning_effort: %q", runtime.CodexReasoningEffort)
	}
}

func TestLoadFromFile_ImageGenerationConfig(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
image_generation:
  enabled: true
  provider: "  OpenAI  "
  model: "  gpt-image-1.5  "
  base_url: "  https://api.openai.example/v1  "
  timeout_secs: 180
  size: "  1024x1024  "
  quality: "  HIGH  "
  background: "  OPAQUE  "
  output_format: "  PNG  "
  input_fidelity: "  HIGH  "
  use_current_attachments: false
`)

	if !runtime.ImageGeneration.Enabled {
		t.Fatal("expected image_generation.enabled to be true")
	}
	if runtime.ImageGeneration.Provider != "openai" {
		t.Fatalf("unexpected image_generation.provider: %q", runtime.ImageGeneration.Provider)
	}
	if runtime.ImageGeneration.Model != "gpt-image-1.5" {
		t.Fatalf("unexpected image_generation.model: %q", runtime.ImageGeneration.Model)
	}
	if runtime.ImageGeneration.BaseURL != "https://api.openai.example/v1" {
		t.Fatalf("unexpected image_generation.base_url: %q", runtime.ImageGeneration.BaseURL)
	}
	if runtime.ImageGeneration.TimeoutSecs != 180 {
		t.Fatalf("unexpected image_generation.timeout_secs: %d", runtime.ImageGeneration.TimeoutSecs)
	}
	if runtime.ImageGeneration.Size != "1024x1024" {
		t.Fatalf("unexpected image_generation.size: %q", runtime.ImageGeneration.Size)
	}
	if runtime.ImageGeneration.Quality != "high" {
		t.Fatalf("unexpected image_generation.quality: %q", runtime.ImageGeneration.Quality)
	}
	if runtime.ImageGeneration.Background != "opaque" {
		t.Fatalf("unexpected image_generation.background: %q", runtime.ImageGeneration.Background)
	}
	if runtime.ImageGeneration.OutputFormat != "png" {
		t.Fatalf("unexpected image_generation.output_format: %q", runtime.ImageGeneration.OutputFormat)
	}
	if runtime.ImageGeneration.InputFidelity != "high" {
		t.Fatalf("unexpected image_generation.input_fidelity: %q", runtime.ImageGeneration.InputFidelity)
	}
	if runtime.ImageGeneration.UseCurrentAttachments {
		t.Fatal("expected use_current_attachments to be false")
	}
}

func TestLoadFromFile_ImageGenerationProxyRejected(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
image_generation:
  proxy:
    https_proxy: "http://127.0.0.1:7890"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "image_generation.proxy has been removed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_EnvInvalidKey(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
env:
  "BAD=KEY": "v"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must not contain '='") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LogConfigTrimmed(t *testing.T) {
	cfg, _ := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
`, `
log_level: "  DEBUG  "
log_file: "  logs/alice.log  "
log_max_size_mb: 0
log_max_backups: -1
log_max_age_days: 0
log_compress: true
`)

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

func TestLoadFromFile_GroupScenesConfig(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  chat:
    provider: codex
    model: gpt-5.4-mini
    reasoning_effort: low
    personality: Friendly
  work:
    provider: codex
    model: gpt-5.4
    reasoning_effort: xhigh
    personality: pragmatic
group_scenes:
  chat:
    enabled: true
    llm_profile: chat
  work:
    enabled: true
    llm_profile: work
`)

	if !runtime.GroupScenes.Chat.Enabled || runtime.GroupScenes.Chat.LLMProfile != "chat" {
		t.Fatalf("unexpected chat scene: %#v", runtime.GroupScenes.Chat)
	}
	if !runtime.GroupScenes.Work.Enabled || runtime.GroupScenes.Work.LLMProfile != "work" {
		t.Fatalf("unexpected work scene: %#v", runtime.GroupScenes.Work)
	}
	if runtime.LLMProfiles["chat"].Personality != "friendly" {
		t.Fatalf("unexpected chat personality: %#v", runtime.LLMProfiles["chat"])
	}
	if runtime.LLMProfiles["work"].ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected work profile: %#v", runtime.LLMProfiles["work"])
	}
}

func TestLoadFromFile_AutomationTaskTimeoutSecsInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
automation_task_timeout_secs: 0
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "automation_task_timeout_secs must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_FeishuBotIDsTrimmed(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
feishu_bot_open_id: "  ou_bot  "
feishu_bot_user_id: "  123456  "
`)

	if runtime.FeishuBotOpenID != "ou_bot" {
		t.Fatalf("unexpected feishu_bot_open_id: %q", runtime.FeishuBotOpenID)
	}
	if runtime.FeishuBotUserID != "123456" {
		t.Fatalf("unexpected feishu_bot_user_id: %q", runtime.FeishuBotUserID)
	}
}

func TestLoadFromFile_TriggerModeTrimmedLowercase(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "  PrEfIx  "
trigger_prefix: "  !alice  "
`)

	if runtime.TriggerMode != TriggerModePrefix {
		t.Fatalf("unexpected trigger_mode: %q", runtime.TriggerMode)
	}
	if runtime.TriggerPrefix != "!alice" {
		t.Fatalf("unexpected trigger_prefix: %q", runtime.TriggerPrefix)
	}
}

func TestLoadFromFile_TriggerModeInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "all"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported trigger_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_ImmediateFeedbackModeTrimmedLowercase(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
immediate_feedback_mode: "  ReAcTiOn  "
immediate_feedback_reaction: "  smile  "
`)

	if runtime.ImmediateFeedbackMode != ImmediateFeedbackModeReaction {
		t.Fatalf("unexpected immediate_feedback_mode: %q", runtime.ImmediateFeedbackMode)
	}
	if runtime.ImmediateFeedbackReaction != "SMILE" {
		t.Fatalf("unexpected immediate_feedback_reaction: %q", runtime.ImmediateFeedbackReaction)
	}
}

func TestLoadFromFile_ImmediateFeedbackModeInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
immediate_feedback_mode: "wave"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported immediate_feedback_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_TriggerModePrefixRequiresPrefix(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "prefix"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "trigger_prefix is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LLMProviderInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: openai
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported llm_provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LLMProviderTrimmedLowercase(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: "  CoDeX  "
`)

	if runtime.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected llm_provider: %q", runtime.LLMProvider)
	}
}

func TestLoadFromFile_LLMProviderClaude(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: "  ClAuDe  "
claude_command: "  claude-custom  "
claude_timeout_secs: 233
claude_prompt_prefix: "  你是Claude助手  "
`)

	if runtime.LLMProvider != LLMProviderClaude {
		t.Fatalf("unexpected llm_provider: %q", runtime.LLMProvider)
	}
	if runtime.ClaudeCommand != "claude-custom" {
		t.Fatalf("unexpected claude_command: %q", runtime.ClaudeCommand)
	}
	if runtime.ClaudeTimeout != 233*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", runtime.ClaudeTimeout)
	}
	if runtime.ClaudePromptPrefix != "你是Claude助手" {
		t.Fatalf("unexpected claude_prompt_prefix: %q", runtime.ClaudePromptPrefix)
	}
}

func TestLoadFromFile_LLMProviderGemini(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: "  GeMiNi  "
gemini_command: "  gemini-custom  "
gemini_timeout_secs: 321
gemini_prompt_prefix: "  你是Gemini助手  "
`)

	if runtime.LLMProvider != LLMProviderGemini {
		t.Fatalf("unexpected llm_provider: %q", runtime.LLMProvider)
	}
	if runtime.GeminiCommand != "gemini-custom" {
		t.Fatalf("unexpected gemini_command: %q", runtime.GeminiCommand)
	}
	if runtime.GeminiTimeout != 321*time.Second {
		t.Fatalf("unexpected gemini_timeout: %s", runtime.GeminiTimeout)
	}
	if runtime.GeminiPromptPrefix != "你是Gemini助手" {
		t.Fatalf("unexpected gemini_prompt_prefix: %q", runtime.GeminiPromptPrefix)
	}
}

func TestLoadFromFile_GeminiTimeoutInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: gemini
gemini_timeout_secs: 0
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gemini_timeout_secs must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_CodexTimeoutIgnoredWhenGeminiProvider(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: gemini
codex_timeout_secs: 0
gemini_timeout_secs: 60
`)

	if runtime.CodexTimeout != 172800*time.Second {
		t.Fatalf("unexpected codex_timeout fallback: %s", runtime.CodexTimeout)
	}
	if runtime.GeminiTimeout != 60*time.Second {
		t.Fatalf("unexpected gemini_timeout: %s", runtime.GeminiTimeout)
	}
}

func TestLoadFromFile_ClaudeTimeoutInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: claude
claude_timeout_secs: 0
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "claude_timeout_secs must be > 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_CodexTimeoutIgnoredWhenClaudeProvider(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_provider: claude
codex_timeout_secs: 0
claude_timeout_secs: 60
`)

	if runtime.CodexTimeout != 172800*time.Second {
		t.Fatalf("unexpected codex_timeout fallback: %s", runtime.CodexTimeout)
	}
	if runtime.ClaudeTimeout != 60*time.Second {
		t.Fatalf("unexpected claude_timeout: %s", runtime.ClaudeTimeout)
	}
}

func loadSingleBotRuntime(t *testing.T, botBody string, rootBody ...string) (Config, Config) {
	t.Helper()
	path := writeSingleBotConfig(t, botBody, rootBody...)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	runtime, err := cfg.RuntimeConfigForBot("main")
	if err != nil {
		t.Fatalf("resolve runtime failed: %v", err)
	}
	return cfg, runtime
}

func writeSingleBotConfig(t *testing.T, botBody string, rootBody ...string) string {
	t.Helper()
	builder := strings.Builder{}
	for _, block := range rootBody {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		builder.WriteString(trimmed)
		builder.WriteString("\n")
	}
	builder.WriteString("bots:\n")
	builder.WriteString("  main:\n")
	builder.WriteString(indentYAML(strings.TrimSpace(botBody), "    "))
	builder.WriteString("\n")
	return writeConfigFile(t, builder.String())
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	return path
}

func indentYAML(content, prefix string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for idx, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[idx] = prefix
			continue
		}
		lines[idx] = prefix + line
	}
	return strings.Join(lines, "\n")
}
