package connector

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/imagegen"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

type Processor struct {
	llm              llm.Backend
	sender           Sender
	replies          *replyDispatcher
	failureMessage   string
	thinkingMessage  string
	feedbackMode     string
	feedbackEmoji    string
	imageGeneration  config.ImageGenerationConfig
	imageProvider    imagegen.Provider
	runtimeMu        sync.RWMutex
	mu               sync.Mutex
	sessions         map[string]sessionState
	stateFilePath    string
	stateVersion     uint64
	flushedVersion   uint64
	now              func() time.Time
	newImageProvider func(config.ImageGenerationConfig, map[string]string) (imagegen.Provider, error)
	runtimeAPIBase   string
	runtimeAPIToken  string
	runtimeAPIBin    string
	helpConfig       builtinHelpConfig
	prompts          *prompting.Loader
}

type builtinHelpConfig struct {
	chatEnabled       bool
	workEnabled       bool
	workTriggerTag    string
	workTriggerMode   string
	workTriggerPrefix string
}

const interruptedReplyMessage = "已收到你的新消息，当前回复已中断并切换到最新输入。"
const fileChangeEventPrefix = "[file_change] "
const immediateFeedbackReplyText = "收到！"
const immediateFeedbackModeReply = "reply"
const immediateFeedbackModeReaction = "reaction"
const defaultImmediateFeedbackEmoji = "SMILE"

func NewProcessor(
	backend llm.Backend,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	return &Processor{
		llm:              backend,
		sender:           sender,
		replies:          newReplyDispatcher(sender),
		failureMessage:   failureMessage,
		thinkingMessage:  thinkingMessage,
		feedbackMode:     immediateFeedbackModeReply,
		feedbackEmoji:    defaultImmediateFeedbackEmoji,
		sessions:         make(map[string]sessionState),
		now:              time.Now,
		newImageProvider: imagegen.NewProvider,
		helpConfig:       defaultBuiltinHelpConfig(),
		prompts:          prompting.DefaultLoader(),
	}
}

func (p *Processor) SetPromptLoader(loader *prompting.Loader) {
	if p == nil || loader == nil {
		return
	}
	p.prompts = loader
}

func (p *Processor) SetImmediateFeedback(mode, emojiType string) {
	if p == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.feedbackMode = normalizeImmediateFeedbackMode(mode)
	p.feedbackEmoji = normalizeImmediateFeedbackEmoji(emojiType)
}

func (p *Processor) SetBuiltinHelpConfig(cfg config.Config) {
	if p == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.helpConfig = builtinHelpConfigFromConfig(cfg)
}

func (p *Processor) SetRuntimeAPI(baseURL, token, runtimeBin string) {
	if p == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.runtimeAPIBase = strings.TrimSpace(baseURL)
	p.runtimeAPIToken = strings.TrimSpace(token)
	p.runtimeAPIBin = strings.TrimSpace(runtimeBin)
}

func (p *Processor) SetImageGeneration(cfg config.ImageGenerationConfig, env map[string]string) error {
	if p == nil {
		return nil
	}
	cfg = config.ImageGenerationConfig{
		Enabled:               cfg.Enabled,
		Provider:              strings.TrimSpace(cfg.Provider),
		Model:                 strings.TrimSpace(cfg.Model),
		BaseURL:               strings.TrimSpace(cfg.BaseURL),
		TimeoutSecs:           cfg.TimeoutSecs,
		Size:                  strings.TrimSpace(cfg.Size),
		Quality:               strings.TrimSpace(cfg.Quality),
		Background:            strings.TrimSpace(cfg.Background),
		OutputFormat:          strings.TrimSpace(cfg.OutputFormat),
		InputFidelity:         strings.TrimSpace(cfg.InputFidelity),
		UseCurrentAttachments: cfg.UseCurrentAttachments,
	}
	var provider imagegen.Provider
	if cfg.Enabled {
		factory := p.newImageProvider
		if factory == nil {
			factory = imagegen.NewProvider
		}
		var err error
		provider, err = factory(cfg, env)
		if err != nil {
			return err
		}
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.imageGeneration = cfg
	p.imageProvider = provider
	return nil
}

func (p *Processor) SetLLMBackend(backend llm.Backend) {
	if p == nil || backend == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.llm = backend
}

func (p *Processor) SetReplyMessages(failureMessage, thinkingMessage string) {
	if p == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.failureMessage = strings.TrimSpace(failureMessage)
	p.thinkingMessage = strings.TrimSpace(thinkingMessage)
}

func (p *Processor) runtimeSnapshot() processorRuntimeSnapshot {
	if p == nil {
		return processorRuntimeSnapshot{}
	}
	p.runtimeMu.RLock()
	defer p.runtimeMu.RUnlock()
	return processorRuntimeSnapshot{
		llm:             p.llm,
		failureMessage:  p.failureMessage,
		thinkingMessage: p.thinkingMessage,
		feedbackMode:    p.feedbackMode,
		feedbackEmoji:   p.feedbackEmoji,
		imageGeneration: p.imageGeneration,
		imageProvider:   p.imageProvider,
		runtimeAPIBase:  p.runtimeAPIBase,
		runtimeAPIToken: p.runtimeAPIToken,
		runtimeAPIBin:   p.runtimeAPIBin,
		helpConfig:      p.helpConfig,
	}
}

type processorRuntimeSnapshot struct {
	llm             llm.Backend
	failureMessage  string
	thinkingMessage string
	feedbackMode    string
	feedbackEmoji   string
	imageGeneration config.ImageGenerationConfig
	imageProvider   imagegen.Provider
	runtimeAPIBase  string
	runtimeAPIToken string
	runtimeAPIBin   string
	helpConfig      builtinHelpConfig
}

func defaultBuiltinHelpConfig() builtinHelpConfig {
	return builtinHelpConfig{
		chatEnabled:       true,
		workEnabled:       true,
		workTriggerTag:    "#work",
		workTriggerMode:   config.TriggerModeAt,
		workTriggerPrefix: "",
	}
}

func builtinHelpConfigFromConfig(cfg config.Config) builtinHelpConfig {
	return builtinHelpConfig{
		chatEnabled:       cfg.GroupScenes.Chat.Enabled,
		workEnabled:       cfg.GroupScenes.Work.Enabled,
		workTriggerTag:    strings.TrimSpace(cfg.GroupScenes.Work.TriggerTag),
		workTriggerMode:   strings.TrimSpace(cfg.TriggerMode),
		workTriggerPrefix: strings.TrimSpace(cfg.TriggerPrefix),
	}
}

func (p *Processor) ProcessJob(ctx context.Context, job Job) bool {
	return p.ProcessJobState(ctx, job) == JobProcessCompleted
}

func (p *Processor) ProcessJobState(ctx context.Context, job Job) JobProcessState {
	job.WorkflowPhase = normalizeJobWorkflowPhase(job.WorkflowPhase)
	p.enrichJobUserNames(ctx, &job)
	if handled, state := p.processBuiltinCommand(ctx, job); handled {
		return state
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
	if effectiveJobResponseMode(job) == jobResponseModeReply && strings.TrimSpace(job.SourceMessageID) != "" {
		return p.processReplyMessage(ctx, job)
	}
	return p.processSendMessage(ctx, job)
}

func (p *Processor) processSendMessage(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	p.prepareJobForLLM(ctx, &job)
	currentThreadID := p.getThreadID(sessionKey)
	promptText := p.buildPrompt(ctx, job, currentThreadID)
	reply, nextThreadID, err := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		nil,
	)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(err, context.Canceled) {
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
	p.startImageGeneration(ctx, job, reply, "")
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
	finalReply, nextThreadID, runErr := p.runLLM(
		ctx,
		currentThreadID,
		promptText,
		jobLLMRunOptions(job),
		p.buildLLMRunEnv(job),
		sendAgentMessage,
	)
	p.setThreadID(sessionKey, nextThreadID)
	if errors.Is(runErr, context.Canceled) {
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
			p.startImageGeneration(ctx, job, finalReply, messageID)
		}
	} else {
		p.startImageGeneration(ctx, job, finalReply, "")
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
		Scene:           job.Scene,
		Model:           job.LLMModel,
		Profile:         job.LLMProfile,
		ReasoningEffort: job.LLMReasoningEffort,
		Personality:     job.LLMPersonality,
		NoReplyToken:    job.NoReplyToken,
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
	p.rememberSessionAliases(sessionKey, baseKey+"|message:"+messageID)
}
