package alice

import (
	"embed"
	"io/fs"
	"log"
)

// PromptFS exposes the bundled prompt templates from the repository's prompts directory.
//
//go:embed all:prompts all:skills all:opencode-plugin config.example.yaml
var embeddedFiles embed.FS

var PromptFS fs.FS
var SkillsFS fs.FS
var ConfigExampleYAML []byte
var SoulExampleMarkdown []byte
var OpenCodePluginJS []byte
var SystemdUnitTmpl []byte

func init() {
	PromptFS = initSub("prompts")
	SkillsFS = initSub("skills")
	ConfigExampleYAML = initReadFile("config.example.yaml")
	SoulExampleMarkdown = initReadFile("prompts/SOUL.md.example")
	OpenCodePluginJS = initReadFile("opencode-plugin/delegate.js")
	SystemdUnitTmpl = initReadFile("opencode-plugin/alice.service.tmpl")
}

func initSub(dir string) fs.FS {
	sub, err := fs.Sub(embeddedFiles, dir)
	if err != nil {
		log.Fatalf("embedded: fs.Sub(%s): %v", dir, err)
	}
	return sub
}

func initReadFile(name string) []byte {
	content, err := fs.ReadFile(embeddedFiles, name)
	if err != nil {
		log.Fatalf("embedded: fs.ReadFile(%s): %v", name, err)
	}
	return content
}
