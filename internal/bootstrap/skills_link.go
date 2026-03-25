package bootstrap

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	aliceassets "github.com/Alice-space/alice"
	"github.com/Alice-space/alice/internal/config"
)

const embeddedSkillMarkerFile = ".alice-embedded-skill"

type SkillLinkReport struct {
	AliceHome       string
	SourceRoot      string
	AgentsSkillsDir string
	ClaudeSkillsDir string
	Discovered      int
	Linked          int
	Updated         int
	Unchanged       int
	Failed          int
}

func EnsureBundledSkillsLinked(workspaceDir string) (SkillLinkReport, error) {
	_ = workspaceDir
	return EnsureBundledSkillsLinkedForAliceHome(config.AliceHomeDir(), nil)
}

func EnsureBundledSkillsLinkedForAliceHome(aliceHome string, allowedSkills []string) (SkillLinkReport, error) {
	report := SkillLinkReport{}
	entries, err := fs.ReadDir(aliceassets.SkillsFS, ".")
	if err != nil {
		return report, fmt.Errorf("read embedded bundled skills failed: %w", err)
	}

	aliceHome = config.ResolveAliceHomeDir(aliceHome)
	if strings.TrimSpace(aliceHome) == "" {
		return report, fmt.Errorf("alice home is empty")
	}
	report.AliceHome = aliceHome
	report.SourceRoot = config.BundledSkillSourceDirForAliceHome(aliceHome)
	report.AgentsSkillsDir = config.DefaultAgentsSkillsDir()
	report.ClaudeSkillsDir = config.DefaultClaudeSkillsDir()

	if err := ensureDirectoryRoot(report.SourceRoot); err != nil {
		return report, fmt.Errorf("prepare bundled skill source dir failed: %w", err)
	}
	if err := ensureDirectoryRoot(report.AgentsSkillsDir); err != nil {
		return report, fmt.Errorf("prepare agents skills dir failed: %w", err)
	}

	allowed := make(map[string]struct{}, len(allowedSkills))
	for _, skill := range allowedSkills {
		trimmed := strings.TrimSpace(skill)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}

	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !hasEmbeddedSkillManifest(name) {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}

		report.Discovered++
		sourceDir := filepath.Join(report.SourceRoot, name)
		sourceChanged, failed := ensureEmbeddedSkillMaterialized(name, sourceDir)
		if failed {
			report.Failed++
			continue
		}
		agentSkillDir := filepath.Join(report.AgentsSkillsDir, name)
		linkChanged, failed := ensureSkillSymlink(agentSkillDir, sourceDir)
		if failed {
			report.Failed++
			continue
		}

		switch classifySkillSync(sourceChanged, linkChanged) {
		case "linked":
			report.Linked++
		case "updated":
			report.Updated++
		default:
			report.Unchanged++
		}
	}

	if err := ensureClaudeSkillsAlias(report.ClaudeSkillsDir, report.AgentsSkillsDir); err != nil {
		return report, fmt.Errorf("prepare claude skills link failed: %w", err)
	}

	return report, nil
}

func hasEmbeddedSkillManifest(skillName string) bool {
	info, err := fs.Stat(aliceassets.SkillsFS, path.Join(skillName, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func ensureEmbeddedSkillMaterialized(skillName, dst string) (changed string, failed bool) {
	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			if installErr := materializeEmbeddedSkill(skillName, dst); installErr != nil {
				return "", true
			}
			return "linked", false
		}
		return "", true
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return "", true
	}

	if !info.IsDir() {
		return "", true
	}

	if hasEmbeddedMarker(dst) {
		if removeErr := os.RemoveAll(dst); removeErr != nil {
			return "", true
		}
		if installErr := materializeEmbeddedSkill(skillName, dst); installErr != nil {
			return "", true
		}
		return "updated", false
	}

	return "unchanged", false
}

func materializeEmbeddedSkill(skillName, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	err := fs.WalkDir(aliceassets.SkillsFS, skillName, func(srcPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == skillName {
			return nil
		}

		rel := strings.TrimPrefix(srcPath, skillName+"/")
		if strings.TrimSpace(rel) == "" {
			return fmt.Errorf("resolve embedded skill path failed skill=%s src=%s", skillName, srcPath)
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		content, err := fs.ReadFile(aliceassets.SkillsFS, srcPath)
		if err != nil {
			return err
		}
		mode := filePerm(entry)
		if writeErr := os.WriteFile(target, content, mode); writeErr != nil {
			return writeErr
		}
		return nil
	})
	if err != nil {
		return err
	}

	markerPath := filepath.Join(dst, embeddedSkillMarkerFile)
	return os.WriteFile(markerPath, []byte("alice-embedded-skill\n"), 0o644)
}

func filePerm(entry fs.DirEntry) os.FileMode {
	if entry == nil {
		return 0o644
	}
	info, err := entry.Info()
	if err != nil {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".sh") {
			return 0o755
		}
		return 0o644
	}
	mode := os.FileMode(info.Mode().Perm())
	if mode == 0 {
		mode = 0o644
	}
	if strings.HasSuffix(strings.ToLower(entry.Name()), ".sh") && mode&0o111 == 0 {
		mode |= 0o111
	}
	return mode
}

func hasEmbeddedMarker(dst string) bool {
	info, err := os.Stat(filepath.Join(dst, embeddedSkillMarkerFile))
	return err == nil && !info.IsDir()
}

func ensureDirectoryRoot(dst string) error {
	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dst, 0o755)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a directory, got symlink", dst)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a directory", dst)
	}
	return nil
}

func ensureSkillSymlink(dst, target string) (changed string, failed bool) {
	if err := ensureDirectoryRoot(filepath.Dir(dst)); err != nil {
		return "", true
	}

	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			if linkErr := os.Symlink(target, dst); linkErr != nil {
				return "", true
			}
			return "linked", false
		}
		return "", true
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", true
	}
	same, err := symlinkPointsTo(dst, target)
	if err != nil || !same {
		return "", true
	}
	return "unchanged", false
}

func ensureClaudeSkillsAlias(dst, target string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return os.Symlink(target, dst)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s already exists and is not a symlink", dst)
	}
	same, err := symlinkPointsTo(dst, target)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("%s already points elsewhere", dst)
	}
	return nil
}

func symlinkPointsTo(linkPath, target string) (bool, error) {
	current, err := os.Readlink(linkPath)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(current) {
		current = filepath.Join(filepath.Dir(linkPath), current)
	}
	return filepath.Clean(current) == filepath.Clean(target), nil
}

func classifySkillSync(sourceChanged, linkChanged string) string {
	if linkChanged == "linked" {
		return "linked"
	}
	if sourceChanged == "linked" || sourceChanged == "updated" {
		return "updated"
	}
	return "unchanged"
}
