package llm

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
)

// NewInteractiveProviderSession creates one logical interactive conversation
// for the configured provider.
func NewInteractiveProviderSession(cfg FactoryConfig) (*InteractiveSession, error) {
	provider := normalizeProvider(cfg.Provider)
	if provider == "" {
		provider = ProviderCodex
	}
	switch provider {
	case ProviderCodex:
		return NewInteractiveSession(newCodexAppServerDriver(cfg.Codex)), nil
	case ProviderKimi:
		return NewInteractiveSession(newKimiWireDriver(cfg.Kimi)), nil
	case ProviderClaude:
		if !cfg.Claude.DisableStreamJSON {
			return NewInteractiveSession(newClaudeStreamDriver(cfg.Claude)), nil
		}
		return NewInteractiveSession(newRunDriver(provider, newClaudeBackend(cfg.Claude))), nil
	case ProviderOpenCode:
		if !cfg.OpenCode.DisableAppServer {
			return NewInteractiveSession(newOpenCodeAppServerDriver(cfg.OpenCode)), nil
		}
		return NewInteractiveSession(newRunDriver(provider, newOpenCodeBackend(cfg.OpenCode))), nil
	case ProviderGemini:
		return NewInteractiveSession(newRunDriver(provider, newGeminiBackend(cfg.Gemini))), nil
	default:
		return nil, ErrUnsupportedProvider(provider)
	}
}

type ErrUnsupportedProvider string

func (e ErrUnsupportedProvider) Error() string {
	return "unsupported llm_provider " + strconv.Quote(string(e))
}

type runDriver struct {
	provider string
	backend  Backend
	events   chan TurnEvent
	nextID   atomic.Uint64
}

func newRunDriver(provider string, backend Backend) *runDriver {
	return &runDriver{
		provider: strings.TrimSpace(provider),
		backend:  backend,
		events:   make(chan TurnEvent, 128),
	}
}

func (d *runDriver) SteerMode() SteerMode {
	return SteerModeQueueWhenBusy
}

func (d *runDriver) StartTurn(ctx context.Context, req RunRequest) (TurnRef, error) {
	id := d.nextID.Add(1)
	turn := TurnRef{
		ThreadID: strings.TrimSpace(req.ThreadID),
		TurnID:   "run-" + strconv.FormatUint(id, 10),
	}
	d.events <- TurnEvent{Provider: d.provider, ThreadID: turn.ThreadID, TurnID: turn.TurnID, Kind: TurnEventStarted}

	originalProgress := req.OnProgress
	req.OnProgress = func(step string) {
		if originalProgress != nil {
			originalProgress(step)
		}
		d.events <- TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     progressEventKind(step),
			Text:     strings.TrimSpace(step),
		}
	}
	go func() {
		result, err := d.backend.Run(ctx, req)
		if strings.TrimSpace(result.NextThreadID) != "" {
			turn.ThreadID = strings.TrimSpace(result.NextThreadID)
		}
		if err != nil {
			d.events <- TurnEvent{Provider: d.provider, ThreadID: turn.ThreadID, TurnID: turn.TurnID, Kind: TurnEventError, Err: err}
			return
		}
		if strings.TrimSpace(result.Reply) != "" {
			d.events <- TurnEvent{Provider: d.provider, ThreadID: turn.ThreadID, TurnID: turn.TurnID, Kind: TurnEventAssistantText, Text: strings.TrimSpace(result.Reply)}
		}
		d.events <- TurnEvent{Provider: d.provider, ThreadID: turn.ThreadID, TurnID: turn.TurnID, Kind: TurnEventCompleted, Usage: result.Usage}
	}()
	return turn, nil
}

func (d *runDriver) SteerTurn(context.Context, TurnRef, RunRequest) error {
	return ErrSteerUnsupported
}

func (d *runDriver) InterruptTurn(context.Context, TurnRef) error {
	return ErrSteerUnsupported
}

func (d *runDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *runDriver) Close() error {
	return nil
}

var ErrSteerUnsupported = errString("provider does not support native steer")

type errString string

func (e errString) Error() string { return string(e) }

func progressEventKind(step string) TurnEventKind {
	if strings.HasPrefix(step, "[file_change] ") {
		return TurnEventFileChange
	}
	return TurnEventAssistantText
}
