package connector

import (
	"context"
	"strings"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

type Processor struct {
	llm             agentbridge.Backend
	sender          Sender
	replies         *replyDispatcher
	failureMessage  string
	thinkingMessage string
	feedbackMode    string
	feedbackEmoji   string
	heartbeatConfig llmHeartbeatConfig
	runtimeMu       sync.RWMutex
	mu              sync.Mutex
	sessions        map[string]sessionState
	sessionAliases  map[string]string
	stateFilePath   string
	stateVersion    uint64
	flushedVersion  uint64
	now             func() time.Time
	runtimeAPIBase  string
	runtimeAPIToken string
	runtimeAPIBin   string
	helpConfig      builtinHelpConfig
	statusService   *builtinStatusService
	workspaceDir    string
	prompts         *prompting.Loader
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
const finalReplyDoneEmoji = "DONE"

func NewProcessor(
	backend agentbridge.Backend,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	processor := &Processor{
		llm:             backend,
		sender:          sender,
		replies:         newReplyDispatcher(sender),
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
		feedbackMode:    immediateFeedbackModeReply,
		feedbackEmoji:   defaultImmediateFeedbackEmoji,
		heartbeatConfig: defaultLLMHeartbeatConfig(),
		sessions:        make(map[string]sessionState),
		sessionAliases:  make(map[string]string),
		now:             time.Now,
		helpConfig:      defaultBuiltinHelpConfig(),
		prompts:         prompting.DefaultLoader(),
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

func (p *Processor) SetStatusStores(automationStore *automation.Store) {
	if p == nil {
		return
	}
	p.runtimeMu.RLock()
	status := p.statusService
	p.runtimeMu.RUnlock()
	if status != nil {
		status.SetStores(automationStore)
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

func (p *Processor) SetLLMBackend(backend agentbridge.Backend) {
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

func (p *Processor) SetWorkspaceDir(workspaceDir string) {
	if p == nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	p.workspaceDir = strings.TrimSpace(workspaceDir)
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
		heartbeatConfig: p.heartbeatConfig,
		runtimeAPIBase:  p.runtimeAPIBase,
		runtimeAPIToken: p.runtimeAPIToken,
		runtimeAPIBin:   p.runtimeAPIBin,
		helpConfig:      p.helpConfig,
		statusService:   p.statusService,
		workspaceDir:    p.workspaceDir,
	}
}

type processorRuntimeSnapshot struct {
	llm             agentbridge.Backend
	failureMessage  string
	thinkingMessage string
	feedbackMode    string
	feedbackEmoji   string
	heartbeatConfig llmHeartbeatConfig
	runtimeAPIBase  string
	runtimeAPIToken string
	runtimeAPIBin   string
	helpConfig      builtinHelpConfig
	statusService   *builtinStatusService
	workspaceDir    string
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
	if handled, state := p.applyLeadingSessionDirective(ctx, &job); handled {
		return state
	}
	if p.isEmptyWorkThreadBootstrap(job) {
		return p.processWorkThreadBootstrap(ctx, job)
	}
	if handled, state := p.processBuiltinCommand(ctx, job); handled {
		return state
	}

	sessionKey := sessionKeyForJob(job)
	p.touchSessionMessage(sessionKey, p.now())
	p.recordSessionMetadata(sessionKey, job)

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

func (p *Processor) applyLeadingSessionDirective(ctx context.Context, job *Job) (bool, JobProcessState) {
	if p == nil || job == nil || !isSessionCommand(job.Text) {
		return false, JobProcessCompleted
	}

	directive, err := parseSessionDirective(job.Text)
	if err != nil {
		return true, p.processSessionCommand(ctx, *job)
	}
	if strings.TrimSpace(job.Scene) != jobSceneWork {
		return true, p.processSessionCommand(ctx, *job)
	}
	if err := validateBackendSessionID(directive.SessionID); err != nil {
		return true, p.processSessionCommand(ctx, *job)
	}

	sessionKey := sessionKeyForJob(*job)
	p.touchSessionMessage(sessionKey, p.now())
	p.recordSessionMetadata(sessionKey, *job)
	p.setThreadID(sessionKey, directive.SessionID)
	job.Text = strings.TrimSpace(directive.Remainder)
	if job.Text == "" && len(job.Attachments) == 0 {
		reply := p.buildWorkThreadStatusMarkdown(*job, "已绑定后端 session。")
		if err := p.replies.respondCardWithTitle(ctx, *job, builtinWorkThreadCardTitle, reply); err != nil {
			logging.Errorf("send session bootstrap reply failed event_id=%s: %v", job.EventID, err)
		}
		return true, JobProcessCompleted
	}
	return false, JobProcessCompleted
}

func (p *Processor) isEmptyWorkThreadBootstrap(job Job) bool {
	return strings.TrimSpace(job.Scene) == jobSceneWork &&
		strings.TrimSpace(job.Text) == "" &&
		len(job.Attachments) == 0
}

func (p *Processor) processWorkThreadBootstrap(ctx context.Context, job Job) JobProcessState {
	sessionKey := sessionKeyForJob(job)
	p.touchSessionMessage(sessionKey, p.now())
	p.recordSessionMetadata(sessionKey, job)
	reply := p.buildWorkThreadStatusMarkdown(job, "Work thread 已创建。")
	if err := p.replies.respondCardWithTitle(ctx, job, builtinWorkThreadCardTitle, reply); err != nil {
		logging.Errorf("send work thread bootstrap reply failed event_id=%s: %v", job.EventID, err)
	}
	return JobProcessCompleted
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
