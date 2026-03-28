package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	if !strings.Contains(content, "bots:") {
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
	if err := os.WriteFile(configPath, []byte("bots:\n  main:\n    feishu_app_id: \"\"\n    feishu_app_secret: \"\"\n"), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	ready, err := configHasRequiredCredentials(configPath)
	if err != nil {
		t.Fatalf("check required credentials failed: %v", err)
	}
	if ready {
		t.Fatal("expected empty credentials to be not ready")
	}

	if err := os.WriteFile(configPath, []byte("bots:\n  main:\n    feishu_app_id: \"cli_x\"\n    feishu_app_secret: \"sec\"\n"), 0o600); err != nil {
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

func TestIsHeadlessExecutable(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{
			name: "empty argv",
			argv: nil,
			want: false,
		},
		{
			name: "regular alice binary",
			argv: []string{"/tmp/bin/alice"},
			want: false,
		},
		{
			name: "headless alias",
			argv: []string{"/tmp/bin/alice-headless"},
			want: true,
		},
		{
			name: "headless suffix with extension",
			argv: []string{`C:\tmp\alice-headless.exe`},
			want: true,
		},
		{
			name: "other headless suffix",
			argv: []string{"/tmp/bin/alice-debug-headless"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHeadlessExecutable(tt.argv); got != tt.want {
				t.Fatalf("isHeadlessExecutable(%q)=%v want=%v", tt.argv, got, tt.want)
			}
		})
	}
}

func TestResolveConnectorStartupMode_RejectsMissingModeForRegularConnector(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")

	_, err := resolveConnectorStartupMode(cmd, false, false, []string{"/tmp/bin/alice"})
	if err == nil {
		t.Fatal("expected regular connector without explicit mode to be rejected")
	}
	if !strings.Contains(err.Error(), "--feishu-websocket") {
		t.Fatalf("expected mode selection guidance, got: %v", err)
	}
}

func TestResolveConnectorStartupMode_SelectsRuntimeOnly(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")
	if err := cmd.Flags().Set("runtime-only", "true"); err != nil {
		t.Fatalf("set runtime-only flag failed: %v", err)
	}

	got, err := resolveConnectorStartupMode(cmd, true, false, []string{"/tmp/bin/alice"})
	if err != nil {
		t.Fatalf("resolveConnectorStartupMode returned error: %v", err)
	}
	if !got {
		t.Fatal("expected explicit runtime-only=true to be honored")
	}
}

func TestResolveConnectorStartupMode_SelectsFeishuWebsocket(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")
	if err := cmd.Flags().Set("feishu-websocket", "true"); err != nil {
		t.Fatalf("set feishu-websocket flag failed: %v", err)
	}

	got, err := resolveConnectorStartupMode(cmd, false, true, []string{"/tmp/bin/alice"})
	if err != nil {
		t.Fatalf("resolveConnectorStartupMode returned error: %v", err)
	}
	if got {
		t.Fatal("expected --feishu-websocket to keep runtimeOnly disabled")
	}
}

func TestResolveConnectorStartupMode_RejectsBothModes(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")
	if err := cmd.Flags().Set("runtime-only", "true"); err != nil {
		t.Fatalf("set runtime-only flag failed: %v", err)
	}
	if err := cmd.Flags().Set("feishu-websocket", "true"); err != nil {
		t.Fatalf("set feishu-websocket flag failed: %v", err)
	}

	_, err := resolveConnectorStartupMode(cmd, true, true, []string{"/tmp/bin/alice"})
	if err == nil {
		t.Fatal("expected conflicting startup modes to be rejected")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected exact-one guidance, got: %v", err)
	}
}

func TestResolveConnectorStartupMode_RejectsHeadlessWithoutRuntimeOnly(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")

	_, err := resolveConnectorStartupMode(cmd, false, false, []string{"/tmp/bin/alice-headless"})
	if err == nil {
		t.Fatal("expected headless startup without --runtime-only to be rejected")
	}
	if !strings.Contains(err.Error(), "--runtime-only") {
		t.Fatalf("expected runtime-only guidance, got: %v", err)
	}
}

func TestResolveConnectorStartupMode_RejectsHeadlessFeishuWebsocket(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")
	if err := cmd.Flags().Set("feishu-websocket", "true"); err != nil {
		t.Fatalf("set feishu-websocket flag failed: %v", err)
	}

	_, err := resolveConnectorStartupMode(cmd, false, true, []string{"/tmp/bin/alice-headless"})
	if err == nil {
		t.Fatal("expected headless websocket startup to be rejected")
	}
	if !strings.Contains(err.Error(), "only supports --runtime-only") {
		t.Fatalf("expected headless runtime-only guidance, got: %v", err)
	}
}

func TestResolveConnectorStartupMode_AllowsHeadlessRuntimeOnly(t *testing.T) {
	cmd := &cobra.Command{Use: "alice"}
	cmd.Flags().Bool("runtime-only", false, "")
	cmd.Flags().Bool("feishu-websocket", false, "")
	if err := cmd.Flags().Set("runtime-only", "true"); err != nil {
		t.Fatalf("set runtime-only flag failed: %v", err)
	}

	got, err := resolveConnectorStartupMode(cmd, true, false, []string{"/tmp/bin/alice-headless"})
	if err != nil {
		t.Fatalf("resolveConnectorStartupMode returned error: %v", err)
	}
	if !got {
		t.Fatal("expected headless runtime-only start to be allowed")
	}
}
