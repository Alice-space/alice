package config

import (
	"strings"
	"testing"
)

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
  moderation: "  LOW  "
  n: 3
  output_compression: 85
  response_format: "  B64_JSON  "
  size: "  1024x1024  "
  quality: "  HIGH  "
  background: "  OPAQUE  "
  output_format: "  PNG  "
  partial_images: 2
  stream: true
  style: "  NATURAL  "
  input_fidelity: "  HIGH  "
  mask_path: "  /tmp/mask.png  "
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
	if runtime.ImageGeneration.Moderation != "low" {
		t.Fatalf("unexpected image_generation.moderation: %q", runtime.ImageGeneration.Moderation)
	}
	if runtime.ImageGeneration.N != 3 {
		t.Fatalf("unexpected image_generation.n: %d", runtime.ImageGeneration.N)
	}
	if runtime.ImageGeneration.OutputCompression != 85 {
		t.Fatalf("unexpected image_generation.output_compression: %d", runtime.ImageGeneration.OutputCompression)
	}
	if runtime.ImageGeneration.ResponseFormat != "b64_json" {
		t.Fatalf("unexpected image_generation.response_format: %q", runtime.ImageGeneration.ResponseFormat)
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
	if runtime.ImageGeneration.PartialImages != 2 {
		t.Fatalf("unexpected image_generation.partial_images: %d", runtime.ImageGeneration.PartialImages)
	}
	if !runtime.ImageGeneration.Stream {
		t.Fatal("expected image_generation.stream to be true")
	}
	if runtime.ImageGeneration.Style != "natural" {
		t.Fatalf("unexpected image_generation.style: %q", runtime.ImageGeneration.Style)
	}
	if runtime.ImageGeneration.InputFidelity != "high" {
		t.Fatalf("unexpected image_generation.input_fidelity: %q", runtime.ImageGeneration.InputFidelity)
	}
	if runtime.ImageGeneration.MaskPath != "/tmp/mask.png" {
		t.Fatalf("unexpected image_generation.mask_path: %q", runtime.ImageGeneration.MaskPath)
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

func TestLoadFromFile_LLMProviderDerivedFromSceneProfiles(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  chat:
    provider: claude
    model: claude-sonnet-4-20250514
  work:
    provider: claude
    model: claude-opus-4-20250514
group_scenes:
  chat:
    enabled: true
    llm_profile: chat
  work:
    enabled: true
    llm_profile: work
`)

	if runtime.LLMProvider != LLMProviderClaude {
		t.Fatalf("unexpected derived llm_provider: %q", runtime.LLMProvider)
	}
}

func TestLoadFromFile_LLMProviderDerivedDefaultsToCodexWhenProfilesOmitProvider(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  chat:
    model: gpt-5.4-mini
group_scenes:
  chat:
    enabled: true
    llm_profile: chat
`)

	if runtime.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected default llm_provider: %q", runtime.LLMProvider)
	}
}

func TestLoadFromFile_LLMProviderAllowsMixedActiveSceneProviders(t *testing.T) {
	_, runtime := loadSingleBotRuntime(t, `
feishu_app_id: cli_xxx
feishu_app_secret: sss
llm_profiles:
  chat:
    provider: codex
    model: gpt-5.4-mini
  work:
    provider: claude
    model: claude-sonnet-4-20250514
group_scenes:
  chat:
    enabled: true
    llm_profile: chat
  work:
    enabled: true
    llm_profile: work
`)

	if runtime.LLMProvider != DefaultLLMProvider {
		t.Fatalf("unexpected fallback llm_provider: %q", runtime.LLMProvider)
	}
}
