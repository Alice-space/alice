package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
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

func ResolveMCPServerCommand(configAbsPath string) string {
	if executablePath, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(executablePath), "alice-mcp-server")
		if stat, statErr := os.Stat(sibling); statErr == nil && !stat.IsDir() {
			return sibling
		}
	}
	configDir := filepath.Dir(strings.TrimSpace(configAbsPath))
	if configDir == "" {
		configDir = "."
	}
	return filepath.Join(configDir, "bin", "alice-mcp-server")
}
