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

type llmRunOptions struct {
	Model           string
	Profile         string
	ReasoningEffort string
	Personality     string
	NoReplyToken    string
}

func (p *Processor) runLLM(
	ctx context.Context,
	threadID string,
	userText string,
	options llmRunOptions,
	env map[string]string,
	onAgentMessage func(message string),
) (string, string, error) {
	snapshot := p.runtimeSnapshot()
	if snapshot.llm == nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("llm backend is nil")
	}
	result, err := snapshot.llm.Run(ctx, llm.RunRequest{
		ThreadID:        strings.TrimSpace(threadID),
		AgentName:       "assistant",
		UserText:        userText,
		Model:           strings.TrimSpace(options.Model),
		Profile:         strings.TrimSpace(options.Profile),
		ReasoningEffort: strings.TrimSpace(options.ReasoningEffort),
		Personality:     strings.TrimSpace(options.Personality),
		NoReplyToken:    strings.TrimSpace(options.NoReplyToken),
		Env:             env,
		OnProgress:      onAgentMessage,
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
	userText = p.appendRuntimeSkillHint(userText, job)

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

func (p *Processor) appendRuntimeSkillHint(userText string, job Job) string {
	if strings.TrimSpace(job.SourceMessageID) == "" {
		return userText
	}

	rendered, err := p.renderPromptFile(connectorPromptRuntimeSkillHint, map[string]any{
		"UserText": strings.TrimSpace(userText),
	})
	if err != nil {
		logging.Warnf("render runtime skill hint failed event_id=%s: %v", job.EventID, err)
		return strings.TrimSpace(userText)
	}
	return rendered
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

	combined, err := p.renderPromptFile(connectorPromptReplyContext, map[string]any{
		"ParentText": clipText(parentText, 2000),
		"UserText":   currentText,
	})
	if err != nil {
		logging.Warnf("render reply context failed event_id=%s parent_message_id=%s: %v", job.EventID, parentMessageID, err)
		return currentText
	}
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
	runtimeCfg := p.runtimeSnapshot()
	if strings.TrimSpace(runtimeCfg.runtimeAPIBase) != "" {
		env[runtimeapi.EnvBaseURL] = strings.TrimSpace(runtimeCfg.runtimeAPIBase)
	}
	if strings.TrimSpace(runtimeCfg.runtimeAPIToken) != "" {
		env[runtimeapi.EnvToken] = strings.TrimSpace(runtimeCfg.runtimeAPIToken)
	}
	if strings.TrimSpace(runtimeCfg.runtimeAPIBin) != "" {
		env[runtimeapi.EnvBin] = strings.TrimSpace(runtimeCfg.runtimeAPIBin)
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

	botOpenID := strings.TrimSpace(job.BotOpenID)
	botUserID := strings.TrimSpace(job.BotUserID)
	senderName := normalizeUserDisplayName(strings.TrimSpace(job.SenderName), "用户")
	mentionedNames := buildMentionDisplayNames(job.MentionedUsers, botOpenID, botUserID)
	speakerKnown := strings.TrimSpace(job.SenderName) != ""
	mappings := buildUserIDMappings(job, senderName, botOpenID, botUserID)
	identityContextEnabled := speakerKnown || len(mentionedNames) > 0 || len(mappings) > 0

	speechText := baseText
	if len(mentionedNames) > 0 {
		mentionedText := "@" + strings.Join(mentionedNames, " @")
		if speechText == "" {
			speechText = mentionedText
		} else {
			speechText = mentionedText + " " + speechText
		}
	}

	rendered, err := p.renderPromptFile(connectorPromptCurrentUserInput, currentUserInputPromptData{
		HasIdentityContext: identityContextEnabled,
		SenderName:         senderName,
		SpeechText:         speechText,
		BaseText:           baseText,
		UserMappings:       mappings,
		Attachments:        buildAttachmentPromptData(job.Attachments),
	})
	if err != nil {
		logging.Warnf("render current user input failed event_id=%s: %v", job.EventID, err)
		return baseText
	}
	return rendered
}

func buildUserIDMappings(
	job Job,
	senderName string,
	botOpenID string,
	botUserID string,
) []userMappingPromptData {
	senderID := preferredID(job.SenderOpenID, job.SenderUserID, job.SenderUnionID)
	mappings := make([]userMappingPromptData, 0, len(job.MentionedUsers)+1)
	seen := make(map[string]struct{}, len(job.MentionedUsers)+1)

	if senderID != "" && !isBotIdentity(job.SenderOpenID, job.SenderUserID, senderID, botOpenID, botUserID) {
		key := senderName + "\x00" + senderID
		mappings = append(mappings, userMappingPromptData{Name: senderName, ID: senderID})
		seen[key] = struct{}{}
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
		key := name + "\x00" + id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mappings = append(mappings, userMappingPromptData{Name: name, ID: id})
	}
	return mappings
}

func buildAttachmentPromptData(attachments []Attachment) []attachmentPromptData {
	if len(attachments) == 0 {
		return nil
	}
	data := make([]attachmentPromptData, 0, len(attachments))
	for idx, attachment := range attachments {
		data = append(data, attachmentPromptData{
			Index:         idx + 1,
			Kind:          strings.TrimSpace(attachment.Kind),
			FileName:      strings.TrimSpace(attachment.FileName),
			ImageKey:      strings.TrimSpace(attachment.ImageKey),
			FileKey:       strings.TrimSpace(attachment.FileKey),
			LocalPath:     strings.TrimSpace(attachment.LocalPath),
			DownloadError: strings.TrimSpace(attachment.DownloadError),
		})
	}
	return data
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
