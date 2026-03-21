package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFinalizeConfig_MultiBotPrimaryPreservesLegacyPaths(t *testing.T) {
	base := t.TempDir()
	rootAliceHome := filepath.Join(base, "alice-home")
	rootWorkspace := filepath.Join(base, "workspace")
	rootPromptDir := filepath.Join(base, "prompts")
	rootCodexHome := filepath.Join(base, "codex-home")
	cfg := Config{
		FeishuAppID:     "cli_primary",
		FeishuAppSecret: "secret_primary",
		AliceHome:       rootAliceHome,
		WorkspaceDir:    rootWorkspace,
		PromptDir:       rootPromptDir,
		CodexHome:       rootCodexHome,
		PrimaryBotID:    "primary",
		RuntimeHTTPAddr: "127.0.0.1:7331",
		Bots: normalizeBots(map[string]BotConfig{
			"primary": {
				Name:            "Alice Primary",
				FeishuAppID:     "cli_primary",
				FeishuAppSecret: "secret_primary",
			},
			"helper": {
				Name:            "Alice Helper",
				FeishuAppID:     "cli_helper",
				FeishuAppSecret: "secret_helper",
			},
		}),
	}

	primaryBotID, err := cfg.resolvePrimaryBotID()
	if err != nil {
		t.Fatalf("resolve primary bot failed: %v", err)
	}
	if primaryBotID != "primary" {
		t.Fatalf("unexpected primary bot id: %q", primaryBotID)
	}

	primaryBot := cfg.Bots["primary"]
	if got := deriveBotAliceHome(cfg, primaryBot, "primary", true); got != rootAliceHome {
		t.Fatalf("primary bot should preserve alice_home: got=%q want=%q", got, rootAliceHome)
	}
	if got := deriveBotWorkspaceDir(cfg, primaryBot, rootAliceHome, true); got != rootWorkspace {
		t.Fatalf("primary bot should preserve workspace_dir: got=%q want=%q", got, rootWorkspace)
	}
	if got := deriveBotCodexHome(cfg, primaryBot, rootAliceHome, true); got != rootCodexHome {
		t.Fatalf("primary bot should preserve codex_home: got=%q want=%q", got, rootCodexHome)
	}

	helperBot := cfg.Bots["helper"]
	helperAliceHome := deriveBotAliceHome(cfg, helperBot, "helper", false)
	if helperAliceHome != filepath.Join(rootAliceHome, "bots", "helper") {
		t.Fatalf("unexpected helper alice_home: %q", helperAliceHome)
	}
	if got := deriveBotWorkspaceDir(cfg, helperBot, helperAliceHome, false); got != WorkspaceDirForAliceHome(helperAliceHome) {
		t.Fatalf("unexpected helper workspace_dir: %q", got)
	}
	if got := deriveBotRuntimeHTTPAddr(cfg, helperBot, 1, false); got != "127.0.0.1:7332" {
		t.Fatalf("unexpected helper runtime_http_addr: %q", got)
	}
}

func TestFinalizeConfig_PreservesCodexAddDirsCase(t *testing.T) {
	cfg := normalizeBotPermissions(BotPermissionsConfig{
		Codex: SceneCodexPoliciesConfig{
			Chat: CodexExecPolicyConfig{
				AddDirs: []string{"./DataDir", "./DataDir", "/Tmp/MixedCase"},
			},
		},
	})

	want := []string{"./DataDir", "/Tmp/MixedCase"}
	if !reflect.DeepEqual(cfg.Codex.Chat.AddDirs, want) {
		t.Fatalf("unexpected chat add_dirs: got=%#v want=%#v", cfg.Codex.Chat.AddDirs, want)
	}
}

func TestAllowedBundledSkills_RespectsRuntimePermissions(t *testing.T) {
	cfg := Config{
		Permissions: normalizeBotPermissions(BotPermissionsConfig{
			RuntimeAutomation: boolPtr(false),
			RuntimeCampaigns:  boolPtr(false),
		}),
	}

	got := cfg.AllowedBundledSkills()
	if containsString(got, "alice-scheduler") {
		t.Fatalf("chat-only skills should exclude alice-scheduler, got %#v", got)
	}
	if containsString(got, "alice-code-army") {
		t.Fatalf("chat-only skills should exclude alice-code-army, got %#v", got)
	}
	if !containsString(got, "alice-message") {
		t.Fatalf("chat-only skills should keep alice-message, got %#v", got)
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
