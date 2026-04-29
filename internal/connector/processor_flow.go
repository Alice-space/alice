package connector

import (
	"context"
	"errors"
	"strings"
	"time"

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
		p.jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		nil,
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

	messageID, sendErr := p.replies.send(ctx, job, job.ReceiveIDType, job.ReceiveID, reply)
	if sendErr != nil {
		logging.Errorf("send message failed event_id=%s: %v", job.EventID, sendErr)
	} else {
		p.markFinalReplyDone(ctx, job, messageID)
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
	lastSentAgentReplyMessageID := ""
	sendAgentMessage := func(agentMessage string) {
		normalized := strings.TrimSpace(agentMessage)
		if normalized == "" {
			return
		}
		if strings.HasPrefix(normalized, fileChangeEventPrefix) {
			return
		}
		if shouldSuppressReply(job, normalized) {
			return
		}
		if normalized == lastSentAgentMessage {
			return
		}
		messageID, sendErr := p.replies.reply(ctx, job, job.SourceMessageID, normalized)
		if sendErr != nil {
			logging.Errorf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
			return
		}
		p.rememberReplySessionMessage(job, messageID)
		lastSentAgentReplyMessageID = messageID
		lastSentAgentMessage = normalized
	}

	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPrompt(ctx, job, currentThreadID)
	heartbeat := p.startLLMHeartbeat(ctx, job)
	stopHeartbeat := func(state string) {
		if heartbeat == nil {
			return
		}
		notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		heartbeat.Stop(notifyCtx, state)
	}
	finalReply, nextThreadID, usage, runErr := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		p.jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		sendAgentMessage,
		heartbeat,
	)
	p.setThreadID(sessionKey, nextThreadID)
	p.recordSessionUsage(sessionKey, usage)
	if errors.Is(runErr, context.Canceled) {
		if wasStoppedByCommand(ctx) {
			logging.Infof("llm stopped by slash command event_id=%s", job.EventID)
			stopHeartbeat("已中断")
			return JobProcessRetryAfterRestart
		}
		if wasInterruptedByNewMessage(ctx) {
			stopHeartbeat("已中断")
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
			stopHeartbeat("已中断")
			return JobProcessRetryAfterRestart
		}
		stopHeartbeat("已中断")
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
		stopHeartbeat("失败")
	} else {
		stopHeartbeat("已完成")
	}
	if shouldSuppressReply(job, finalReply) {
		finalReply = ""
	}
	finalMessageID := ""
	if strings.TrimSpace(finalReply) != "" {
		if strings.TrimSpace(finalReply) == lastSentAgentMessage {
			finalMessageID = lastSentAgentReplyMessageID
		} else {
			messageID, replyErr := p.replies.reply(ctx, job, job.SourceMessageID, finalReply)
			if replyErr != nil {
				logging.Errorf("send final reply failed event_id=%s: %v", job.EventID, replyErr)
			} else {
				p.rememberReplySessionMessage(job, messageID)
				finalMessageID = messageID
			}
		}
		p.markFinalReplyDone(ctx, job, finalMessageID)
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

func (p *Processor) jobLLMRunOptions(job Job) llmRunOptions {
	return llmRunOptions{
		EventID:         job.EventID,
		Scene:           job.Scene,
		Provider:        job.LLMProvider,
		Model:           job.LLMModel,
		Profile:         job.LLMProfile,
		ReasoningEffort: job.LLMReasoningEffort,
		Variant:         job.LLMVariant,
		Personality:     job.LLMPersonality,
		NoReplyToken:    job.NoReplyToken,
		PromptPrefix:    job.LLMPromptPrefix,
		WorkDir:         p.getSessionWorkDir(sessionKeyForJob(job)),
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

func (p *Processor) markFinalReplyDone(ctx context.Context, job Job, messageID string) {
	if p == nil || p.sender == nil {
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	if err := p.sender.AddReaction(ctx, messageID, finalReplyDoneEmoji); err != nil {
		logging.Warnf(
			"send final reply reaction failed event_id=%s message_id=%s emoji=%s: %v",
			job.EventID,
			messageID,
			finalReplyDoneEmoji,
			err,
		)
	}
}
