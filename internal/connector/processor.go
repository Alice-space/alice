package connector

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
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
	statusService    *builtinStatusService
	prompts          *prompting.Loader
}

type StatusUsageSource struct {
	BotID            string
	BotName          string
	SessionStatePath string
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
	processor := &Processor{
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
	processor.statusService = newBuiltinStatusService(processor)
	return processor
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

func (p *Processor) SetStatusStores(automationStore *automation.Store, campaignStore *campaign.Store) {
	if p == nil {
		return
	}
	p.runtimeMu.RLock()
	status := p.statusService
	p.runtimeMu.RUnlock()
	if status != nil {
		status.SetStores(automationStore, campaignStore)
	}
}

func (p *Processor) SetStatusIdentity(botID, botName string) {
	if p == nil {
		return
	}
	p.runtimeMu.RLock()
	status := p.statusService
	p.runtimeMu.RUnlock()
	if status != nil {
		status.SetIdentity(botID, botName)
	}
}

func (p *Processor) SetStatusUsageSources(sources []StatusUsageSource) {
	if p == nil {
		return
	}
	p.runtimeMu.RLock()
	status := p.statusService
	p.runtimeMu.RUnlock()
	if status != nil {
		status.SetUsageSources(sources)
	}
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
		Moderation:            strings.TrimSpace(cfg.Moderation),
		N:                     cfg.N,
		OutputCompression:     cfg.OutputCompression,
		ResponseFormat:        strings.TrimSpace(cfg.ResponseFormat),
		Size:                  strings.TrimSpace(cfg.Size),
		Quality:               strings.TrimSpace(cfg.Quality),
		Background:            strings.TrimSpace(cfg.Background),
		OutputFormat:          strings.TrimSpace(cfg.OutputFormat),
		PartialImages:         cfg.PartialImages,
		Stream:                cfg.Stream,
		Style:                 strings.TrimSpace(cfg.Style),
		InputFidelity:         strings.TrimSpace(cfg.InputFidelity),
		MaskPath:              strings.TrimSpace(cfg.MaskPath),
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
		statusService:   p.statusService,
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
	statusService   *builtinStatusService
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
