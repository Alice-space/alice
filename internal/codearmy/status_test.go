package codearmy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/prompting"
)

func TestInspector_ListAndGet_BySessionKey(t *testing.T) {
	stateDir := t.TempDir()
	backend := &backendStub{
		results: []llm.RunResult{
			{Reply: "manager plan", NextThreadID: "thread-manager"},
		},
	}
	runner := NewRunner(stateDir, backend, prompting.NewLoader(filepath.Join("..", "..", "prompts")))
	req := automation.WorkflowRunRequest{
		Workflow: automation.WorkflowCodeArmy,
		TaskID:   "task_001",
		Prompt:   "推进 code army",
		Env: map[string]string{
			"ALICE_MCP_SESSION_KEY": "chat_id:oc_group|thread:omt_alpha",
		},
	}
	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}

	inspector := NewInspector(stateDir)
	states, err := inspector.List("chat_id:oc_group|thread:omt_alpha")
	if err != nil {
		t.Fatalf("list states failed: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if states[0].StateKey != "default" {
		t.Fatalf("unexpected state key: %+v", states[0])
	}

	state, err := inspector.Get("chat_id:oc_group|thread:omt_alpha", "default")
	if err != nil {
		t.Fatalf("get state failed: %v", err)
	}
	if state.SessionKey != "chat_id:oc_group|thread:omt_alpha" {
		t.Fatalf("unexpected session key: %+v", state)
	}
	if state.Phase != phaseWorker {
		t.Fatalf("expected next phase worker after manager run, got %+v", state)
	}
}
