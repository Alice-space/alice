package llm

import "context"

type ProgressFunc func(step string)

type RunRequest struct {
	ThreadID   string
	UserText   string
	Model      string
	Profile    string
	Env        map[string]string
	OnProgress ProgressFunc
}

type RunResult struct {
	Reply        string
	NextThreadID string
}

type Backend interface {
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}
