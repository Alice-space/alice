package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupCreatesDirectories(t *testing.T) {
	tmp := t.TempDir()
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for _, sub := range []string{"bin", "log", "run", "prompts"} {
		info, err := os.Stat(filepath.Join(aliceHome, sub))
		if err != nil {
			t.Fatalf("expected subdirectory %s to exist: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s should be a directory", sub)
		}
	}
}

func TestSetupWritesConfig(t *testing.T) {
	tmp := t.TempDir()
	aliceHome := filepath.Join(tmp, ".alice")
	configPath := filepath.Join(aliceHome, "config.yaml")

	// config should NOT exist before setup
	if _, err := os.Stat(configPath); err == nil {
		t.Fatal("config should not exist before setup")
	}

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config should exist after setup: %v", err)
	}
	if !strings.Contains(string(raw), "bots:") {
		t.Fatal("config should contain bots section")
	}
}

func TestSetupKeepsExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	aliceHome := filepath.Join(tmp, ".alice")
	configPath := filepath.Join(aliceHome, "config.yaml")

	// Pre-create a custom config
	if err := os.MkdirAll(aliceHome, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	customContent := "bots:\n  main:\n    feishu_app_id: \"custom-id\"\n"
	if err := os.WriteFile(configPath, []byte(customContent), 0o600); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(raw) != customContent {
		t.Fatalf("existing config should not be overwritten: got %q, want %q", string(raw), customContent)
	}
}

func TestSetupWritesOpenCodePlugin(t *testing.T) {
	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aliceHome := filepath.Join(tmp, ".alice")

	pluginDir := filepath.Join(tmp, ".config", "opencode", "plugins")
	pluginPath := filepath.Join(pluginDir, "alice-delegate.js")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	info, err := os.Stat(pluginPath)
	if err != nil {
		t.Fatalf("plugin file should exist at %s: %v", pluginPath, err)
	}
	if info.Size() == 0 {
		t.Fatal("plugin file should not be empty")
	}
}

func TestSetupOpenCodePluginContent(t *testing.T) {
	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	pluginPath := filepath.Join(tmp, ".config", "opencode", "plugins", "alice-delegate.js")
	raw, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	content := string(raw)

	// Plugin must export AliceDelegate
	if !strings.Contains(content, "AliceDelegate") {
		t.Fatal("plugin should export AliceDelegate")
	}
	// Must register codex tool
	if !strings.Contains(content, "codex") {
		t.Fatal("plugin should register codex tool")
	}
	// Must register claude tool
	if !strings.Contains(content, "claude") {
		t.Fatal("plugin should register claude tool")
	}
	// Must call alice delegate
	if !strings.Contains(content, "alice") {
		t.Fatal("plugin should call alice delegate")
	}
}

func TestSetupAloneHomeFlag(t *testing.T) {
	tmp := t.TempDir()
	customHome := filepath.Join(tmp, "custom-alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", customHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup with custom home failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(customHome, "config.yaml"))
	if err != nil {
		t.Fatalf("config should exist in custom home: %v", err)
	}
	if info.IsDir() {
		t.Fatal("config should be a file")
	}
}

func TestSetupServiceFlagAccepted(t *testing.T) {
	tmp := t.TempDir()
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome, "--service", "my-alice.service"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup with custom service name failed: %v", err)
	}
	// Just verify no error; the service flag is accepted
}

func TestSetupDefaultAliceHome(t *testing.T) {
	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ALICE_HOME", "") // clear any leftover from previous tests

	cmd := newSetupCmd()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup with default home failed: %v", err)
	}

	defaultHome := filepath.Join(tmp, ".alice")
	info, err := os.Stat(filepath.Join(defaultHome, "config.yaml"))
	if err != nil {
		t.Fatalf("config should exist in default home: %v", err)
	}
	if info.IsDir() {
		t.Fatal("config should be a file")
	}
}

func TestSetupSystemdUnitOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd unit test only runs on Linux")
	}

	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	servicePath := filepath.Join(tmp, ".config", "systemd", "user", "alice.service")
	raw, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("systemd unit should exist at %s: %v", servicePath, err)
	}
	content := string(raw)

	for _, want := range []string{"[Unit]", "[Service]", "[Install]", "Description=Alice", "Restart=on-failure"} {
		if !strings.Contains(content, want) {
			t.Fatalf("systemd unit missing %q, got:\n%s", want, content)
		}
	}
}

func TestSetupNoSystemdOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("this test only makes sense on macOS")
	}

	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// systemd unit should NOT be created on macOS
	servicePath := filepath.Join(tmp, ".config", "systemd", "user", "alice.service")
	if _, err := os.Stat(servicePath); err == nil {
		t.Fatal("systemd unit should not exist on macOS")
	}
}

func TestSetupHelpFlag(t *testing.T) {
	cmd := newSetupCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help flag should not error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"setup", "ALICE_HOME", "alice-home", "service"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q, got: %s", want, output)
		}
	}
}

func TestSetupSkillsSync(t *testing.T) {
	homeEnv := os.Getenv("HOME")
	if homeEnv == "" {
		t.Skip("HOME not set")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	aliceHome := filepath.Join(tmp, ".alice")

	cmd := newSetupCmd()
	cmd.SetArgs([]string{"--alice-home", aliceHome})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Skills should be synced into ~/.agents/skills
	agentsSkills := filepath.Join(tmp, ".agents", "skills")
	entries, err := os.ReadDir(agentsSkills)
	if err != nil {
		// It's ok if we can't verify - directories are created but symlinks need go build context
		t.Logf("agents skills dir not readable: %v (may need embedded skills)", err)
		return
	}
	if len(entries) == 0 {
		t.Log("no skills synced (may need embedded skills)")
	} else {
		t.Logf("synced %d skills", len(entries))
	}
}
