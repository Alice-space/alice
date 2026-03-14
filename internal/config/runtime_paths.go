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
		override = expandHomePrefix(override)
		if filepath.IsAbs(override) {
			return filepath.Clean(override)
		}
		if abs, err := filepath.Abs(override); err == nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(override)
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

func DefaultConfigPath() string {
	return filepath.Join(AliceHomeDir(), defaultConfigFileName)
}

func DefaultWorkspaceDir() string {
	return filepath.Join(AliceHomeDir(), defaultWorkspaceDirName)
}

func DefaultMemoryDir() string {
	return filepath.Join(AliceHomeDir(), defaultMemoryDirName)
}

func DefaultPromptDir() string {
	return filepath.Join(AliceHomeDir(), defaultPromptDirName)
}

func DefaultRunDir() string {
	return filepath.Join(AliceHomeDir(), defaultRunDirName)
}

func DefaultPIDFilePath() string {
	return filepath.Join(DefaultRunDir(), defaultPIDFileName)
}

func DefaultRuntimeBinaryPath() string {
	return filepath.Join(AliceHomeDir(), defaultBinaryDirName, defaultConnectorBinaryName)
}

func DefaultCodexHome() string {
	return filepath.Join(AliceHomeDir(), defaultCodexHomeDirName)
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
