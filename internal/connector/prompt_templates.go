package connector

import (
	"fmt"

	"github.com/Alice-space/alice/internal/prompting"
)

const (
	connectorPromptCurrentUserInput = "connector/current_user_input.md.tmpl"
	connectorPromptBotSoul          = "connector/bot_soul.md.tmpl"
	connectorPromptReplyContext     = "connector/reply_context.md.tmpl"
	connectorPromptRuntimeSkillHint = "connector/runtime_skill_hint.md.tmpl"
	connectorPromptSyntheticMention = "connector/synthetic_mention.md.tmpl"
	connectorPromptBuiltinHelp      = "connector/help.md.tmpl"
)

type currentUserInputPromptData struct {
	HasIdentityContext bool
	SenderName         string
	SpeechText         string
	BaseText           string
	Attachments        []attachmentPromptData
}

type attachmentPromptData struct {
	Index         int
	Kind          string
	FileName      string
	ImageKey      string
	FileKey       string
	LocalPath     string
	DownloadError string
}

func renderPromptFile(loader *prompting.Loader, name string, data any) (string, error) {
	if loader == nil {
		loader = prompting.DefaultLoader()
	}
	if loader == nil {
		return "", fmt.Errorf("prompt loader is nil for template %q", name)
	}
	return loader.RenderFile(name, data)
}

func (p *Processor) renderPromptFile(name string, data any) (string, error) {
	return renderPromptFile(p.prompts, name, data)
}

func (a *App) renderPromptFile(name string, data any) (string, error) {
	return renderPromptFile(a.prompts, name, data)
}
