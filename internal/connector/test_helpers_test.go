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
	"gitee.com/alicespace/alice/internal/llm"
)

type codexStub struct {
	resp string
	err  error
}

func (c codexStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	return llm.RunResult{Reply: c.resp}, c.err
}

type codexStreamingStub struct {
	resp          string
	err           error
	agentMessages []string
}

func (c codexStreamingStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	if req.OnProgress != nil {
		for _, step := range c.agentMessages {
			req.OnProgress(step)
		}
	}
	return llm.RunResult{Reply: c.resp}, c.err
}

type codexCaptureStub struct {
	resp      string
	err       error
	lastInput string
}

func (c *codexCaptureStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	c.lastInput = req.UserText
	return llm.RunResult{Reply: c.resp}, c.err
}

type codexResumableCaptureStub struct {
	respByCall   []string
	threadByCall []string

	receivedThreadIDs []string
	receivedInputs    []string
}

func (c *codexResumableCaptureStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	c.receivedThreadIDs = append(c.receivedThreadIDs, req.ThreadID)
	c.receivedInputs = append(c.receivedInputs, req.UserText)
	idx := len(c.receivedInputs) - 1
	return llm.RunResult{
		Reply:        c.responseForCall(idx),
		NextThreadID: c.threadForCall(idx),
	}, nil
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

func (c *blockingResumableCodexStub) Run(
	ctx context.Context,
	req llm.RunRequest,
) (llm.RunResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return llm.RunResult{}, ctx.Err()
	case <-c.release:
		return llm.RunResult{
			Reply:        "- summary",
			NextThreadID: req.ThreadID,
		}, nil
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
	mu sync.Mutex

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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildCalls++
	m.lastBuildInput = userText
	return m.prompt, nil
}

func (m *memoryStub) SaveInteraction(userText, assistantText string, failed bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	m.lastSaveUser = userText
	m.lastSaveReply = assistantText
	m.lastSaveFailed = failed
	return true, nil
}

func (m *memoryStub) AppendDailySummary(sessionKey, summary string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dailySummaryCalls++
	m.lastSummarySession = sessionKey
	m.lastSummaryText = summary
	m.lastSummaryAt = at
	return m.appendSummaryErr
}

func (m *memoryStub) DailySummaryCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dailySummaryCalls
}

func (m *memoryStub) LastSummarySession() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSummarySession
}

type senderStub struct {
	mu sync.Mutex

	sendCalls      int
	lastSendText   string
	sendCardCalls  int
	lastSendCard   string
	sendCards      []string
	sendCardErr    error
	replyTextCalls int
	lastReplyText  string
	replyTexts     []string
	replyTargets   []string
	replyTextErr   error
	replyRichCalls int
	lastReplyRich  []string
	replyRichLines [][]string

	replyRichMarkdownCalls int
	lastReplyMarkdown      string
	replyMarkdownTexts     []string
	replyRichMarkdownErr   error

	replyCardCalls  int
	lastReplyCard   string
	replyCards      []string
	replyCardErr    error
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

	resolveUserNameCalls int
	resolveUserNameErr   error
	userNameByIdentity   map[string]string
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	s.lastSendText = text
	return nil
}

func (s *senderStub) SendCard(_ context.Context, _, _ string, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastSendCard = cardContent
	s.sendCards = append(s.sendCards, cardContent)
	return s.sendCardErr
}

func (s *senderStub) ReplyText(_ context.Context, sourceMessageID string, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyTextCalls++
	s.lastReplyText = text
	s.replyTexts = append(s.replyTexts, text)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyTextErr != nil {
		return "", s.replyTextErr
	}
	return "om_reply_text", nil
}

func (s *senderStub) ReplyRichText(_ context.Context, sourceMessageID string, lines []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyRichCalls++
	cloned := append([]string(nil), lines...)
	s.lastReplyRich = cloned
	s.replyRichLines = append(s.replyRichLines, cloned)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	return "om_reply_rich", nil
}

func (s *senderStub) ReplyRichTextMarkdown(_ context.Context, sourceMessageID, markdown string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyRichMarkdownCalls++
	s.lastReplyMarkdown = markdown
	s.replyMarkdownTexts = append(s.replyMarkdownTexts, markdown)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyRichMarkdownErr != nil {
		return "", s.replyRichMarkdownErr
	}
	return "om_reply_rich_markdown", nil
}

func (s *senderStub) ReplyCard(_ context.Context, _ string, cardContent string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyCardCalls++
	s.lastReplyCard = cardContent
	s.replyCards = append(s.replyCards, cardContent)
	if s.replyCardErr != nil {
		return "", s.replyCardErr
	}
	return "om_reply_card", nil
}

func (s *senderStub) PatchCard(_ context.Context, _ string, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patchCardCalls++
	s.lastPatchedCard = cardContent
	return s.patchCardErr
}

func (s *senderStub) GetMessageText(_ context.Context, messageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getMessageTextCalls++
	if s.getMessageTextErr != nil {
		return "", s.getMessageTextErr
	}
	if s.messageTextByID == nil {
		return "", nil
	}
	return s.messageTextByID[messageID], nil
}

func (s *senderStub) ResolveUserName(_ context.Context, openID, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveUserNameCalls++
	if s.resolveUserNameErr != nil {
		return "", s.resolveUserNameErr
	}
	if s.userNameByIdentity == nil {
		return "", nil
	}

	openKey := "open_id:" + strings.TrimSpace(openID)
	if name, ok := s.userNameByIdentity[openKey]; ok {
		return name, nil
	}
	userKey := "user_id:" + strings.TrimSpace(userID)
	if name, ok := s.userNameByIdentity[userKey]; ok {
		return name, nil
	}
	return "", nil
}

func (s *senderStub) DownloadAttachment(_ context.Context, sourceMessageID string, attachment *Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *senderStub) SendCardCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendCardCalls
}

func (s *senderStub) LastSendCard() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSendCard
}

func strPtr(s string) *string { return &s }

func configForTest() config.Config {
	return config.Config{
		QueueCapacity:     8,
		WorkerConcurrency: 1,
	}
}
