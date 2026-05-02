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

func TestLoadFromFile_CodexIdleTimeoutSecsInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
codex_idle_timeout_secs: 0
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "codex_idle_timeout_secs must be > 0") {
		t.Fatalf("unexpected error: %v", err)
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
trigger_mode: "wave"
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported trigger_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_TriggerModeAll(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
trigger_mode: "  AlL  "
`)

	if runtime.TriggerMode != TriggerModeAll {
		t.Fatalf("unexpected trigger_mode: %q", runtime.TriggerMode)
	}
	if runtime.TriggerPrefix != "" {
		t.Fatalf("unexpected trigger_prefix: %q", runtime.TriggerPrefix)
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

func TestLoadFromFile_LLMProfileProviderInvalid(t *testing.T) {
	path := writeSingleBotConfig(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  main:
    provider: openai
`)

	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFromFile_LLMProfileProviderTrimmedLowercase(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  main:
    provider: "  CoDeX  "
`)

	profile, ok := runtime.LLMProfiles["main"]
	if !ok {
		t.Fatal("expected llm_profiles.main to exist")
	}
	if profile.Provider != DefaultLLMProvider {
		t.Fatalf("unexpected provider: %q", profile.Provider)
	}
}

func TestLoadFromFile_LLMProfileClaude(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  main:
    provider: "  ClAuDe  "
    command: "  claude-custom  "
    timeout_secs: 233
    prompt_prefix: "  你是Claude助手  "
`)

	profile, ok := runtime.LLMProfiles["main"]
	if !ok {
		t.Fatal("expected llm_profiles.main to exist")
	}
	if profile.Provider != LLMProviderClaude {
		t.Fatalf("unexpected provider: %q", profile.Provider)
	}
	if profile.Command != "claude-custom" {
		t.Fatalf("unexpected command: %q", profile.Command)
	}
	if profile.Timeout != 233*time.Second {
		t.Fatalf("unexpected timeout: %s", profile.Timeout)
	}
	if profile.PromptPrefix != "你是Claude助手" {
		t.Fatalf("unexpected prompt_prefix: %q", profile.PromptPrefix)
	}
}

func TestLoadFromFile_LLMProfileGemini(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  main:
    provider: "  GeMiNi  "
    command: "  gemini-custom  "
    timeout_secs: 321
    prompt_prefix: "  你是Gemini助手  "
`)

	profile, ok := runtime.LLMProfiles["main"]
	if !ok {
		t.Fatal("expected llm_profiles.main to exist")
	}
	if profile.Provider != LLMProviderGemini {
		t.Fatalf("unexpected provider: %q", profile.Provider)
	}
	if profile.Command != "gemini-custom" {
		t.Fatalf("unexpected command: %q", profile.Command)
	}
	if profile.Timeout != 321*time.Second {
		t.Fatalf("unexpected timeout: %s", profile.Timeout)
	}
	if profile.PromptPrefix != "你是Gemini助手" {
		t.Fatalf("unexpected prompt_prefix: %q", profile.PromptPrefix)
	}
}

func TestLoadFromFile_LLMProfileTimeoutDefaultsWhenZero(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  gemini:
    provider: gemini
    timeout_secs: 0
  codex:
    provider: codex
    timeout_secs: 60
`)

	geminiProfile, ok := runtime.LLMProfiles["gemini"]
	if !ok {
		t.Fatal("expected llm_profiles.gemini to exist")
	}
	if geminiProfile.Timeout != DefaultLLMTimeoutSecs*time.Second {
		t.Fatalf("unexpected gemini timeout: %s (expected default %s)", geminiProfile.Timeout, time.Duration(DefaultLLMTimeoutSecs)*time.Second)
	}

	codexProfile, ok := runtime.LLMProfiles["codex"]
	if !ok {
		t.Fatal("expected llm_profiles.codex to exist")
	}
	if codexProfile.Timeout != 60*time.Second {
		t.Fatalf("unexpected codex timeout: %s", codexProfile.Timeout)
	}
}

func TestLoadFromFile_LLMProfileCommandDefaultsByProvider(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  claude:
    provider: claude
  gemini:
    provider: gemini
  kimi:
    provider: kimi
  codex:
    provider: codex
`)

	cases := []struct {
		name    string
		wantCmd string
	}{
		{"claude", "claude"},
		{"gemini", "gemini"},
		{"kimi", "kimi"},
		{"codex", "codex"},
	}
	for _, tc := range cases {
		profile, ok := runtime.LLMProfiles[tc.name]
		if !ok {
			t.Fatalf("expected llm_profiles.%s to exist", tc.name)
		}
		if profile.Command != tc.wantCmd {
			t.Fatalf("llm_profiles.%s: unexpected command %q, want %q", tc.name, profile.Command, tc.wantCmd)
		}
	}
}
