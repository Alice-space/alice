package bootstrap

import (
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/config"
)

func TestApplyLLMProcessEnvDefaults_AddsDefaultCodexHome(t *testing.T) {
	env := applyLLMProcessEnvDefaults(map[string]string{
		"HTTPS_PROXY": " http://127.0.0.1:7890 ",
	}, "")
	if env["HTTPS_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected HTTPS_PROXY value: %q", env["HTTPS_PROXY"])
	}
	if env[config.EnvCodexHome] != config.DefaultCodexHome() {
		t.Fatalf("unexpected default CODEX_HOME: %q", env[config.EnvCodexHome])
	}
}

func TestApplyLLMProcessEnvDefaults_PreservesExplicitCodexHome(t *testing.T) {
	env := applyLLMProcessEnvDefaults(map[string]string{
		config.EnvCodexHome: " /tmp/custom-codex-home ",
	}, "")
	if env[config.EnvCodexHome] != "/tmp/custom-codex-home" {
		t.Fatalf("explicit CODEX_HOME should be preserved, got=%q", env[config.EnvCodexHome])
	}
}

func TestBuildFactoryConfig_KeepsOuterProfileNameAndInnerProviderProfile(t *testing.T) {
	cfg := config.Config{
		LLMProvider: "codex",
		LLMProfiles: map[string]config.LLMProfileConfig{
			"work": {
				Provider: "codex",
				Command:  "codex-work",
				Timeout:  45 * time.Second,
				Model:    "gpt-5.4",
				Profile:  "work-cli",
				Permissions: &config.CodexExecPolicyConfig{
					Sandbox:        "danger-full-access",
					AskForApproval: "never",
					AddDirs:        []string{"/tmp/work"},
				},
			},
		},
	}

	factory := buildFactoryConfig(cfg)
	override, ok := factory.Codex.ProfileOverrides["work"]
	if !ok {
		t.Fatal("expected codex profile override for outer profile name")
	}
	if override.Command != "codex-work" {
		t.Fatalf("unexpected override command: %q", override.Command)
	}
	if override.Timeout != 45*time.Second {
		t.Fatalf("unexpected override timeout: %s", override.Timeout)
	}
	if override.ProviderProfile != "work-cli" {
		t.Fatalf("unexpected provider profile: %q", override.ProviderProfile)
	}
	if override.ExecPolicy.Sandbox != "danger-full-access" {
		t.Fatalf("unexpected sandbox: %q", override.ExecPolicy.Sandbox)
	}
	if override.ExecPolicy.AskForApproval != "never" {
		t.Fatalf("unexpected ask_for_approval: %q", override.ExecPolicy.AskForApproval)
	}
	if len(override.ExecPolicy.AddDirs) != 1 || override.ExecPolicy.AddDirs[0] != "/tmp/work" {
		t.Fatalf("unexpected add_dirs: %#v", override.ExecPolicy.AddDirs)
	}
}
