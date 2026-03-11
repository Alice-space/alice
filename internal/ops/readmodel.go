package ops

import (
	"context"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

// readModel is an in-memory projection of domain events for read operations.
type readModel struct {
	VisibleHLC         string
	Events             []EventView
	eventsByID         map[string]EventView
	eventsByExternalID map[string]EventView
	requests           map[string]*RequestView
	tasks              map[string]*TaskView
	schedules          map[string]*ScheduleView
	approvals          map[string]*ApprovalView
	humanWaits         map[string]*HumanWaitView
	deadletters        map[string]*DeadletterView
}

// newReadModel creates a new empty read model.
func newReadModel() *readModel {
	return &readModel{
		eventsByID:         map[string]EventView{},
		eventsByExternalID: map[string]EventView{},
		requests:           map[string]*RequestView{},
		tasks:              map[string]*TaskView{},
		schedules:          map[string]*ScheduleView{},
		approvals:          map[string]*ApprovalView{},
		humanWaits:         map[string]*HumanWaitView{},
		deadletters:        map[string]*DeadletterView{},
	}
}

// buildReadModel builds a read model by replaying events from the store.
func buildReadModel(ctx context.Context, st *store.Store) (*readModel, error) {
	model := newReadModel()
	if st == nil {
		return model, nil
	}
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		model.consume(evt)
		return nil
	}); err != nil {
		return nil, err
	}
	// Load outbox for each task
	for _, task := range model.tasks {
		if st.Indexes == nil {
			continue
		}
		outbox, err := st.Indexes.ListPendingOutbox(ctx, "", time.Now().UTC(), 100)
		if err != nil {
			continue
		}
		for _, item := range outbox {
			if item.TaskID == task.TaskID {
				task.Outbox = append(task.Outbox, item)
			}
		}
	}
	return model, nil
}

// ensureRequest returns or creates a request view.
func (m *readModel) ensureRequest(id string) *RequestView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &RequestView{}
	}
	v, ok := m.requests[id]
	if ok {
		return v
	}
	v = &RequestView{RequestID: id}
	m.requests[id] = v
	return v
}

// ensureTask returns or creates a task view.
func (m *readModel) ensureTask(id string) *TaskView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &TaskView{}
	}
	v, ok := m.tasks[id]
	if ok {
		return v
	}
	v = &TaskView{TaskID: id}
	m.tasks[id] = v
	return v
}

// ensureSchedule returns or creates a schedule view.
func (m *readModel) ensureSchedule(id string) *ScheduleView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &ScheduleView{}
	}
	v, ok := m.schedules[id]
	if ok {
		return v
	}
	v = &ScheduleView{ScheduledTaskID: id}
	m.schedules[id] = v
	return v
}

// ensureApproval returns or creates an approval view.
func (m *readModel) ensureApproval(id string) *ApprovalView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &ApprovalView{}
	}
	v, ok := m.approvals[id]
	if ok {
		return v
	}
	v = &ApprovalView{ApprovalRequestID: id}
	m.approvals[id] = v
	return v
}

// ensureHumanWait returns or creates a human wait view.
func (m *readModel) ensureHumanWait(id string) *HumanWaitView {
	id = strings.TrimSpace(id)
	if id == "" {
		return &HumanWaitView{}
	}
	v, ok := m.humanWaits[id]
	if ok {
		return v
	}
	v = &HumanWaitView{HumanWaitID: id}
	m.humanWaits[id] = v
	return v
}
