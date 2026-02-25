package llm

import (
	"context"

	corecodex "gitee.com/alicespace/alice/internal/llm/codex"
)

type codexMCPRegistrar struct {
	command string
}

func newCodexMCPRegistrar(cfg CodexConfig) *codexMCPRegistrar {
	return &codexMCPRegistrar{
		command: cfg.Command,
	}
}

func (r *codexMCPRegistrar) EnsureMCPServerRegistered(ctx context.Context, req MCPRegistration) error {
	return corecodex.EnsureMCPServerRegistered(ctx, corecodex.MCPRegistration{
		CodexCommand:  r.command,
		ServerName:    req.ServerName,
		ServerCommand: req.ServerCommand,
		ServerArgs:    req.ServerArgs,
	})
}

var _ MCPRegistrar = (*codexMCPRegistrar)(nil)
