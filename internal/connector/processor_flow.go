package connector

import (
	"context"
	"errors"
	"strings"

	"github.com/Alice-space/alice/internal/logging"
)

func (p *Processor) processSendMessage(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPrompt(ctx, job, currentThreadID)
	reply, nextThreadID, usage, err := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		nil,
	)
	p.setThreadID(sessionKey, nextThreadID)
	p.recordSessionUsage(sessionKey, usage)
	if errors.Is(err, context.Canceled) {
		if wasStoppedByCommand(ctx) {
			logging.Infof("llm stopped by slash command event_id=%s", job.EventID)
			return JobProcessRetryAfterRestart
		}
		if wasInterruptedByNewMessage(ctx) {
			logging.Infof("llm interrupted by newer message event_id=%s", job.EventID)
			return JobProcessRetryAfterRestart
		}
		logging.Infof("llm canceled event_id=%s", job.EventID)
		return JobProcessRetryAfterRestart
	}
	if err != nil {
		logging.Errorf("llm failed event_id=%s: %v", job.EventID, err)
		reply = p.runtimeSnapshot().failureMessage
	}
	if shouldSuppressReply(job, reply) {
		reply = ""
	}

	if sendErr := p.replies.send(ctx, job, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		logging.Errorf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
	return JobProcessCompleted
}

func (p *Processor) processReplyMessage(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	ackDelivered := false
	if !job.DisableAck {
		ackDelivered = p.sendImmediateFeedback(ctx, job)
	}

	lastSentAgentMessage := ""
	sendAgentMessage := func(agentMessage string) {
		normalized := strings.TrimSpace(agentMessage)
		isFileChange := strings.HasPrefix(normalized, fileChangeEventPrefix)
		if isFileChange {
			normalized = strings.TrimSpace(strings.TrimPrefix(normalized, fileChangeEventPrefix))
		}
		if normalized == "" {
			return
		}
		if shouldSuppressReply(job, normalized) {
			return
		}
		if normalized == lastSentAgentMessage {
			return
		}
		if isFileChange {
			delivered := false
			for _, replyTarget := range fileChangeReplyTargets(job) {
				if _, sendErr := p.replies.reply(ctx, job, replyTarget, normalized); sendErr == nil {
					delivered = true
					break
				}
			}
			if !delivered {
				if sendErr := p.replies.send(ctx, job, job.ReceiveIDType, job.ReceiveID, normalized); sendErr != nil {
					logging.Errorf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
					return
				}
			}
		} else {
			messageID, sendErr := p.replies.reply(ctx, job, job.SourceMessageID, normalized)
			if sendErr != nil {
				logging.Errorf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
				return
			}
			p.rememberReplySessionMessage(job, messageID)
		}
		lastSentAgentMessage = normalized
	}

	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPrompt(ctx, job, currentThreadID)
	finalReply, nextThreadID, usage, runErr := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		sendAgentMessage,
	)
	p.setThreadID(sessionKey, nextThreadID)
	p.recordSessionUsage(sessionKey, usage)
	if errors.Is(runErr, context.Canceled) {
		if wasStoppedByCommand(ctx) {
			logging.Infof("llm stopped by slash command event_id=%s", job.EventID)
			return JobProcessRetryAfterRestart
		}
		if wasInterruptedByNewMessage(ctx) {
			notifyCtx := context.WithoutCancel(ctx)
			if ackDelivered {
				messageID, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage)
				if replyErr != nil {
					logging.Warnf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
				} else {
					p.rememberReplySessionMessage(job, messageID)
				}
			} else if messageID, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
				logging.Warnf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			} else {
				p.rememberReplySessionMessage(job, messageID)
			}
			return JobProcessRetryAfterRestart
		}
		// Parent context cancellation usually means app shutdown.
		if ctx.Err() != nil {
			logging.Debugf(
				"job state decided event_id=%s state=%s reason=context_canceled",
				job.EventID,
				JobProcessRetryAfterRestart,
			)
			return JobProcessRetryAfterRestart
		}
		notifyCtx := context.WithoutCancel(ctx)
		if ackDelivered {
			messageID, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage)
			if replyErr != nil {
				logging.Warnf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			} else {
				p.rememberReplySessionMessage(job, messageID)
			}
		} else if messageID, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
			logging.Warnf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
		} else {
			p.rememberReplySessionMessage(job, messageID)
		}
		return JobProcessRetryAfterRestart
	}
	if runErr != nil {
		logging.Errorf("llm failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.runtimeSnapshot().failureMessage
	}
	if shouldSuppressReply(job, finalReply) {
		finalReply = ""
	}
	if strings.TrimSpace(finalReply) != "" &&
		strings.TrimSpace(finalReply) != lastSentAgentMessage {
		messageID, replyErr := p.replies.reply(ctx, job, job.SourceMessageID, finalReply)
		if replyErr != nil {
			logging.Errorf("send final reply failed event_id=%s: %v", job.EventID, replyErr)
		} else {
			p.rememberReplySessionMessage(job, messageID)
		}
	}
	return JobProcessCompleted
}

func effectiveJobResponseMode(job Job) string {
	switch strings.ToLower(strings.TrimSpace(job.ResponseMode)) {
	case jobResponseModeSend:
		return jobResponseModeSend
	case jobResponseModeReply:
		return jobResponseModeReply
	default:
		if strings.TrimSpace(job.SourceMessageID) != "" {
			return jobResponseModeReply
		}
		return jobResponseModeSend
	}
}

func jobLLMRunOptions(job Job) llmRunOptions {
	return llmRunOptions{
		EventID:         job.EventID,
		Scene:           job.Scene,
		Provider:        job.LLMProvider,
		Model:           job.LLMModel,
		Profile:         job.LLMProfile,
		ReasoningEffort: job.LLMReasoningEffort,
		Personality:     job.LLMPersonality,
		NoReplyToken:    job.NoReplyToken,
		PromptPrefix:    job.LLMPromptPrefix,
	}
}

func shouldSuppressReply(job Job, reply string) bool {
	token := job.SoulDoc.OutputContract.effectiveSuppressToken(job.NoReplyToken)
	if token == "" {
		return false
	}
	return stripHiddenReplyMetadata(reply, job.SoulDoc.OutputContract) == token
}

func (p *Processor) sendImmediateFeedback(ctx context.Context, job Job) bool {
	if p == nil {
		return false
	}
	snapshot := p.runtimeSnapshot()
	if snapshot.feedbackMode == immediateFeedbackModeReaction && strings.TrimSpace(job.SourceMessageID) != "" {
		if err := p.sender.AddReaction(ctx, job.SourceMessageID, snapshot.feedbackEmoji); err == nil {
			return true
		} else {
			logging.Warnf(
				"send ack reaction failed event_id=%s message_id=%s emoji=%s: %v",
				job.EventID,
				job.SourceMessageID,
				snapshot.feedbackEmoji,
				err,
			)
		}
	}

	messageID, err := p.replies.reply(ctx, job, job.SourceMessageID, immediateFeedbackReplyText)
	if err != nil {
		logging.Warnf("send ack reply failed event_id=%s: %v", job.EventID, err)
		return false
	}
	p.rememberReplySessionMessage(job, messageID)
	return true
}

func fileChangeReplyTargets(job Job) []string {
	candidates := []string{
		job.SourceMessageID,
		job.ReplyParentMessageID,
		job.ThreadID,
		job.RootID,
	}
	targets := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		normalized := strings.TrimSpace(candidate)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		targets = append(targets, normalized)
	}
	return targets
}

func (p *Processor) rememberReplySessionMessage(job Job, messageID string) {
	if p == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	baseKey := buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	sessionKey := sessionKeyForJob(job)
	if baseKey == "" || sessionKey == "" {
		return
	}
	p.rememberSessionAliases(sessionKey, baseKey+messageAliasToken+messageID)
}
