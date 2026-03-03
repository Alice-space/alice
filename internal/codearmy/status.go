package codearmy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrStateNotFound = errors.New("code_army state not found")

type Inspector struct {
	stateDir string
}

type StateSnapshot struct {
	Workflow       string          `json:"workflow"`
	SessionKey     string          `json:"session_key,omitempty"`
	StateKey       string          `json:"state_key"`
	TaskID         string          `json:"task_id,omitempty"`
	Phase          string          `json:"phase"`
	Iteration      int             `json:"iteration"`
	Objective      string          `json:"objective"`
	ManagerPlan    string          `json:"manager_plan,omitempty"`
	WorkerOutput   string          `json:"worker_output,omitempty"`
	ReviewerReport string          `json:"reviewer_report,omitempty"`
	LastDecision   string          `json:"last_decision,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at"`
	History        []HistoryRecord `json:"history,omitempty"`
}

type HistoryRecord struct {
	At       time.Time `json:"at"`
	Phase    string    `json:"phase"`
	Summary  string    `json:"summary"`
	Decision string    `json:"decision,omitempty"`
}

func NewInspector(stateDir string) *Inspector {
	return &Inspector{stateDir: strings.TrimSpace(stateDir)}
}

func (i *Inspector) Get(sessionKey, stateKey string) (StateSnapshot, error) {
	if i == nil {
		return StateSnapshot{}, errors.New("code_army inspector is nil")
	}
	path := i.stateFilePath(sessionKey, stateKey)
	state, err := i.readStateFile(path)
	if err != nil {
		return StateSnapshot{}, err
	}
	return snapshotFromState(state), nil
}

func (i *Inspector) List(sessionKey string) ([]StateSnapshot, error) {
	if i == nil {
		return nil, errors.New("code_army inspector is nil")
	}
	dir := i.sessionDir(sessionKey)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read code_army session dir failed: %w", err)
	}

	out := make([]StateSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		state, err := i.readStateFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, snapshotFromState(state))
	}

	sort.Slice(out, func(left, right int) bool {
		if !out[left].UpdatedAt.Equal(out[right].UpdatedAt) {
			return out[left].UpdatedAt.After(out[right].UpdatedAt)
		}
		return out[left].StateKey < out[right].StateKey
	})
	return out, nil
}

func (i *Inspector) readStateFile(path string) (workflowState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return workflowState{}, ErrStateNotFound
		}
		return workflowState{}, fmt.Errorf("read code_army state failed: %w", err)
	}
	var state workflowState
	if err := json.Unmarshal(data, &state); err != nil {
		return workflowState{}, fmt.Errorf("parse code_army state failed: %w", err)
	}
	return normalizeState(state), nil
}

func (i *Inspector) stateFilePath(sessionKey, stateKey string) string {
	root := strings.TrimSpace(i.stateDir)
	if root == "" {
		root = filepath.Join(".memory", "code_army")
	}
	stateKey = sanitizeStateKey(stateKey)
	if stateKey == "" {
		stateKey = defaultStateKey
	}
	sessionKey = sanitizeSessionKey(sessionKey)
	if sessionKey == "" {
		return filepath.Join(root, stateKey+".json")
	}
	return filepath.Join(root, sessionKey, stateKey+".json")
}

func (i *Inspector) sessionDir(sessionKey string) string {
	root := strings.TrimSpace(i.stateDir)
	if root == "" {
		root = filepath.Join(".memory", "code_army")
	}
	sessionKey = sanitizeSessionKey(sessionKey)
	if sessionKey == "" {
		return root
	}
	return filepath.Join(root, sessionKey)
}

func snapshotFromState(state workflowState) StateSnapshot {
	history := make([]HistoryRecord, 0, len(state.History))
	for _, item := range state.History {
		history = append(history, HistoryRecord{
			At:       item.At,
			Phase:    item.Phase,
			Summary:  item.Summary,
			Decision: item.Decision,
		})
	}
	return StateSnapshot{
		Workflow:       state.Workflow,
		SessionKey:     state.SessionKey,
		StateKey:       state.Key,
		TaskID:         state.TaskID,
		Phase:          state.Phase,
		Iteration:      state.Iteration,
		Objective:      state.Objective,
		ManagerPlan:    state.ManagerPlan,
		WorkerOutput:   state.WorkerOutput,
		ReviewerReport: state.ReviewerReport,
		LastDecision:   state.LastDecision,
		UpdatedAt:      state.UpdatedAt,
		History:        history,
	}
}
