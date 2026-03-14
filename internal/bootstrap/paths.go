package bootstrap

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func ResolveMemoryDir(workspaceDir, memoryDir string) string {
	dir := strings.TrimSpace(memoryDir)
	if dir == "" {
		return config.DefaultMemoryDir()
	}
	if filepath.IsAbs(dir) {
		return dir
	}

	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = config.DefaultWorkspaceDir()
	}
	return filepath.Join(base, dir)
}

func ResolvePromptDir(workspaceDir, promptDir string) string {
	dir := strings.TrimSpace(promptDir)
	if dir == "" {
		return config.DefaultPromptDir()
	}
	if filepath.IsAbs(dir) {
		return dir
	}

	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = config.DefaultWorkspaceDir()
	}
	return filepath.Join(base, dir)
}

func ResolveConfigPath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return config.DefaultConfigPath()
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
	if defaultBinary := config.DefaultRuntimeBinaryPath(); strings.TrimSpace(defaultBinary) != "" {
		if stat, err := os.Stat(defaultBinary); err == nil && !stat.IsDir() {
			return defaultBinary
		}
	}
	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		base = config.DefaultWorkspaceDir()
	}
	candidate := filepath.Join(base, "bin", "alice")
	if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
		return candidate
	}
	return ""
}
