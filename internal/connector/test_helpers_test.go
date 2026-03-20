package connector

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/memory"
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
	lastEnv   map[string]string
	lastReq   llm.RunRequest
}

func (c *codexCaptureStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	c.lastReq = req
	c.lastInput = req.UserText
	if len(req.Env) == 0 {
		c.lastReq.Env = nil
		c.lastEnv = nil
	} else {
		c.lastReq.Env = make(map[string]string, len(req.Env))
		c.lastEnv = make(map[string]string, len(req.Env))
		for key, value := range req.Env {
			c.lastReq.Env[key] = value
			c.lastEnv[key] = value
		}
	}
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

type interruptibleResumableCodexStub struct {
	mu sync.Mutex

	started       chan struct{}
	firstCallDone chan struct{}

	callCount    int
	threadByCall []string
	inputByCall  []string
}

func newInterruptibleResumableCodexStub() *interruptibleResumableCodexStub {
	return &interruptibleResumableCodexStub{
		started:       make(chan struct{}, 8),
		firstCallDone: make(chan struct{}),
	}
}

func (c *interruptibleResumableCodexStub) Run(
	ctx context.Context,
	req llm.RunRequest,
) (llm.RunResult, error) {
	c.mu.Lock()
	callIndex := c.callCount
	c.callCount++
	c.threadByCall = append(c.threadByCall, strings.TrimSpace(req.ThreadID))
	c.inputByCall = append(c.inputByCall, req.UserText)
	c.mu.Unlock()

	select {
	case c.started <- struct{}{}:
	default:
	}

	if callIndex == 0 {
		select {
		case <-ctx.Done():
			close(c.firstCallDone)
			return llm.RunResult{NextThreadID: "thread_after_interrupt"}, ctx.Err()
		case <-c.firstCallDone:
			return llm.RunResult{Reply: "unexpected release"}, nil
		}
	}

	return llm.RunResult{
		Reply:        "latest answer",
		NextThreadID: strings.TrimSpace(req.ThreadID),
	}, nil
}

func (c *interruptibleResumableCodexStub) WaitForCall(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-c.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for codex call %d", i+1)
		}
	}
}

func (c *interruptibleResumableCodexStub) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

func (c *interruptibleResumableCodexStub) ThreadIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.threadByCall...)
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
	lastBuildScope string
	lastBuildInput string

	saveCalls      int
	lastSaveScope  string
	lastSaveUser   string
	lastSaveReply  string
	lastSaveFailed bool

	dailySummaryCalls  int
	lastSummaryScope   string
	lastSummarySession string
	lastSummaryText    string
	lastSummaryAt      time.Time
	appendSummaryErr   error
}

func (m *memoryStub) BuildPrompt(memoryScopeKey, userText string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildCalls++
	m.lastBuildScope = memoryScopeKey
	m.lastBuildInput = userText
	return m.prompt, nil
}

func (m *memoryStub) SaveInteraction(memoryScopeKey, userText, assistantText string, failed bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	m.lastSaveScope = memoryScopeKey
	m.lastSaveUser = userText
	m.lastSaveReply = assistantText
	m.lastSaveFailed = failed
	return true, nil
}

func (m *memoryStub) AppendDailySummary(memoryScopeKey, sessionKey, summary string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dailySummaryCalls++
	m.lastSummaryScope = memoryScopeKey
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

func (m *memoryStub) LastSummaryScope() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSummaryScope
}

type senderStub struct {
	mu sync.Mutex

	sendCalls       int
	lastSendText    string
	sendImages      []string
	sendImageCalls  int
	sendFiles       []string
	sendFileCalls   int
	uploadImageErr  error
	uploadFileErr   error
	imageKeyByPath  map[string]string
	fileKeyByPath   map[string]string
	sendCardCalls   int
	lastSendCard    string
	sendCards       []string
	sendCardErr     error
	reactionCalls   int
	reactionErr     error
	reactionTypes   []string
	reactionTargets []string
	replyTextCalls  int
	lastReplyText   string
	replyTexts      []string
	replyTargets    []string
	replyTextErr    error
	replyRichCalls  int
	lastReplyRich   []string
	replyRichLines  [][]string

	replyRichMarkdownCalls int
	lastReplyMarkdown      string
	replyMarkdownTexts     []string
	replyRichMarkdownErr   error

	replyCardCalls int
	lastReplyCard  string
	replyCards     []string
	replyCardErr   error

	getMessageTextCalls int
	getMessageTextErr   error
	messageTextByID     map[string]string

	downloadCalls            int
	downloadScopeKeys        []string
	downloadSourceMessageIDs []string
	downloadPathByKey        map[string]string
	downloadErrByKey         map[string]error

	resourceRoot string

	resolveUserNameCalls int
	resolveUserNameErr   error
	userNameByIdentity   map[string]string

	resolveChatMemberNameCalls int
	resolveChatMemberNameErr   error
	chatMemberNameByIdentity   map[string]string
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	s.lastSendText = text
	return nil
}

func (s *senderStub) SendImage(_ context.Context, _, _ string, imageKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendImageCalls++
	s.sendImages = append(s.sendImages, strings.TrimSpace(imageKey))
	return nil
}

func (s *senderStub) SendFile(_ context.Context, _, _ string, fileKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendFileCalls++
	s.sendFiles = append(s.sendFiles, strings.TrimSpace(fileKey))
	return nil
}

func (s *senderStub) UploadImage(_ context.Context, localPath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadImageErr != nil {
		return "", s.uploadImageErr
	}
	key := ""
	if s.imageKeyByPath != nil {
		key = strings.TrimSpace(s.imageKeyByPath[strings.TrimSpace(localPath)])
	}
	if key == "" {
		key = "img_uploaded"
	}
	return key, nil
}

func (s *senderStub) UploadFile(_ context.Context, localPath, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadFileErr != nil {
		return "", s.uploadFileErr
	}
	key := ""
	if s.fileKeyByPath != nil {
		key = strings.TrimSpace(s.fileKeyByPath[strings.TrimSpace(localPath)])
	}
	if key == "" {
		key = "file_uploaded"
	}
	return key, nil
}

func (s *senderStub) ResourceRootForScope(memoryScopeKey string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := strings.TrimSpace(s.resourceRoot)
	if root == "" {
		return ""
	}
	return memory.ResolveScopedResourceRoot(root, memoryScopeKey)
}

func (s *senderStub) SendCard(_ context.Context, _, _ string, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastSendCard = cardContent
	s.sendCards = append(s.sendCards, cardContent)
	return s.sendCardErr
}

func (s *senderStub) AddReaction(_ context.Context, messageID, emojiType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reactionCalls++
	s.reactionTargets = append(s.reactionTargets, strings.TrimSpace(messageID))
	s.reactionTypes = append(s.reactionTypes, strings.ToUpper(strings.TrimSpace(emojiType)))
	return s.reactionErr
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

func (s *senderStub) ReplyCard(_ context.Context, sourceMessageID string, cardContent string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyCardCalls++
	s.lastReplyCard = cardContent
	s.replyCards = append(s.replyCards, cardContent)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyCardErr != nil {
		return "", s.replyCardErr
	}
	return "om_reply_card", nil
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

func (s *senderStub) ResolveChatMemberName(_ context.Context, chatID, openID, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveChatMemberNameCalls++
	if s.resolveChatMemberNameErr != nil {
		return "", s.resolveChatMemberNameErr
	}
	if s.chatMemberNameByIdentity == nil {
		return "", nil
	}

	chatKey := "chat_id:" + strings.TrimSpace(chatID) + "|"
	openKey := chatKey + "open_id:" + strings.TrimSpace(openID)
	if name, ok := s.chatMemberNameByIdentity[openKey]; ok {
		return name, nil
	}
	userKey := chatKey + "user_id:" + strings.TrimSpace(userID)
	if name, ok := s.chatMemberNameByIdentity[userKey]; ok {
		return name, nil
	}
	return "", nil
}

func (s *senderStub) DownloadAttachment(_ context.Context, memoryScopeKey, sourceMessageID string, attachment *Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloadCalls++
	s.downloadScopeKeys = append(s.downloadScopeKeys, strings.TrimSpace(memoryScopeKey))
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
