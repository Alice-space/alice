package connector

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Alice-space/alice/internal/statusview"
)

type processorStatusUsageProvider struct {
	processor *Processor

	mu      sync.RWMutex
	botID   string
	botName string
	peers   []StatusUsageSource
}

func (p *processorStatusUsageProvider) SetIdentity(botID, botName string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.botID = strings.TrimSpace(botID)
	p.botName = strings.TrimSpace(botName)
}

func (p *processorStatusUsageProvider) Identity() (string, string) {
	if p == nil {
		return "", ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.botID, p.botName
}

func (p *processorStatusUsageProvider) SetSources(sources []StatusUsageSource) {
	if p == nil {
		return
	}
	normalized := make([]StatusUsageSource, 0, len(sources))
	for _, source := range sources {
		path := strings.TrimSpace(source.SessionStatePath)
		if path == "" {
			continue
		}
		normalized = append(normalized, StatusUsageSource{
			BotID:            strings.TrimSpace(source.BotID),
			BotName:          strings.TrimSpace(source.BotName),
			SessionStatePath: path,
		})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = normalized
}

func (p *processorStatusUsageProvider) UsageForScope(scopeKey string) (statusview.UsageStats, []statusview.BotUsage, error) {
	scopeKey = strings.TrimSpace(scopeKey)
	if p == nil || scopeKey == "" {
		return statusview.UsageStats{}, nil, os.ErrInvalid
	}

	p.mu.RLock()
	currentBotID, currentBotName := statusview.NormalizeBotLabel(p.botID, p.botName)
	peers := append([]StatusUsageSource(nil), p.peers...)
	processor := p.processor
	p.mu.RUnlock()

	byBot := make(map[string]statusview.BotUsage)
	currentUsage := collectInMemoryUsageForScope(processor, scopeKey)
	if currentBotID != "" || currentUsage.HasUsage() {
		byBot[currentBotID] = statusview.BotUsage{
			BotID:   currentBotID,
			BotName: currentBotName,
			Usage:   currentUsage,
		}
	}

	var firstErr error
	for _, source := range peers {
		sourceBotID, sourceBotName := statusview.NormalizeBotLabel(source.BotID, source.BotName)
		if currentBotID != "" && sourceBotID == currentBotID {
			continue
		}
		usage, err := loadUsageFromSessionState(source.SessionStatePath, scopeKey)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		existing := byBot[sourceBotID]
		existing.BotID = sourceBotID
		existing.BotName = sourceBotName
		statusview.AddUsageStats(&existing.Usage, usage)
		byBot[sourceBotID] = existing
	}

	total := statusview.UsageStats{}
	items := make([]statusview.BotUsage, 0, len(byBot))
	for _, item := range byBot {
		statusview.AddUsageStats(&total, item.Usage)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.Usage.TotalTokens() != right.Usage.TotalTokens() {
			return left.Usage.TotalTokens() > right.Usage.TotalTokens()
		}
		return left.BotName < right.BotName
	})
	return total, items, firstErr
}

func collectInMemoryUsageForScope(processor *Processor, scopeKey string) statusview.UsageStats {
	if processor == nil {
		return statusview.UsageStats{}
	}
	return processor.collectInMemoryUsageForScope(scopeKey)
}

func (p *Processor) collectInMemoryUsageForScope(scopeKey string) statusview.UsageStats {
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" {
		return statusview.UsageStats{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	total := statusview.UsageStats{}
	for key, state := range p.sessions {
		stateScopeKey := strings.TrimSpace(state.ScopeKey)
		if stateScopeKey == "" {
			stateScopeKey = scopeKeyFromSessionKey(key)
		}
		if stateScopeKey != scopeKey {
			continue
		}
		statusview.AddUsageStats(&total, state.Usage)
	}
	return total
}

func loadUsageFromSessionState(path, scopeKey string) (statusview.UsageStats, error) {
	path = strings.TrimSpace(path)
	scopeKey = strings.TrimSpace(scopeKey)
	if path == "" || scopeKey == "" {
		return statusview.UsageStats{}, os.ErrInvalid
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return statusview.UsageStats{}, nil
		}
		return statusview.UsageStats{}, err
	}

	var snapshot sessionStateSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return statusview.UsageStats{}, err
	}

	total := statusview.UsageStats{}
	for key, state := range snapshot.Sessions {
		stateScopeKey := strings.TrimSpace(state.ScopeKey)
		if stateScopeKey == "" {
			stateScopeKey = scopeKeyFromSessionKey(key)
		}
		if stateScopeKey != scopeKey {
			continue
		}
		statusview.AddUsageStats(&total, state.Usage)
	}
	return total, nil
}

func formatBuiltinStatusUsageLine(item statusview.BotUsage) string {
	botLabel := strings.TrimSpace(item.BotName)
	if botLabel == "" {
		botLabel = strings.TrimSpace(item.BotID)
	}
	if botLabel == "" {
		botLabel = "unknown"
	}

	parts := []string{
		"- `" + sanitizeInlineCode(botLabel) + "`",
		"total `" + formatBuiltinStatusTokenCount(item.Usage.TotalTokens()) + "`",
		"input `" + formatBuiltinStatusTokenCount(item.Usage.InputTokens) + "`",
		"cached `" + formatBuiltinStatusTokenCount(item.Usage.CachedInputTokens) + "`",
		"output `" + formatBuiltinStatusTokenCount(item.Usage.OutputTokens) + "`",
	}
	if item.Usage.Turns > 0 {
		parts = append(parts, "turns `"+formatBuiltinStatusTokenCount(item.Usage.Turns)+"`")
	}
	if updatedAt := formatBuiltinStatusTime(item.Usage.UpdatedAt); updatedAt != "" {
		parts = append(parts, "updated `"+updatedAt+"`")
	}
	return strings.Join(parts, " | ")
}

func formatBuiltinStatusTokenCount(value int64) string {
	if value < 0 {
		value = 0
	}
	raw := []byte(strconv.FormatInt(value, 10))
	n := len(raw)
	if n <= 3 {
		return string(raw)
	}

	out := make([]byte, 0, n+(n-1)/3)
	prefix := n % 3
	if prefix == 0 {
		prefix = 3
	}
	out = append(out, raw[:prefix]...)
	for idx := prefix; idx < n; idx += 3 {
		out = append(out, ',')
		out = append(out, raw[idx:idx+3]...)
	}
	return string(out)
}
