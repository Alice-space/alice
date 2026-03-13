package connector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/codearmy"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
)

type Processor struct {
	llm             llm.Backend
	memory          MemoryManager
	sender          Sender
	replies         *replyDispatcher
	failureMessage  string
	thinkingMessage string
	feedbackMode    string
	feedbackEmoji   string
	mu              sync.Mutex
	sessions        map[string]sessionState
	stateFilePath   string
	stateVersion    uint64
	flushedVersion  uint64
	now             func() time.Time
	runtimeAPIBase  string
	runtimeAPIToken string
	codeArmyStatus  *codearmy.Inspector
	automationStore *automation.Store
}

const interruptedReplyMessage = "已收到你的新消息，当前回复已中断并切换到最新输入。"
const restartNotificationMessage = "Alice已重新启动"
const fileChangeEventPrefix = "[file_change] "
const immediateFeedbackReplyText = "收到！"
const immediateFeedbackModeReply = "reply"
const immediateFeedbackModeReaction = "reaction"
const defaultImmediateFeedbackEmoji = "SMILE"
const idleSummaryPrompt = "请基于当前会话上下文，提炼后续仍有价值的信息摘要。\n" +
	"要求：\n" +
	"1. 只提炼：事实、约束、决策、待办、偏好变化。\n" +
	"2. 不包含：寒暄、一次性执行细节、敏感信息。\n" +
	"3. 输出 5-12 条短要点；若无重要信息仅输出“无重要新增信息”。"

var selfUpdateIntentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)self[\s_-]*update`),
	regexp.MustCompile(`(?i)update[\s_-]*self`),
	regexp.MustCompile(`(?i)update.*restart`),
	regexp.MustCompile(`(?i)restart.*(self|yourself|bot)`),
	regexp.MustCompile(`更新.*重启`),
	regexp.MustCompile(`重启.*(自己|你自己)`),
	regexp.MustCompile(`(?i)update-self-and-sync-skill\.sh`),
}

func NewProcessor(
	backend llm.Backend,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	return NewProcessorWithMemory(backend, sender, failureMessage, thinkingMessage, nil)
}

func NewProcessorWithMemory(
	backend llm.Backend,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
	memoryManager MemoryManager,
) *Processor {
	return &Processor{
		llm:             backend,
		memory:          memoryManager,
		sender:          sender,
		replies:         newReplyDispatcher(sender),
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
		feedbackMode:    immediateFeedbackModeReply,
		feedbackEmoji:   defaultImmediateFeedbackEmoji,
		sessions:        make(map[string]sessionState),
		now:             time.Now,
	}
}

func (p *Processor) SetImmediateFeedback(mode, emojiType string) {
	if p == nil {
		return
	}
	p.feedbackMode = normalizeImmediateFeedbackMode(mode)
	p.feedbackEmoji = normalizeImmediateFeedbackEmoji(emojiType)
}

func (p *Processor) SetRuntimeAPI(baseURL, token string) {
	if p == nil {
		return
	}
	p.runtimeAPIBase = strings.TrimSpace(baseURL)
	p.runtimeAPIToken = strings.TrimSpace(token)
}

func (p *Processor) ProcessJob(ctx context.Context, job Job) bool {
	return p.ProcessJobState(ctx, job) == JobProcessCompleted
}

func (p *Processor) ProcessJobState(ctx context.Context, job Job) JobProcessState {
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)
	p.enrichJobUserNames(ctx, &job)
	if job.WorkflowPhase == jobWorkflowPhaseRestartNotification {
		return p.processRestartNotification(ctx, job)
	}
	if job.WorkflowPhase == jobWorkflowPhasePostRestartFinalize {
		return p.processPostRestartFinalize(ctx, job)
	}
	if handled, state := p.processBuiltinCommand(ctx, job); handled {
		return state
	}

	sessionKey := sessionKeyForJob(job)
	p.rememberSessionScope(sessionKey, memoryScopeKeyForJob(job))
	p.touchSessionMessage(sessionKey, p.now())

	logging.Debugf(
		"process job start event_id=%s receive_id_type=%s receive_id=%s source_message_id=%s message_type=%s text=%q attachments=%d",
		job.EventID,
		job.ReceiveIDType,
		job.ReceiveID,
		job.SourceMessageID,
		job.MessageType,
		job.Text,
		len(job.Attachments),
	)
	if strings.TrimSpace(job.SourceMessageID) != "" {
		return p.processReplyMessage(ctx, job)
	}

	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPromptWithMemory(ctx, job, currentThreadID)
	reply, nextThreadID, err := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		p.buildLLMRunEnv(job),
		nil,
	)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(err, context.Canceled) {
		if wasInterruptedByNewMessage(ctx) {
			logging.Infof("llm interrupted by newer message event_id=%s", job.EventID)
			logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
			return JobProcessRetryAfterRestart
		}
		if ctx.Err() != nil && isRestartIntentJob(job) {
			logging.Debugf(
				"job state decided event_id=%s state=%s reason=shutdown_restart_intent",
				job.EventID,
				JobProcessPostRestartFinalize,
			)
			return JobProcessPostRestartFinalize
		}
		logging.Infof("llm canceled event_id=%s", job.EventID)
		logging.Debugf("memory update skipped event_id=%s changed=false reason=llm_canceled", job.EventID)
		return JobProcessRetryAfterRestart
	}
	failed := err != nil
	if err != nil {
		logging.Errorf("llm failed event_id=%s: %v", job.EventID, err)
		reply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), reply, failed)

	if sendErr := p.replies.send(ctx, job, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		logging.Errorf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
	return JobProcessCompleted
}

func (p *Processor) processReplyMessage(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	p.rememberSessionScope(sessionKey, memoryScopeKeyForJob(job))
	ackDelivered := p.sendImmediateFeedback(ctx, job)

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
			if _, sendErr := p.replies.reply(ctx, job, job.SourceMessageID, normalized); sendErr != nil {
				logging.Errorf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
				return
			}
		}
		lastSentAgentMessage = normalized
	}

	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPromptWithMemory(ctx, job, currentThreadID)
	finalReply, nextThreadID, runErr := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		p.buildLLMRunEnv(job),
		sendAgentMessage,
	)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(runErr, context.Canceled) {
		if wasInterruptedByNewMessage(ctx) {
			notifyCtx := context.WithoutCancel(ctx)
			if ackDelivered {
				if _, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
					logging.Warnf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
				}
			} else if _, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
				logging.Warnf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			}
			logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
			return JobProcessRetryAfterRestart
		}
		// Parent context cancellation usually means app shutdown.
		if ctx.Err() != nil {
			if isRestartIntentJob(job) {
				logging.Debugf(
					"job state decided event_id=%s state=%s reason=shutdown_restart_intent",
					job.EventID,
					JobProcessPostRestartFinalize,
				)
				return JobProcessPostRestartFinalize
			}
			logging.Debugf(
				"job state decided event_id=%s state=%s reason=context_canceled",
				job.EventID,
				JobProcessRetryAfterRestart,
			)
			return JobProcessRetryAfterRestart
		}
		notifyCtx := context.WithoutCancel(ctx)
		if ackDelivered {
			if _, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
				logging.Warnf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			}
		} else if _, replyErr := p.replies.reply(notifyCtx, job, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
			logging.Warnf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
		}
		logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
		return JobProcessRetryAfterRestart
	}
	failed := runErr != nil
	if failed {
		logging.Errorf("llm failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), finalReply, failed)
	if strings.TrimSpace(finalReply) != "" &&
		strings.TrimSpace(finalReply) != lastSentAgentMessage {
		if _, replyErr := p.replies.reply(ctx, job, job.SourceMessageID, finalReply); replyErr != nil {
			logging.Errorf("send final reply failed event_id=%s: %v", job.EventID, replyErr)
		}
	}
	return JobProcessCompleted
}

func (p *Processor) sendImmediateFeedback(ctx context.Context, job Job) bool {
	if p == nil {
		return false
	}
	if p.feedbackMode == immediateFeedbackModeReaction && strings.TrimSpace(job.SourceMessageID) != "" {
		if err := p.sender.AddReaction(ctx, job.SourceMessageID, p.feedbackEmoji); err == nil {
			return true
		} else {
			logging.Warnf("send ack reaction failed event_id=%s message_id=%s emoji=%s: %v", job.EventID, job.SourceMessageID, p.feedbackEmoji, err)
		}
	}

	if _, err := p.replies.reply(ctx, job, job.SourceMessageID, immediateFeedbackReplyText); err != nil {
		logging.Warnf("send ack reply failed event_id=%s: %v", job.EventID, err)
		return false
	}
	return true
}

func normalizeImmediateFeedbackMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case immediateFeedbackModeReaction:
		return immediateFeedbackModeReaction
	case immediateFeedbackModeReply:
		fallthrough
	default:
		return immediateFeedbackModeReply
	}
}

func normalizeImmediateFeedbackEmoji(raw string) string {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	if normalized == "" {
		return defaultImmediateFeedbackEmoji
	}
	return normalized
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

func isRestartIntentJob(job Job) bool {
	candidates := []string{
		strings.TrimSpace(job.Text),
		strings.TrimSpace(job.RawContent),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		for _, pattern := range selfUpdateIntentPatterns {
			if pattern.MatchString(candidate) {
				return true
			}
		}
	}
	return false
}

func (p *Processor) processRestartNotification(ctx context.Context, job Job) JobProcessState {
	sendErr := p.replies.respond(ctx, job, restartNotificationMessage)
	if sendErr != nil {
		logging.Warnf("send restart notification failed event_id=%s: %v", job.EventID, sendErr)
		logging.Debugf(
			"job state decided event_id=%s state=%s reason=restart_notification_send_failed",
			job.EventID,
			JobProcessRetryAfterRestart,
		)
		return JobProcessRetryAfterRestart
	}

	p.recordInteraction(job, p.buildCurrentUserInput(job), restartNotificationMessage, false)
	logging.Debugf(
		"job state decided event_id=%s state=%s reason=restart_notification_completed",
		job.EventID,
		JobProcessCompleted,
	)
	return JobProcessCompleted
}

func (p *Processor) processPostRestartFinalize(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	threadID := strings.TrimSpace(p.getThreadID(sessionKey))
	now := p.now()
	pid := os.Getpid()

	summary := fmt.Sprintf(
		"重启操作已完成，并已在重启后自检通过。\n时间：%s\n进程：PID=%d\n会话：%s\n线程：%s",
		now.Format(time.RFC3339),
		pid,
		sessionKey,
		defaultIfEmpty(threadID, "无"),
	)

	sendErr := p.replies.respond(ctx, job, summary)
	if sendErr != nil {
		logging.Warnf("send post-restart finalize reply failed event_id=%s: %v", job.EventID, sendErr)
		logging.Debugf(
			"job state decided event_id=%s state=%s reason=post_restart_finalize_send_failed",
			job.EventID,
			JobProcessRetryAfterRestart,
		)
		return JobProcessRetryAfterRestart
	}

	p.recordInteraction(job, p.buildCurrentUserInput(job), summary, false)
	logging.Debugf(
		"job state decided event_id=%s state=%s reason=post_restart_finalize_completed",
		job.EventID,
		JobProcessCompleted,
	)
	return JobProcessCompleted
}
