package connector

import (
	"context"
	"fmt"
	"strconv"
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
	maxHeartbeatFileChangeRows   = 5
	maxHeartbeatFileChangeItems  = 50
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
	RecordFileChange(message string)
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
	fileChanges     map[string]llmHeartbeatFileChange
	fileChangeOrder []string
	statusMessageID string
}

type llmHeartbeatFileChange struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	HasStats  bool
	Raw       string
	SeenAt    time.Time
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
	h.mu.Lock()
	h.lastVisibleAt = now
	h.lastBackendAt = now
	h.lastBackendKind = "progress"
	h.mu.Unlock()
}

func (h *llmHeartbeat) RecordFileChange(message string) {
	if h == nil {
		return
	}
	now := h.processor.now()
	changes := parseLLMHeartbeatFileChanges(message, now)
	h.mu.Lock()
	h.lastBackendAt = now
	h.lastBackendKind = "file_change"
	for _, change := range changes {
		h.upsertFileChangeLocked(change)
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
		FileChanges:     snapshot.fileChangeLines,
		FileChangeTotal: snapshot.fileChangeTotal,
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
		FileChanges:     snapshot.fileChangeLines,
		FileChangeTotal: snapshot.fileChangeTotal,
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
	fileChangeLines []string
	fileChangeTotal int
}

func (h *llmHeartbeat) snapshot() llmHeartbeatSnapshot {
	now := h.processor.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	fileChangeLines, fileChangeTotal := h.fileChangeSnapshotLocked()
	return llmHeartbeatSnapshot{
		statusMessageID: h.statusMessageID,
		elapsed:         now.Sub(h.started),
		sinceVisible:    now.Sub(h.lastVisibleAt),
		sinceBackend:    now.Sub(h.lastBackendAt),
		lastBackendKind: h.lastBackendKind,
		fileChangeLines: fileChangeLines,
		fileChangeTotal: fileChangeTotal,
	}
}

func (h *llmHeartbeat) upsertFileChangeLocked(change llmHeartbeatFileChange) {
	key := strings.TrimSpace(change.Path)
	if key == "" {
		key = strings.TrimSpace(change.Raw)
	}
	if key == "" {
		return
	}
	if h.fileChanges == nil {
		h.fileChanges = make(map[string]llmHeartbeatFileChange)
	}
	h.fileChanges[key] = change
	h.fileChangeOrder = moveStringToEnd(h.fileChangeOrder, key)
	for len(h.fileChangeOrder) > maxHeartbeatFileChangeItems {
		drop := h.fileChangeOrder[0]
		h.fileChangeOrder = h.fileChangeOrder[1:]
		delete(h.fileChanges, drop)
	}
}

func (h *llmHeartbeat) fileChangeSnapshotLocked() ([]string, int) {
	total := len(h.fileChangeOrder)
	if total == 0 {
		return nil, 0
	}
	start := total - maxHeartbeatFileChangeRows
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, total-start)
	for _, key := range h.fileChangeOrder[start:] {
		change, ok := h.fileChanges[key]
		if !ok {
			continue
		}
		line := formatLLMHeartbeatFileChange(change)
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, total
}

func parseLLMHeartbeatFileChanges(message string, seenAt time.Time) []llmHeartbeatFileChange {
	lines := splitMessageLines(message)
	changes := make([]llmHeartbeatFileChange, 0, len(lines))
	for _, line := range lines {
		change := parseLLMHeartbeatFileChangeLine(line, seenAt)
		if strings.TrimSpace(change.Path) == "" && strings.TrimSpace(change.Raw) == "" {
			continue
		}
		changes = append(changes, change)
	}
	return changes
}

func parseLLMHeartbeatFileChangeLine(line string, seenAt time.Time) llmHeartbeatFileChange {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
	if line == "" {
		return llmHeartbeatFileChange{}
	}
	change := llmHeartbeatFileChange{
		Raw:    clipText(line, 500),
		Status: "已更改",
		SeenAt: seenAt,
	}
	if !strings.HasPrefix(line, "`") {
		return change
	}
	rest := strings.TrimPrefix(line, "`")
	endPath := strings.Index(rest, "`")
	if endPath <= 0 {
		return change
	}
	change.Path = strings.TrimSpace(rest[:endPath])
	tail := strings.TrimSpace(rest[endPath+1:])
	status, additions, deletions, hasStats := parseLLMHeartbeatFileChangeTail(tail)
	if status != "" {
		change.Status = status
	}
	change.Additions = additions
	change.Deletions = deletions
	change.HasStats = hasStats
	return change
}

func parseLLMHeartbeatFileChangeTail(tail string) (string, int, int, bool) {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return "", 0, 0, false
	}
	status := tail
	if idx := strings.LastIndex(tail, " (+"); idx >= 0 && strings.HasSuffix(tail, ")") {
		status = strings.TrimSpace(tail[:idx])
		stats := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(tail[idx+2:]), "+"), ")")
		parts := strings.SplitN(stats, "/-", 2)
		if len(parts) == 2 {
			additions, addErr := strconv.Atoi(strings.TrimSpace(parts[0]))
			deletions, delErr := strconv.Atoi(strings.TrimSpace(parts[1]))
			if addErr == nil && delErr == nil {
				return status, additions, deletions, true
			}
		}
	}
	return status, 0, 0, false
}

func formatLLMHeartbeatFileChange(change llmHeartbeatFileChange) string {
	if strings.TrimSpace(change.Path) == "" {
		return clipText(change.Raw, 300)
	}
	line := fmt.Sprintf("`%s` %s", change.Path, defaultIfEmpty(change.Status, "已更改"))
	if change.HasStats {
		line += fmt.Sprintf(" (+%d/-%d)", change.Additions, change.Deletions)
	}
	return line
}

func moveStringToEnd(values []string, value string) []string {
	next := values[:0]
	for _, item := range values {
		if item == value {
			continue
		}
		next = append(next, item)
	}
	return append(next, value)
}
