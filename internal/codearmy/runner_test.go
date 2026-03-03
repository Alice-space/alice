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

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/llm"
)

type backendStub struct {
	mu      sync.Mutex
	calls   []llm.RunRequest
	results []llm.RunResult
}

func (b *backendStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, req)
	if len(b.results) == 0 {
		return llm.RunResult{Reply: "fallback", NextThreadID: ""}, nil
	}
	result := b.results[0]
	b.results = b.results[1:]
	return result, nil
}

func TestRunner_Run_TransitionsAndPersistsState(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "code_army")
	backend := &backendStub{
		results: []llm.RunResult{
			{Reply: "manager plan", NextThreadID: "thread-manager"},
			{Reply: "worker output", NextThreadID: "thread-worker"},
			{Reply: "review details\nDECISION: PASS", NextThreadID: "thread-reviewer"},
		},
	}
	runner := NewRunner(stateDir, backend)
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
			"ALICE_MCP_SESSION_KEY": "chat_id:oc_group|thread:omt_alpha",
		},
	}

	msg1, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run manager failed: %v", err)
	}
	if !strings.Contains(msg1.Message, "manager") {
		t.Fatalf("unexpected manager message: %q", msg1.Message)
	}

	msg2, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run worker failed: %v", err)
	}
	if !strings.Contains(msg2.Message, "worker") {
		t.Fatalf("unexpected worker message: %q", msg2.Message)
	}

	msg3, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run reviewer failed: %v", err)
	}
	if !strings.Contains(strings.ToUpper(msg3.Message), "PASS") {
		t.Fatalf("unexpected reviewer message: %q", msg3.Message)
	}

	msg4, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run gate failed: %v", err)
	}
	if !strings.Contains(msg4.Message, "通过") {
		t.Fatalf("unexpected gate message: %q", msg4.Message)
	}

	backend.mu.Lock()
	if len(backend.calls) != 3 {
		backend.mu.Unlock()
		t.Fatalf("expected 3 llm calls before gate, got %d", len(backend.calls))
	}
	for _, call := range backend.calls {
		if call.Model != "gpt-4.1-mini" {
			backend.mu.Unlock()
			t.Fatalf("unexpected model: %q", call.Model)
		}
		if call.Profile != "worker-cheap" {
			backend.mu.Unlock()
			t.Fatalf("unexpected profile: %q", call.Profile)
		}
	}
	backend.mu.Unlock()

	statePath := runner.stateFilePath("chat_id:oc_group|thread:omt_alpha", "default")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file failed: %v", err)
	}
	var state workflowState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("parse state file failed: %v", err)
	}
	if state.Phase != phaseManager {
		t.Fatalf("expected gate pass to switch phase to manager, got %q", state.Phase)
	}
	if state.Iteration != 2 {
		t.Fatalf("expected iteration increment to 2, got %d", state.Iteration)
	}
	if state.ManagerThreadID == "" || state.WorkerThreadID == "" || state.ReviewerThreadID == "" {
		t.Fatalf("expected role thread ids persisted, got %+v", state)
	}
	if state.SessionKey != "chat_id:oc_group|thread:omt_alpha" {
		t.Fatalf("expected session key persisted, got %+v", state)
	}
}
