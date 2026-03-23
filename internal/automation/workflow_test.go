package automation

import (
	"context"
	"testing"

	"github.com/Alice-space/alice/internal/llm"
)

type workflowBackendStub struct {
	result llm.RunResult
	err    error
}

func (s workflowBackendStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	return s.result, s.err
}

func TestPromptWorkflowRunner_ExtractsHiddenCommands(t *testing.T) {
	runner := NewPromptWorkflowRunner(workflowBackendStub{
		result: llm.RunResult{
			Reply: "可见回复\n\n<alice_command>/alice needs-human waiting for approval</alice_command>",
		},
	})

	result, err := runner.Run(context.Background(), WorkflowRunRequest{
		Workflow: "code_army",
		Prompt:   "run",
	})
	if err != nil {
		t.Fatalf("run workflow failed: %v", err)
	}
	if result.Message != "可见回复" {
		t.Fatalf("unexpected visible message: %q", result.Message)
	}
	if len(result.Commands) != 1 {
		t.Fatalf("expected one command, got %#v", result.Commands)
	}
	if result.Commands[0].Text != "/alice needs-human waiting for approval" {
		t.Fatalf("unexpected command: %#v", result.Commands[0])
	}
}
