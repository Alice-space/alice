package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInteractiveSession_NativeSteer(t *testing.T) {
	driver := newFakeInteractiveDriver(SteerModeNative)
	session := NewInteractiveSession(driver)
	defer session.Close()

	first, err := session.Submit(context.Background(), RunRequest{UserText: "first"})
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if first.Mode != SubmitStarted {
		t.Fatalf("first mode = %q, want %q", first.Mode, SubmitStarted)
	}

	second, err := session.Submit(context.Background(), RunRequest{UserText: "second"})
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if second.Mode != SubmitSteered {
		t.Fatalf("second mode = %q, want %q", second.Mode, SubmitSteered)
	}
	started, _, steered := driver.snapshot()
	if len(steered) != 1 || steered[0] != "second" {
		t.Fatalf("steered = %#v, want second", steered)
	}
	if len(started) != 1 {
		t.Fatalf("started = %#v, want only first start", started)
	}
}

func TestInteractiveSession_NativeEnqueue(t *testing.T) {
	driver := newFakeInteractiveDriver(SteerModeNativeEnqueue)
	session := NewInteractiveSession(driver)
	defer session.Close()

	first, err := session.Submit(context.Background(), RunRequest{UserText: "first"})
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if first.Mode != SubmitStarted {
		t.Fatalf("first mode = %q, want %q", first.Mode, SubmitStarted)
	}

	second, err := session.Submit(context.Background(), RunRequest{UserText: "second"})
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if second.Mode != SubmitSteered {
		t.Fatalf("second mode = %q, want %q", second.Mode, SubmitSteered)
	}
	started, _, steered := driver.snapshot()
	if len(steered) != 1 || steered[0] != "second" {
		t.Fatalf("steered = %#v, want second", steered)
	}
	if len(started) != 1 {
		t.Fatalf("started = %#v, want only first start", started)
	}
}

func TestInteractiveSession_QueueWhenBusy(t *testing.T) {
	driver := newFakeInteractiveDriver(SteerModeQueueWhenBusy)
	session := NewInteractiveSession(driver)
	defer session.Close()

	first, err := session.Submit(context.Background(), RunRequest{UserText: "first"})
	if err != nil {
		t.Fatalf("first submit failed: %v", err)
	}
	if first.Mode != SubmitStarted {
		t.Fatalf("first mode = %q, want %q", first.Mode, SubmitStarted)
	}

	second, err := session.Submit(context.Background(), RunRequest{UserText: "second"})
	if err != nil {
		t.Fatalf("second submit failed: %v", err)
	}
	if second.Mode != SubmitQueued || second.QueueDepth != 1 {
		t.Fatalf("second result = %#v, want queued depth 1", second)
	}
	_, _, steered := driver.snapshot()
	if len(steered) != 0 {
		t.Fatalf("steered = %#v, want none", steered)
	}

	driver.emit(TurnEvent{Kind: TurnEventCompleted, ThreadID: "thread_after_first", TurnID: first.TurnID})
	waitFor(t, time.Second, func() bool {
		started, _, _ := driver.snapshot()
		return len(started) == 2
	}, "queued turn should start")
	started, startedThreadIDs, _ := driver.snapshot()
	if started[1] != "second" {
		t.Fatalf("second started prompt = %q", started[1])
	}
	if startedThreadIDs[1] != "thread_after_first" {
		t.Fatalf("second started thread id = %q, want completed thread id", startedThreadIDs[1])
	}
}

func TestInteractiveSession_SteerWithoutActiveTurn(t *testing.T) {
	driver := newFakeInteractiveDriver(SteerModeNative)
	session := NewInteractiveSession(driver)
	defer session.Close()

	_, err := session.Steer(context.Background(), RunRequest{UserText: "second"})
	if !errors.Is(err, ErrNoActiveTurn) {
		t.Fatalf("Steer error = %v, want ErrNoActiveTurn", err)
	}
}

type fakeInteractiveDriver struct {
	mu               sync.Mutex
	mode             SteerMode
	events           chan TurnEvent
	nextID           int
	started          []string
	startedThreadIDs []string
	steered          []string
}

func newFakeInteractiveDriver(mode SteerMode) *fakeInteractiveDriver {
	return &fakeInteractiveDriver{mode: mode, events: make(chan TurnEvent, 16)}
}

func (d *fakeInteractiveDriver) SteerMode() SteerMode {
	return d.mode
}

func (d *fakeInteractiveDriver) StartTurn(_ context.Context, req RunRequest) (TurnRef, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextID++
	d.started = append(d.started, req.UserText)
	d.startedThreadIDs = append(d.startedThreadIDs, strings.TrimSpace(req.ThreadID))
	return TurnRef{ThreadID: "thread", TurnID: "turn-" + string(rune('0'+d.nextID))}, nil
}

func (d *fakeInteractiveDriver) SteerTurn(_ context.Context, _ TurnRef, req RunRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.steered = append(d.steered, req.UserText)
	return nil
}

func (d *fakeInteractiveDriver) InterruptTurn(context.Context, TurnRef) error {
	return nil
}

func (d *fakeInteractiveDriver) Events() <-chan TurnEvent {
	return d.events
}

func (d *fakeInteractiveDriver) Close() error {
	close(d.events)
	return nil
}

func (d *fakeInteractiveDriver) emit(event TurnEvent) {
	d.events <- event
}

func (d *fakeInteractiveDriver) snapshot() (started []string, startedThreadIDs []string, steered []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.started...), append([]string(nil), d.startedThreadIDs...), append([]string(nil), d.steered...)
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool, message string) {
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
