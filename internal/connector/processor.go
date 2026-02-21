package connector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gitee.com/alicespace/alice/internal/logging"
)

type Processor struct {
	codex           CodexRunner
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
const fileChangeEventPrefix = "[filechange] "
const idleSummaryPrompt = "请基于当前会话上下文，提炼后续仍有价值的信息摘要。\n" +
	"要求：\n" +
	"1. 只提炼：事实、约束、决策、待办、偏好变化。\n" +
	"2. 不包含：寒暄、一次性执行细节、敏感信息。\n" +
	"3. 输出 5-12 条短要点；若无重要信息仅输出“无重要新增信息”。"

func NewProcessor(
	codexRunner CodexRunner,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	return NewProcessorWithMemory(codexRunner, sender, failureMessage, thinkingMessage, nil)
}

func NewProcessorWithMemory(
	codexRunner CodexRunner,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
	memoryManager MemoryManager,
) *Processor {
	return &Processor{
		codex:           codexRunner,
		memory:          memoryManager,
		sender:          sender,
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
		sessions:        make(map[string]sessionState),
		now:             time.Now,
	}
}

func (p *Processor) ProcessJob(ctx context.Context, job Job) {
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
		p.processReplyMessage(ctx, job)
		return
	}

	p.prepareJobForCodex(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPromptWithMemory(ctx, job, currentThreadID)
	reply, nextThreadID, err := p.runCodex(ctx, currentThreadID, promptText, nil)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(err, context.Canceled) {
		log.Printf("codex canceled event_id=%s", job.EventID)
		logging.Debugf("memory update skipped event_id=%s changed=false reason=codex_canceled", job.EventID)
		return
	}
	failed := err != nil
	if err != nil {
		log.Printf("codex failed event_id=%s: %v", job.EventID, err)
		reply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), reply, failed)

	if sendErr := p.sender.SendText(ctx, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		log.Printf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
}

func (p *Processor) processReplyMessage(ctx context.Context, job Job) {
	sessionKey := sessionKeyForJob(job)
	ackMessageID, err := p.sender.ReplyText(ctx, job.SourceMessageID, "收到！")
	if err != nil {
		log.Printf("send ack reply failed event_id=%s: %v", job.EventID, err)
		ackMessageID = ""
	}

	lastSentAgentMessage := ""
	sendAgentMessage := func(agentMessage string) {
		normalized := strings.TrimSpace(agentMessage)
		isFileChange := false
		if strings.HasPrefix(normalized, fileChangeEventPrefix) {
			isFileChange = true
			normalized = strings.TrimSpace(strings.TrimPrefix(normalized, fileChangeEventPrefix))
		}
		if normalized == "" {
			return
		}
		if normalized == lastSentAgentMessage {
			return
		}
		if isFileChange {
			if _, richErr := p.sender.ReplyRichText(ctx, job.SourceMessageID, splitMessageLines(normalized)); richErr != nil {
				if _, sendErr := p.sender.ReplyText(ctx, job.SourceMessageID, normalized); sendErr != nil {
					log.Printf("send filechange message failed event_id=%s: %v", job.EventID, sendErr)
					return
				}
			}
		} else {
			if _, sendErr := p.sender.ReplyText(ctx, job.SourceMessageID, normalized); sendErr != nil {
				log.Printf("send agent message failed event_id=%s: %v", job.EventID, sendErr)
				return
			}
		}
		lastSentAgentMessage = normalized
	}

	p.prepareJobForCodex(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPromptWithMemory(ctx, job, currentThreadID)
	finalReply, nextThreadID, runErr := p.runCodex(ctx, currentThreadID, promptText, sendAgentMessage)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(runErr, context.Canceled) {
		// Parent context cancellation usually means app shutdown.
		if ctx.Err() != nil {
			logging.Debugf("memory update skipped event_id=%s changed=false reason=context_canceled", job.EventID)
			return
		}
		if ackMessageID != "" {
			if _, replyErr := p.sender.ReplyText(ctx, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
				log.Printf("send interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
			}
		} else if _, replyErr := p.sender.ReplyText(ctx, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
			log.Printf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
		}
		logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
		return
	}
	failed := runErr != nil
	if failed {
		log.Printf("codex failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.failureMessage
	}
	p.recordInteraction(job, p.buildCurrentUserInput(job), finalReply, failed)
	if strings.TrimSpace(finalReply) != "" && strings.TrimSpace(finalReply) != lastSentAgentMessage {
		if _, replyErr := p.sender.ReplyText(ctx, job.SourceMessageID, finalReply); replyErr != nil {
			log.Printf("send final reply text failed event_id=%s: %v", job.EventID, replyErr)
		}
	}
}

func splitMessageLines(message string) []string {
	rawLines := strings.Split(strings.TrimSpace(message), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func (p *Processor) runCodex(
	ctx context.Context,
	threadID string,
	userText string,
	onAgentMessage func(message string),
) (string, string, error) {
	if runner, ok := p.codex.(ResumableStreamingCodexRunner); ok {
		return runner.RunWithThreadAndProgress(ctx, threadID, userText, onAgentMessage)
	}
	if runner, ok := p.codex.(ResumableCodexRunner); ok {
		return runner.RunWithThread(ctx, threadID, userText)
	}
	if runner, ok := p.codex.(StreamingCodexRunner); ok {
		reply, err := runner.RunWithProgress(ctx, userText, onAgentMessage)
		return reply, strings.TrimSpace(threadID), err
	}
	reply, err := p.codex.Run(ctx, userText)
	return reply, strings.TrimSpace(threadID), err
}

func (p *Processor) buildPromptWithMemory(ctx context.Context, job Job, threadID string) string {
	userText := p.buildUserTextWithReplyContext(ctx, job, threadID)
	if strings.TrimSpace(threadID) != "" {
		logging.Debugf(
			"prompt assemble event_id=%s strategy=resume_direct thread_id=%s final_prompt=%q",
			job.EventID,
			strings.TrimSpace(threadID),
			userText,
		)
		return userText
	}

	logging.Debugf("prompt assemble start event_id=%s memory_enabled=%t user_text=%q", job.EventID, p.memory != nil, userText)
	if p.memory == nil {
		logging.Debugf("prompt assemble event_id=%s strategy=direct final_prompt=%q", job.EventID, userText)
		return userText
	}

	prompt, err := p.memory.BuildPrompt(userText)
	if err != nil {
		log.Printf("build memory prompt failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("prompt assemble fallback event_id=%s strategy=direct reason=%v final_prompt=%q", job.EventID, err, userText)
		return userText
	}
	logging.Debugf("prompt assemble event_id=%s strategy=memory final_prompt=%q", job.EventID, prompt)
	return prompt
}

func (p *Processor) buildUserTextWithReplyContext(ctx context.Context, job Job, threadID string) string {
	currentText := p.buildCurrentUserInput(job)
	if strings.TrimSpace(threadID) != "" {
		logging.Debugf(
			"reply context skipped event_id=%s reason=resume_thread thread_id=%s",
			job.EventID,
			strings.TrimSpace(threadID),
		)
		return currentText
	}

	parentMessageID := strings.TrimSpace(job.ReplyParentMessageID)
	if currentText == "" || parentMessageID == "" {
		return currentText
	}

	replyContextProvider, ok := p.sender.(ReplyContextProvider)
	if !ok {
		logging.Debugf(
			"reply context skipped event_id=%s parent_message_id=%s reason=no_provider",
			job.EventID,
			parentMessageID,
		)
		return currentText
	}

	parentText, err := replyContextProvider.GetMessageText(ctx, parentMessageID)
	if err != nil {
		logging.Debugf(
			"reply context fetch failed event_id=%s parent_message_id=%s err=%v",
			job.EventID,
			parentMessageID,
			err,
		)
		return currentText
	}
	parentText = strings.TrimSpace(parentText)
	if parentText == "" {
		logging.Debugf("reply context empty event_id=%s parent_message_id=%s", job.EventID, parentMessageID)
		return currentText
	}

	combined := "你正在回复下面这条消息，请基于其上下文回答。\n" +
		"被回复消息：\n" + clipText(parentText, 2000) + "\n\n" +
		"用户当前回复：\n" + currentText
	logging.Debugf(
		"reply context attached event_id=%s parent_message_id=%s parent_text=%q combined_user_text=%q",
		job.EventID,
		parentMessageID,
		parentText,
		combined,
	)
	return combined
}

func sessionKeyForJob(job Job) string {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey != "" {
		return sessionKey
	}
	return buildSessionKey(job.ReceiveIDType, job.ReceiveID)
}

func (p *Processor) prepareJobForCodex(ctx context.Context, job *Job) {
	if job == nil || len(job.Attachments) == 0 {
		return
	}

	downloader, ok := p.sender.(AttachmentDownloader)
	if !ok {
		for i := range job.Attachments {
			if strings.TrimSpace(job.Attachments[i].DownloadError) == "" {
				job.Attachments[i].DownloadError = "sender does not support attachment download"
			}
		}
		return
	}

	for i := range job.Attachments {
		attachment := &job.Attachments[i]
		if strings.TrimSpace(attachment.LocalPath) != "" {
			continue
		}
		if err := downloader.DownloadAttachment(ctx, job.SourceMessageID, attachment); err != nil {
			attachment.DownloadError = err.Error()
			log.Printf(
				"download attachment failed event_id=%s message_type=%s kind=%s file_key=%s image_key=%s err=%v",
				job.EventID,
				job.MessageType,
				attachment.Kind,
				attachment.FileKey,
				attachment.ImageKey,
				err,
			)
		}
	}
}

func (p *Processor) buildCurrentUserInput(job Job) string {
	baseText := strings.TrimSpace(job.Text)
	if len(job.Attachments) == 0 {
		return baseText
	}

	var builder strings.Builder
	if baseText != "" {
		builder.WriteString(baseText)
		builder.WriteString("\n\n")
	}
	builder.WriteString("附加资源信息：\n")
	for idx, attachment := range job.Attachments {
		builder.WriteString(fmt.Sprintf("%d. 类型：%s\n", idx+1, strings.TrimSpace(attachment.Kind)))
		if name := strings.TrimSpace(attachment.FileName); name != "" {
			builder.WriteString("   文件名：")
			builder.WriteString(name)
			builder.WriteString("\n")
		}
		if key := strings.TrimSpace(attachment.ImageKey); key != "" {
			builder.WriteString("   image_key：")
			builder.WriteString(key)
			builder.WriteString("\n")
		}
		if key := strings.TrimSpace(attachment.FileKey); key != "" {
			builder.WriteString("   file_key：")
			builder.WriteString(key)
			builder.WriteString("\n")
		}
		if localPath := strings.TrimSpace(attachment.LocalPath); localPath != "" {
			builder.WriteString("   本地路径：")
			builder.WriteString(localPath)
			builder.WriteString("\n")
		}
		if errText := strings.TrimSpace(attachment.DownloadError); errText != "" {
			builder.WriteString("   下载状态：失败（")
			builder.WriteString(errText)
			builder.WriteString("）\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func (p *Processor) recordInteraction(job Job, userText, reply string, failed bool) {
	if p.memory == nil {
		logging.Debugf("memory update skipped event_id=%s changed=false reason=no_memory_manager", job.EventID)
		return
	}
	changed, err := p.memory.SaveInteraction(strings.TrimSpace(userText), reply, failed)
	if err != nil {
		log.Printf("save memory failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("memory update result event_id=%s changed=unknown error=%v", job.EventID, err)
		return
	}
	logging.Debugf("memory update result event_id=%s changed=%t failed=%t", job.EventID, changed, failed)
}
