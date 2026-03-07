package governance

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/domain"
	"github.com/Alice-space/alice/internal/state"
	"github.com/Alice-space/alice/internal/util"
)

type LockRequest struct {
	Type domain.LockType
	Key  string
}

type Lease struct {
	LockID     string
	LockType   domain.LockType
	LockKey    string
	OwnerRunID string
	LeaseToken int64
	ExpiresAt  time.Time
}

type LockManager struct {
	store *state.Store
	clock util.Clock

	mu         sync.Mutex
	nextTokens map[string]int64
	owned      map[string][]Lease
}

func NewLockManager(store *state.Store, clock util.Clock) *LockManager {
	return &LockManager{
		store:      store,
		clock:      clock,
		nextTokens: map[string]int64{},
		owned:      map[string][]Lease{},
	}
}

func (m *LockManager) Acquire(runID string, ttl time.Duration, locks ...LockRequest) ([]Lease, error) {
	if len(locks) == 0 {
		return nil, nil
	}
	if err := validateOrder(locks); err != nil {
		return nil, err
	}
	acquired := make([]Lease, 0, len(locks))
	for _, req := range locks {
		lease, err := m.acquireOne(runID, ttl, req)
		if err != nil {
			_ = m.Release(runID, acquired...)
			return nil, err
		}
		acquired = append(acquired, lease)
	}
	m.mu.Lock()
	m.owned[runID] = append(m.owned[runID], acquired...)
	m.mu.Unlock()
	return acquired, nil
}

func (m *LockManager) acquireOne(runID string, ttl time.Duration, req LockRequest) (Lease, error) {
	if req.Key == "" {
		return Lease{}, fmt.Errorf("empty lock key for %s", req.Type)
	}
	token := m.nextLeaseToken(req.Type, req.Key)
	now := m.clock.Now()
	rec := domain.LockRecord{
		LockID:     util.NewID("lock"),
		LockType:   req.Type,
		LockKey:    req.Key,
		OwnerRunID: runID,
		LeaseToken: token,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		Status:     domain.LockStatusHeld,
	}
	if err := m.store.AcquireLock(rec); err != nil {
		return Lease{}, err
	}
	return Lease{
		LockID:     rec.LockID,
		LockType:   rec.LockType,
		LockKey:    rec.LockKey,
		OwnerRunID: rec.OwnerRunID,
		LeaseToken: rec.LeaseToken,
		ExpiresAt:  rec.ExpiresAt,
	}, nil
}

func (m *LockManager) Renew(runID string, ttl time.Duration, leases ...Lease) ([]Lease, error) {
	if len(leases) == 0 {
		return nil, nil
	}
	updated := make([]Lease, 0, len(leases))
	for _, lease := range leases {
		rec, err := m.store.RenewLock(lease.LockType, lease.LockKey, runID, lease.LeaseToken, ttl)
		if err != nil {
			return nil, err
		}
		updated = append(updated, Lease{
			LockID:     rec.LockID,
			LockType:   rec.LockType,
			LockKey:    rec.LockKey,
			OwnerRunID: rec.OwnerRunID,
			LeaseToken: rec.LeaseToken,
			ExpiresAt:  rec.ExpiresAt,
		})
	}
	m.mu.Lock()
	m.owned[runID] = append([]Lease(nil), updated...)
	m.mu.Unlock()
	return updated, nil
}

func (m *LockManager) Release(runID string, leases ...Lease) error {
	for _, lease := range leases {
		if err := m.store.ReleaseLock(lease.LockType, lease.LockKey, runID, lease.LeaseToken); err != nil {
			return err
		}
	}
	m.mu.Lock()
	delete(m.owned, runID)
	m.mu.Unlock()
	return nil
}

func (m *LockManager) ReleaseAll(runID string) error {
	m.mu.Lock()
	owned := append([]Lease(nil), m.owned[runID]...)
	m.mu.Unlock()
	return m.Release(runID, owned...)
}

func (m *LockManager) ReconcileExpired() error {
	now := m.clock.Now()
	locks := m.store.ListActiveLocks()
	for _, l := range locks {
		if l.ExpiresAt.After(now) {
			continue
		}
		_ = m.store.ReleaseLock(l.LockType, l.LockKey, l.OwnerRunID, l.LeaseToken)
	}
	return nil
}

func (m *LockManager) nextLeaseToken(lockType domain.LockType, key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := string(lockType) + ":" + key
	m.nextTokens[k]++
	return m.nextTokens[k]
}

func lockOrder(lockType domain.LockType) int {
	switch lockType {
	case domain.LockTypeProject:
		return 1
	case domain.LockTypeRepoBranchWrite:
		return 2
	case domain.LockTypeWorkspace:
		return 3
	case domain.LockTypeExecutorSlot:
		return 4
	case domain.LockTypeMemoryPromotion:
		return 5
	default:
		return 100
	}
}

func validateOrder(reqs []LockRequest) error {
	if len(reqs) <= 1 {
		return nil
	}
	copyReqs := append([]LockRequest(nil), reqs...)
	sort.Slice(copyReqs, func(i, j int) bool {
		return lockOrder(copyReqs[i].Type) < lockOrder(copyReqs[j].Type)
	})
	for i := range reqs {
		if reqs[i].Type != copyReqs[i].Type {
			return fmt.Errorf("lock acquire order violated, expected %v before %v", copyReqs[i].Type, reqs[i].Type)
		}
	}
	return nil
}
