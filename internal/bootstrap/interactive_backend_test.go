package bootstrap

import (
	"context"
	"sync"
	"testing"

	agentbridge "github.com/Alice-space/agentbridge"
)

func TestInteractiveProviderBackendForwardsAssistantTextAndDropsToolUse(t *testing.T) {
	for _, provider := range []string{
		agentbridge.ProviderCodex,
		agentbridge.ProviderClaude,
		agentbridge.ProviderOpenCode,
	} {
		t.Run(provider, func(t *testing.T) {
			sessionKey := "session-" + provider
			driver := &interactiveBackendTestDriver{
				provider: provider,
				events:   make(chan agentbridge.TurnEvent, 8),
			}
			session := agentbridge.NewInteractiveSession(driver)
			defer session.Close()

			backend := &interactiveProviderBackend{
				provider: provider,
				sessions: map[string]*agentbridge.InteractiveSession{
					sessionKey: session,
				},
				runMu: map[string]*sync.Mutex{},
			}

			var progress []string
			result, err := backend.runInteractive(context.Background(), sessionKey, agentbridge.RunRequest{
				UserText: "hello",
				OnProgress: func(step string) {
					progress = append(progress, step)
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
		})
	}
}

type interactiveBackendTestDriver struct {
	provider string
	events   chan agentbridge.TurnEvent
}

func (d *interactiveBackendTestDriver) SteerMode() agentbridge.SteerMode {
	return agentbridge.SteerModeNative
}

func (d *interactiveBackendTestDriver) StartTurn(_ context.Context, req agentbridge.RunRequest) (agentbridge.TurnRef, error) {
	turn := agentbridge.TurnRef{ThreadID: "thread-1", TurnID: "turn-1"}
	go func() {
		d.events <- agentbridge.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     agentbridge.TurnEventUserText,
			Text:     "hello",
		}
		d.events <- agentbridge.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     agentbridge.TurnEventToolUse,
			Text:     "tool_use tool=`bash` command=`pwd`",
		}
		d.events <- agentbridge.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     agentbridge.TurnEventAssistantText,
			Text:     d.provider + " middle",
		}
		d.events <- agentbridge.TurnEvent{
			Provider: d.provider,
			ThreadID: turn.ThreadID,
			TurnID:   turn.TurnID,
			Kind:     agentbridge.TurnEventCompleted,
		}
	}()
	_ = req
	return turn, nil
}

func (d *interactiveBackendTestDriver) SteerTurn(context.Context, agentbridge.TurnRef, agentbridge.RunRequest) error {
	return nil
}

func (d *interactiveBackendTestDriver) InterruptTurn(context.Context, agentbridge.TurnRef) error {
	return nil
}

func (d *interactiveBackendTestDriver) Events() <-chan agentbridge.TurnEvent {
	return d.events
}

func (d *interactiveBackendTestDriver) Close() error {
	close(d.events)
	return nil
}
