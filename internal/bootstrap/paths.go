package bootstrap

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Alice-space/alice/internal/runtimeapi"
)

func ResolveMemoryDir(workspaceDir, memoryDir string) string {
	dir := strings.TrimSpace(memoryDir)
	if dir == "" {
		dir = ".memory"
	}
	if filepath.IsAbs(dir) {
		return dir
	}

	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, dir)
}

func ResolvePromptDir(workspaceDir, promptDir string) string {
	dir := strings.TrimSpace(promptDir)
	if dir == "" {
		dir = "prompts"
	}
	if filepath.IsAbs(dir) {
		return dir
	}

	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, dir)
}

func ResolveConfigPath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return "config.yaml"
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return configPath
	}
	return abs
}

func ResolveRuntimeBinary(workspaceDir string) string {
	if override := strings.TrimSpace(os.Getenv(runtimeapi.EnvBin)); override != "" {
		return override
	}
	if executablePath, err := os.Executable(); err == nil && strings.TrimSpace(executablePath) != "" {
		return executablePath
	}
	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = "."
	}
	candidate := filepath.Join(base, "bin", "alice-connector")
	if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
		return candidate
	}
	return ""
}
