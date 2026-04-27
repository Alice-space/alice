package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRuntimeConfigs_MultiBotUsesSharedCodexHomeByDefault(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv(EnvAliceHome, base)
	t.Setenv(EnvCodexHome, "")
	cfgPath := writeConfigFile(t, `
bots:
  work:
    name: "Alice Work"
    feishu_app_id: "cli_work"
    feishu_app_secret: "secret_work"
  chat:
    name: "Alice Chat"
    feishu_app_id: "cli_chat"
    feishu_app_secret: "secret_chat"
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	runtimes, err := cfg.RuntimeConfigs()
	if err != nil {
		t.Fatalf("build runtime configs failed: %v", err)
	}
	if len(runtimes) != 2 {
		t.Fatalf("unexpected runtime count: %d", len(runtimes))
	}

	chat, err := cfg.RuntimeConfigForBot("chat")
	if err != nil {
		t.Fatalf("resolve chat runtime failed: %v", err)
	}
	if chat.AliceHome != filepath.Join(base, "bots", "chat") {
		t.Fatalf("unexpected chat alice_home: %q", chat.AliceHome)
	}
	if chat.WorkspaceDir != WorkspaceDirForAliceHome(chat.AliceHome) {
		t.Fatalf("unexpected chat workspace_dir: %q", chat.WorkspaceDir)
	}
	if chat.CodexHome != filepath.Join(base, ".codex") {
		t.Fatalf("unexpected chat codex_home: %q", chat.CodexHome)
	}
	if chat.RuntimeHTTPAddr != "127.0.0.1:7331" {
		t.Fatalf("unexpected chat runtime_http_addr: %q", chat.RuntimeHTTPAddr)
	}

	work, err := cfg.RuntimeConfigForBot("work")
	if err != nil {
		t.Fatalf("resolve work runtime failed: %v", err)
	}
	if work.AliceHome != filepath.Join(base, "bots", "work") {
		t.Fatalf("unexpected work alice_home: %q", work.AliceHome)
	}
	if work.CodexHome != filepath.Join(base, ".codex") {
		t.Fatalf("unexpected work codex_home: %q", work.CodexHome)
	}
	if work.RuntimeHTTPAddr != "127.0.0.1:7332" {
		t.Fatalf("unexpected work runtime_http_addr: %q", work.RuntimeHTTPAddr)
	}
}

func TestRuntimeConfigForBot_UsesExplicitBotPaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvAliceHome, base)
	cfgPath := writeConfigFile(t, `
bots:
  chat:
    feishu_app_id: "cli_chat"
    feishu_app_secret: "secret_chat"
    alice_home: "`+filepath.Join(base, "custom-home")+`"
    workspace_dir: "`+filepath.Join(base, "custom-workspace")+`"
    prompt_dir: "`+filepath.Join(base, "custom-prompts")+`"
    codex_home: "`+filepath.Join(base, "custom-codex")+`"
    soul_path: "souls/chat.md"
    runtime_http_addr: "127.0.0.1:7441"
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	runtime, err := cfg.RuntimeConfigForBot("chat")
	if err != nil {
		t.Fatalf("resolve runtime failed: %v", err)
	}
	if runtime.AliceHome != filepath.Join(base, "custom-home") {
		t.Fatalf("unexpected alice_home: %q", runtime.AliceHome)
	}
	if runtime.WorkspaceDir != filepath.Join(base, "custom-workspace") {
		t.Fatalf("unexpected workspace_dir: %q", runtime.WorkspaceDir)
	}
	if runtime.PromptDir != filepath.Join(base, "custom-prompts") {
		t.Fatalf("unexpected prompt_dir: %q", runtime.PromptDir)
	}
	if runtime.CodexHome != filepath.Join(base, "custom-codex") {
		t.Fatalf("unexpected codex_home: %q", runtime.CodexHome)
	}
	if runtime.SoulPath != filepath.Join(runtime.AliceHome, "souls/chat.md") {
		t.Fatalf("unexpected soul_path: %q", runtime.SoulPath)
	}
	if runtime.RuntimeHTTPAddr != "127.0.0.1:7441" {
		t.Fatalf("unexpected runtime_http_addr: %q", runtime.RuntimeHTTPAddr)
	}
}

func TestFinalizeConfig_LLMProfilePreservesAddDirsCase(t *testing.T) {
	policy := normalizeCodexExecPolicy(CodexExecPolicyConfig{
		AddDirs: []string{"./DataDir", "./DataDir", "/Tmp/MixedCase"},
	})

	want := []string{"./DataDir", "/Tmp/MixedCase"}
	if !reflect.DeepEqual(policy.AddDirs, want) {
		t.Fatalf("unexpected add_dirs: got=%#v want=%#v", policy.AddDirs, want)
	}
}

func TestAllowedBundledSkills_RespectsRuntimePermissions(t *testing.T) {
	cfg := Config{
		Permissions: normalizeBotPermissions(BotPermissionsConfig{
			RuntimeAutomation: boolPtr(false),
		}),
	}

	got := cfg.AllowedBundledSkills()
	if containsString(got, "alice-scheduler") {
		t.Fatalf("chat-only skills should exclude alice-scheduler, got %#v", got)
	}
	if !containsString(got, "alice-message") {
		t.Fatalf("chat-only skills should keep alice-message, got %#v", got)
	}
}

func TestFinalizeLLMProfiles_DefaultPermissionsFollowProviderBehavior(t *testing.T) {
	profiles := finalizeLLMProfiles(map[string]LLMProfileConfig{
		"codex": {
			Provider: "codex",
		},
		"claude": {
			Provider: "claude",
		},
		"kimi": {
			Provider: "kimi",
		},
		"claude_partial": {
			Provider: "claude",
			Permissions: &CodexExecPolicyConfig{
				AskForApproval: CodexApprovalOnRequest,
			},
		},
	})

	if profiles["codex"].Permissions == nil || profiles["codex"].Permissions.Sandbox != CodexSandboxWorkspaceWrite {
		t.Fatalf("codex default sandbox should stay workspace-write, got %#v", profiles["codex"].Permissions)
	}
	if profiles["claude"].Permissions == nil || profiles["claude"].Permissions.Sandbox != CodexSandboxDangerFullAccess {
		t.Fatalf("claude default sandbox should reflect bypass behavior, got %#v", profiles["claude"].Permissions)
	}
	if profiles["kimi"].Permissions == nil || profiles["kimi"].Permissions.Sandbox != CodexSandboxDangerFullAccess {
		t.Fatalf("kimi default sandbox should reflect yolo behavior, got %#v", profiles["kimi"].Permissions)
	}
	if profiles["claude_partial"].Permissions == nil {
		t.Fatal("claude partial permissions should be preserved")
	}
	if profiles["claude_partial"].Permissions.Sandbox != CodexSandboxDangerFullAccess {
		t.Fatalf("claude partial sandbox should default to danger-full-access, got %#v", profiles["claude_partial"].Permissions)
	}
	if profiles["claude_partial"].Permissions.AskForApproval != CodexApprovalOnRequest {
		t.Fatalf("explicit ask_for_approval should be preserved, got %#v", profiles["claude_partial"].Permissions)
	}
	if profiles["codex"].Timeout != time.Duration(DefaultLLMTimeoutSecs)*time.Second {
		t.Fatalf("finalize should still populate timeout, got %s", profiles["codex"].Timeout)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
