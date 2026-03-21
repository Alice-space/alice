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
	CodexHome  string
	Discovered int
	Linked     int
	Updated    int
	Unchanged  int
	Failed     int
}

func EnsureBundledSkillsLinked(workspaceDir string) (SkillLinkReport, error) {
	_ = workspaceDir
	codexHome, err := resolveCodexHome()
	if err != nil {
		return SkillLinkReport{}, err
	}
	return EnsureBundledSkillsLinkedForCodexHome(codexHome, nil)
}

func EnsureBundledSkillsLinkedForCodexHome(codexHome string, allowedSkills []string) (SkillLinkReport, error) {
	report := SkillLinkReport{}
	entries, err := fs.ReadDir(aliceassets.SkillsFS, ".")
	if err != nil {
		return report, fmt.Errorf("read embedded bundled skills failed: %w", err)
	}

	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return report, fmt.Errorf("codex home is empty")
	}
	report.CodexHome = codexHome
	dstRoot := filepath.Join(codexHome, "skills")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return report, fmt.Errorf("create codex skills dir failed: %w", err)
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
		dst := filepath.Join(dstRoot, name)
		changed, failed := ensureEmbeddedSkillInstalled(name, dst)
		if failed {
			report.Failed++
			continue
		}
		switch changed {
		case "linked":
			report.Linked++
		case "updated":
			report.Updated++
		default:
			report.Unchanged++
		}
	}

	return report, nil
}

func resolveCodexHome() (string, error) {
	codexHome := strings.TrimSpace(os.Getenv(config.EnvCodexHome))
	if codexHome != "" {
		return codexHome, nil
	}
	return config.DefaultCodexHome(), nil
}

func hasEmbeddedSkillManifest(skillName string) bool {
	info, err := fs.Stat(aliceassets.SkillsFS, path.Join(skillName, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func ensureEmbeddedSkillInstalled(skillName, dst string) (changed string, failed bool) {
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
