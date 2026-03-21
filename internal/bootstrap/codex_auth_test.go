package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/config"
)

func TestEnsureCodexAuthForCodexHome_CopiesFromExplicitSource(t *testing.T) {
	sourceCodexHome := t.TempDir()
	sourceAuthPath := filepath.Join(sourceCodexHome, "auth.json")
	if err := os.WriteFile(sourceAuthPath, []byte("source-token"), 0o600); err != nil {
		t.Fatalf("write source auth failed: %v", err)
	}

	targetCodexHome := filepath.Join(t.TempDir(), "bot", ".codex")
	report, err := EnsureCodexAuthForCodexHome(targetCodexHome, sourceCodexHome)
	if err != nil {
		t.Fatalf("ensure auth failed: %v", err)
	}
	if !report.Copied {
		t.Fatal("expected auth to be copied")
	}
	if report.Source != sourceAuthPath {
		t.Fatalf("unexpected source path: %q", report.Source)
	}

	content, err := os.ReadFile(filepath.Join(targetCodexHome, "auth.json"))
	if err != nil {
		t.Fatalf("read target auth failed: %v", err)
	}
	if string(content) != "source-token" {
		t.Fatalf("unexpected target auth content: %q", string(content))
	}
}

func TestEnsureCodexAuthForCodexHome_PreservesExistingTarget(t *testing.T) {
	sourceCodexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceCodexHome, "auth.json"), []byte("source-token"), 0o600); err != nil {
		t.Fatalf("write source auth failed: %v", err)
	}

	targetCodexHome := t.TempDir()
	targetAuthPath := filepath.Join(targetCodexHome, "auth.json")
	if err := os.WriteFile(targetAuthPath, []byte("target-token"), 0o600); err != nil {
		t.Fatalf("write target auth failed: %v", err)
	}

	report, err := EnsureCodexAuthForCodexHome(targetCodexHome, sourceCodexHome)
	if err != nil {
		t.Fatalf("ensure auth failed: %v", err)
	}
	if report.Copied {
		t.Fatal("expected existing auth to be preserved")
	}

	content, err := os.ReadFile(targetAuthPath)
	if err != nil {
		t.Fatalf("read target auth failed: %v", err)
	}
	if string(content) != "target-token" {
		t.Fatalf("unexpected target auth content: %q", string(content))
	}
}

func TestEnsureCodexAuthForCodexHome_FallsBackToDefaultCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.EnvCodexHome, "")

	defaultCodexHomeAuthPath := filepath.Join(config.DefaultCodexHome(), "auth.json")
	if err := os.MkdirAll(filepath.Dir(defaultCodexHomeAuthPath), 0o755); err != nil {
		t.Fatalf("create default codex dir failed: %v", err)
	}
	if err := os.WriteFile(defaultCodexHomeAuthPath, []byte("default-token"), 0o600); err != nil {
		t.Fatalf("write default auth failed: %v", err)
	}

	targetCodexHome := filepath.Join(t.TempDir(), "bot", ".codex")
	report, err := EnsureCodexAuthForCodexHome(targetCodexHome)
	if err != nil {
		t.Fatalf("ensure auth failed: %v", err)
	}
	if !report.Copied {
		t.Fatal("expected auth to be copied from default codex dir")
	}
	if report.Source != defaultCodexHomeAuthPath {
		t.Fatalf("unexpected source path: %q", report.Source)
	}
}

func TestEnsureCodexAuthForCodexHome_FallsBackToLegacyHomeCodexAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.EnvCodexHome, "")
	t.Setenv(config.EnvAliceHome, filepath.Join(t.TempDir(), "alice-home-without-auth"))

	legacyHomeAuthPath := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(legacyHomeAuthPath), 0o755); err != nil {
		t.Fatalf("create legacy home codex dir failed: %v", err)
	}
	if err := os.WriteFile(legacyHomeAuthPath, []byte("legacy-home-token"), 0o600); err != nil {
		t.Fatalf("write legacy home auth failed: %v", err)
	}

	targetCodexHome := filepath.Join(t.TempDir(), "bot", ".codex")
	report, err := EnsureCodexAuthForCodexHome(targetCodexHome, filepath.Join(t.TempDir(), "missing-codex-home"))
	if err != nil {
		t.Fatalf("ensure auth failed: %v", err)
	}
	if !report.Copied {
		t.Fatal("expected auth to be copied from legacy home codex dir")
	}
	if report.Source != legacyHomeAuthPath {
		t.Fatalf("unexpected source path: %q", report.Source)
	}
}
