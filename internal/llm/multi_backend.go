package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type MultiBackend struct {
	defaultProvider string
	backends        map[string]Backend
}

func NewMultiBackend(defaultProvider string, backends map[string]Backend) (*MultiBackend, error) {
	normalizedDefault := normalizeProvider(defaultProvider)
	out := make(map[string]Backend, len(backends))
	for rawProvider, backend := range backends {
		if backend == nil {
			continue
		}
		provider := normalizeProvider(rawProvider)
		if provider == "" {
			provider = normalizedDefault
		}
		if provider == "" {
			provider = ProviderCodex
		}
		out[provider] = backend
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("multi backend requires at least one backend")
	}
	if normalizedDefault == "" {
		if len(out) == 1 {
			for provider := range out {
				normalizedDefault = provider
			}
		} else {
			normalizedDefault = ProviderCodex
		}
	}
	return &MultiBackend{
		defaultProvider: normalizedDefault,
		backends:        out,
	}, nil
}

func (m *MultiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if m == nil {
		return RunResult{}, fmt.Errorf("multi backend is nil")
	}
	provider := normalizeProvider(req.Provider)
	if provider == "" {
		provider = normalizeProvider(m.defaultProvider)
	}
	if provider == "" {
		provider = ProviderCodex
	}
	backend, ok := m.backends[provider]
	if !ok {
		return RunResult{}, fmt.Errorf("llm backend for provider %q is unavailable; configured providers: %s", provider, strings.Join(m.providerList(), ", "))
	}
	req.Provider = provider
	return backend.Run(ctx, req)
}

func (m *MultiBackend) providerList() []string {
	if m == nil || len(m.backends) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.backends))
	for provider := range m.backends {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}
