package workflow

import (
	"fmt"
	"time"

	"github.com/Alice-space/alice/internal/domain"
	"github.com/Alice-space/alice/internal/state"
	"github.com/Alice-space/alice/internal/util"
)

type Engine struct {
	store *state.Store
	clock util.Clock
}

func NewEngine(store *state.Store, clock util.Clock) *Engine {
	return &Engine{store: store, clock: clock}
}

func (e *Engine) StartRun(runID string) (domain.RunRecord, error) {
	run, ok := e.store.GetRun(runID)
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if err := domain.ValidateRunStatusTransition(run.RunStatus, domain.RunStatusRunning); err != nil {
		return domain.RunRecord{}, err
	}
	if _, err := e.writeEvent(domain.Event{
		EventID:        util.NewID("evt"),
		EventType:      domain.EventRunStatusChanged,
		Source:         "workflow",
		Target:         runID,
		ProjectID:      "",
		TaskID:         run.TaskID,
		RunID:          runID,
		Severity:       domain.SeverityInfo,
		Payload:        map[string]any{"from": run.RunStatus, "to": domain.RunStatusRunning},
		Attempt:        1,
		CreatedAt:      e.clock.Now(),
		IdempotencyKey: util.NewID("idempo"),
	}); err != nil {
		return domain.RunRecord{}, err
	}
	updated, err := e.store.CASRun(runID, run.StateVersion, func(r *domain.RunRecord) error {
		r.RunStatus = domain.RunStatusRunning
		r.Timeline.StartedAt = e.clock.Now()
		return nil
	})
	if err != nil {
		return domain.RunRecord{}, err
	}
	return updated, nil
}

func (e *Engine) ChangeRunStatus(runID string, to domain.RunStatus, reason string, idempotencyKey string) (domain.RunRecord, error) {
	run, ok := e.store.GetRun(runID)
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if err := domain.ValidateRunStatusTransition(run.RunStatus, to); err != nil {
		return domain.RunRecord{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = util.NewID("idempo")
	}
	if _, err := e.writeEvent(domain.Event{
		EventID:        util.NewID("evt"),
		EventType:      domain.EventRunStatusChanged,
		Source:         "workflow",
		Target:         runID,
		ProjectID:      "",
		TaskID:         run.TaskID,
		RunID:          runID,
		Severity:       domain.SeverityInfo,
		Payload:        map[string]any{"from": run.RunStatus, "to": to, "reason": reason},
		Attempt:        1,
		CreatedAt:      e.clock.Now(),
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return domain.RunRecord{}, err
	}
	updated, err := e.store.CASRun(runID, run.StateVersion, func(r *domain.RunRecord) error {
		r.RunStatus = to
		if to.IsTerminal() {
			r.Timeline.CompletedAt = e.clock.Now()
		}
		if to == domain.RunStatusWaitingHuman {
			r.WorkflowPhase = domain.WorkflowPhaseWaitingHuman
		}
		if to == domain.RunStatusBlocked {
			r.WorkflowPhase = domain.WorkflowPhaseBlocked
		}
		if to == domain.RunStatusSucceeded {
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhaseDone
			}
		}
		if to == domain.RunStatusAborted {
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhaseAborted
			}
		}
		return nil
	})
	if err != nil {
		return domain.RunRecord{}, err
	}
	return updated, nil
}

func (e *Engine) ChangeWorkflowPhase(runID string, to domain.WorkflowPhase, reason string, idempotencyKey string) (domain.RunRecord, error) {
	run, ok := e.store.GetRun(runID)
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if run.WorkflowPhase == "" {
		return domain.RunRecord{}, fmt.Errorf("run %s has no workflow phase", runID)
	}
	if err := domain.ValidateWorkflowPhaseTransition(run.WorkflowPhase, to); err != nil {
		return domain.RunRecord{}, err
	}
	if idempotencyKey == "" {
		idempotencyKey = util.NewID("idempo")
	}
	if _, err := e.writeEvent(domain.Event{
		EventID:        util.NewID("evt"),
		EventType:      domain.EventWorkflowPhaseChanged,
		Source:         "workflow",
		Target:         runID,
		ProjectID:      "",
		TaskID:         run.TaskID,
		RunID:          runID,
		Severity:       domain.SeverityInfo,
		Payload:        map[string]any{"from": run.WorkflowPhase, "to": to, "reason": reason},
		Attempt:        1,
		CreatedAt:      e.clock.Now(),
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return domain.RunRecord{}, err
	}
	updated, err := e.store.CASRun(runID, run.StateVersion, func(r *domain.RunRecord) error {
		r.WorkflowPhase = to
		switch to {
		case domain.WorkflowPhaseWaitingHuman:
			r.RunStatus = domain.RunStatusWaitingHuman
		case domain.WorkflowPhaseBlocked:
			r.RunStatus = domain.RunStatusBlocked
		case domain.WorkflowPhaseDone:
			r.RunStatus = domain.RunStatusSucceeded
			r.Timeline.CompletedAt = e.clock.Now()
		case domain.WorkflowPhaseAborted:
			r.RunStatus = domain.RunStatusAborted
			r.Timeline.CompletedAt = e.clock.Now()
		default:
			if r.RunStatus == domain.RunStatusQueued {
				r.RunStatus = domain.RunStatusRunning
			}
		}
		return nil
	})
	if err != nil {
		return domain.RunRecord{}, err
	}
	return updated, nil
}

func (e *Engine) MarkSuperseded(runID string, supersededBy string, reason string) (domain.RunRecord, error) {
	run, ok := e.store.GetRun(runID)
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if run.RunStatus.IsTerminal() {
		return run, nil
	}
	if _, err := e.writeEvent(domain.Event{
		EventID:        util.NewID("evt"),
		EventType:      domain.EventRunSuperseded,
		Source:         "workflow",
		Target:         runID,
		ProjectID:      "",
		TaskID:         run.TaskID,
		RunID:          runID,
		Severity:       domain.SeverityInfo,
		Payload:        map[string]any{"superseded_by": supersededBy, "reason": reason},
		Attempt:        1,
		CreatedAt:      e.clock.Now(),
		IdempotencyKey: util.NewID("idempo"),
	}); err != nil {
		return domain.RunRecord{}, err
	}
	updated, err := e.store.CASRun(runID, run.StateVersion, func(r *domain.RunRecord) error {
		r.RunStatus = domain.RunStatusSuperseded
		r.SupersededBy = supersededBy
		r.Timeline.CompletedAt = e.clock.Now()
		return nil
	})
	if err != nil {
		return domain.RunRecord{}, err
	}
	return updated, nil
}

func (e *Engine) ApplyHumanSignal(runID string, actorID string, signal domain.ManualSignal) (domain.RunRecord, error) {
	run, ok := e.store.GetRun(runID)
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if run.RunStatus.IsTerminal() {
		_, _ = e.writeEvent(domain.Event{
			EventID:        util.NewID("evt"),
			EventType:      domain.EventHumanSignalReceived,
			Source:         "workflow",
			Target:         runID,
			TaskID:         run.TaskID,
			RunID:          runID,
			Severity:       domain.SeverityWarn,
			Payload:        map[string]any{"ignored": true, "reason": "terminal run", "signal": signal.SignalType},
			Attempt:        1,
			CreatedAt:      e.clock.Now(),
			IdempotencyKey: util.NewID("idempo"),
		})
		return run, nil
	}
	if _, err := e.writeEvent(domain.Event{
		EventID:        util.NewID("evt"),
		EventType:      domain.EventHumanSignalReceived,
		Source:         "workflow",
		Target:         runID,
		TaskID:         run.TaskID,
		RunID:          runID,
		Severity:       domain.SeverityInfo,
		Payload:        map[string]any{"signal": signal.SignalType, "payload": signal.Payload, "actor_id": actorID},
		Attempt:        1,
		CreatedAt:      e.clock.Now(),
		IdempotencyKey: util.NewID("idempo"),
	}); err != nil {
		return domain.RunRecord{}, err
	}

	updated, err := e.store.CASRun(runID, run.StateVersion, func(r *domain.RunRecord) error {
		r.InterventionHistory = append(r.InterventionHistory, domain.InterventionRecord{
			ActorID:    actorID,
			SignalType: signal.SignalType,
			Comment:    fmt.Sprintf("payload=%v", signal.Payload),
			At:         e.clock.Now(),
		})
		switch signal.SignalType {
		case "abort_run":
			r.RunStatus = domain.RunStatusAborted
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhaseAborted
			}
			r.Timeline.CompletedAt = e.clock.Now()
		case "force_report":
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhaseReporting
			}
			if r.RunStatus == domain.RunStatusWaitingHuman || r.RunStatus == domain.RunStatusBlocked {
				r.RunStatus = domain.RunStatusRunning
			}
		case "change_direction":
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhasePlanning
			}
			if r.RunStatus == domain.RunStatusWaitingHuman || r.RunStatus == domain.RunStatusBlocked {
				r.RunStatus = domain.RunStatusRunning
			}
		case "pause_project":
			r.RunStatus = domain.RunStatusWaitingHuman
			if r.WorkflowPhase != "" {
				r.WorkflowPhase = domain.WorkflowPhaseWaitingHuman
			}
		case "resume_project":
			if r.RunStatus == domain.RunStatusWaitingHuman || r.RunStatus == domain.RunStatusBlocked {
				r.RunStatus = domain.RunStatusRunning
				if r.WorkflowPhase == domain.WorkflowPhaseWaitingHuman || r.WorkflowPhase == domain.WorkflowPhaseBlocked {
					r.WorkflowPhase = domain.WorkflowPhasePlanning
				}
			}
		}
		return nil
	})
	if err != nil {
		return domain.RunRecord{}, err
	}
	return updated, nil
}

func (e *Engine) ReconcileNonTerminalRun(run domain.RunRecord) (domain.RunRecord, error) {
	if run.RunStatus.IsTerminal() {
		return run, nil
	}
	staleFor := e.clock.Now().Sub(run.Timeline.UpdatedAt)
	if staleFor < 2*time.Minute {
		return run, nil
	}
	return e.ChangeRunStatus(run.RunID, domain.RunStatusBlocked, "stale run requires reconciliation", util.NewID("reconcile"))
}

func (e *Engine) writeEvent(evt domain.Event) (bool, error) {
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = e.clock.Now()
	}
	if evt.IdempotencyKey == "" {
		evt.IdempotencyKey = util.NewID("idempo")
	}
	return e.store.AppendEvent(evt)
}
