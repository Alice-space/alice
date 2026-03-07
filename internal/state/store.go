package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/domain"
	"github.com/Alice-space/alice/internal/util"
)

type Snapshot struct {
	Projects        map[string]domain.ProjectSpec          `json:"projects"`
	TaskTemplates   map[string]domain.TaskTemplate         `json:"task_templates"`
	Tasks           map[string]domain.TaskSpec             `json:"tasks"`
	Runs            map[string]domain.RunRecord            `json:"runs"`
	Events          []domain.Event                         `json:"events"`
	Schedules       map[string]domain.ScheduleEntry        `json:"schedules"`
	Settings        map[string]domain.RuntimeSetting       `json:"settings"`
	Proposals       map[string]domain.ConfigChangeProposal `json:"proposals"`
	Approvals       map[string]domain.ApprovalTicket       `json:"approvals"`
	Memories        map[string]domain.MemoryRecord         `json:"memories"`
	Baselines       map[string]domain.BaselineSnapshot     `json:"baselines"`
	EvalReports     map[string]domain.EvalReport           `json:"eval_reports"`
	Locks           map[string]domain.LockRecord           `json:"locks"`
	Actions         map[string]domain.ActionIntent         `json:"actions"`
	IdempotencyKeys map[string]string                      `json:"idempotency_keys"`
}

type Store struct {
	mu       sync.RWMutex
	path     string
	snapshot Snapshot
}

func New(path string) (*Store, error) {
	s := &Store{path: path, snapshot: emptySnapshot()}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func emptySnapshot() Snapshot {
	return Snapshot{
		Projects:        map[string]domain.ProjectSpec{},
		TaskTemplates:   map[string]domain.TaskTemplate{},
		Tasks:           map[string]domain.TaskSpec{},
		Runs:            map[string]domain.RunRecord{},
		Events:          []domain.Event{},
		Schedules:       map[string]domain.ScheduleEntry{},
		Settings:        map[string]domain.RuntimeSetting{},
		Proposals:       map[string]domain.ConfigChangeProposal{},
		Approvals:       map[string]domain.ApprovalTicket{},
		Memories:        map[string]domain.MemoryRecord{},
		Baselines:       map[string]domain.BaselineSnapshot{},
		EvalReports:     map[string]domain.EvalReport{},
		Locks:           map[string]domain.LockRecord{},
		Actions:         map[string]domain.ActionIntent{},
		IdempotencyKeys: map[string]string{},
	}
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	s.snapshot = normalizeSnapshot(snap)
	return nil
}

func normalizeSnapshot(in Snapshot) Snapshot {
	out := in
	if out.Projects == nil {
		out.Projects = map[string]domain.ProjectSpec{}
	}
	if out.TaskTemplates == nil {
		out.TaskTemplates = map[string]domain.TaskTemplate{}
	}
	if out.Tasks == nil {
		out.Tasks = map[string]domain.TaskSpec{}
	}
	if out.Runs == nil {
		out.Runs = map[string]domain.RunRecord{}
	}
	if out.Events == nil {
		out.Events = []domain.Event{}
	}
	if out.Schedules == nil {
		out.Schedules = map[string]domain.ScheduleEntry{}
	}
	if out.Settings == nil {
		out.Settings = map[string]domain.RuntimeSetting{}
	}
	if out.Proposals == nil {
		out.Proposals = map[string]domain.ConfigChangeProposal{}
	}
	if out.Approvals == nil {
		out.Approvals = map[string]domain.ApprovalTicket{}
	}
	if out.Memories == nil {
		out.Memories = map[string]domain.MemoryRecord{}
	}
	if out.Baselines == nil {
		out.Baselines = map[string]domain.BaselineSnapshot{}
	}
	if out.EvalReports == nil {
		out.EvalReports = map[string]domain.EvalReport{}
	}
	if out.Locks == nil {
		out.Locks = map[string]domain.LockRecord{}
	}
	if out.Actions == nil {
		out.Actions = map[string]domain.ActionIntent{}
	}
	if out.IdempotencyKeys == nil {
		out.IdempotencyKeys = map[string]string{}
	}
	return out
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := s.snapshot
	copy.Events = append([]domain.Event(nil), s.snapshot.Events...)
	copy.Projects = cloneMap(s.snapshot.Projects)
	copy.TaskTemplates = cloneMap(s.snapshot.TaskTemplates)
	copy.Tasks = cloneMap(s.snapshot.Tasks)
	copy.Runs = cloneMap(s.snapshot.Runs)
	copy.Schedules = cloneMap(s.snapshot.Schedules)
	copy.Settings = cloneMap(s.snapshot.Settings)
	copy.Proposals = cloneMap(s.snapshot.Proposals)
	copy.Approvals = cloneMap(s.snapshot.Approvals)
	copy.Memories = cloneMap(s.snapshot.Memories)
	copy.Baselines = cloneMap(s.snapshot.Baselines)
	copy.EvalReports = cloneMap(s.snapshot.EvalReports)
	copy.Locks = cloneMap(s.snapshot.Locks)
	copy.Actions = cloneMap(s.snapshot.Actions)
	copy.IdempotencyKeys = cloneMap(s.snapshot.IdempotencyKeys)
	return copy
}

func cloneMap[T any](in map[string]T) map[string]T {
	out := make(map[string]T, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Store) UpsertProject(p domain.ProjectSpec) error {
	if err := domain.ValidateProjectSpec(p); err != nil {
		return err
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Projects[p.ProjectID] = p
	return s.persistLocked()
}

func (s *Store) GetProject(projectID string) (domain.ProjectSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Projects[projectID]
	return v, ok
}

func (s *Store) ListProjects() []domain.ProjectSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.ProjectSpec, 0, len(s.snapshot.Projects))
	for _, p := range s.snapshot.Projects {
		items = append(items, p)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProjectID < items[j].ProjectID })
	return items
}

func (s *Store) UpsertTaskTemplate(t domain.TaskTemplate) error {
	if err := domain.ValidateTaskTemplate(t); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.TaskTemplates[t.TaskTemplateID] = t
	return s.persistLocked()
}

func (s *Store) GetTaskTemplate(taskTemplateID string) (domain.TaskTemplate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.TaskTemplates[taskTemplateID]
	return v, ok
}

func (s *Store) UpsertTask(t domain.TaskSpec) error {
	if err := domain.ValidateTaskSpec(t); err != nil {
		return err
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Tasks[t.TaskID] = t
	return s.persistLocked()
}

func (s *Store) GetTask(taskID string) (domain.TaskSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Tasks[taskID]
	return v, ok
}

func (s *Store) ListTasksByProject(projectID string) []domain.TaskSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.TaskSpec, 0)
	for _, t := range s.snapshot.Tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) CreateRun(r domain.RunRecord) error {
	if err := domain.ValidateRunRecord(r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.snapshot.Runs[r.RunID]; exists {
		return util.NewConflict("run already exists: %s", r.RunID)
	}
	r.StateVersion = 1
	if r.Timeline.CreatedAt.IsZero() {
		r.Timeline.CreatedAt = time.Now().UTC()
		r.Timeline.UpdatedAt = r.Timeline.CreatedAt
	}
	s.snapshot.Runs[r.RunID] = r
	return s.persistLocked()
}

func (s *Store) UpsertRun(r domain.RunRecord) error {
	if err := domain.ValidateRunRecord(r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.snapshot.Runs[r.RunID]; ok {
		r.StateVersion = existing.StateVersion + 1
	} else if r.StateVersion == 0 {
		r.StateVersion = 1
	}
	r.Timeline.UpdatedAt = time.Now().UTC()
	s.snapshot.Runs[r.RunID] = r
	return s.persistLocked()
}

func (s *Store) CASRun(runID string, expectedVersion int64, mutate func(*domain.RunRecord) error) (domain.RunRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.snapshot.Runs[runID]
	if !ok {
		return domain.RunRecord{}, fmt.Errorf("run not found: %s", runID)
	}
	if expectedVersion > 0 && r.StateVersion != expectedVersion {
		return domain.RunRecord{}, util.NewConflict("run %s version mismatch: expected=%d current=%d", runID, expectedVersion, r.StateVersion)
	}
	if err := mutate(&r); err != nil {
		return domain.RunRecord{}, err
	}
	r.StateVersion++
	r.Timeline.UpdatedAt = time.Now().UTC()
	if err := domain.ValidateRunRecord(r); err != nil {
		return domain.RunRecord{}, err
	}
	s.snapshot.Runs[runID] = r
	if err := s.persistLocked(); err != nil {
		return domain.RunRecord{}, err
	}
	return r, nil
}

func (s *Store) GetRun(runID string) (domain.RunRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Runs[runID]
	return v, ok
}

func (s *Store) ListRunsByTask(taskID string) []domain.RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.RunRecord, 0)
	for _, r := range s.snapshot.Runs {
		if r.TaskID == taskID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timeline.CreatedAt.Before(out[j].Timeline.CreatedAt)
	})
	return out
}

func (s *Store) ListNonTerminalRuns() []domain.RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.RunRecord, 0)
	for _, r := range s.snapshot.Runs {
		if !r.RunStatus.IsTerminal() {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timeline.UpdatedAt.Before(out[j].Timeline.UpdatedAt)
	})
	return out
}

func (s *Store) AppendEvent(e domain.Event) (bool, error) {
	if err := domain.ValidateEvent(e); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.snapshot.IdempotencyKeys[e.IdempotencyKey]; ok {
		if prev != "" {
			return false, nil
		}
	}
	s.snapshot.Events = append(s.snapshot.Events, e)
	s.snapshot.IdempotencyKeys[e.IdempotencyKey] = e.EventID
	if err := s.persistLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListEventsByRun(runID string) []domain.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Event, 0)
	for _, e := range s.snapshot.Events {
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) UpsertSchedule(entry domain.ScheduleEntry) error {
	if err := domain.ValidateScheduleEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Schedules[entry.ScheduleID] = entry
	return s.persistLocked()
}

func (s *Store) GetSchedule(scheduleID string) (domain.ScheduleEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Schedules[scheduleID]
	return v, ok
}

func (s *Store) ListSchedules() []domain.ScheduleEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.ScheduleEntry, 0, len(s.snapshot.Schedules))
	for _, se := range s.snapshot.Schedules {
		out = append(out, se)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScheduleID < out[j].ScheduleID })
	return out
}

func (s *Store) UpsertRuntimeSetting(setting domain.RuntimeSetting) error {
	if err := domain.ValidateRuntimeSetting(setting); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.snapshot.Settings[setting.SettingID]; ok {
		setting.Version = old.Version + 1
	} else if setting.Version == 0 {
		setting.Version = 1
	}
	s.snapshot.Settings[setting.SettingID] = setting
	return s.persistLocked()
}

func (s *Store) UpsertConfigProposal(p domain.ConfigChangeProposal) error {
	if err := domain.ValidateConfigChangeProposal(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Proposals[p.ProposalID] = p
	return s.persistLocked()
}

func (s *Store) GetConfigProposal(proposalID string) (domain.ConfigChangeProposal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Proposals[proposalID]
	return v, ok
}

func (s *Store) UpsertApproval(ticket domain.ApprovalTicket) error {
	if err := domain.ValidateApprovalTicket(ticket); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Approvals[ticket.ApprovalID] = ticket
	return s.persistLocked()
}

func (s *Store) GetApproval(approvalID string) (domain.ApprovalTicket, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Approvals[approvalID]
	return v, ok
}

func (s *Store) UpsertMemory(record domain.MemoryRecord) error {
	if err := domain.ValidateMemoryRecord(record); err != nil {
		return err
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Memories[record.MemoryID] = record
	return s.persistLocked()
}

func (s *Store) ListMemoryByProject(projectID string) []domain.MemoryRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.MemoryRecord, 0)
	for _, m := range s.snapshot.Memories {
		if projectID == "" || m.ProjectID == projectID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *Store) UpsertBaseline(b domain.BaselineSnapshot) error {
	if b.BaselineID == "" || b.ProjectID == "" {
		return errors.New("baseline requires baseline_id and project_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Baselines[b.BaselineID] = b
	return s.persistLocked()
}

func (s *Store) GetBaseline(baselineID string) (domain.BaselineSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.snapshot.Baselines[baselineID]
	return v, ok
}

func (s *Store) ListBaselines(projectID string) []domain.BaselineSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.BaselineSnapshot, 0)
	for _, b := range s.snapshot.Baselines {
		if b.ProjectID == projectID {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VerifiedAt.After(out[j].VerifiedAt) })
	return out
}

func (s *Store) UpsertEvalReport(report domain.EvalReport) error {
	if report.EvalID == "" || report.RunID == "" {
		return errors.New("eval report requires eval_id and run_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := report.EvalID + ":" + report.RunID
	s.snapshot.EvalReports[key] = report
	return s.persistLocked()
}

func (s *Store) ListEvalReports(evalID string) []domain.EvalReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.EvalReport, 0)
	for k, r := range s.snapshot.EvalReports {
		if evalID == "" || hasPrefix(k, evalID+":") {
			out = append(out, r)
		}
	}
	return out
}

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func (s *Store) AcquireLock(lock domain.LockRecord) error {
	if err := domain.ValidateLockRecord(lock); err != nil {
		return err
	}
	key := lockKey(lock.LockType, lock.LockKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.snapshot.Locks[key]; ok {
		if existing.Status == domain.LockStatusHeld && existing.ExpiresAt.After(time.Now().UTC()) {
			return util.NewConflict("lock is held: %s", key)
		}
	}
	s.snapshot.Locks[key] = lock
	return s.persistLocked()
}

func (s *Store) RenewLock(lockType domain.LockType, key string, ownerRunID string, leaseToken int64, ttl time.Duration) (domain.LockRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := lockKey(lockType, key)
	cur, ok := s.snapshot.Locks[k]
	if !ok {
		return domain.LockRecord{}, fmt.Errorf("lock not found: %s", k)
	}
	if cur.OwnerRunID != ownerRunID || cur.LeaseToken != leaseToken {
		return domain.LockRecord{}, util.NewConflict("lock fencing mismatch for %s", k)
	}
	cur.ExpiresAt = time.Now().UTC().Add(ttl)
	s.snapshot.Locks[k] = cur
	if err := s.persistLocked(); err != nil {
		return domain.LockRecord{}, err
	}
	return cur, nil
}

func (s *Store) ReleaseLock(lockType domain.LockType, key string, ownerRunID string, leaseToken int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := lockKey(lockType, key)
	cur, ok := s.snapshot.Locks[k]
	if !ok {
		return nil
	}
	if cur.OwnerRunID != ownerRunID || cur.LeaseToken != leaseToken {
		return util.NewConflict("lock fencing mismatch for release %s", k)
	}
	cur.Status = domain.LockStatusReleased
	s.snapshot.Locks[k] = cur
	return s.persistLocked()
}

func (s *Store) ListActiveLocks() []domain.LockRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.LockRecord, 0)
	now := time.Now().UTC()
	for _, l := range s.snapshot.Locks {
		if l.Status == domain.LockStatusHeld && l.ExpiresAt.After(now) {
			out = append(out, l)
		}
	}
	return out
}

func (s *Store) PutActionIntent(intent domain.ActionIntent) error {
	if intent.ActionID == "" || intent.RunID == "" || intent.IdempotencyKey == "" {
		return errors.New("action intent requires action_id, run_id, idempotency_key")
	}
	now := time.Now().UTC()
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = now
	}
	intent.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, ok := s.snapshot.IdempotencyKeys[intent.IdempotencyKey]; ok && existingID != intent.ActionID {
		return util.NewConflict("duplicate idempotency_key %s", intent.IdempotencyKey)
	}
	s.snapshot.Actions[intent.ActionID] = intent
	s.snapshot.IdempotencyKeys[intent.IdempotencyKey] = intent.ActionID
	return s.persistLocked()
}

func (s *Store) UpdateActionIntent(actionID string, mutate func(*domain.ActionIntent) error) (domain.ActionIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ai, ok := s.snapshot.Actions[actionID]
	if !ok {
		return domain.ActionIntent{}, fmt.Errorf("action intent not found: %s", actionID)
	}
	if err := mutate(&ai); err != nil {
		return domain.ActionIntent{}, err
	}
	ai.UpdatedAt = time.Now().UTC()
	s.snapshot.Actions[actionID] = ai
	if err := s.persistLocked(); err != nil {
		return domain.ActionIntent{}, err
	}
	return ai, nil
}

func (s *Store) ListPendingActions() []domain.ActionIntent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.ActionIntent, 0)
	for _, a := range s.snapshot.Actions {
		if a.Status != "done" && a.Status != "aborted" {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) SeenIdempotencyKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.snapshot.IdempotencyKeys[key]
	return ok
}

func lockKey(lockType domain.LockType, key string) string {
	return string(lockType) + ":" + key
}
