package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"gitee.com/alicespace/alice/internal/llm"
	"gitee.com/alicespace/alice/internal/logging"
)

type Processor struct {
	llm             llm.Backend
	memory          MemoryManager
	sender          Sender
	failureMessage  string
	thinkingMessage string
	mu              sync.Mutex
	sessions        map[string]sessionState
	stateFilePath   string
	stateVersion    uint64
	flushedVersion  uint64
	now             func() time.Time
}

const interruptedReplyMessage = "已收到你的新消息，当前回复已中断并切换到最新输入。"
const restartNotificationMessage = "Alice已重新启动"
const fileChangeEventPrefix = "[file_change] "
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
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
		sessions:        make(map[string]sessionState),
		now:             time.Now,
	}
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

	sessionKey := sessionKeyForJob(job)
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
	reply, nextThreadID, err := p.runLLM(ctx, currentThreadID, promptText, nil)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(err, context.Canceled) {
		if ctx.Err() != nil && isRestartIntentJob(job) {
			logging.Debugf(
				"job state decided event_id=%s state=%s reason=shutdown_restart_intent",
				job.EventID,
				JobProcessPostRestartFinalize,
			)
			return JobProcessPostRestartFinalize
		}
		log.Printf("llm canceled event_id=%s", job.EventID)
		logging.Debugf("memory update skipped event_id=%s changed=false reason=llm_canceled", job.EventID)
		return JobProcessRetryAfterRestart
	}
	failed := err != nil
	if err != nil {
		log.Printf("llm failed event_id=%s: %v", job.EventID, err)
		reply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), reply, failed)

	if sendErr := p.sendCardWithFallback(ctx, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		log.Printf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
	return JobProcessCompleted
}

func (p *Processor) processReplyMessage(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	ackMessageID, err := p.replyCardWithFallback(ctx, job.SourceMessageID, "收到！")
	if err != nil {
		log.Printf("send ack reply failed event_id=%s: %v", job.EventID, err)
		ackMessageID = ""
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
		if normalized == lastSentAgentMessage {
			return
		}
		if isFileChange {
			if sendErr := p.sendCardWithFallback(ctx, job.ReceiveIDType, job.ReceiveID, normalized); sendErr != nil {
				log.Printf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
				return
			}
		} else {
			if _, sendErr := p.replyCardWithFallback(ctx, job.SourceMessageID, normalized); sendErr != nil {
				log.Printf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
				return
			}
		}
		lastSentAgentMessage = normalized
	}

	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPromptWithMemory(ctx, job, currentThreadID)
	finalReply, nextThreadID, runErr := p.runLLM(ctx, currentThreadID, promptText, sendAgentMessage)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(runErr, context.Canceled) {
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
		if ackMessageID != "" {
			if _, replyErr := p.replyCardWithFallback(ctx, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
				log.Printf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			}
		} else if _, replyErr := p.replyCardWithFallback(ctx, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
			log.Printf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
		}
		logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
		return JobProcessRetryAfterRestart
	}
	failed := runErr != nil
	if failed {
		log.Printf("llm failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), finalReply, failed)
	if strings.TrimSpace(finalReply) != "" && strings.TrimSpace(finalReply) != lastSentAgentMessage {
		if _, replyErr := p.replyCardWithFallback(ctx, job.SourceMessageID, finalReply); replyErr != nil {
			log.Printf("send final reply failed event_id=%s: %v", job.EventID, replyErr)
		}
	}
	return JobProcessCompleted
}

func (p *Processor) replyMarkdownPostWithFallback(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	normalized := strings.TrimSpace(markdown)
	if normalized == "" {
		return "", nil
	}
	if messageID, richErr := p.sender.ReplyRichTextMarkdown(ctx, sourceMessageID, normalized); richErr == nil {
		return messageID, nil
	}
	messageID, textErr := p.sender.ReplyText(ctx, sourceMessageID, normalized)
	if textErr != nil {
		return "", textErr
	}
	return messageID, nil
}

func (p *Processor) replyCardWithFallback(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	normalized := strings.TrimSpace(markdown)
	if normalized == "" {
		return "", nil
	}
	if messageID, cardErr := p.sender.ReplyCard(ctx, sourceMessageID, buildReplyCardContent(normalized)); cardErr == nil {
		return messageID, nil
	}
	return p.replyMarkdownPostWithFallback(ctx, sourceMessageID, normalized)
}

func (p *Processor) sendCardWithFallback(ctx context.Context, receiveIDType, receiveID, markdown string) error {
	normalized := strings.TrimSpace(markdown)
	if normalized == "" {
		return nil
	}
	type sendCardCapable interface {
		SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error
	}
	if sender, ok := p.sender.(sendCardCapable); ok {
		if cardErr := sender.SendCard(ctx, receiveIDType, receiveID, buildReplyCardContent(normalized)); cardErr == nil {
			return nil
		}
	}
	return p.sender.SendText(ctx, receiveIDType, receiveID, normalized)
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
	var sendErr error
	if strings.TrimSpace(job.SourceMessageID) != "" {
		_, sendErr = p.replyCardWithFallback(ctx, job.SourceMessageID, restartNotificationMessage)
	} else {
		sendErr = p.sendCardWithFallback(ctx, job.ReceiveIDType, job.ReceiveID, restartNotificationMessage)
	}
	if sendErr != nil {
		log.Printf("send restart notification failed event_id=%s: %v", job.EventID, sendErr)
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

	var sendErr error
	if strings.TrimSpace(job.SourceMessageID) != "" {
		_, sendErr = p.replyCardWithFallback(ctx, job.SourceMessageID, summary)
	} else {
		sendErr = p.sendCardWithFallback(ctx, job.ReceiveIDType, job.ReceiveID, summary)
	}
	if sendErr != nil {
		log.Printf("send post-restart finalize reply failed event_id=%s: %v", job.EventID, sendErr)
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
