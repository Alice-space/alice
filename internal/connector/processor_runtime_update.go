package connector

import (
	llm "github.com/Alice-space/alice/internal/llm"
)

type ProcessorRuntimeUpdate struct {
	Backend                llm.Backend
	FailureMessage         string
	ThinkingMessage        string
	ImmediateFeedbackMode  string
	ImmediateFeedbackEmoji string
}

func (p *Processor) UpdateRuntimeConfig(update ProcessorRuntimeUpdate) error {
	if p == nil {
		return nil
	}
	if update.Backend != nil {
		p.SetLLMBackend(update.Backend)
	}
	p.SetReplyMessages(update.FailureMessage, update.ThinkingMessage)
	p.SetImmediateFeedback(update.ImmediateFeedbackMode, update.ImmediateFeedbackEmoji)
	return nil
}
