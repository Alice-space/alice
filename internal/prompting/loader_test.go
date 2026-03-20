package prompting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderRenderFileFallsBackToEmbeddedPrompts(t *testing.T) {
	loader := NewLoader(filepath.Join(t.TempDir(), "missing-prompts"))

	got, err := loader.RenderFile("llm/initial_prompt.md.tmpl", map[string]any{
		"Resume":       false,
		"ThreadID":     "",
		"PromptPrefix": "",
		"UserText":     "hi from embed",
	})
	if err != nil {
		t.Fatalf("render embedded prompt failed: %v", err)
	}
	if got != "hi from embed" {
		t.Fatalf("unexpected rendered prompt, got=%q want=%q", got, "hi from embed")
	}
}

func TestLoaderRenderFilePrefersFilesystemOverride(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "llm", "initial_prompt.md.tmpl")
	if err := os.MkdirAll(filepath.Dir(templatePath), 0o755); err != nil {
		t.Fatalf("create prompt dir failed: %v", err)
	}
	if err := os.WriteFile(templatePath, []byte("override: {{ .UserText }}"), 0o644); err != nil {
		t.Fatalf("write prompt template failed: %v", err)
	}

	loader := NewLoader(root)
	got, err := loader.RenderFile("llm/initial_prompt.md.tmpl", map[string]any{
		"UserText": "hello",
	})
	if err != nil {
		t.Fatalf("render overridden prompt failed: %v", err)
	}
	if got != "override: hello" {
		t.Fatalf("unexpected rendered prompt, got=%q want=%q", got, "override: hello")
	}
}

func TestLoaderRejectsEscapingTemplateNames(t *testing.T) {
	loader := NewLoader("")

	_, err := loader.RenderFile("../llm/initial_prompt.md.tmpl", nil)
	if err == nil {
		t.Fatal("expected path traversal error")
	}
	if !strings.Contains(err.Error(), "escapes embedded prompt root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComposePromptPrefix_IgnoresPersonalityText(t *testing.T) {
	loader := NewLoader(filepath.Join("..", "..", "prompts"))

	got, err := ComposePromptPrefix(loader, "你是 Alice。", "friendly", "[[NO_REPLY]]")
	if err != nil {
		t.Fatalf("compose prompt prefix failed: %v", err)
	}
	if got != "你是 Alice。" {
		t.Fatalf("unexpected composed prompt: %q", got)
	}
}
