package connector

import (
	"context"
	"fmt"
	"log"
	"strings"

	"gitee.com/alicespace/alice/internal/logging"
)

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
		downloadSourceMessageID := strings.TrimSpace(attachment.SourceMessageID)
		if downloadSourceMessageID == "" {
			downloadSourceMessageID = strings.TrimSpace(job.SourceMessageID)
		}
		if err := downloader.DownloadAttachment(ctx, downloadSourceMessageID, attachment); err != nil {
			attachment.DownloadError = err.Error()
			log.Printf(
				"download attachment failed event_id=%s message_type=%s kind=%s source_message_id=%s file_key=%s image_key=%s err=%v",
				job.EventID,
				job.MessageType,
				attachment.Kind,
				downloadSourceMessageID,
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
