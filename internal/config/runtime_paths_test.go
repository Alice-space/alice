package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultAliceHomeName_Fallback(t *testing.T) {
	origin := defaultAliceHomeName
	t.Cleanup(func() {
		defaultAliceHomeName = origin
	})

	defaultAliceHomeName = ""
	if got := DefaultAliceHomeName(); got != ".alice" {
		t.Fatalf("unexpected fallback alice home name: %q", got)
	}
}

func TestDefaultAliceHomeName_Override(t *testing.T) {
	origin := defaultAliceHomeName
	t.Cleanup(func() {
		defaultAliceHomeName = origin
	})

	defaultAliceHomeName = ".alice-dev"
	if got := DefaultAliceHomeName(); got != ".alice-dev" {
		t.Fatalf("unexpected overridden alice home name: %q", got)
	}
}

func TestAliceHomeDir_DefaultFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvAliceHome, "")

	got := AliceHomeDir()
	want := filepath.Join(home, DefaultAliceHomeName())
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

	aliceHome := filepath.Join(home, DefaultAliceHomeName())
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
	if got := DefaultLogDir(); got != filepath.Join(aliceHome, "log") {
		t.Fatalf("unexpected default log dir path: %q", got)
	}
	if got := DefaultLogFilePath(); filepath.Dir(got) != filepath.Join(aliceHome, "log") {
		t.Fatalf("unexpected default log file dir: %q", got)
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

func TestRuntimePaths_ForAliceHomeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvAliceHome, "")

	override := "~/.alice-custom"
	aliceHome := filepath.Join(home, ".alice-custom")
	if got := ResolveAliceHomeDir(override); got != aliceHome {
		t.Fatalf("unexpected resolved alice home got=%q want=%q", got, aliceHome)
	}
	if got := ConfigPathForAliceHome(override); got != filepath.Join(aliceHome, "config.yaml") {
		t.Fatalf("unexpected config path: %q", got)
	}
	if got := WorkspaceDirForAliceHome(override); got != filepath.Join(aliceHome, "workspace") {
		t.Fatalf("unexpected workspace path: %q", got)
	}
	if got := MemoryDirForAliceHome(override); got != filepath.Join(aliceHome, "memory") {
		t.Fatalf("unexpected memory path: %q", got)
	}
	if got := PromptDirForAliceHome(override); got != filepath.Join(aliceHome, "prompts") {
		t.Fatalf("unexpected prompt path: %q", got)
	}
	if got := LogDirForAliceHome(override); got != filepath.Join(aliceHome, "log") {
		t.Fatalf("unexpected log dir path: %q", got)
	}
	logAt := time.Date(2026, 3, 14, 9, 30, 0, 0, time.Local)
	if got := LogFilePathForAliceHomeAt(override, logAt); got != filepath.Join(aliceHome, "log", "2026-03-14.log") {
		t.Fatalf("unexpected log file path: %q", got)
	}
	if got := PIDFilePathForAliceHome(override); got != filepath.Join(aliceHome, "run", "alice.pid") {
		t.Fatalf("unexpected pid path: %q", got)
	}
	if got := RuntimeBinaryPathForAliceHome(override); got != filepath.Join(aliceHome, "bin", "alice") {
		t.Fatalf("unexpected runtime binary path: %q", got)
	}
	if got := CodexHomeForAliceHome(override); got != filepath.Join(aliceHome, ".codex") {
		t.Fatalf("unexpected codex home path: %q", got)
	}
}
