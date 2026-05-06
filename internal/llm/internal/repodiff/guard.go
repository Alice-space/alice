package repodiff

import (
	"sort"
	"strings"
	"sync"
)

type Lease struct {
	repos        []string
	repoVersions map[string]uint64
	contaminated bool
}

type RunGuard struct {
	mu             sync.Mutex
	active         map[string]int
	overlapVersion map[string]uint64
	versionSeed    uint64
}

func NewRunGuard() *RunGuard {
	return &RunGuard{
		active:         make(map[string]int),
		overlapVersion: make(map[string]uint64),
	}
}

func (g *RunGuard) Acquire(repos []string) *Lease {
	normalized := normalizeRepoList(repos)
	lease := &Lease{
		repos:        normalized,
		repoVersions: make(map[string]uint64, len(normalized)),
	}
	if len(normalized) == 0 {
		return lease
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, repo := range normalized {
		if g.active[repo] > 0 {
			g.versionSeed++
			g.overlapVersion[repo] = g.versionSeed
			lease.contaminated = true
		}
	}
	for _, repo := range normalized {
		g.active[repo]++
		lease.repoVersions[repo] = g.overlapVersion[repo]
	}
	return lease
}

func (g *RunGuard) Release(lease *Lease) {
	if g == nil || lease == nil || len(lease.repos) == 0 {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for _, repo := range lease.repos {
		count := g.active[repo]
		if count <= 1 {
			delete(g.active, repo)
			continue
		}
		g.active[repo] = count - 1
	}
	lease.repos = nil
}

func (g *RunGuard) CanEmit(lease *Lease) bool {
	if g == nil || lease == nil || len(lease.repos) == 0 {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if lease.contaminated {
		return false
	}
	for _, repo := range lease.repos {
		if g.active[repo] != 1 {
			lease.contaminated = true
			return false
		}
		if g.overlapVersion[repo] != lease.repoVersions[repo] {
			lease.contaminated = true
			return false
		}
	}
	return true
}

func normalizeRepoList(repos []string) []string {
	seen := make(map[string]struct{}, len(repos))
	normalized := make([]string, 0, len(repos))
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		if _, exists := seen[repo]; exists {
			continue
		}
		seen[repo] = struct{}{}
		normalized = append(normalized, repo)
	}
	sort.Strings(normalized)
	return normalized
}
