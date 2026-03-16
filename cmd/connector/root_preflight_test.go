package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigFileExists_WritesEmbeddedTemplate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "alice", "config.yaml")

	created, err := ensureConfigFileExists(configPath)
	if err != nil {
		t.Fatalf("ensure config file failed: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created on first call")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read created config failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "feishu_app_id:") {
		t.Fatalf("created config missing expected template keys, got: %q", content)
	}

	created, err = ensureConfigFileExists(configPath)
	if err != nil {
		t.Fatalf("ensure config file second call failed: %v", err)
	}
	if created {
		t.Fatal("expected second call to keep existing config")
	}
}

func TestConfigHasRequiredCredentials(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("feishu_app_id: \"\"\nfeishu_app_secret: \"\"\n"), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	ready, err := configHasRequiredCredentials(configPath)
	if err != nil {
		t.Fatalf("check required credentials failed: %v", err)
	}
	if ready {
		t.Fatal("expected empty credentials to be not ready")
	}

	if err := os.WriteFile(configPath, []byte("feishu_app_id: \"cli_x\"\nfeishu_app_secret: \"sec\"\n"), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	ready, err = configHasRequiredCredentials(configPath)
	if err != nil {
		t.Fatalf("check required credentials failed: %v", err)
	}
	if !ready {
		t.Fatal("expected non-empty credentials to be ready")
	}
}
