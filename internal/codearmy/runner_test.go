package codearmy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/prompting"
)

type backendStub struct {
	mu      sync.Mutex
	calls   []llm.RunRequest
	results []llm.RunResult
	errs    []error
}

func (b *backendStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, req)
	if len(b.errs) > 0 {
		err := b.errs[0]
		b.errs = b.errs[1:]
		if err != nil {
			return llm.RunResult{}, err
		}
	}
	if len(b.results) == 0 {
		return llm.RunResult{Reply: "fallback", NextThreadID: ""}, nil
	}
	result := b.results[0]
	b.results = b.results[1:]
	return result, nil
}

func TestRunner_Run_TransitionsAndPersistsState(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "code_army")
	sessionKey := "chat_id:oc_group|thread:omt_alpha"
	backend := &backendStub{
		results: []llm.RunResult{
			{Reply: "manager plan", NextThreadID: "thread-manager"},
			{Reply: "worker output", NextThreadID: "thread-worker"},
			{Reply: "review details\nDECISION: PASS", NextThreadID: "thread-reviewer"},
		},
	}
	runner := NewRunner(stateDir, backend, prompting.NewLoader(filepath.Join("..", "..", "prompts")))
	runner.now = func() time.Time {
		return time.Date(2026, 2, 24, 9, 30, 0, 0, time.UTC)
	}

	req := automation.WorkflowRunRequest{
		Workflow: automation.WorkflowCodeArmy,
		TaskID:   "task_001",
		Prompt:   "实现自动化代码军队流程",
		Model:    "gpt-4.1-mini",
		Profile:  "worker-cheap",
		Env: map[string]string{
			"ALICE_MCP_RECEIVE_ID":  "oc_group",
			"ALICE_MCP_SESSION_KEY": sessionKey,
		},
	}
	statePath := runner.stateFilePath(sessionKey, "default")
	loadState := func() workflowState {
		t.Helper()
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state file failed: %v", err)
		}
		var state workflowState
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatalf("parse state file failed: %v", err)
		}
		return state
	}
	assertHistory := func(state workflowState, want []struct {
		phase          string
		summaryContain string
		decision       string
	}) {
		t.Helper()
		if len(state.History) != len(want) {
			t.Fatalf("expected %d history records, got %+v", len(want), state.History)
		}
		for i, item := range want {
			if state.History[i].Phase != item.phase {
				t.Fatalf("expected history[%d] phase %q, got %+v", i, item.phase, state.History[i])
			}
			if !strings.Contains(state.History[i].Summary, item.summaryContain) {
				t.Fatalf("expected history[%d] summary to contain %q, got %+v", i, item.summaryContain, state.History[i])
			}
			if state.History[i].Decision != item.decision {
				t.Fatalf("expected history[%d] decision %q, got %+v", i, item.decision, state.History[i])
			}
		}
	}

	msg1, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}
	if !strings.Contains(msg1.Message, "## Code Army 进度") {
		t.Fatalf("expected markdown workflow summary, got %q", msg1.Message)
	}
	if !strings.Contains(msg1.Message, "**本次推进**") {
		t.Fatalf("expected step section in workflow summary, got %q", msg1.Message)
	}
	for _, needle := range []string{"manager", "worker", "reviewer", "gate"} {
		if !strings.Contains(strings.ToLower(msg1.Message), needle) {
			t.Fatalf("expected workflow summary to include %q, got %q", needle, msg1.Message)
		}
	}
	state := loadState()
	if state.SessionKey != sessionKey {
		t.Fatalf("expected session key %q after workflow run, got %+v", sessionKey, state)
	}
	if state.Phase != phaseManager {
		t.Fatalf("expected gate pass to switch phase to manager, got %+v", state)
	}
	if state.Iteration != 2 {
		t.Fatalf("expected iteration increment to 2, got %+v", state)
	}
	if state.ManagerThreadID != "thread-manager" || state.WorkerThreadID != "thread-worker" || state.ReviewerThreadID != "thread-reviewer" {
		t.Fatalf("unexpected thread ids after gate run: %+v", state)
	}
	assertHistory(state, []struct {
		phase          string
		summaryContain string
		decision       string
	}{
		{phase: phaseManager, summaryContain: "manager plan"},
		{phase: phaseWorker, summaryContain: "worker output"},
		{phase: phaseReviewer, summaryContain: "review details", decision: decisionPass},
		{phase: phaseGate, summaryContain: "gate passed", decision: decisionPass},
	})

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.calls) != 3 {
		t.Fatalf("expected 3 llm calls before gate, got %d", len(backend.calls))
	}
	for _, call := range backend.calls {
		if call.Model != "gpt-4.1-mini" {
			t.Fatalf("unexpected model: %q", call.Model)
		}
		if call.Profile != "worker-cheap" {
			t.Fatalf("unexpected profile: %q", call.Profile)
		}
	}
}

func TestRunner_Run_PersistsCompletedPhaseBeforeLaterError(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "code_army")
	sessionKey := "chat_id:oc_group|thread:omt_beta"
	backend := &backendStub{
		results: []llm.RunResult{
			{Reply: "manager plan", NextThreadID: "thread-manager"},
		},
		errs: []error{
			nil,
			context.DeadlineExceeded,
		},
	}
	runner := NewRunner(stateDir, backend, prompting.NewLoader(filepath.Join("..", "..", "prompts")))
	runner.now = func() time.Time {
		return time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC)
	}

	req := automation.WorkflowRunRequest{
		Workflow: automation.WorkflowCodeArmy,
		TaskID:   "task_timeout",
		Prompt:   "推进第 4 轮",
		Env: map[string]string{
			"ALICE_MCP_SESSION_KEY": sessionKey,
		},
	}

	_, err := runner.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected workflow run to fail after second phase")
	}

	statePath := runner.stateFilePath(sessionKey, "default")
	raw, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("expected partial state to be persisted, read failed: %v", readErr)
	}

	var state workflowState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("parse state file failed: %v", err)
	}
	if state.Phase != phaseWorker {
		t.Fatalf("expected persisted phase worker after manager step, got %+v", state)
	}
	if state.Iteration != 1 {
		t.Fatalf("expected iteration to remain 1 after partial progress, got %+v", state)
	}
	if state.ManagerPlan != "manager plan" {
		t.Fatalf("expected manager output to be persisted, got %+v", state)
	}
	if state.ManagerThreadID != "thread-manager" {
		t.Fatalf("expected manager thread id to be persisted, got %+v", state)
	}
	if len(state.History) != 1 || state.History[0].Phase != phaseManager {
		t.Fatalf("expected only manager history record after partial progress, got %+v", state.History)
	}
}
