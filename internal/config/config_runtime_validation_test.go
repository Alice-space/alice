package config

import (
	"strings"
	"testing"
	"time"
)

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
