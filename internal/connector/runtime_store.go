package connector

import (
	"context"
	"sync"
)

type activeSessionRun struct {
	eventID string
	version uint64
	cancel  context.CancelCauseFunc
}

// runtimeStore groups mutable connector runtime state in one place so App can
// focus on orchestration instead of map-level bookkeeping.
type runtimeStore struct {
	mu         sync.Mutex
	latest     map[string]uint64
	pending    map[string]Job
	sessionMu  map[string]*sync.Mutex
	active     map[string]activeSessionRun
	superseded map[string]uint64

	runtimeStatePath           string
	runtimeStateVersion        uint64
	runtimeStateFlushedVersion uint64
}

func newRuntimeStore() *runtimeStore {
	return &runtimeStore{
		latest:     make(map[string]uint64),
		pending:    make(map[string]Job),
		sessionMu:  make(map[string]*sync.Mutex),
		active:     make(map[string]activeSessionRun),
		superseded: make(map[string]uint64),
	}
}
