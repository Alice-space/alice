package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/config"
)

func TestEnsureBundledSkillsLinked_InstallsEmbeddedSkills(t *testing.T) {
	home := t.TempDir()
	aliceHome := filepath.Join(home, ".alice")
	t.Setenv("HOME", home)
	t.Setenv(config.EnvAliceHome, aliceHome)

	report, err := EnsureBundledSkillsLinked(t.TempDir())
	if err != nil {
		t.Fatalf("sync bundled skills failed: %v", err)
	}
	if report.Discovered <= 0 {
		t.Fatalf("expected discovered skills > 0, got %+v", report)
	}
	if report.Linked <= 0 {
		t.Fatalf("expected linked skills > 0 on first sync, got %+v", report)
	}

	sourceSkillDir := filepath.Join(aliceHome, "skills", "alice-message")
	if isSymlink(t, sourceSkillDir) {
		t.Fatalf("embedded source install should create regular directory, got symlink: %s", sourceSkillDir)
	}
	if _, err := os.Stat(filepath.Join(sourceSkillDir, "SKILL.md")); err != nil {
		t.Fatalf("embedded skill manifest missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sourceSkillDir, embeddedSkillMarkerFile)); err != nil {
		t.Fatalf("embedded skill marker missing: %v", err)
	}

	agentSkillDir := filepath.Join(home, ".agents", "skills", "alice-message")
	if !isSymlink(t, agentSkillDir) {
		t.Fatalf("agents skill entry should be a symlink: %s", agentSkillDir)
	}
	assertSymlinkTarget(t, agentSkillDir, sourceSkillDir)

	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	if !isSymlink(t, claudeSkillsDir) {
		t.Fatalf("claude skills dir should be a symlink: %s", claudeSkillsDir)
	}
	assertSymlinkTarget(t, claudeSkillsDir, filepath.Join(home, ".agents", "skills"))
}

func TestEnsureBundledSkillsLinked_RejectsConflictingAgentSkillSymlink(t *testing.T) {
	home := t.TempDir()
	aliceHome := filepath.Join(home, ".alice")
	t.Setenv("HOME", home)
	t.Setenv(config.EnvAliceHome, aliceHome)

	agentSkillDir := filepath.Join(home, ".agents", "skills", "alice-message")
	if err := os.MkdirAll(filepath.Dir(agentSkillDir), 0o755); err != nil {
		t.Fatalf("create agents skills dir failed: %v", err)
	}
	legacy := t.TempDir()
	if err := os.Symlink(legacy, agentSkillDir); err != nil {
		t.Fatalf("seed legacy symlink failed: %v", err)
	}

	report, err := EnsureBundledSkillsLinked(t.TempDir())
	if err != nil {
		t.Fatalf("sync bundled skills failed: %v", err)
	}
	if report.Failed <= 0 {
		t.Fatalf("expected failed skills > 0 when conflicting symlink exists, got %+v", report)
	}
	assertSymlinkTarget(t, agentSkillDir, legacy)
}

func TestEnsureBundledSkillsLinked_KeepCustomSourceDirectory(t *testing.T) {
	home := t.TempDir()
	aliceHome := filepath.Join(home, ".alice")
	t.Setenv("HOME", home)
	t.Setenv(config.EnvAliceHome, aliceHome)

	sourceSkillDir := filepath.Join(aliceHome, "skills", "alice-message")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		t.Fatalf("create custom skill dir failed: %v", err)
	}
	custom := []byte("custom-skill\n")
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), custom, 0o644); err != nil {
		t.Fatalf("write custom skill file failed: %v", err)
	}

	firstReport, err := EnsureBundledSkillsLinked(t.TempDir())
	if err != nil {
		t.Fatalf("sync bundled skills failed: %v", err)
	}
	if firstReport.Linked <= 0 {
		t.Fatalf("expected first sync to create agent symlink, got %+v", firstReport)
	}

	raw, err := os.ReadFile(filepath.Join(sourceSkillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read custom skill file failed: %v", err)
	}
	if string(raw) != string(custom) {
		t.Fatalf("custom skill should not be overwritten, got=%q want=%q", string(raw), string(custom))
	}

	secondReport, err := EnsureBundledSkillsLinked(t.TempDir())
	if err != nil {
		t.Fatalf("second sync bundled skills failed: %v", err)
	}
	if secondReport.Unchanged <= 0 {
		t.Fatalf("expected unchanged skills > 0 on second sync, got %+v", secondReport)
	}

	assertSymlinkTarget(t, filepath.Join(home, ".agents", "skills", "alice-message"), sourceSkillDir)
}

func TestEnsureBundledSkillsLinked_RejectsConflictingClaudeSkillsDir(t *testing.T) {
	home := t.TempDir()
	aliceHome := filepath.Join(home, ".alice")
	t.Setenv("HOME", home)
	t.Setenv(config.EnvAliceHome, aliceHome)

	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claudeSkillsDir, 0o755); err != nil {
		t.Fatalf("create conflicting claude skills dir failed: %v", err)
	}

	_, err := EnsureBundledSkillsLinked(t.TempDir())
	if err == nil {
		t.Fatal("expected sync to fail when ~/.claude/skills is a real directory")
	}
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat failed path=%s err=%v", path, err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func assertSymlinkTarget(t *testing.T, linkPath, wantTarget string) {
	t.Helper()
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink failed path=%s err=%v", linkPath, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	if got, want := filepath.Clean(target), filepath.Clean(wantTarget); got != want {
		t.Fatalf("unexpected symlink target path=%s got=%q want=%q", linkPath, got, want)
	}
}
