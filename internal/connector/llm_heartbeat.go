package connector

import (
	"context"
	"strings"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/logging"
)

const (
	defaultHeartbeatFirstSilence = time.Minute
	defaultHeartbeatUpdate       = time.Minute
	defaultHeartbeatBackendStale = 5 * time.Minute
	maxHeartbeatFileChanges      = 5
)

type llmHeartbeatConfig struct {
	Enabled           bool
	FirstSilenceAfter time.Duration
	UpdateInterval    time.Duration
	BackendStaleAfter time.Duration
}

func defaultLLMHeartbeatConfig() llmHeartbeatConfig {
	return llmHeartbeatConfig{
		Enabled:           true,
		FirstSilenceAfter: defaultHeartbeatFirstSilence,
		UpdateInterval:    defaultHeartbeatUpdate,
		BackendStaleAfter: defaultHeartbeatBackendStale,
	}
}

func (c llmHeartbeatConfig) normalized() llmHeartbeatConfig {
	if !c.Enabled {
		return c
	}
	if c.FirstSilenceAfter <= 0 {
		c.FirstSilenceAfter = defaultHeartbeatFirstSilence
	}
	if c.UpdateInterval <= 0 {
		c.UpdateInterval = defaultHeartbeatUpdate
	}
	if c.BackendStaleAfter <= 0 {
		c.BackendStaleAfter = defaultHeartbeatBackendStale
	}
	return c
}

type llmRunObserver interface {
	RecordVisibleOutput(message string)
	RecordBackendEvent(agentbridge.RawEvent)
}

type cardPatcher interface {
	PatchCard(ctx context.Context, messageID, cardContent string) error
}

type llmHeartbeat struct {
	processor *Processor
	job       Job
	cfg       llmHeartbeatConfig
	started   time.Time

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu              sync.Mutex
	lastVisibleAt   time.Time
	lastBackendAt   time.Time
	lastBackendKind string
	fileChanges     []string
	statusMessageID string
}

func (p *Processor) startLLMHeartbeat(ctx context.Context, job Job) *llmHeartbeat {
	if p == nil {
		return nil
	}
	cfg := p.runtimeSnapshot().heartbeatConfig.normalized()
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(job.SourceMessageID) == "" {
		return nil
	}
	started := p.now()
	hctx, cancel := context.WithCancel(ctx)
	h := &llmHeartbeat{
		processor:     p,
		job:           job,
		cfg:           cfg,
		started:       started,
		ctx:           hctx,
		cancel:        cancel,
		done:          make(chan struct{}),
		lastVisibleAt: started,
		lastBackendAt: started,
	}
	go h.run()
	return h
}

func (h *llmHeartbeat) RecordVisibleOutput(message string) {
	if h == nil {
		return
	}
	now := h.processor.now()
	normalized := strings.TrimSpace(message)
	fileChange := ""
	if strings.HasPrefix(normalized, fileChangeEventPrefix) {
		fileChange = strings.TrimSpace(strings.TrimPrefix(normalized, fileChangeEventPrefix))
	}
	h.mu.Lock()
	h.lastVisibleAt = now
	h.lastBackendAt = now
	h.lastBackendKind = "progress"
	if fileChange != "" {
		h.fileChanges = append(h.fileChanges, clipText(fileChange, 500))
		if len(h.fileChanges) > maxHeartbeatFileChanges {
			h.fileChanges = append([]string(nil), h.fileChanges[len(h.fileChanges)-maxHeartbeatFileChanges:]...)
		}
	}
	h.mu.Unlock()
}

func (h *llmHeartbeat) RecordBackendEvent(event agentbridge.RawEvent) {
	if h == nil {
		return
	}
	kind := strings.TrimSpace(event.Kind)
	if kind == "" {
		kind = "raw"
	}
	h.mu.Lock()
	h.lastBackendAt = h.processor.now()
	h.lastBackendKind = kind
	h.mu.Unlock()
}

func (h *llmHeartbeat) Stop(ctx context.Context, state string) {
	if h == nil {
		return
	}
	h.cancel()
	<-h.done
	h.patchFinalState(ctx, state)
}

func (h *llmHeartbeat) run() {
	defer close(h.done)
	ticker := time.NewTicker(h.cfg.UpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.tick(h.ctx)
		}
	}
}

func (h *llmHeartbeat) tick(ctx context.Context) {
	snapshot := h.snapshot()
	if snapshot.statusMessageID == "" && snapshot.sinceVisible < h.cfg.FirstSilenceAfter {
		return
	}
	content := buildLLMHeartbeatCardContent(llmHeartbeatCardState{
		Status:          h.statusLabel(snapshot),
		Elapsed:         snapshot.elapsed,
		SinceVisible:    snapshot.sinceVisible,
		SinceBackend:    snapshot.sinceBackend,
		LastBackendKind: snapshot.lastBackendKind,
		FileChanges:     snapshot.fileChanges,
	})
	if snapshot.statusMessageID == "" {
		h.createStatusCard(ctx, content)
		return
	}
	h.patchStatusCard(ctx, snapshot.statusMessageID, content)
}

func (h *llmHeartbeat) patchFinalState(ctx context.Context, state string) {
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	snapshot := h.snapshot()
	if snapshot.statusMessageID == "" {
		return
	}
	content := buildLLMHeartbeatCardContent(llmHeartbeatCardState{
		Status:          state,
		Elapsed:         snapshot.elapsed,
		SinceVisible:    snapshot.sinceVisible,
		SinceBackend:    snapshot.sinceBackend,
		LastBackendKind: snapshot.lastBackendKind,
		FileChanges:     snapshot.fileChanges,
	})
	h.patchStatusCard(ctx, snapshot.statusMessageID, content)
}

func (h *llmHeartbeat) statusLabel(snapshot llmHeartbeatSnapshot) string {
	if snapshot.sinceBackend >= h.cfg.BackendStaleAfter {
		return "疑似无响应"
	}
	if snapshot.sinceVisible >= h.cfg.FirstSilenceAfter {
		return "运行中（后端仍有活动）"
	}
	return "运行中"
}

func (h *llmHeartbeat) createStatusCard(ctx context.Context, content string) {
	if h == nil || h.processor == nil || h.processor.replies == nil {
		return
	}
	messageID, err := h.processor.replies.replyCard(ctx, h.job.SourceMessageID, content, jobPrefersThreadReply(h.job))
	if err != nil {
		logging.Warnf("send llm heartbeat card failed event_id=%s: %v", h.job.EventID, err)
		return
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}
	h.mu.Lock()
	if h.statusMessageID == "" {
		h.statusMessageID = messageID
	}
	h.mu.Unlock()
	h.processor.rememberReplySessionMessage(h.job, messageID)
}

func (h *llmHeartbeat) patchStatusCard(ctx context.Context, messageID, content string) {
	if h == nil || h.processor == nil || h.processor.sender == nil {
		return
	}
	patcher, ok := h.processor.sender.(cardPatcher)
	if !ok {
		return
	}
	if err := patcher.PatchCard(ctx, messageID, content); err != nil {
		logging.Warnf("patch llm heartbeat card failed event_id=%s message_id=%s: %v", h.job.EventID, messageID, err)
	}
}

type llmHeartbeatSnapshot struct {
	statusMessageID string
	elapsed         time.Duration
	sinceVisible    time.Duration
	sinceBackend    time.Duration
	lastBackendKind string
	fileChanges     []string
}

func (h *llmHeartbeat) snapshot() llmHeartbeatSnapshot {
	now := h.processor.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	return llmHeartbeatSnapshot{
		statusMessageID: h.statusMessageID,
		elapsed:         now.Sub(h.started),
		sinceVisible:    now.Sub(h.lastVisibleAt),
		sinceBackend:    now.Sub(h.lastBackendAt),
		lastBackendKind: h.lastBackendKind,
		fileChanges:     append([]string(nil), h.fileChanges...),
	}
}
