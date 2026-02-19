package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gitee.com/alicespace/alice/internal/logging"
)

const (
	LongTermFileName = "MEMORY.md"
	ShortTermDirName = "daily"
)

const (
	shortTermLayout     = "2006-01-02"
	shortTermFileSuffix = ".md"
	timestampLayout     = "2006-01-02 15:04:05"
)

const (
	defaultMaxLongTermRunes  = 6000
	defaultMaxShortTermRunes = 8000
	defaultMaxEntryRunes     = 2000
)

var longTermKeywords = []string{
	"记住",
	"长期记忆",
	"长期保存",
	"remember this",
	"save this",
}

type Manager struct {
	Dir string

	MaxLongTermRunes  int
	MaxShortTermRunes int
	MaxEntryRunes     int

	now func() time.Time
	mu  sync.Mutex
}

func NewManager(dir string) *Manager {
	return &Manager{
		Dir:               strings.TrimSpace(dir),
		MaxLongTermRunes:  defaultMaxLongTermRunes,
		MaxShortTermRunes: defaultMaxShortTermRunes,
		MaxEntryRunes:     defaultMaxEntryRunes,
		now:               time.Now,
	}
}

func (m *Manager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureFiles(m.now())
}

func (m *Manager) BuildPrompt(userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if err := m.ensureFiles(now); err != nil {
		return "", err
	}

	longTermPath := filepath.Join(m.Dir, LongTermFileName)

	longBytes, err := os.ReadFile(longTermPath)
	if err != nil {
		return "", fmt.Errorf("read long-term memory failed: %w", err)
	}

	longText := normalizeMemoryText(string(longBytes), m.maxLongTermRunes())
	shortTermName := shortTermFileName(now)
	shortTermDir := m.shortTermDir()
	if absDir, absErr := filepath.Abs(shortTermDir); absErr == nil {
		shortTermDir = absDir
	}

	prompt := "以下是记忆模块内容。请优先遵循当前用户消息；如果记忆与当前消息冲突，以当前用户消息为准。\n\n" +
		"长期记忆（" + LongTermFileName + "）：\n" + longText + "\n\n" +
		"分日期记忆（按需检索，不直接注入）：\n" +
		"- 目录位置：" + shortTermDir + "\n" +
		"- 文件命名：YYYY-MM-DD.md（例如：" + shortTermName + "）\n" +
		"- 需要历史信息时，请按日期自行检索对应文件。\n\n" +
		"当前用户消息：\n" + userText + "\n\n" +
		"按需记忆更新：\n" +
		"- 仅当用户明确提出“记住 / 长期记忆 / 长期保存 / remember this / save this”时，才视为长期记忆更新请求。\n" +
		"- 若用户未明确要求，不要将临时任务细节升级为长期偏好。"
	logging.Debugf(
		"memory prompt assembled dir=%s long_term_file=%s short_term_dir=%s user_text=%q prompt=%q",
		m.Dir,
		longTermPath,
		shortTermDir,
		userText,
		prompt,
	)
	return prompt, nil
}

func (m *Manager) SaveInteraction(userText, assistantText string, failed bool) error {
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if assistantText == "" {
		assistantText = "（空）"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if err := m.ensureFiles(now); err != nil {
		return err
	}

	status := "success"
	if failed {
		status = "failed"
	}
	longTermUpdated := shouldWriteLongTerm(userText)

	entry := fmt.Sprintf("[%s]\n用户：%s\n助手：%s\n状态：%s\n\n",
		now.Format(timestampLayout),
		clipRunes(sanitizeLineEndings(userText), m.maxEntryRunes()),
		clipRunes(sanitizeLineEndings(assistantText), m.maxEntryRunes()),
		status,
	)
	shortTermPath := m.shortTermPath(now)
	if err := appendText(shortTermPath, entry); err != nil {
		return fmt.Errorf("append short-term memory failed: %w", err)
	}

	if longTermUpdated {
		longEntry := fmt.Sprintf("[%s] %s\n",
			now.Format(timestampLayout),
			clipRunes(sanitizeLineEndings(userText), m.maxEntryRunes()),
		)
		longTermPath := filepath.Join(m.Dir, LongTermFileName)
		if err := appendText(longTermPath, longEntry); err != nil {
			return fmt.Errorf("append long-term memory failed: %w", err)
		}
	}
	logging.Debugf(
		"memory update finished dir=%s short_term_file=%s short_term_updated=true long_term_updated=%t status=%s user_text=%q assistant_text=%q",
		m.Dir,
		shortTermPath,
		longTermUpdated,
		status,
		userText,
		assistantText,
	)

	return nil
}

func (m *Manager) ensureFiles(now time.Time) error {
	dir := strings.TrimSpace(m.Dir)
	if dir == "" {
		return errors.New("memory dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create memory dir failed: %w", err)
	}

	longTermPath := filepath.Join(dir, LongTermFileName)
	if err := ensureFile(longTermPath, "长期记忆（建议手动维护稳定偏好与约束）\n\n"); err != nil {
		return fmt.Errorf("ensure long-term memory failed: %w", err)
	}

	shortTermDir := filepath.Join(dir, ShortTermDirName)
	if err := os.MkdirAll(shortTermDir, 0o755); err != nil {
		return fmt.Errorf("create short-term dir failed: %w", err)
	}

	shortTermPath := filepath.Join(shortTermDir, shortTermFileName(now))
	shortTermBootstrap := fmt.Sprintf("短期记忆（%s）\n\n", now.Format(shortTermLayout))
	if err := ensureFile(shortTermPath, shortTermBootstrap); err != nil {
		return fmt.Errorf("ensure short-term memory failed: %w", err)
	}
	return nil
}

func (m *Manager) maxLongTermRunes() int {
	if m.MaxLongTermRunes <= 0 {
		return defaultMaxLongTermRunes
	}
	return m.MaxLongTermRunes
}

func (m *Manager) maxShortTermRunes() int {
	if m.MaxShortTermRunes <= 0 {
		return defaultMaxShortTermRunes
	}
	return m.MaxShortTermRunes
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

func (m *Manager) shortTermDir() string {
	return filepath.Join(m.Dir, ShortTermDirName)
}

func (m *Manager) shortTermPath(now time.Time) string {
	return filepath.Join(m.shortTermDir(), shortTermFileName(now))
}

func ensureFile(path, bootstrap string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(bootstrap), 0o644)
}

func appendText(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(text)
	return err
}

func normalizeMemoryText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "（空）"
	}
	return clipTailRunes(text, maxRunes)
}

func sanitizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
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

func shouldWriteLongTerm(userText string) bool {
	normalized := strings.ToLower(strings.TrimSpace(userText))
	if normalized == "" {
		return false
	}
	for _, keyword := range longTermKeywords {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}
