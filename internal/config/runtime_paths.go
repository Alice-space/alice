package config

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvAliceHome = "ALICE_HOME"
	EnvCodexHome = "CODEX_HOME"

	defaultAliceHomeName       = ".alice"
	defaultConfigFileName      = "config.yaml"
	defaultWorkspaceDirName    = "workspace"
	defaultMemoryDirName       = "memory"
	defaultPromptDirName       = "prompts"
	defaultRunDirName          = "run"
	defaultPIDFileName         = "alice.pid"
	defaultBinaryDirName       = "bin"
	defaultConnectorBinaryName = "alice"
	defaultCodexHomeDirName    = ".codex"
)

func AliceHomeDir() string {
	if override := strings.TrimSpace(os.Getenv(EnvAliceHome)); override != "" {
		return normalizeHomePath(override)
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, defaultAliceHomeName)
	}
	if abs, absErr := filepath.Abs(defaultAliceHomeName); absErr == nil {
		return abs
	}
	return defaultAliceHomeName
}

func ResolveAliceHomeDir(override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return normalizeHomePath(override)
	}
	return AliceHomeDir()
}

func DefaultConfigPath() string {
	return ConfigPathForAliceHome("")
}

func DefaultWorkspaceDir() string {
	return WorkspaceDirForAliceHome("")
}

func DefaultMemoryDir() string {
	return MemoryDirForAliceHome("")
}

func DefaultPromptDir() string {
	return PromptDirForAliceHome("")
}

func DefaultRunDir() string {
	return RunDirForAliceHome("")
}

func DefaultPIDFilePath() string {
	return PIDFilePathForAliceHome("")
}

func DefaultRuntimeBinaryPath() string {
	return RuntimeBinaryPathForAliceHome("")
}

func DefaultCodexHome() string {
	return CodexHomeForAliceHome("")
}

func ConfigPathForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultConfigFileName)
}

func WorkspaceDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultWorkspaceDirName)
}

func MemoryDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultMemoryDirName)
}

func PromptDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultPromptDirName)
}

func RunDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultRunDirName)
}

func PIDFilePathForAliceHome(aliceHome string) string {
	return filepath.Join(RunDirForAliceHome(aliceHome), defaultPIDFileName)
}

func RuntimeBinaryPathForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultBinaryDirName, defaultConnectorBinaryName)
}

func CodexHomeForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultCodexHomeDirName)
}

func normalizeHomePath(path string) string {
	path = expandHomePrefix(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func expandHomePrefix(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			return home
		}
		return path
	}
	if !strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"+string(os.PathSeparator)))
}
