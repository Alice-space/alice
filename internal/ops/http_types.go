package ops

import (
	"net/http"

	"alice/internal/bus"
	"alice/internal/store"
)

// AdminHooks provides hooks for admin operations.
type AdminHooks struct {
	ReconcileOutbox    func(r *http.Request) error
	ReconcileSchedules func(r *http.Request) error
	RebuildIndexes     func(r *http.Request) error
	ReplayFromHLC      func(r *http.Request, hlc string) error
	RedriveDeadletter  func(r *http.Request, deadletterID string) error
	CancelTask         func(r *http.Request, taskID string) error
}

// SurfaceConfig holds surface configuration.
type SurfaceConfig struct {
	AdminEventInjectionEnabled     bool
	AdminScheduleFireReplayEnabled bool
}

// HTTPManager handles HTTP operations.
type HTTPManager struct {
	store     *store.Store
	indexes   *store.BoltIndexStore
	runtime   *bus.Runtime
	reception bus.Reception
	hooks     AdminHooks
	config    SurfaceConfig
}

type adminActionIDKey struct{}

type humanActionQueueEntry struct {
	EntryID       string `json:"entry_id"`
	EntryKind     string `json:"entry_kind"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	UpdatedHLC    string `json:"updated_hlc,omitempty"`
	ApprovalID    string `json:"approval_request_id,omitempty"`
	HumanWaitID   string `json:"human_wait_id,omitempty"`
	WaitingReason string `json:"waiting_reason,omitempty"`
}

// NewHTTPManager creates a new HTTPManager instance.
func NewHTTPManager(st *store.Store, runtime *bus.Runtime, reception bus.Reception, hooks AdminHooks, cfg SurfaceConfig) *HTTPManager {
	var indexes *store.BoltIndexStore
	if st != nil {
		indexes = st.Indexes
	}
	return &HTTPManager{
		store:     st,
		indexes:   indexes,
		runtime:   runtime,
		reception: reception,
		hooks:     hooks,
		config:    cfg,
	}
}
