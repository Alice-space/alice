package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	EnvAliceHome = "ALICE_HOME"
	EnvCodexHome = "CODEX_HOME"

	defaultConfigFileName      = "config.yaml"
	defaultWorkspaceDirName    = "workspace"
	defaultPromptDirName       = "prompts"
	defaultSkillDirName        = "skills"
	defaultLogDirName          = "log"
	defaultRunDirName          = "run"
	defaultPIDFileName         = "alice.pid"
	defaultBinaryDirName       = "bin"
	defaultConnectorBinaryName = "alice"
	defaultCodexHomeDirName    = ".codex"
	defaultAgentsHomeDirName   = ".agents"
	defaultClaudeHomeDirName   = ".claude"
)

// defaultAliceHomeName is intentionally mutable via -ldflags -X.
// Release builds should keep ".alice"; dev builds can set ".alice-dev".
var defaultAliceHomeName = ".alice"

func DefaultAliceHomeName() string {
	name := strings.TrimSpace(defaultAliceHomeName)
	if name == "" {
		return ".alice"
	}
	return name
}

func AliceHomeDir() string {
	if override := strings.TrimSpace(os.Getenv(EnvAliceHome)); override != "" {
		return normalizeHomePath(override)
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, DefaultAliceHomeName())
	}
	if abs, absErr := filepath.Abs(DefaultAliceHomeName()); absErr == nil {
		return abs
	}
	return DefaultAliceHomeName()
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

func DefaultPromptDir() string {
	return PromptDirForAliceHome("")
}

func DefaultBundledSkillSourceDir() string {
	return BundledSkillSourceDirForAliceHome("")
}

func DefaultAgentsSkillsDir() string {
	return filepath.Join(defaultUserHomeScopedDir(defaultAgentsHomeDirName), defaultSkillDirName)
}

func DefaultClaudeSkillsDir() string {
	return filepath.Join(defaultUserHomeScopedDir(defaultClaudeHomeDirName), defaultSkillDirName)
}

func DefaultRunDir() string {
	return RunDirForAliceHome("")
}

func DefaultLogDir() string {
	return LogDirForAliceHome("")
}

func DefaultLogFilePath() string {
	return LogFilePathForAliceHome("")
}

func DefaultPIDFilePath() string {
	return PIDFilePathForAliceHome("")
}

func DefaultRuntimeBinaryPath() string {
	return RuntimeBinaryPathForAliceHome("")
}

func DefaultCodexHome() string {
	if override := strings.TrimSpace(os.Getenv(EnvCodexHome)); override != "" {
		return normalizeHomePath(override)
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, defaultCodexHomeDirName)
	}
	if abs, absErr := filepath.Abs(defaultCodexHomeDirName); absErr == nil {
		return abs
	}
	return defaultCodexHomeDirName
}

func ResolveCodexHomeDir(override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return normalizeHomePath(override)
	}
	return DefaultCodexHome()
}

func ConfigPathForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultConfigFileName)
}

func WorkspaceDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultWorkspaceDirName)
}

func PromptDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultPromptDirName)
}

func BundledSkillSourceDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultSkillDirName)
}

func RunDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultRunDirName)
}

func SoulPathForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), "SOUL.md")
}

func LogDirForAliceHome(aliceHome string) string {
	return filepath.Join(ResolveAliceHomeDir(aliceHome), defaultLogDirName)
}

func LogFilePathForAliceHome(aliceHome string) string {
	return LogFilePathForAliceHomeAt(aliceHome, time.Now())
}

func LogFilePathForAliceHomeAt(aliceHome string, at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	at = at.Local()
	return filepath.Join(LogDirForAliceHome(aliceHome), at.Format("2006-01-02")+".log")
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

func defaultUserHomeScopedDir(name string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, name)
	}
	if abs, absErr := filepath.Abs(name); absErr == nil {
		return abs
	}
	return name
}
