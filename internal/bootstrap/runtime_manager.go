package bootstrap

import (
	"context"
	"fmt"

	"github.com/oklog/run"

	"github.com/Alice-space/alice/internal/config"
)

type RuntimeManager struct {
	Config   config.Config
	Runtimes []*ConnectorRuntime
}

func BuildRuntimeManager(cfg config.Config) (*RuntimeManager, error) {
	runtimeConfigs, err := cfg.RuntimeConfigs()
	if err != nil {
		return nil, err
	}
	manager := &RuntimeManager{
		Config:   cfg,
		Runtimes: make([]*ConnectorRuntime, 0, len(runtimeConfigs)),
	}
	for _, runtimeCfg := range runtimeConfigs {
		backend, err := NewLLMBackend(runtimeCfg)
		if err != nil {
			return nil, fmt.Errorf("build llm provider for bot %q failed: %w", runtimeCfg.BotID, err)
		}
		runtime, err := BuildConnectorRuntime(runtimeCfg, backend)
		if err != nil {
			return nil, fmt.Errorf("build runtime for bot %q failed: %w", runtimeCfg.BotID, err)
		}
		manager.Runtimes = append(manager.Runtimes, runtime)
	}
	return manager, nil
}

func (m *RuntimeManager) Run(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("runtime manager is nil")
	}
	if len(m.Runtimes) == 0 {
		return fmt.Errorf("runtime manager has no runtimes")
	}
	var group run.Group
	for _, runtime := range m.Runtimes {
		if runtime == nil {
			continue
		}
		runtime := runtime
		runtimeCtx, cancel := context.WithCancel(ctx)
		group.Add(func() error {
			return runtime.Run(runtimeCtx)
		}, func(error) {
			cancel()
		})
	}
	return group.Run()
}
