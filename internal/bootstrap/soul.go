package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	aliceassets "github.com/Alice-space/alice"
)

type SoulTemplateReport struct {
	Path    string
	Created bool
}

func EnsureBotSoulFile(soulPath string) (SoulTemplateReport, error) {
	soulPath = strings.TrimSpace(soulPath)
	report := SoulTemplateReport{Path: soulPath}
	if soulPath == "" {
		return report, fmt.Errorf("soul path is empty")
	}

	soulPath = filepath.Clean(soulPath)
	report.Path = soulPath
	info, err := os.Stat(soulPath)
	switch {
	case err == nil:
		if info.IsDir() {
			return report, fmt.Errorf("soul path is a directory: %s", soulPath)
		}
		return report, nil
	case !os.IsNotExist(err):
		return report, fmt.Errorf("stat soul path failed: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(soulPath), 0o750); err != nil {
		return report, fmt.Errorf("create soul parent directory failed: %w", err)
	}
	if err := os.WriteFile(soulPath, aliceassets.SoulExampleMarkdown, 0o644); err != nil {
		return report, fmt.Errorf("write soul template failed: %w", err)
	}
	report.Created = true
	return report, nil
}
