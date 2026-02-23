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

	if strings.TrimSpace(m.Dir) == "" {
		return errors.New("memory dir is empty")
	}

	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return fmt.Errorf("create memory dir failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(m.Dir, ShortTermDirName), 0o755); err != nil {
		return fmt.Errorf("create short-term memory dir failed: %w", err)
	}
	if err := ensureFileExists(filepath.Join(m.Dir, LongTermFileName), 0o644); err != nil {
		return fmt.Errorf("create long-term memory file failed: %w", err)
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

func (m *Manager) BuildPrompt(userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.Dir) == "" {
		return "", errors.New("memory dir is empty")
	}

	now := m.now()
	longTermPath := filepath.Join(m.Dir, LongTermFileName)
	longText, err := readOptionalFile(longTermPath)
	if err != nil {
		return "", fmt.Errorf("read long-term memory failed: %w", err)
	}

	longText = normalizeMemoryText(longText, m.maxLongTermRunes())
	longTermPromptPath := absOrSame(longTermPath)
	shortTermName := shortTermFileName(now)
	shortTermDir := absOrSame(filepath.Join(m.Dir, ShortTermDirName))

	prompt := "---\n" +
		"记忆内容与更新规则：\n" +
		"长期记忆：\n" +
		"- 文件位置：" + longTermPromptPath + "\n- 文件内容：\n" +
		longText + "\n\n" +
		"分日期记忆：\n" +
		"- 目录位置：" + shortTermDir + "\n" +
		"- 文件命名：YYYY-MM-DD.md（例如：" + shortTermName + "）\n" +
		"- 需要历史信息时，请按日期自行检索对应文件。\n\n" +
		"按需记忆更新：\n" +
		"- 系统仅会在会话空闲超时后自动追加“空闲摘要”到分日期记忆；其余记忆更新请你自行编辑上述文件。\n" +
		"- 长期记忆内容有限，若用户未明确要求，不要将临时任务细节升级为长期偏好。\n" +
		"---\n\n" +
		"当前用户消息：\n" + userText
	logging.Debugf(
		"memory prompt assembled dir=%s long_term_file=%s short_term_dir=%s user_text=%q prompt=%q",
		m.Dir,
		longTermPromptPath,
		shortTermDir,
		userText,
		prompt,
	)

	return prompt, nil
}

func (m *Manager) SaveInteraction(userText, assistantText string, failed bool) (bool, error) {
	logging.Debugf(
		"memory save delegated to llm dir=%s changed=false user_text=%q assistant_text=%q failed=%t",
		m.Dir,
		strings.TrimSpace(userText),
		strings.TrimSpace(assistantText),
		failed,
	)
	return false, nil
}

func (m *Manager) AppendDailySummary(sessionKey, summary string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(m.Dir) == "" {
		return errors.New("memory dir is empty")
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

	dailyDir := filepath.Join(m.Dir, ShortTermDirName)
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		return fmt.Errorf("create daily memory dir failed: %w", err)
	}

	dailyPath := filepath.Join(dailyDir, shortTermFileName(at))
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
		"daily memory appended dir=%s session=%s file=%s summary=%q",
		m.Dir,
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
