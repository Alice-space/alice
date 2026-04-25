package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	aliceassets "github.com/Alice-space/alice"
)

func TestEnsureBotSoulFile_CreatesEmbeddedTemplate(t *testing.T) {
	soulPath := filepath.Join(t.TempDir(), "workspace", "SOUL.md")

	report, err := EnsureBotSoulFile(soulPath)
	if err != nil {
		t.Fatalf("ensure bot soul failed: %v", err)
	}
	if !report.Created {
		t.Fatal("expected soul template to be created")
	}
	if report.Path != soulPath {
		t.Fatalf("unexpected report path: %q", report.Path)
	}

	content, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("read soul template failed: %v", err)
	}
	if string(content) != string(aliceassets.SoulExampleMarkdown) {
		t.Fatalf("unexpected soul template content: %q", string(content))
	}
}

func TestEnsureBotSoulFile_PreservesExistingFile(t *testing.T) {
	soulPath := filepath.Join(t.TempDir(), "workspace", "SOUL.md")
	if err := os.MkdirAll(filepath.Dir(soulPath), 0o750); err != nil {
		t.Fatalf("create workspace dir failed: %v", err)
	}
	if err := os.WriteFile(soulPath, []byte("custom soul"), 0o644); err != nil {
		t.Fatalf("write custom soul failed: %v", err)
	}

	report, err := EnsureBotSoulFile(soulPath)
	if err != nil {
		t.Fatalf("ensure bot soul failed: %v", err)
	}
	if report.Created {
		t.Fatal("expected existing soul file to be preserved")
	}

	content, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("read custom soul failed: %v", err)
	}
	if string(content) != "custom soul" {
		t.Fatalf("unexpected soul content: %q", string(content))
	}
}

func TestEnsureBotSoulFile_RejectsDirectoryPath(t *testing.T) {
	soulPath := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(soulPath, 0o750); err != nil {
		t.Fatalf("create directory failed: %v", err)
	}

	_, err := EnsureBotSoulFile(soulPath)
	if err == nil {
		t.Fatal("expected directory soul path to fail")
	}
}
