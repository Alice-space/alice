package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// SteerMode describes how a provider handles input submitted while a turn is
// already running.
type SteerMode string

const (
	// SteerModeNative means the provider can inject input into the active turn.
	SteerModeNative SteerMode = "native"
	// SteerModeNativeEnqueue means the provider can append input to the active
	// session without agentbridge waiting for the current turn to finish. The
	// provider may consume it at the next model/tool boundary.
	SteerModeNativeEnqueue SteerMode = "native_enqueue"
	// SteerModeQueueWhenBusy means agentbridge queues input until the turn is idle.
	SteerModeQueueWhenBusy SteerMode = "queue_when_busy"
	// SteerModeInterruptOnly means the provider has no safe enqueue semantics.
	SteerModeInterruptOnly SteerMode = "interrupt_only"
)

// TurnEventKind is a normalized event emitted by interactive backends.
type TurnEventKind string

const (
	TurnEventStarted TurnEventKind = "turn_started"
	// TurnEventAssistantText contains a complete assistant message. Providers
	// that stream chunks coalesce them before emitting this normalized event.
	TurnEventAssistantText TurnEventKind = "assistant_text"
	// TurnEventUserText contains user-authored text observed on a provider
	// event stream.
	TurnEventUserText      TurnEventKind = "user_text"
	TurnEventReasoning     TurnEventKind = "reasoning"
	TurnEventToolUse       TurnEventKind = "tool_use"
	TurnEventFileChange    TurnEventKind = "file_change"
	TurnEventSteerConsumed TurnEventKind = "steer_consumed"
	TurnEventCompleted     TurnEventKind = "turn_completed"
	TurnEventInterrupted   TurnEventKind = "turn_interrupted"
	TurnEventError         TurnEventKind = "error"
)

// TurnEvent is the provider-neutral event stream used by interactive sessions.
type TurnEvent struct {
	Provider string
	ThreadID string
	TurnID   string
	Kind     TurnEventKind
	Text     string
	Usage    Usage
	Err      error
	Raw      string
}

// TurnRef identifies an in-flight provider turn.
type TurnRef struct {
	ThreadID string
	TurnID   string
}

// SubmitMode describes how a submitted user message was accepted.
type SubmitMode string

const (
	SubmitStarted SubmitMode = "started"
	SubmitSteered SubmitMode = "steered"
	SubmitQueued  SubmitMode = "queued"
)

// SubmitResult is returned immediately after input is accepted.
type SubmitResult struct {
	ThreadID   string
	TurnID     string
	Mode       SubmitMode
	QueueDepth int
}

// InteractiveDriver is implemented by provider-specific long-lived transports.
type InteractiveDriver interface {
	SteerMode() SteerMode
	StartTurn(ctx context.Context, req RunRequest) (TurnRef, error)
	SteerTurn(ctx context.Context, turn TurnRef, req RunRequest) error
	InterruptTurn(ctx context.Context, turn TurnRef) error
	Events() <-chan TurnEvent
	Close() error
}

var ErrInteractiveClosed = errors.New("interactive session is closed")
var ErrNoActiveTurn = errors.New("no active turn to steer")

// InteractiveSession serializes user input for one logical conversation.
//
// If the driver supports native steer/enqueue, Submit injects or appends new
// input into the active provider session. Otherwise Submit queues input and
// starts the next turn after completion.
type InteractiveSession struct {
	driver InteractiveDriver
	events chan TurnEvent
	done   chan struct{}

	mu     sync.Mutex
	closed bool
	active *TurnRef
	queue  []RunRequest
}

// NewInteractiveSession wraps a provider driver with common steer/queue state.
func NewInteractiveSession(driver InteractiveDriver) *InteractiveSession {
	s := &InteractiveSession{
		driver: driver,
		events: make(chan TurnEvent, 128),
		done:   make(chan struct{}),
	}
	go s.forwardEvents()
	return s
}

// Events returns the normalized event stream for this conversation.
func (s *InteractiveSession) Events() <-chan TurnEvent {
	if s == nil {
		ch := make(chan TurnEvent)
		close(ch)
		return ch
	}
	return s.events
}

// SteerMode returns the active provider's busy-turn behavior.
func (s *InteractiveSession) SteerMode() SteerMode {
	if s == nil || s.driver == nil {
		return SteerModeInterruptOnly
	}
	return s.driver.SteerMode()
}

// Submit starts a turn, steers the active turn, or queues the input depending
// on the provider capability and current state.
func (s *InteractiveSession) Submit(ctx context.Context, req RunRequest) (SubmitResult, error) {
	if s == nil || s.driver == nil {
		return SubmitResult{}, ErrInteractiveClosed
	}
	req.UserText = strings.TrimSpace(req.UserText)
	if req.UserText == "" {
		return SubmitResult{}, errors.New("empty prompt")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SubmitResult{}, ErrInteractiveClosed
	}

	if s.active == nil {
		turn, err := s.driver.StartTurn(ctx, req)
		if err != nil {
			return SubmitResult{}, err
		}
		s.active = &turn
		return SubmitResult{ThreadID: turn.ThreadID, TurnID: turn.TurnID, Mode: SubmitStarted}, nil
	}

	if isProviderNativeSteerMode(s.driver.SteerMode()) {
		turn := *s.active
		if err := s.driver.SteerTurn(ctx, turn, req); err != nil {
			return SubmitResult{}, err
		}
		return SubmitResult{ThreadID: turn.ThreadID, TurnID: turn.TurnID, Mode: SubmitSteered}, nil
	}

	s.queue = append(s.queue, req)
	return SubmitResult{
		ThreadID:   s.active.ThreadID,
		TurnID:     s.active.TurnID,
		Mode:       SubmitQueued,
		QueueDepth: len(s.queue),
	}, nil
}

// Steer injects/appends input into the active turn and fails if there is no
// active provider-native turn. It never starts a new turn.
func (s *InteractiveSession) Steer(ctx context.Context, req RunRequest) (SubmitResult, error) {
	if s == nil || s.driver == nil {
		return SubmitResult{}, ErrInteractiveClosed
	}
	req.UserText = strings.TrimSpace(req.UserText)
	if req.UserText == "" {
		return SubmitResult{}, errors.New("empty prompt")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return SubmitResult{}, ErrInteractiveClosed
	}
	if s.active == nil {
		return SubmitResult{}, ErrNoActiveTurn
	}
	if !isProviderNativeSteerMode(s.driver.SteerMode()) {
		return SubmitResult{}, ErrSteerUnsupported
	}
	turn := *s.active
	if err := s.driver.SteerTurn(ctx, turn, req); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{ThreadID: turn.ThreadID, TurnID: turn.TurnID, Mode: SubmitSteered}, nil
}

func isProviderNativeSteerMode(mode SteerMode) bool {
	return mode == SteerModeNative || mode == SteerModeNativeEnqueue
}

// Interrupt requests cancellation of the active turn.
func (s *InteractiveSession) Interrupt(ctx context.Context) error {
	if s == nil || s.driver == nil {
		return ErrInteractiveClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrInteractiveClosed
	}
	if s.active == nil {
		return nil
	}
	return s.driver.InterruptTurn(ctx, *s.active)
}

// Close stops the underlying provider process.
func (s *InteractiveSession) Close() error {
	if s == nil || s.driver == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	return s.driver.Close()
}

func (s *InteractiveSession) forwardEvents() {
	defer close(s.events)
	for {
		select {
		case <-s.done:
			return
		case event, ok := <-s.driver.Events():
			if !ok {
				s.mu.Lock()
				s.closed = true
				s.active = nil
				s.queue = nil
				s.mu.Unlock()
				return
			}
			s.handleEvent(event)
			select {
			case s.events <- event:
			case <-s.done:
				return
			}
		}
	}
}

func (s *InteractiveSession) handleEvent(event TurnEvent) {
	switch event.Kind {
	case TurnEventCompleted, TurnEventInterrupted, TurnEventError:
	default:
		return
	}

	var next *RunRequest
	s.mu.Lock()
	if s.active != nil && (event.TurnID == "" || event.TurnID == s.active.TurnID) {
		s.active = nil
	}
	if !s.closed && s.active == nil && len(s.queue) > 0 {
		req := s.queue[0]
		if strings.TrimSpace(req.ThreadID) == "" && strings.TrimSpace(event.ThreadID) != "" {
			req.ThreadID = strings.TrimSpace(event.ThreadID)
		}
		copy(s.queue, s.queue[1:])
		s.queue = s.queue[:len(s.queue)-1]
		next = &req
	}
	s.mu.Unlock()

	if next != nil {
		go s.startQueued(*next)
	}
}

func (s *InteractiveSession) startQueued(req RunRequest) {
	turn, err := s.driver.StartTurn(context.TODO(), req)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if err == nil {
		s.active = &turn
	}
	s.mu.Unlock()

	if err != nil {
		select {
		case s.events <- TurnEvent{Kind: TurnEventError, Err: err}:
		case <-s.done:
		}
	}
}
