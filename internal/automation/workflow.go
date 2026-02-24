package automation

import (
	"context"
	"strings"
)

const WorkflowCodeArmy = "code_army"

type WorkflowRunRequest struct {
	Workflow string
	TaskID   string
	StateKey string
	Prompt   string
	Model    string
	Profile  string
	Env      map[string]string
}

type WorkflowRunResult struct {
	Message string
}

type WorkflowRunner interface {
	Run(ctx context.Context, req WorkflowRunRequest) (WorkflowRunResult, error)
}

func normalizeWorkflowName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
