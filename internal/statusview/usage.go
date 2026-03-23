package statusview

import (
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm"
)

type UsageStats struct {
	InputTokens       int64     `json:"input_tokens,omitempty"`
	CachedInputTokens int64     `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64     `json:"output_tokens,omitempty"`
	Turns             int64     `json:"turns,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

func (s UsageStats) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens
}

func (s UsageStats) HasUsage() bool {
	return s.InputTokens != 0 || s.CachedInputTokens != 0 || s.OutputTokens != 0 || s.Turns != 0
}

func (s *UsageStats) AddUsage(usage llm.Usage, at time.Time) {
	if s == nil || !usage.HasUsage() {
		return
	}
	s.InputTokens += usage.InputTokens
	s.CachedInputTokens += usage.CachedInputTokens
	s.OutputTokens += usage.OutputTokens
	s.Turns++
	if !at.IsZero() {
		s.UpdatedAt = at
	}
}

func AddUsageStats(dst *UsageStats, src UsageStats) {
	if dst == nil || !src.HasUsage() {
		return
	}
	dst.InputTokens += src.InputTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.OutputTokens += src.OutputTokens
	dst.Turns += src.Turns
	if dst.UpdatedAt.Before(src.UpdatedAt) {
		dst.UpdatedAt = src.UpdatedAt
	}
}

type BotUsage struct {
	BotID   string
	BotName string
	Usage   UsageStats
}

func NormalizeBotLabel(botID, botName string) (string, string) {
	botID = strings.TrimSpace(botID)
	botName = strings.TrimSpace(botName)
	if botName == "" {
		botName = botID
	}
	if botID == "" {
		botID = botName
	}
	return botID, botName
}

func NewestUsageUpdate(items []BotUsage) time.Time {
	latest := time.Time{}
	for _, item := range items {
		if latest.Before(item.Usage.UpdatedAt) {
			latest = item.Usage.UpdatedAt
		}
	}
	return latest
}
