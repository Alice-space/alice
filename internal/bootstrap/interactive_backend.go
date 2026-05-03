package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/sessionctx"
)

type steerBackend interface {
	agentbridge.Backend
	Steer(ctx context.Context, req agentbridge.RunRequest) error
}

type interactiveMultiBackend struct {
	defaultProvider string
	backends        map[string]agentbridge.Backend
}

func newInteractiveMultiBackend(defaultProvider string, backends map[string]agentbridge.Backend) (*interactiveMultiBackend, error) {
	normalizedDefault := normalizeBackendProvider(defaultProvider)
	out := make(map[string]agentbridge.Backend, len(backends))
	for rawProvider, backend := range backends {
		if backend == nil {
			continue
		}
		provider := normalizeBackendProvider(rawProvider)
		if provider == "" {
			provider = normalizedDefault
		}
		if provider == "" {
			provider = agentbridge.ProviderCodex
		}
		out[provider] = backend
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("multi backend requires at least one backend")
	}
	if normalizedDefault == "" {
		if _, ok := out[agentbridge.ProviderCodex]; ok {
			normalizedDefault = agentbridge.ProviderCodex
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

func (m *interactiveMultiBackend) Run(ctx context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	backend, provider, err := m.resolve(req.Provider)
	if err != nil {
		return agentbridge.RunResult{}, err
	}
	req.Provider = provider
	return backend.Run(ctx, req)
}

func (m *interactiveMultiBackend) Steer(ctx context.Context, req agentbridge.RunRequest) error {
	backend, provider, err := m.resolve(req.Provider)
	if err != nil {
		return err
	}
	steer, ok := backend.(steerBackend)
	if !ok {
		return agentbridge.ErrSteerUnsupported
	}
	req.Provider = provider
	return steer.Steer(ctx, req)
}

func (m *interactiveMultiBackend) resolve(rawProvider string) (agentbridge.Backend, string, error) {
	if m == nil {
		return nil, "", fmt.Errorf("multi backend is nil")
	}
	provider := normalizeBackendProvider(rawProvider)
	if provider == "" {
		provider = m.defaultProvider
	}
	if provider == "" {
		provider = agentbridge.ProviderCodex
	}
	backend, ok := m.backends[provider]
	if !ok {
		return nil, "", fmt.Errorf("llm backend for provider %q is unavailable", provider)
	}
	return backend, provider, nil
}

type interactiveProviderBackend struct {
	provider string
	cfg      agentbridge.FactoryConfig
	fallback agentbridge.Backend
	timeout  time.Duration

	mu       sync.Mutex
	sessions map[string]*agentbridge.InteractiveSession
	runMu    map[string]*sync.Mutex
}

func newInteractiveProviderBackend(provider string, cfg agentbridge.FactoryConfig, fallback agentbridge.Backend) *interactiveProviderBackend {
	return &interactiveProviderBackend{
		provider: normalizeBackendProvider(provider),
		cfg:      cfg,
		fallback: fallback,
		timeout:  providerTimeout(cfg),
		sessions: make(map[string]*agentbridge.InteractiveSession),
		runMu:    make(map[string]*sync.Mutex),
	}
}

func (b *interactiveProviderBackend) Run(ctx context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	sessionKey := runRequestSessionKey(req)
	if sessionKey == "" {
		return b.fallback.Run(ctx, req)
	}
	req.Provider = b.provider
	return b.runInteractive(ctx, sessionKey, req)
}

func (b *interactiveProviderBackend) Steer(ctx context.Context, req agentbridge.RunRequest) error {
	sessionKey := runRequestSessionKey(req)
	if sessionKey == "" {
		return agentbridge.ErrNoActiveTurn
	}
	req.Provider = b.provider
	session := b.session(sessionKey)
	if session == nil {
		return agentbridge.ErrNoActiveTurn
	}
	_, err := session.Steer(ctx, req)
	return err
}

func (b *interactiveProviderBackend) runInteractive(ctx context.Context, sessionKey string, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	runMu := b.sessionRunMutex(sessionKey)
	runMu.Lock()
	defer runMu.Unlock()

	session, err := b.ensureSession(sessionKey)
	if err != nil {
		return agentbridge.RunResult{}, err
	}

	runCtx := ctx
	cancel := func() {}
	if b.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, b.timeout)
	}
	defer cancel()

	submitted, err := session.Submit(runCtx, req)
	if err != nil {
		return agentbridge.RunResult{}, err
	}

	reply := ""
	nextThreadID := strings.TrimSpace(submitted.ThreadID)
	var usage agentbridge.Usage
	for {
		select {
		case <-runCtx.Done():
			b.interruptAndDropSession(sessionKey, session)
			return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, runCtx.Err()
		case event, ok := <-session.Events():
			if !ok {
				b.dropSession(sessionKey, session)
				return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, context.Canceled
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
			case agentbridge.TurnEventAssistantText:
				text := strings.TrimSpace(event.Text)
				if text != "" {
					reply = text
					if req.OnProgress != nil {
						req.OnProgress(text)
					}
				}
			case agentbridge.TurnEventFileChange:
				if req.OnProgress != nil && strings.TrimSpace(event.Text) != "" {
					req.OnProgress("[file_change] " + strings.TrimSpace(event.Text))
				}
			case agentbridge.TurnEventUserText, agentbridge.TurnEventReasoning, agentbridge.TurnEventToolUse:
				// User echoes, reasoning, and tool-use events are backend
				// context, not Feishu progress messages.
				emitInteractiveRawEvent(req.OnRawEvent, event)
			case agentbridge.TurnEventCompleted:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, nil
			case agentbridge.TurnEventInterrupted:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, context.Canceled
			case agentbridge.TurnEventError:
				emitInteractiveRawEvent(req.OnRawEvent, event)
				if event.Err != nil {
					return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, event.Err
				}
				return agentbridge.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, fmt.Errorf("%s turn failed", b.provider)
			}
		}
	}
}

func emitInteractiveRawEvent(fn agentbridge.RawEventFunc, event agentbridge.TurnEvent) {
	if fn == nil {
		return
	}
	kind := interactiveRawEventKind(event.Kind)
	if kind == "" {
		return
	}
	fn(agentbridge.RawEvent{
		Kind:   kind,
		Line:   strings.TrimSpace(event.Raw),
		Detail: strings.TrimSpace(event.Text),
	})
}

func interactiveRawEventKind(kind agentbridge.TurnEventKind) string {
	switch kind {
	case agentbridge.TurnEventUserText:
		return "user_text"
	case agentbridge.TurnEventReasoning:
		return "reasoning"
	case agentbridge.TurnEventToolUse:
		return "tool_use"
	case agentbridge.TurnEventCompleted:
		return "turn_completed"
	case agentbridge.TurnEventInterrupted:
		return "turn_interrupted"
	case agentbridge.TurnEventError:
		return "error"
	default:
		return ""
	}
}

func (b *interactiveProviderBackend) sessionRunMutex(sessionKey string) *sync.Mutex {
	sessionKey = strings.TrimSpace(sessionKey)
	b.mu.Lock()
	defer b.mu.Unlock()
	if mu := b.runMu[sessionKey]; mu != nil {
		return mu
	}
	mu := &sync.Mutex{}
	b.runMu[sessionKey] = mu
	return mu
}

func (b *interactiveProviderBackend) session(sessionKey string) *agentbridge.InteractiveSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[strings.TrimSpace(sessionKey)]
}

func (b *interactiveProviderBackend) ensureSession(sessionKey string) (*agentbridge.InteractiveSession, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil, agentbridge.ErrNoActiveTurn
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if session := b.sessions[sessionKey]; session != nil {
		return session, nil
	}
	session, err := agentbridge.NewInteractiveProviderSession(b.cfg)
	if err != nil {
		return nil, err
	}
	b.sessions[sessionKey] = session
	return session, nil
}

func (b *interactiveProviderBackend) interruptAndDropSession(sessionKey string, session *agentbridge.InteractiveSession) {
	if session != nil {
		interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = session.Interrupt(interruptCtx)
		cancel()
	}
	b.dropSession(sessionKey, session)
}

func (b *interactiveProviderBackend) dropSession(sessionKey string, session *agentbridge.InteractiveSession) {
	if session == nil {
		return
	}
	sessionKey = strings.TrimSpace(sessionKey)
	b.mu.Lock()
	if b.sessions[sessionKey] == session {
		delete(b.sessions, sessionKey)
	}
	b.mu.Unlock()
	_ = session.Close()
}

func runRequestSessionKey(req agentbridge.RunRequest) string {
	if req.Env == nil {
		return ""
	}
	return strings.TrimSpace(req.Env[sessionctx.EnvSessionKey])
}

func providerTimeout(cfg agentbridge.FactoryConfig) time.Duration {
	switch normalizeBackendProvider(cfg.Provider) {
	case agentbridge.ProviderClaude:
		return cfg.Claude.Timeout
	case agentbridge.ProviderGemini:
		return cfg.Gemini.Timeout
	case agentbridge.ProviderKimi:
		return cfg.Kimi.Timeout
	case agentbridge.ProviderOpenCode:
		return cfg.OpenCode.Timeout
	case agentbridge.ProviderCodex, "":
		return cfg.Codex.Timeout
	default:
		return 0
	}
}

func normalizeBackendProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
