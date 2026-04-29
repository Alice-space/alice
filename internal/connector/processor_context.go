package connector

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/runtimeapi"
	"github.com/Alice-space/alice/internal/sessionctx"
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
	EventID         string
	Scene           string
	Provider        string
	Model           string
	Profile         string
	ReasoningEffort string
	Variant         string
	Personality     string
	NoReplyToken    string
	PromptPrefix    string
	WorkDir         string
}

func (p *Processor) runLLM(
	ctx context.Context,
	threadID string,
	userText string,
	options llmRunOptions,
	env map[string]string,
	onAgentMessage func(message string),
	observer llmRunObserver,
) (string, string, agentbridge.Usage, error) {
	snapshot := p.runtimeSnapshot()
	if snapshot.llm == nil {
		return "", strings.TrimSpace(threadID), agentbridge.Usage{}, fmt.Errorf("llm backend is nil")
	}
	assembledText := assemblePrompt(options.PromptPrefix, options.Personality, options.NoReplyToken, userText, strings.TrimSpace(threadID) != "")
	provider := strings.TrimSpace(options.Provider)
	if provider == "" {
		provider = "default"
	}
	requestThreadID := strings.TrimSpace(threadID)
	startedAt := time.Now()
	logging.Debugf(
		"%s run start event_id=%s scene=%s model=%q profile=%q thread_id=%s",
		provider,
		options.EventID,
		strings.TrimSpace(options.Scene),
		strings.TrimSpace(options.Model),
		strings.TrimSpace(options.Profile),
		requestThreadID,
	)
	logProgress := func(message string) {
		normalized := strings.TrimSpace(message)
		if normalized == "" {
			return
		}
		isFileChange := strings.HasPrefix(normalized, fileChangeEventPrefix)
		if isFileChange {
			fileChange := strings.TrimSpace(strings.TrimPrefix(normalized, fileChangeEventPrefix))
			logging.Debugf(
				"%s file_change event_id=%s thread_id=%s file_change=%q",
				provider,
				options.EventID,
				requestThreadID,
				clipText(fileChange, 500),
			)
			if observer != nil {
				observer.RecordFileChange(fileChange)
			}
			return
		} else {
			logging.Debugf(
				"%s agent_message event_id=%s thread_id=%s agent_message=%q",
				provider,
				options.EventID,
				requestThreadID,
				clipText(normalized, 500),
			)
		}
		if onAgentMessage != nil {
			onAgentMessage(message)
		}
		if observer != nil {
			observer.RecordVisibleOutput(normalized)
		}
	}
	logRawEvent := func(event agentbridge.RawEvent) {
		if observer != nil {
			observer.RecordBackendEvent(event)
		}
		if !logging.IsDebugEnabled() {
			return
		}
		switch event.Kind {
		case "stdout_line":
			logging.Debugf(
				"%s stdout_line event_id=%s line=%q",
				provider, options.EventID, clipText(event.Line, 500),
			)
		case "reasoning":
			logging.Debugf(
				"%s reasoning event_id=%s detail=%q",
				provider, options.EventID, clipText(event.Detail, 500),
			)
		case "tool_call":
			logging.Debugf(
				"%s tool_call event_id=%s detail=%q",
				provider, options.EventID, clipText(event.Detail, 500),
			)
		case "tool_use":
			logging.Debugf(
				"%s tool_use event_id=%s detail=%q",
				provider, options.EventID, clipText(event.Detail, 500),
			)
		}
	}
	result, err := snapshot.llm.Run(ctx, agentbridge.RunRequest{
		ThreadID:        strings.TrimSpace(threadID),
		AgentName:       "assistant",
		UserText:        assembledText,
		Scene:           strings.TrimSpace(options.Scene),
		Provider:        strings.TrimSpace(options.Provider),
		Model:           strings.TrimSpace(options.Model),
		Profile:         strings.TrimSpace(options.Profile),
		ReasoningEffort: strings.TrimSpace(options.ReasoningEffort),
		Variant:         strings.TrimSpace(options.Variant),
		Personality:     strings.TrimSpace(options.Personality),
		WorkspaceDir:    strings.TrimSpace(options.WorkDir),
		Env:             env,
		OnProgress:      logProgress,
		OnRawEvent:      logRawEvent,
	})
	nextThreadID := strings.TrimSpace(result.NextThreadID)
	if nextThreadID == "" {
		nextThreadID = strings.TrimSpace(threadID)
	}
	if result.Usage.HasUsage() {
		logging.Debugf(
			"%s usage event_id=%s input_tokens=%d cached_input_tokens=%d output_tokens=%d total_tokens=%d",
			provider,
			options.EventID,
			result.Usage.InputTokens,
			result.Usage.CachedInputTokens,
			result.Usage.OutputTokens,
			result.Usage.TotalTokens(),
		)
	}
	if nextThreadID != "" && nextThreadID != requestThreadID {
		logging.Debugf("%s thread started event_id=%s thread_id=%s", provider, options.EventID, nextThreadID)
	}
	if err != nil {
		logging.Debugf(
			"%s run failed event_id=%s elapsed=%s thread_id=%s next_thread_id=%s err=%v",
			provider,
			options.EventID,
			time.Since(startedAt),
			requestThreadID,
			nextThreadID,
			err,
		)
	} else {
		logging.Debugf(
			"%s run completed event_id=%s elapsed=%s thread_id=%s next_thread_id=%s final_message=%q",
			provider,
			options.EventID,
			time.Since(startedAt),
			nextThreadID,
			nextThreadID,
			clipText(strings.TrimSpace(result.Reply), 500),
		)
	}
	logging.DebugAgentTrace(logging.AgentTrace{
		Provider: provider,
		Agent:    "assistant",
		ThreadID: nextThreadID,
		Model:    strings.TrimSpace(options.Model),
		Profile:  strings.TrimSpace(options.Profile),
		Input:    assembledText,
		Output:   strings.TrimSpace(result.Reply),
		Error:    errorText(err),
	})
	return result.Reply, nextThreadID, result.Usage, err
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (p *Processor) buildPrompt(ctx context.Context, job Job, threadID string) string {
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
	userText = p.appendBotSoul(userText, job)
	userText = p.appendRuntimeSkillHint(userText, job)
	logging.Debugf("prompt assemble event_id=%s strategy=direct final_prompt=%q", job.EventID, userText)
	return userText
}

func (p *Processor) appendBotSoul(userText string, job Job) string {
	if strings.TrimSpace(job.Scene) == jobSceneWork {
		return strings.TrimSpace(userText)
	}
	soulPath := strings.TrimSpace(job.SoulPath)
	if soulPath == "" {
		return strings.TrimSpace(userText)
	}
	soulDoc := job.SoulDoc
	if !soulDoc.Loaded {
		// #nosec G304 -- soulPath comes from validated configuration, not raw user input
		raw, err := os.ReadFile(soulPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logging.Warnf("read bot soul failed event_id=%s path=%s: %v", job.EventID, soulPath, err)
			}
			return strings.TrimSpace(userText)
		}
		soulDoc = parseSoulDocument(string(raw))
	}
	soulText := strings.TrimSpace(soulDoc.Body)
	if soulText == "" {
		return strings.TrimSpace(userText)
	}
	rendered, err := p.renderPromptFile(connectorPromptBotSoul, map[string]any{
		"BotName":  defaultIfEmpty(job.BotName, "Alice"),
		"SoulText": soulText,
		"UserText": strings.TrimSpace(userText),
	})
	if err != nil {
		logging.Warnf("render bot soul failed event_id=%s path=%s: %v", job.EventID, soulPath, err)
		return strings.TrimSpace(userText)
	}
	return rendered
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
	currentText := p.buildCurrentUserInputWithThread(job, threadID)
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
	sk := sessionKeyForJob(job)
	sessionContext := sessionctx.SessionContext{
		ReceiveIDType:   strings.TrimSpace(job.ReceiveIDType),
		ReceiveID:       strings.TrimSpace(job.ReceiveID),
		SourceMessageID: strings.TrimSpace(job.SourceMessageID),
		ActorUserID:     strings.TrimSpace(job.SenderUserID),
		ActorOpenID:     strings.TrimSpace(job.SenderOpenID),
		ChatType:        strings.TrimSpace(job.ChatType),
		SessionKey:      sk,
		ResumeThreadID:  p.getThreadID(sk),
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
	if job == nil {
		return
	}

	if !job.SoulDoc.Loaded {
		soulPath := strings.TrimSpace(job.SoulPath)
		if soulPath != "" {
			// #nosec G304 -- soulPath comes from validated configuration, not raw user input
			raw, err := os.ReadFile(soulPath)
			if err != nil {
				if !os.IsNotExist(err) {
					logging.Warnf("read bot soul failed event_id=%s path=%s: %v", job.EventID, soulPath, err)
				}
			} else {
				job.SoulDoc = parseSoulDocument(string(raw))
				job.NoReplyToken = job.SoulDoc.OutputContract.effectiveSuppressToken(job.NoReplyToken)
			}
		}
	}

	if len(job.Attachments) == 0 {
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
	scopeKey := resourceScopeKeyForJob(*job)

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
	return p.buildCurrentUserInputWithThread(job, "")
}

func (p *Processor) buildCurrentUserInputWithThread(job Job, threadID string) string {
	baseText := strings.TrimSpace(job.Text)

	botOpenID := strings.TrimSpace(job.BotOpenID)
	botUserID := strings.TrimSpace(job.BotUserID)
	senderName := normalizeUserDisplayName(strings.TrimSpace(job.SenderName), "用户")
	mentionedNames := buildMentionDisplayNames(job.MentionedUsers, botOpenID, botUserID)
	speakerKnown := strings.TrimSpace(job.SenderName) != ""
	identityContextEnabled := speakerKnown || len(mentionedNames) > 0

	speechText := baseText
	if len(mentionedNames) > 0 {
		missingMentions := missingMentionDisplayNames(speechText, mentionedNames)
		if len(missingMentions) > 0 {
			mentionedText := "@" + strings.Join(missingMentions, " @")
			if speechText == "" {
				speechText = mentionedText
			} else {
				speechText = mentionedText + " " + speechText
			}
		}
	}

	rendered, err := p.renderPromptFile(connectorPromptCurrentUserInput, currentUserInputPromptData{
		HasIdentityContext: identityContextEnabled,
		SenderName:         senderName,
		SpeechText:         speechText,
		BaseText:           baseText,
		Attachments:        buildAttachmentPromptData(job.Attachments),
	})
	if err != nil {
		logging.Warnf("render current user input failed event_id=%s: %v", job.EventID, err)
		return baseText
	}
	return rendered
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

func missingMentionDisplayNames(text string, mentionedNames []string) []string {
	if len(mentionedNames) == 0 {
		return nil
	}
	missing := make([]string, 0, len(mentionedNames))
	for _, name := range mentionedNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(text, "@"+name) {
			continue
		}
		missing = append(missing, name)
	}
	return missing
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

// assemblePrompt prepends a system prefix to userText for new threads.
// On resume (isResume=true), userText is returned as-is (the CLI handles continuation).
func assemblePrompt(promptPrefix, personality, noReplyToken, userText string, isResume bool) string {
	if isResume {
		return userText
	}
	parts := make([]string, 0, 3)
	if p := strings.TrimSpace(promptPrefix); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(personality); p != "" {
		parts = append(parts, "Preferred response style/personality: "+p+".")
	}
	if t := strings.TrimSpace(noReplyToken); t != "" {
		parts = append(parts, "If no reply is appropriate, return exactly this token and nothing else: "+t)
	}
	prefix := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if prefix == "" {
		return userText
	}
	return prefix + "\n\n" + strings.TrimSpace(userText)
}
