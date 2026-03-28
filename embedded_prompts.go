package alice

import (
	"embed"
	"io/fs"
)

// PromptFS exposes the bundled prompt templates from the repository's prompts directory.
//
//go:embed all:prompts all:skills config.example.yaml SOUL.md.example
var embeddedFiles embed.FS

var PromptFS = mustSub(embeddedFiles, "prompts")
var SkillsFS = mustSub(embeddedFiles, "skills")
var ConfigExampleYAML = mustReadFile(embeddedFiles, "config.example.yaml")
var SoulExampleMarkdown = mustReadFile(embeddedFiles, "SOUL.md.example")

func mustSub(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func mustReadFile(root fs.FS, name string) []byte {
	content, err := fs.ReadFile(root, name)
	if err != nil {
		panic(err)
	}
	return content
}
