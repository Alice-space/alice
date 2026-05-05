package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// MultiBackend routes Run calls to one of several backends based on
// RunRequest.Provider, falling back to the default provider when unset.
type MultiBackend struct {
	defaultProvider string
	backends        map[string]Backend
}

// NewMultiBackend constructs a MultiBackend. At least one backend must be
// provided. When defaultProvider is empty and exactly one backend is
// registered, that backend becomes the default.
func NewMultiBackend(defaultProvider string, backends map[string]Backend) (*MultiBackend, error) {
	normalizedDefault := normalizeProvider(defaultProvider)
	out := make(map[string]Backend, len(backends))
	for rawProvider, backend := range backends {
		if backend == nil {
			continue
		}
		provider := normalizeProvider(rawProvider)
		if provider == "" {
			if normalizedDefault != "" {
				provider = normalizedDefault
			} else {
				provider = ProviderCodex
			}
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
		} else if _, ok := out[ProviderCodex]; ok {
			normalizedDefault = ProviderCodex
		} else {
			return nil, fmt.Errorf("multi backend: defaultProvider must be set when multiple backends are configured and %q is not one of them", ProviderCodex)
		}
	} else if _, ok := out[normalizedDefault]; !ok {
		return nil, fmt.Errorf("multi backend: defaultProvider %q is not in the registered backends", normalizedDefault)
	}
	return &MultiBackend{
		defaultProvider: normalizedDefault,
		backends:        out,
	}, nil
}

// Run dispatches to the backend for req.Provider, or the default backend when
// req.Provider is empty.
func (m *MultiBackend) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if m == nil {
		return RunResult{}, fmt.Errorf("multi backend is nil")
	}
	provider := normalizeProvider(req.Provider)
	if provider == "" {
		provider = m.defaultProvider
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
