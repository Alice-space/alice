package connector

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
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

func (p *Processor) runLLM(
	ctx context.Context,
	threadID string,
	userText string,
	env map[string]string,
	onAgentMessage func(message string),
) (string, string, error) {
	if p.llm == nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("llm backend is nil")
	}
	result, err := p.llm.Run(ctx, llm.RunRequest{
		ThreadID:   strings.TrimSpace(threadID),
		AgentName:  "assistant",
		UserText:   userText,
		Env:        env,
		OnProgress: onAgentMessage,
	})
	nextThreadID := strings.TrimSpace(result.NextThreadID)
	if nextThreadID == "" {
		nextThreadID = strings.TrimSpace(threadID)
	}
	return result.Reply, nextThreadID, err
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
	userText = appendMCPToolContextHint(userText, job)

	logging.Debugf("prompt assemble start event_id=%s memory_enabled=%t user_text=%q", job.EventID, p.memory != nil, userText)
	if p.memory == nil {
		logging.Debugf("prompt assemble event_id=%s strategy=direct final_prompt=%q", job.EventID, userText)
		return userText
	}

	prompt, err := p.memory.BuildPrompt(memoryScopeKeyForJob(job), userText)
	if err != nil {
		logging.Warnf("build memory prompt failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("prompt assemble fallback event_id=%s strategy=direct reason=%v final_prompt=%q", job.EventID, err, userText)
		return userText
	}
	logging.Debugf("prompt assemble event_id=%s strategy=memory final_prompt=%q", job.EventID, prompt)
	return prompt
}

func appendMCPToolContextHint(userText string, job Job) string {
	if strings.TrimSpace(job.SourceMessageID) == "" {
		return userText
	}

	hint := "工具调用说明：alice-feishu 的 send_image/send_file 无需传 receive_id_type、receive_id、source_message_id，系统会按当前会话自动路由。\n\n"
	return hint + strings.TrimSpace(userText)
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

func (p *Processor) buildLLMRunEnv(job Job) map[string]string {
	scopeKey := memoryScopeKeyForJob(job)
	sessionContext := mcpbridge.SessionContext{
		ReceiveIDType:   strings.TrimSpace(job.ReceiveIDType),
		ReceiveID:       strings.TrimSpace(job.ReceiveID),
		SourceMessageID: strings.TrimSpace(job.SourceMessageID),
		ActorUserID:     strings.TrimSpace(job.SenderUserID),
		ActorOpenID:     strings.TrimSpace(job.SenderOpenID),
		ChatType:        strings.TrimSpace(job.ChatType),
		SessionKey:      sessionKeyForJob(job),
	}
	type resourceRootProvider interface {
		ResourceRootForScope(memoryScopeKey string) string
	}
	if provider, ok := p.sender.(resourceRootProvider); ok {
		sessionContext.ResourceRoot = strings.TrimSpace(provider.ResourceRootForScope(scopeKey))
		if sessionContext.ResourceRoot != "" {
			if err := os.MkdirAll(sessionContext.ResourceRoot, 0o755); err != nil {
				logging.Warnf("prepare scoped resource root failed event_id=%s scope=%s err=%v", job.EventID, scopeKey, err)
			}
		}
	}
	if err := sessionContext.Validate(); err != nil {
		return nil
	}
	env := sessionContext.ToEnv()
	if strings.TrimSpace(p.runtimeAPIBase) != "" {
		env[runtimeapi.EnvBaseURL] = strings.TrimSpace(p.runtimeAPIBase)
	}
	if strings.TrimSpace(p.runtimeAPIToken) != "" {
		env[runtimeapi.EnvToken] = strings.TrimSpace(p.runtimeAPIToken)
	}
	return env
}

func (p *Processor) prepareJobForLLM(ctx context.Context, job *Job) {
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
	scopeKey := memoryScopeKeyForJob(*job)

	for i := range job.Attachments {
		attachment := &job.Attachments[i]
		if strings.TrimSpace(attachment.LocalPath) != "" {
			continue
		}
		downloadSourceMessageID := strings.TrimSpace(attachment.SourceMessageID)
		if downloadSourceMessageID == "" {
			downloadSourceMessageID = strings.TrimSpace(job.SourceMessageID)
		}
		if err := downloader.DownloadAttachment(ctx, scopeKey, downloadSourceMessageID, attachment); err != nil {
			attachment.DownloadError = err.Error()
			logging.Warnf(
				"download attachment failed event_id=%s scope=%s message_type=%s kind=%s source_message_id=%s file_key=%s image_key=%s err=%v",
				job.EventID,
				scopeKey,
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
	var builder strings.Builder

	botOpenID := strings.TrimSpace(job.BotOpenID)
	botUserID := strings.TrimSpace(job.BotUserID)
	senderName := normalizeUserDisplayName(strings.TrimSpace(job.SenderName), "用户")
	senderID := preferredID(job.SenderOpenID, job.SenderUserID, job.SenderUnionID)
	mentionedNames := buildMentionDisplayNames(job.MentionedUsers, botOpenID, botUserID)
	mappingLines := buildUserIDMappingLines(job, senderName, senderID, botOpenID, botUserID)
	speakerKnown := strings.TrimSpace(job.SenderName) != ""
	identityContextEnabled := speakerKnown || len(mentionedNames) > 0 || len(mappingLines) > 0
	if identityContextEnabled {
		for _, mapping := range mappingLines {
			builder.WriteString(mapping)
			builder.WriteString("\n")
		}
		if len(mappingLines) > 0 {
			builder.WriteString("@提及规则：若需要在回复中艾特某人，请直接写 @姓名 或 @用户id（如 @ou_xxx），系统会自动转换为飞书 mention。\n\n")
		} else if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(senderName)
		builder.WriteString("说：")
		if len(mentionedNames) > 0 {
			builder.WriteString("@")
			builder.WriteString(strings.Join(mentionedNames, " @"))
			builder.WriteString(" ")
		}
		builder.WriteString(baseText)
	} else if baseText != "" {
		builder.WriteString(baseText)
	}

	if len(job.Attachments) > 0 {
		if builder.Len() > 0 {
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
	}

	return strings.TrimSpace(builder.String())
}

func buildUserIDMappingLines(
	job Job,
	senderName string,
	senderID string,
	botOpenID string,
	botUserID string,
) []string {
	lines := make([]string, 0, len(job.MentionedUsers)+1)
	seen := make(map[string]struct{}, len(job.MentionedUsers)+1)

	if senderID != "" && !isBotIdentity(job.SenderOpenID, job.SenderUserID, senderID, botOpenID, botUserID) {
		line := fmt.Sprintf("用户%s的id是%s", senderName, senderID)
		lines = append(lines, line)
		seen[line] = struct{}{}
	}

	for _, mentioned := range job.MentionedUsers {
		name := strings.TrimSpace(mentioned.Name)
		if name == "" {
			name = "用户"
		}
		name = normalizeUserDisplayName(name, "用户")
		id := preferredID(mentioned.OpenID, mentioned.UserID, mentioned.UnionID)
		if id == "" || isBotIdentity(mentioned.OpenID, mentioned.UserID, id, botOpenID, botUserID) {
			continue
		}
		line := fmt.Sprintf("用户%s的id是%s", name, id)
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		lines = append(lines, line)
	}
	return lines
}

func buildMentionDisplayNames(mentionedUsers []MentionedUser, botOpenID, botUserID string) []string {
	if len(mentionedUsers) == 0 {
		return nil
	}
	names := make([]string, 0, len(mentionedUsers))
	seen := make(map[string]struct{}, len(mentionedUsers))
	for _, mentioned := range mentionedUsers {
		id := preferredID(mentioned.OpenID, mentioned.UserID, mentioned.UnionID)
		if isBotIdentity(mentioned.OpenID, mentioned.UserID, id, botOpenID, botUserID) {
			continue
		}
		name := normalizeUserDisplayName(mentioned.Name, "")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func isBotIdentity(openID, userID, candidateID, botOpenID, botUserID string) bool {
	normalizedBotOpenID := strings.TrimSpace(botOpenID)
	normalizedBotUserID := strings.TrimSpace(botUserID)

	openID = strings.TrimSpace(openID)
	userID = strings.TrimSpace(userID)
	candidateID = strings.TrimSpace(candidateID)
	if normalizedBotOpenID != "" && (openID == normalizedBotOpenID || candidateID == normalizedBotOpenID) {
		return true
	}
	if normalizedBotUserID != "" && (userID == normalizedBotUserID || candidateID == normalizedBotUserID) {
		return true
	}
	return false
}

func normalizeUserDisplayName(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}

func preferredID(openID, userID, unionID string) string {
	openID = strings.TrimSpace(openID)
	if openID != "" {
		return openID
	}
	userID = strings.TrimSpace(userID)
	if userID != "" {
		return userID
	}
	return strings.TrimSpace(unionID)
}

func (p *Processor) recordInteraction(job Job, userText, reply string, failed bool) {
	if p.memory == nil {
		logging.Debugf("memory update skipped event_id=%s changed=false reason=no_memory_manager", job.EventID)
		return
	}
	changed, err := p.memory.SaveInteraction(memoryScopeKeyForJob(job), strings.TrimSpace(userText), reply, failed)
	if err != nil {
		logging.Warnf("save memory failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("memory update result event_id=%s changed=unknown error=%v", job.EventID, err)
		return
	}
	logging.Debugf("memory update result event_id=%s changed=%t failed=%t", job.EventID, changed, failed)
}
