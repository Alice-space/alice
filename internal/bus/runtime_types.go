package bus

import (
	"context"
	"errors"

	"alice/internal/agent"
	"alice/internal/domain"
	"alice/internal/platform"
)

// Clock is an alias to platform.Clock for backward compatibility.
// Deprecated: Use platform.Clock directly.
type Clock = platform.Clock

// Reception assesses incoming events for promotion.
type Reception interface {
	Assess(ctx context.Context, in domain.ReceptionInput) (*domain.PromotionDecision, error)
}

// Config for Runtime.
type Config struct {
	ShardCount int
}

// ProcessResult contains the outcome of processing an event.
type ProcessResult struct {
	RequestID       string `json:"request_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RouteMatched    string `json:"route_matched,omitempty"`
	RouteTargetKind string `json:"route_target_kind,omitempty"`
	RouteTargetID   string `json:"route_target_id,omitempty"`
	EventID         string `json:"event_id,omitempty"`
	CommitHLC       string `json:"commit_hlc,omitempty"`
	Promoted        bool   `json:"promoted,omitempty"`
}

// Logger is an alias to platform.Logger.
type Logger = platform.Logger

// Errors
var (
	ErrScheduleSourceNotFound = errors.New("schedule source not found")
	ErrScheduleSourceDisabled = errors.New("schedule source disabled")
)

// Setters for Runtime configuration.

func (r *Runtime) SetClock(clock platform.Clock) {
	if clock != nil {
		r.clock = clock
	}
}

func (r *Runtime) SetCriticalFailureHandler(fn func(error)) {
	r.onCritical = fn
}

func (r *Runtime) SetDirectAnswerExecutor(exec *agent.DirectAnswerExecutor) {
	r.directAgent = exec
}
