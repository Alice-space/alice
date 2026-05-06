package bootstrap

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestInteractiveProviderBackendForwardsAssistantTextAndDropsToolUse(t *testing.T) {
	for _, provider := range []string{
		llm.ProviderCodex,
		llm.ProviderClaude,
		llm.ProviderKimi,
		llm.ProviderOpenCode,
	} {
		t.Run(provider, func(t *testing.T) {
			sessionKey := "session-" + provider
			driver := &interactiveBackendTestDriver{
				provider: provider,
				events:   make(chan llm.TurnEvent, 8),
			}
			session := llm.NewInteractiveSession(driver)
			defer session.Close()

			backend := &interactiveProviderBackend{
				provider: provider,
				sessions: map[string]*llm.InteractiveSession{
					sessionKey: session,
				},
				runMu: map[string]*sync.Mutex{},
			}

			var progress []string
			var raw []string
			result, err := backend.runInteractive(context.Background(), sessionKey, llm.RunRequest{
				UserText: "hello",
				OnProgress: func(step string) {
					progress = append(progress, step)
				},
				OnRawEvent: func(event llm.RawEvent) {
					raw = append(raw, strings.TrimSpace(event.Kind)+":"+strings.TrimSpace(event.Detail))
				},
			})
			if err != nil {
				t.Fatalf("runInteractive returned error: %v", err)
			}
			if result.Reply != provider+" middle" {
				t.Fatalf("reply = %q, want %q", result.Reply, provider+" middle")
			}
			if len(progress) != 1 || progress[0] != provider+" middle" {
				t.Fatalf("progress = %#v, want only assistant text", progress)
			}
			wantRaw := []string{
				"user_text:hello",
				"tool_use:tool_use tool=`bash` command=`pwd`",
				"reasoning:thinking about the answer",
				"turn_completed:",
			}
			if strings.Join(raw, "\n") != strings.Join(wantRaw, "\n") {
				t.Fatalf("raw events = %#v, want %#v", raw, wantRaw)
			}
		})
	}
}

func TestInteractiveProviderBackendClosesIdleSession(t *testing.T) {
	sessionKey := "session-opencode"
	driver := &interactiveBackendTestDriver{
		provider: llm.ProviderOpenCode,
		events:   make(chan llm.TurnEvent, 8),
	}
	session := llm.NewInteractiveSession(driver)

	backend := &interactiveProviderBackend{
		provider: llm.ProviderOpenCode,
		idleTTL:  10 * time.Millisecond,
		sessions: map[string]*llm.InteractiveSession{
			sessionKey: session,
		},
		runMu: map[string]*sync.Mutex{},
	}

	result, err := backend.runInteractive(context.Background(), sessionKey, llm.RunRequest{UserText: "hello"})
	if err != nil {
		t.Fatalf("runInteractive returned error: %v", err)
	}
	if result.Reply != llm.ProviderOpenCode+" middle" {
		t.Fatalf("reply = %q", result.Reply)
	}

	waitForBootstrap(t, time.Second, func() bool {
		return backend.session(sessionKey) == nil && driver.closeCount() == 1
	}, "idle interactive session should be closed")
}

type interactiveBackendTestDriver struct {
	provider  string
	events    chan llm.TurnEvent
	closeOnce sync.Once
	closeMu   sync.Mutex
	closes    int
}

func (d *interactiveBackendTestDriver) SteerMode() llm.SteerMode {
	return llm.SteerModeNative
}

func (d *interactiveBackendTestDriver) StartTurn(_ context.Context, req llm.RunRequest) (llm.TurnRef, error) {
	turn := llm.TurnRef{ThreadID: "thread-1", TurnID: "turn-1"}
	go func() {
		d.events <- llm.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     llm.TurnEventUserText,
			Text:     "hello",
		}
		d.events <- llm.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     llm.TurnEventToolUse,
			Text:     "tool_use tool=`bash` command=`pwd`",
		}
		d.events <- llm.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     llm.TurnEventReasoning,
			Text:     "thinking about the answer",
		}
		d.events <- llm.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     llm.TurnEventAssistantText,
			Text:     d.provider + " middle",
		}
		d.events <- llm.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     llm.TurnEventCompleted,
		}
	}()
	_ = req
	return turn, nil
}

func (d *interactiveBackendTestDriver) SteerTurn(context.Context, llm.TurnRef, llm.RunRequest) error {
	return nil
}

func (d *interactiveBackendTestDriver) InterruptTurn(context.Context, llm.TurnRef) error {
	return nil
}

func (d *interactiveBackendTestDriver) Events() <-chan llm.TurnEvent {
	return d.events
}

func (d *interactiveBackendTestDriver) Close() error {
	d.closeOnce.Do(func() {
		d.closeMu.Lock()
		d.closes++
		d.closeMu.Unlock()
		close(d.events)
	})
	return nil
}

func (d *interactiveBackendTestDriver) closeCount() int {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()
	return d.closes
}

func waitForBootstrap(t *testing.T, timeout time.Duration, ok func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func TestInteractiveProviderBackendRunRejectsEmptySessionKey(t *testing.T) {
	backend := newInteractiveProviderBackend(llm.ProviderOpenCode, llm.FactoryConfig{
		OpenCode: llm.OpenCodeConfig{Command: "opencode", Timeout: 30 * time.Second},
	})
	_, err := backend.Run(context.Background(), llm.RunRequest{
		UserText: "hello",
		Env:      nil,
	})
	if err == nil {
		t.Fatal("expected error for empty session key (no fallback)")
	}
	if !strings.Contains(err.Error(), "session key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInteractiveProviderBackendRunUsesThreadIDWhenSessionKeyEmpty(t *testing.T) {
	driver := &interactiveBackendTestDriver{
		provider: llm.ProviderOpenCode,
		events:   make(chan llm.TurnEvent, 8),
	}
	session := llm.NewInteractiveSession(driver)
	defer session.Close()

	backend := newInteractiveProviderBackend(llm.ProviderOpenCode, llm.FactoryConfig{})
	backend.sessions = map[string]*llm.InteractiveSession{"ses_test": session}
	backend.runMu = map[string]*sync.Mutex{}

	_, err := backend.Run(context.Background(), llm.RunRequest{
		UserText: "hello",
		Env:      nil,
		ThreadID: "ses_test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInteractiveProviderBackendGoalWakeupForwardsStreamingEvents(t *testing.T) {
	for _, provider := range []string{
		llm.ProviderCodex,
		llm.ProviderClaude,
		llm.ProviderKimi,
		llm.ProviderOpenCode,
	} {
		t.Run(provider, func(t *testing.T) {
			sessionKey := "goal-session-" + provider
			driver := &interactiveBackendTestDriver{
				provider: provider,
				events:   make(chan llm.TurnEvent, 8),
			}
			session := llm.NewInteractiveSession(driver)
			defer session.Close()

			backend := &interactiveProviderBackend{
				provider: provider,
				sessions: map[string]*llm.InteractiveSession{
					sessionKey: session,
				},
				runMu: map[string]*sync.Mutex{},
			}

			var progress []string
			var rawEvents []string

			result, err := backend.runInteractive(context.Background(), sessionKey, llm.RunRequest{
				UserText: "continue your previous work",
				OnProgress: func(step string) {
					progress = append(progress, step)
				},
				OnRawEvent: func(event llm.RawEvent) {
					rawEvents = append(rawEvents, strings.TrimSpace(event.Kind)+":"+strings.TrimSpace(event.Detail))
				},
			})
			if err != nil {
				t.Fatalf("runInteractive: %v", err)
			}
			if result.Reply != provider+" middle" {
				t.Fatalf("reply = %q, want %q", result.Reply, provider+" middle")
			}
			if len(progress) != 1 {
				t.Fatalf("progress = %v, want 1 assistant text event forwarded to Feishu", progress)
			}

			hasReasoning := false
			hasToolUse := false
			for _, ev := range rawEvents {
				if strings.HasPrefix(ev, "reasoning:") {
					hasReasoning = true
				}
				if strings.HasPrefix(ev, "tool_use:") {
					hasToolUse = true
				}
			}
			if !hasReasoning {
				t.Error("expected reasoning event in OnRawEvent (SSE reasoning → feishu)")
			}
			if !hasToolUse {
				t.Error("expected tool_use event in OnRawEvent (SSE tool → feishu)")
			}
		})
	}
}
