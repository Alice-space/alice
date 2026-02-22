package connector

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitee.com/alicespace/alice/internal/config"
)

type codexStub struct {
	resp string
	err  error
}

func (c codexStub) Run(_ context.Context, _ string) (string, error) {
	return c.resp, c.err
}

type codexStreamingStub struct {
	resp          string
	err           error
	agentMessages []string
}

func (c codexStreamingStub) Run(_ context.Context, _ string) (string, error) {
	return c.resp, c.err
}

func (c codexStreamingStub) RunWithProgress(
	_ context.Context,
	_ string,
	onThinking func(step string),
) (string, error) {
	for _, step := range c.agentMessages {
		onThinking(step)
	}
	return c.resp, c.err
}

type codexCaptureStub struct {
	resp      string
	err       error
	lastInput string
}

func (c *codexCaptureStub) Run(_ context.Context, input string) (string, error) {
	c.lastInput = input
	return c.resp, c.err
}

type codexResumableCaptureStub struct {
	respByCall   []string
	threadByCall []string

	receivedThreadIDs []string
	receivedInputs    []string
}

func (c *codexResumableCaptureStub) Run(_ context.Context, input string) (string, error) {
	c.receivedInputs = append(c.receivedInputs, input)
	return c.responseForCall(len(c.receivedInputs) - 1), nil
}

func (c *codexResumableCaptureStub) RunWithThread(
	_ context.Context,
	threadID string,
	input string,
) (string, string, error) {
	c.receivedThreadIDs = append(c.receivedThreadIDs, threadID)
	c.receivedInputs = append(c.receivedInputs, input)
	idx := len(c.receivedInputs) - 1
	return c.responseForCall(idx), c.threadForCall(idx), nil
}

func (c *codexResumableCaptureStub) responseForCall(idx int) string {
	if idx >= 0 && idx < len(c.respByCall) {
		return c.respByCall[idx]
	}
	return "ok"
}

func (c *codexResumableCaptureStub) threadForCall(idx int) string {
	if idx >= 0 && idx < len(c.threadByCall) {
		return c.threadByCall[idx]
	}
	return ""
}

type blockingResumableCodexStub struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func newBlockingResumableCodexStub() *blockingResumableCodexStub {
	return &blockingResumableCodexStub{
		release: make(chan struct{}),
	}
}

func (c *blockingResumableCodexStub) Run(_ context.Context, _ string) (string, error) {
	return "- summary", nil
}

func (c *blockingResumableCodexStub) RunWithThread(
	ctx context.Context,
	threadID string,
	_ string,
) (string, string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return "", threadID, ctx.Err()
	case <-c.release:
		return "- summary", threadID, nil
	}
}

func (c *blockingResumableCodexStub) Release() {
	select {
	case <-c.release:
		return
	default:
		close(c.release)
	}
}

func (c *blockingResumableCodexStub) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

type memoryStub struct {
	prompt string

	buildCalls     int
	lastBuildInput string

	saveCalls      int
	lastSaveUser   string
	lastSaveReply  string
	lastSaveFailed bool

	dailySummaryCalls  int
	lastSummarySession string
	lastSummaryText    string
	lastSummaryAt      time.Time
	appendSummaryErr   error
}

func (m *memoryStub) BuildPrompt(userText string) (string, error) {
	m.buildCalls++
	m.lastBuildInput = userText
	return m.prompt, nil
}

func (m *memoryStub) SaveInteraction(userText, assistantText string, failed bool) (bool, error) {
	m.saveCalls++
	m.lastSaveUser = userText
	m.lastSaveReply = assistantText
	m.lastSaveFailed = failed
	return true, nil
}

func (m *memoryStub) AppendDailySummary(sessionKey, summary string, at time.Time) error {
	m.dailySummaryCalls++
	m.lastSummarySession = sessionKey
	m.lastSummaryText = summary
	m.lastSummaryAt = at
	return m.appendSummaryErr
}

type senderStub struct {
	sendCalls      int
	lastSendText   string
	replyTextCalls int
	lastReplyText  string
	replyTexts     []string
	replyTargets   []string
	replyRichCalls int
	lastReplyRich  []string
	replyRichLines [][]string

	replyRichMarkdownCalls int
	lastReplyMarkdown      string
	replyMarkdownTexts     []string

	replyCardCalls  int
	lastReplyCard   string
	patchCardCalls  int
	lastPatchedCard string
	patchCardErr    error

	getMessageTextCalls int
	getMessageTextErr   error
	messageTextByID     map[string]string

	downloadCalls            int
	downloadSourceMessageIDs []string
	downloadPathByKey        map[string]string
	downloadErrByKey         map[string]error
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.sendCalls++
	s.lastSendText = text
	return nil
}

func (s *senderStub) ReplyText(_ context.Context, sourceMessageID string, text string) (string, error) {
	s.replyTextCalls++
	s.lastReplyText = text
	s.replyTexts = append(s.replyTexts, text)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	return "om_reply_text", nil
}

func (s *senderStub) ReplyRichText(_ context.Context, sourceMessageID string, lines []string) (string, error) {
	s.replyRichCalls++
	cloned := append([]string(nil), lines...)
	s.lastReplyRich = cloned
	s.replyRichLines = append(s.replyRichLines, cloned)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	return "om_reply_rich", nil
}

func (s *senderStub) ReplyRichTextMarkdown(_ context.Context, sourceMessageID, markdown string) (string, error) {
	s.replyRichMarkdownCalls++
	s.lastReplyMarkdown = markdown
	s.replyMarkdownTexts = append(s.replyMarkdownTexts, markdown)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	return "om_reply_rich_markdown", nil
}

func (s *senderStub) ReplyCard(_ context.Context, _ string, cardContent string) (string, error) {
	s.replyCardCalls++
	s.lastReplyCard = cardContent
	return "om_reply_card", nil
}

func (s *senderStub) PatchCard(_ context.Context, _ string, cardContent string) error {
	s.patchCardCalls++
	s.lastPatchedCard = cardContent
	return s.patchCardErr
}

func (s *senderStub) GetMessageText(_ context.Context, messageID string) (string, error) {
	s.getMessageTextCalls++
	if s.getMessageTextErr != nil {
		return "", s.getMessageTextErr
	}
	if s.messageTextByID == nil {
		return "", nil
	}
	return s.messageTextByID[messageID], nil
}

func (s *senderStub) DownloadAttachment(_ context.Context, sourceMessageID string, attachment *Attachment) error {
	s.downloadCalls++
	s.downloadSourceMessageIDs = append(s.downloadSourceMessageIDs, strings.TrimSpace(sourceMessageID))
	if attachment == nil {
		return errors.New("attachment is nil")
	}

	key := strings.TrimSpace(attachment.ImageKey)
	if key == "" {
		key = strings.TrimSpace(attachment.FileKey)
	}
	if s.downloadErrByKey != nil {
		if err, ok := s.downloadErrByKey[key]; ok && err != nil {
			return err
		}
	}

	localPath := ""
	if s.downloadPathByKey != nil {
		localPath = strings.TrimSpace(s.downloadPathByKey[key])
	}
	if localPath == "" {
		localPath = filepath.Join("/tmp", sanitizePathToken(key))
	}
	attachment.LocalPath = localPath
	if strings.TrimSpace(attachment.FileName) == "" {
		attachment.FileName = filepath.Base(localPath)
	}
	return nil
}

func strPtr(s string) *string { return &s }

func configForTest() config.Config {
	return config.Config{
		QueueCapacity:     8,
		WorkerConcurrency: 1,
	}
}
