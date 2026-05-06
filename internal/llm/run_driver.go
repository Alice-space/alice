package llm

import (
	"strconv"
)

// NewInteractiveProviderSession creates one logical interactive conversation
// for the configured provider.
func NewInteractiveProviderSession(cfg FactoryConfig) (*InteractiveSession, error) {
	provider := normalizeProvider(cfg.Provider)
	if provider == "" {
		provider = ProviderCodex
	}
	switch provider {
	case ProviderCodex:
		return NewInteractiveSession(newCodexAppServerDriver(cfg.Codex)), nil
	case ProviderKimi:
		return NewInteractiveSession(newKimiWireDriver(cfg.Kimi)), nil
	case ProviderClaude:
		return NewInteractiveSession(newClaudeStreamDriver(cfg.Claude)), nil
	case ProviderOpenCode:
		return NewInteractiveSession(newOpenCodeAppServerDriver(cfg.OpenCode)), nil
	default:
		return nil, ErrUnsupportedProvider(provider)
	}
}

type ErrUnsupportedProvider string

func (e ErrUnsupportedProvider) Error() string {
	return "unsupported llm_provider " + strconv.Quote(string(e))
}

var ErrSteerUnsupported = errString("provider does not support native steer")

type errString string

func (e errString) Error() string { return string(e) }
