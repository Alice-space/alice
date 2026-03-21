package bootstrap

import (
	"testing"

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
