package config

import (
	"path/filepath"
	"testing"
)

func TestAliceHomeDir_DefaultFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvAliceHome, "")

	got := AliceHomeDir()
	want := filepath.Join(home, ".alice")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("unexpected alice home dir got=%q want=%q", got, want)
	}
}

func TestAliceHomeDir_UsesEnvOverrideWithTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvAliceHome, "~/.alice-custom")

	got := AliceHomeDir()
	want := filepath.Join(home, ".alice-custom")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("unexpected alice home dir got=%q want=%q", got, want)
	}
}

func TestDefaultRuntimePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvAliceHome, "")

	aliceHome := filepath.Join(home, ".alice")
	if got := DefaultConfigPath(); got != filepath.Join(aliceHome, "config.yaml") {
		t.Fatalf("unexpected default config path: %q", got)
	}
	if got := DefaultWorkspaceDir(); got != filepath.Join(aliceHome, "workspace") {
		t.Fatalf("unexpected default workspace path: %q", got)
	}
	if got := DefaultMemoryDir(); got != filepath.Join(aliceHome, "memory") {
		t.Fatalf("unexpected default memory path: %q", got)
	}
	if got := DefaultPromptDir(); got != filepath.Join(aliceHome, "prompts") {
		t.Fatalf("unexpected default prompt path: %q", got)
	}
	if got := DefaultPIDFilePath(); got != filepath.Join(aliceHome, "run", "alice.pid") {
		t.Fatalf("unexpected default pid path: %q", got)
	}
	if got := DefaultRuntimeBinaryPath(); got != filepath.Join(aliceHome, "bin", "alice") {
		t.Fatalf("unexpected default runtime binary path: %q", got)
	}
	if got := DefaultCodexHome(); got != filepath.Join(aliceHome, ".codex") {
		t.Fatalf("unexpected default codex home path: %q", got)
	}
}
