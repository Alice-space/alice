package llm

import (
	"context"
	"strings"

	coreopencode "github.com/Alice-space/alice/internal/llm/providers/opencode"
)

type opencodeBackend struct {
	runner         coreopencode.Runner
	profileRunners map[string]coreopencode.Runner
}

func newOpenCodeBackend(cfg OpenCodeConfig) *opencodeBackend {
	defaultRunner := coreopencode.Runner{
		Command:        cfg.Command,
		Timeout:        cfg.Timeout,
		DefaultModel:   cfg.Model,
		DefaultVariant: cfg.Variant,
		Env:            cfg.Env,
		WorkspaceDir:   cfg.WorkspaceDir,
	}
	profileRunners := make(map[string]coreopencode.Runner, len(cfg.ProfileOverrides))
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
	return &opencodeBackend{runner: defaultRunner, profileRunners: profileRunners}
}

func (b *opencodeBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
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
	reply, nextThreadID, inputTokens, cachedInputTokens, outputTokens, err := runner.RunWithThreadAndProgress(
		ctx,
		strings.TrimSpace(req.ThreadID),
		req.UserText,
		strings.TrimSpace(req.Model),
		strings.TrimSpace(req.Variant),
		req.Env,
		req.OnProgress,
		rawEventFn,
	)
	return RunResult{
		Reply:        reply,
		NextThreadID: strings.TrimSpace(nextThreadID),
		Usage: Usage{
			InputTokens:       inputTokens,
			CachedInputTokens: cachedInputTokens,
			OutputTokens:      outputTokens,
		},
	}, err
}

var _ Backend = (*opencodeBackend)(nil)
