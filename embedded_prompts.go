package alice

import (
	"embed"
	"io/fs"
)

// PromptFS exposes the bundled prompt templates from the repository's prompts directory.
//
//go:embed prompts skills config.example.yaml
var embeddedFiles embed.FS

var PromptFS = mustSub(embeddedFiles, "prompts")
var SkillsFS = mustSub(embeddedFiles, "skills")
var ConfigExampleYAML = mustReadFile(embeddedFiles, "config.example.yaml")

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
