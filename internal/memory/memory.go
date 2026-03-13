package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
)

const (
	LongTermFileName = "MEMORY.md"
	ShortTermDirName = "daily"
)

const (
	shortTermLayout     = "2006-01-02"
	shortTermFileSuffix = ".md"
)

const (
	defaultMaxLongTermRunes  = 6000
	defaultMaxShortTermRunes = 8000
	defaultMaxEntryRunes     = 2000
)

type Manager struct {
	Dir string

	MaxLongTermRunes  int
	MaxShortTermRunes int
	MaxEntryRunes     int

	now     func() time.Time
	prompts *prompting.Loader
	mu      sync.Mutex
}

func NewManager(dir string, prompts *prompting.Loader) *Manager {
	return &Manager{
		Dir:               strings.TrimSpace(dir),
		MaxLongTermRunes:  defaultMaxLongTermRunes,
		MaxShortTermRunes: defaultMaxShortTermRunes,
		MaxEntryRunes:     defaultMaxEntryRunes,
		now:               time.Now,
		prompts:           prompts,
	}
}

func (m *Manager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.Dir) == "" {
		return errors.New("memory dir is empty")
	}

	if err := ensureLayoutDirs(m.Dir); err != nil {
		return fmt.Errorf("create scoped memory layout failed: %w", err)
	}
	return nil
}

func ensureFileExists(path string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	return f.Close()
}

func (m *Manager) BuildPrompt(memoryScopeKey, userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.Dir) == "" {
		return "", errors.New("memory dir is empty")
	}
	if err := ensureLayoutDirs(m.Dir); err != nil {
		return "", fmt.Errorf("prepare scoped memory layout failed: %w", err)
	}

	scope, err := resolveScopePaths(m.Dir, memoryScopeKey)
	if err != nil {
		return "", fmt.Errorf("resolve memory scope failed: %w", err)
	}
	if err := ensureScopeDirs(scope); err != nil {
		return "", fmt.Errorf("prepare scoped memory dir failed: %w", err)
	}

	now := m.now()
	globalLongPath := globalLongTermPath(m.Dir)
	globalLongText, err := readOptionalFile(globalLongPath)
	if err != nil {
		return "", fmt.Errorf("read global long-term memory failed: %w", err)
	}
	scopeLongText, err := readOptionalFile(scope.LongTermPath)
	if err != nil {
		return "", fmt.Errorf("read scoped long-term memory failed: %w", err)
	}

	globalLongText = normalizeMemoryText(globalLongText, m.maxLongTermRunes())
	scopeLongText = normalizeMemoryText(scopeLongText, m.maxLongTermRunes())
	globalLongPromptPath := absOrSame(globalLongPath)
	scopeLongPromptPath := absOrSame(scope.LongTermPath)
	shortTermName := shortTermFileName(now)
	scopeShortTermDir := absOrSame(scope.DailyDir)
	if m.prompts == nil {
		return "", errors.New("memory prompt templates are unavailable")
	}
	prompt, err := m.prompts.RenderFile("memory/prompt.md.tmpl", map[string]any{
		"GlobalLongPath":    globalLongPromptPath,
		"GlobalLongText":    globalLongText,
		"ScopeKey":          scope.Key,
		"ScopeLongPath":     scopeLongPromptPath,
		"ScopeLongText":     scopeLongText,
		"ScopeShortTermDir": scopeShortTermDir,
		"ShortTermName":     shortTermName,
		"UserText":          userText,
	})
	if err != nil {
		return "", err
	}
	logging.Debugf(
		"memory prompt assembled dir=%s scope=%s global_long_term_file=%s scoped_long_term_file=%s scoped_short_term_dir=%s user_text=%q prompt=%q",
		m.Dir,
		scope.Key,
		globalLongPromptPath,
		scopeLongPromptPath,
		scopeShortTermDir,
		userText,
		prompt,
	)

	return prompt, nil
}

func (m *Manager) SaveInteraction(memoryScopeKey, userText, assistantText string, failed bool) (bool, error) {
	logging.Debugf(
		"memory save delegated to llm dir=%s scope=%s changed=false user_text=%q assistant_text=%q failed=%t",
		m.Dir,
		normalizeMemoryScopeKey(memoryScopeKey),
		strings.TrimSpace(userText),
		strings.TrimSpace(assistantText),
		failed,
	)
	return false, nil
}

func (m *Manager) AppendDailySummary(memoryScopeKey, sessionKey, summary string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.Dir) == "" {
		return errors.New("memory dir is empty")
	}
	if err := ensureLayoutDirs(m.Dir); err != nil {
		return fmt.Errorf("prepare scoped memory layout failed: %w", err)
	}

	scope, err := resolveScopePaths(m.Dir, memoryScopeKey)
	if err != nil {
		return fmt.Errorf("resolve memory scope failed: %w", err)
	}
	if err := ensureScopeDirs(scope); err != nil {
		return fmt.Errorf("prepare scoped memory dir failed: %w", err)
	}

	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = "unknown"
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "无重要新增信息"
	}
	summary = clipRunes(summary, m.maxEntryRunes())

	if at.IsZero() {
		at = m.now()
	}
	at = at.Local()

	dailyPath := filepath.Join(scope.DailyDir, shortTermFileName(at))
	f, err := os.OpenFile(dailyPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daily memory file failed: %w", err)
	}
	defer f.Close()

	entry := "## " + at.Format("15:04:05") + " | session: " + sessionKey + "\n" +
		"空闲摘要：\n" + summary + "\n\n"
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append daily memory failed: %w", err)
	}

	logging.Debugf(
		"daily memory appended dir=%s scope=%s session=%s file=%s summary=%q",
		m.Dir,
		scope.Key,
		sessionKey,
		absOrSame(dailyPath),
		summary,
	)
	return nil
}

func (m *Manager) maxLongTermRunes() int {
	if m.MaxLongTermRunes <= 0 {
		return defaultMaxLongTermRunes
	}
	return m.MaxLongTermRunes
}

func (m *Manager) maxEntryRunes() int {
	if m.MaxEntryRunes <= 0 {
		return defaultMaxEntryRunes
	}
	return m.MaxEntryRunes
}

func shortTermFileName(now time.Time) string {
	return now.Format(shortTermLayout) + shortTermFileSuffix
}

func absOrSame(path string) string {
	if absPath, err := filepath.Abs(path); err == nil {
		return absPath
	}
	return path
}

func readOptionalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", err
}

func normalizeMemoryText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "（空）"
	}
	return clipTailRunes(text, maxRunes)
}

func clipTailRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return "...\n" + string(runes[len(runes)-maxRunes:])
}

func clipRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
