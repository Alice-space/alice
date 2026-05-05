package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/sessionctx"
)

type steerBackend interface {
	llm.Backend
	Steer(ctx context.Context, req llm.RunRequest) error
}

type interactiveMultiBackend struct {
	defaultProvider string
	backends        map[string]llm.Backend
}

func newInteractiveMultiBackend(defaultProvider string, backends map[string]llm.Backend) (*interactiveMultiBackend, error) {
	normalizedDefault := normalizeBackendProvider(defaultProvider)
	out := make(map[string]llm.Backend, len(backends))
	for rawProvider, backend := range backends {
		if backend == nil {
			continue
		}
		provider := normalizeBackendProvider(rawProvider)
		if provider == "" {
			provider = normalizedDefault
		}
		if provider == "" {
			provider = llm.ProviderCodex
		}
		out[provider] = backend
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("multi backend requires at least one backend")
	}
	if normalizedDefault == "" {
		if _, ok := out[llm.ProviderCodex]; ok {
			normalizedDefault = llm.ProviderCodex
		} else if len(out) == 1 {
			for provider := range out {
				normalizedDefault = provider
			}
		}
	}
	if _, ok := out[normalizedDefault]; !ok {
		return nil, fmt.Errorf("multi backend: defaultProvider %q is not in the registered backends", normalizedDefault)
	}
	return &interactiveMultiBackend{defaultProvider: normalizedDefault, backends: out}, nil
}

func (m *interactiveMultiBackend) Run(ctx context.Context, req llm.RunRequest) (llm.RunResult, error) {
	backend, provider, err := m.resolve(req.Provider)
	if err != nil {
		return llm.RunResult{}, err
	}
	req.Provider = provider
	return backend.Run(ctx, req)
}

func (m *interactiveMultiBackend) Steer(ctx context.Context, req llm.RunRequest) error {
	backend, provider, err := m.resolve(req.Provider)
	if err != nil {
		return err
	}
	steer, ok := backend.(steerBackend)
	if !ok {
		return llm.ErrSteerUnsupported
	}
	req.Provider = provider
	return steer.Steer(ctx, req)
}

func (m *interactiveMultiBackend) resolve(rawProvider string) (llm.Backend, string, error) {
	if m == nil {
		return nil, "", fmt.Errorf("multi backend is nil")
	}
	provider := normalizeBackendProvider(rawProvider)
	if provider == "" {
		provider = m.defaultProvider
	}
	if provider == "" {
		provider = llm.ProviderCodex
	}
	backend, ok := m.backends[provider]
	if !ok {
		return nil, "", fmt.Errorf("llm backend for provider %q is unavailable", provider)
	}
	return backend, provider, nil
}

type interactiveProviderBackend struct {
	provider string
	cfg      llm.FactoryConfig
	fallback llm.Backend
	timeout  time.Duration
	idleTTL  time.Duration

	mu             sync.Mutex
	sessions       map[string]*llm.InteractiveSession
	runMu          map[string]*sync.Mutex
	idleTimers     map[string]*time.Timer
	idleGeneration map[string]uint64
}

// Keep a short grace window for immediate follow-up turns while preventing one
// long-lived provider process per Feishu session from accumulating indefinitely.
const defaultInteractiveSessionIdleTTL = 30 * time.Second

func newInteractiveProviderBackend(provider string, cfg llm.FactoryConfig, fallback llm.Backend) *interactiveProviderBackend {
	return &interactiveProviderBackend{
		provider:       normalizeBackendProvider(provider),
		cfg:            cfg,
		fallback:       fallback,
		timeout:        providerTimeout(cfg),
		idleTTL:        defaultInteractiveSessionIdleTTL,
		sessions:       make(map[string]*llm.InteractiveSession),
		runMu:          make(map[string]*sync.Mutex),
		idleTimers:     make(map[string]*time.Timer),
		idleGeneration: make(map[string]uint64),
	}
}

func (b *interactiveProviderBackend) Run(ctx context.Context, req llm.RunRequest) (llm.RunResult, error) {
	sessionKey := runRequestSessionKey(req)
	if sessionKey == "" {
		return b.fallback.Run(ctx, req)
	}
	req.Provider = b.provider
	return b.runInteractive(ctx, sessionKey, req)
}

func (b *interactiveProviderBackend) Steer(ctx context.Context, req llm.RunRequest) error {
	sessionKey := runRequestSessionKey(req)
	if sessionKey == "" {
		return llm.ErrNoActiveTurn
	}
	req.Provider = b.provider
	session := b.session(sessionKey)
	if session == nil {
		return llm.ErrNoActiveTurn
	}
	_, err := session.Steer(ctx, req)
	return err
}

func (b *interactiveProviderBackend) runInteractive(ctx context.Context, sessionKey string, req llm.RunRequest) (llm.RunResult, error) {
	runMu := b.sessionRunMutex(sessionKey)
	runMu.Lock()
	defer runMu.Unlock()

	session, err := b.ensureSession(sessionKey)
	if err != nil {
		return llm.RunResult{}, err
	}

	runCtx := ctx
	cancel := func() {}
	if b.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, b.timeout)
	}
	defer cancel()

	submitted, err := session.Submit(runCtx, req)
	if err != nil {
		return llm.RunResult{}, err
	}

	reply := ""
	nextThreadID := strings.TrimSpace(submitted.ThreadID)
	var usage llm.Usage
	for {
		select {
		case <-runCtx.Done():
			b.interruptAndDropSession(sessionKey, session)
			return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, runCtx.Err()
		case event, ok := <-session.Events():
			if !ok {
				b.dropSession(sessionKey, session)
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, context.Canceled
			}
			if event.TurnID != "" && submitted.TurnID != "" && event.TurnID != submitted.TurnID {
				continue
			}
			if threadID := strings.TrimSpace(event.ThreadID); threadID != "" {
				nextThreadID = threadID
			}
			if event.Usage.HasUsage() {
				usage = event.Usage
			}
			switch event.Kind {
			case llm.TurnEventAssistantText:
				text := strings.TrimSpace(event.Text)
				if text != "" {
					reply = text
					if req.OnProgress != nil {
						req.OnProgress(text)
					}
				}
			case llm.TurnEventFileChange:
				if req.OnProgress != nil && strings.TrimSpace(event.Text) != "" {
					req.OnProgress("[file_change] " + strings.TrimSpace(event.Text))
				}
			case llm.TurnEventUserText, llm.TurnEventReasoning, llm.TurnEventToolUse:
				// User echoes, reasoning, and tool-use events are backend
				// context, not Feishu progress messages.
				emitInteractiveRawEvent(req.OnRawEvent, event)
			case llm.TurnEventCompleted:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				b.scheduleSessionIdleClose(sessionKey, session)
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, nil
			case llm.TurnEventInterrupted:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				b.dropSession(sessionKey, session)
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, context.Canceled
			case llm.TurnEventError:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				b.dropSession(sessionKey, session)
				if event.Err != nil {
					return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, event.Err
				}
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, fmt.Errorf("%s turn failed", b.provider)
			}
		}
	}
}

func emitInteractiveRawEvent(fn llm.RawEventFunc, event llm.TurnEvent) {
	if fn == nil {
		return
	}
	kind := interactiveRawEventKind(event.Kind)
	if kind == "" {
		return
	}
	fn(llm.RawEvent{
		Kind:   kind,
		Line:   strings.TrimSpace(event.Raw),
		Detail: strings.TrimSpace(event.Text),
	})
}

func interactiveRawEventKind(kind llm.TurnEventKind) string {
	switch kind {
	case llm.TurnEventUserText:
		return "user_text"
	case llm.TurnEventReasoning:
		return "reasoning"
	case llm.TurnEventToolUse:
		return "tool_use"
	case llm.TurnEventCompleted:
		return "turn_completed"
	case llm.TurnEventInterrupted:
		return "turn_interrupted"
	case llm.TurnEventError:
		return "error"
	default:
		return ""
	}
}

func (b *interactiveProviderBackend) sessionRunMutex(sessionKey string) *sync.Mutex {
	sessionKey = strings.TrimSpace(sessionKey)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.runMu == nil {
		b.runMu = make(map[string]*sync.Mutex)
	}
	if mu := b.runMu[sessionKey]; mu != nil {
		return mu
	}
	mu := &sync.Mutex{}
	b.runMu[sessionKey] = mu
	return mu
}

func (b *interactiveProviderBackend) session(sessionKey string) *llm.InteractiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[strings.TrimSpace(sessionKey)]
}

func (b *interactiveProviderBackend) ensureSession(sessionKey string) (*llm.InteractiveSession, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, llm.ErrNoActiveTurn
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = make(map[string]*llm.InteractiveSession)
	}
	if session := b.sessions[sessionKey]; session != nil {
		b.cancelSessionIdleCloseLocked(sessionKey)
		return session, nil
	}
	session, err := llm.NewInteractiveProviderSession(b.cfg)
	if err != nil {
		return nil, err
	}
	b.sessions[sessionKey] = session
	return session, nil
}

func (b *interactiveProviderBackend) scheduleSessionIdleClose(sessionKey string, session *llm.InteractiveSession) {
	if b == nil || session == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || b.idleTTL <= 0 {
		return
	}
	b.mu.Lock()
	if b.sessions[sessionKey] != session {
		b.mu.Unlock()
		return
	}
	b.ensureIdleMapsLocked()
	if timer := b.idleTimers[sessionKey]; timer != nil {
		timer.Stop()
	}
	b.idleGeneration[sessionKey]++
	generation := b.idleGeneration[sessionKey]
	b.idleTimers[sessionKey] = time.AfterFunc(b.idleTTL, func() {
		b.closeIdleSession(sessionKey, session, generation)
	})
	b.mu.Unlock()
}

func (b *interactiveProviderBackend) closeIdleSession(sessionKey string, session *llm.InteractiveSession, generation uint64) {
	runMu := b.sessionRunMutex(sessionKey)
	runMu.Lock()
	defer runMu.Unlock()

	b.mu.Lock()
	if b.sessions[sessionKey] != session || b.idleGeneration[sessionKey] != generation {
		b.mu.Unlock()
		return
	}
	delete(b.sessions, sessionKey)
	delete(b.idleTimers, sessionKey)
	delete(b.idleGeneration, sessionKey)
	if b.runMu[sessionKey] == runMu {
		delete(b.runMu, sessionKey)
	}
	b.mu.Unlock()

	logging.Infof("interactive backend idle session closed provider=%s session=%s", b.provider, sessionKey)
	_ = session.Close()
}

func (b *interactiveProviderBackend) interruptAndDropSession(sessionKey string, session *llm.InteractiveSession) {
	if session != nil {
		interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = session.Interrupt(interruptCtx)
		cancel()
	}
	b.dropSession(sessionKey, session)
}

func (b *interactiveProviderBackend) dropSession(sessionKey string, session *llm.InteractiveSession) {
	if session == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	b.mu.Lock()
	if b.sessions[sessionKey] == session {
		delete(b.sessions, sessionKey)
	}
	if timer := b.idleTimers[sessionKey]; timer != nil {
		timer.Stop()
	}
	delete(b.idleTimers, sessionKey)
	delete(b.idleGeneration, sessionKey)
	delete(b.runMu, sessionKey)
	b.mu.Unlock()
	_ = session.Close()
}

func (b *interactiveProviderBackend) ensureIdleMapsLocked() {
	if b.idleTimers == nil {
		b.idleTimers = make(map[string]*time.Timer)
	}
	if b.idleGeneration == nil {
		b.idleGeneration = make(map[string]uint64)
	}
}

func (b *interactiveProviderBackend) cancelSessionIdleCloseLocked(sessionKey string) {
	b.ensureIdleMapsLocked()
	if timer := b.idleTimers[sessionKey]; timer != nil {
		timer.Stop()
		delete(b.idleTimers, sessionKey)
	}
	b.idleGeneration[sessionKey]++
}

func runRequestSessionKey(req llm.RunRequest) string {
	if req.Env == nil {
		return ""
	}
	return strings.TrimSpace(req.Env[sessionctx.EnvSessionKey])
}

func providerTimeout(cfg llm.FactoryConfig) time.Duration {
	switch normalizeBackendProvider(cfg.Provider) {
	case llm.ProviderClaude:
		return cfg.Claude.Timeout
	case llm.ProviderGemini:
		return cfg.Gemini.Timeout
	case llm.ProviderKimi:
		return cfg.Kimi.Timeout
	case llm.ProviderOpenCode:
		return cfg.OpenCode.Timeout
	case llm.ProviderCodex, "":
		return cfg.Codex.Timeout
	default:
		return 0
	}
}

func normalizeBackendProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
