package ops

import (
	"sort"
	"strings"
)

// Lookup methods for retrieving views by ID.

func (m *readModel) requestByID(id string) (RequestView, bool) {
	v, ok := m.requests[strings.TrimSpace(id)]
	if !ok || v == nil {
		return RequestView{}, false
	}
	return *v, true
}

func (m *readModel) taskByID(id string) (TaskView, bool) {
	v, ok := m.tasks[strings.TrimSpace(id)]
	if !ok || v == nil {
		return TaskView{}, false
	}
	return *v, true
}

func (m *readModel) scheduleByID(id string) (ScheduleView, bool) {
	v, ok := m.schedules[strings.TrimSpace(id)]
	if !ok || v == nil {
		return ScheduleView{}, false
	}
	return *v, true
}

func (m *readModel) approvalByID(id string) (ApprovalView, bool) {
	v, ok := m.approvals[strings.TrimSpace(id)]
	if !ok || v == nil {
		return ApprovalView{}, false
	}
	return *v, true
}

func (m *readModel) humanWaitByID(id string) (HumanWaitView, bool) {
	v, ok := m.humanWaits[strings.TrimSpace(id)]
	if !ok || v == nil {
		return HumanWaitView{}, false
	}
	return *v, true
}

func (m *readModel) deadletterByID(id string) (DeadletterView, bool) {
	v, ok := m.deadletters[strings.TrimSpace(id)]
	if !ok || v == nil {
		return DeadletterView{}, false
	}
	return *v, true
}

func (m *readModel) eventByID(id string) (EventView, bool) {
	key := strings.TrimSpace(id)
	if v, ok := m.eventsByID[key]; ok {
		return v, true
	}
	v, ok := m.eventsByExternalID[key]
	return v, ok
}

// List methods for retrieving all views of a type.

func (m *readModel) requestList() []RequestView {
	out := make([]RequestView, 0, len(m.requests))
	for _, v := range m.requests {
		if v != nil && v.RequestID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) taskList() []TaskView {
	out := make([]TaskView, 0, len(m.tasks))
	for _, v := range m.tasks {
		if v != nil && v.TaskID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) scheduleList() []ScheduleView {
	out := make([]ScheduleView, 0, len(m.schedules))
	for _, v := range m.schedules {
		if v != nil && v.ScheduledTaskID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) approvalList() []ApprovalView {
	out := make([]ApprovalView, 0, len(m.approvals))
	for _, v := range m.approvals {
		if v != nil && v.ApprovalRequestID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) humanWaitList() []HumanWaitView {
	out := make([]HumanWaitView, 0, len(m.humanWaits))
	for _, v := range m.humanWaits {
		if v != nil && v.HumanWaitID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) deadletterList() []DeadletterView {
	out := make([]DeadletterView, 0, len(m.deadletters))
	for _, v := range m.deadletters {
		if v != nil && v.DeadletterID != "" {
			out = append(out, *v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].UpdatedHLC, out[j].UpdatedHLC) > 0
	})
	return out
}

func (m *readModel) eventList() []EventView {
	out := append([]EventView(nil), m.Events...)
	sort.Slice(out, func(i, j int) bool {
		return compareHLC(out[i].GlobalHLC, out[j].GlobalHLC) > 0
	})
	return out
}
