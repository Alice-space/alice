package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadSingleBotRuntime(t *testing.T, botBody string, rootBody ...string) (Config, Config) {
	t.Helper()
	path := writeSingleBotConfig(t, botBody, rootBody...)
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	runtime, err := cfg.RuntimeConfigForBot("main")
	if err != nil {
		t.Fatalf("resolve runtime failed: %v", err)
	}
	return cfg, runtime
}

func writeSingleBotConfig(t *testing.T, botBody string, rootBody ...string) string {
	t.Helper()
	builder := strings.Builder{}
	for _, block := range rootBody {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		builder.WriteString(trimmed)
		builder.WriteString("\n")
	}
	builder.WriteString("bots:\n")
	builder.WriteString("  main:\n")
	builder.WriteString(indentYAML(strings.TrimSpace(botBody), "    "))
	builder.WriteString("\n")
	return writeConfigFile(t, builder.String())
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	return path
}

func indentYAML(content, prefix string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for idx, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[idx] = prefix
			continue
		}
		lines[idx] = prefix + line
	}
	return strings.Join(lines, "\n")
}
