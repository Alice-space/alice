package llm

import (
	"context"
	"strings"

	corekimi "github.com/Alice-space/alice/internal/llm/providers/kimi"
)

type kimiBackend struct {
	runner         corekimi.Runner
	profileRunners map[string]corekimi.Runner
}

func newKimiBackend(cfg KimiConfig) *kimiBackend {
	defaultRunner := corekimi.Runner{
		Command:      cfg.Command,
		Timeout:      cfg.Timeout,
		Env:          cfg.Env,
		WorkspaceDir: cfg.WorkspaceDir,
	}
	profileRunners := make(map[string]corekimi.Runner, len(cfg.ProfileOverrides))
	for name, override := range cfg.ProfileOverrides {
		r := defaultRunner
		if strings.TrimSpace(override.Command) != "" {
			r.Command = strings.TrimSpace(override.Command)
		}
		if override.Timeout > 0 {
			r.Timeout = override.Timeout
		}
		profileRunners[name] = r
	}
	return &kimiBackend{runner: defaultRunner, profileRunners: profileRunners}
}

func (b *kimiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	runner := b.runner
	if profile := strings.TrimSpace(req.Profile); profile != "" {
		if r, ok := b.profileRunners[profile]; ok {
			runner = r
		}
	}
	if strings.TrimSpace(req.WorkspaceDir) != "" {
		runner.WorkspaceDir = strings.TrimSpace(req.WorkspaceDir)
	}
	var rawEventFn func(kind, line, detail string)
	if req.OnRawEvent != nil {
		fn := req.OnRawEvent
		rawEventFn = func(kind, line, detail string) {
			fn(RawEvent{Kind: kind, Line: line, Detail: detail})
		}
	}
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		req.UserText,
		strings.TrimSpace(req.Model),
		req.Env,
		req.OnProgress,
		rawEventFn,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
	}, err
}

var _ Backend = (*kimiBackend)(nil)
