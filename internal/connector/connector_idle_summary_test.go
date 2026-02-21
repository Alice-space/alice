package connector

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitee.com/alicespace/alice/internal/config"
)

func TestProcessor_SessionStatePersistAndLoad(t *testing.T) {
	statePath := t.TempDir() + "/session_state.json"
	processor := NewProcessorWithMemory(
		codexStub{},
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		&memoryStub{},
	)
	if err := processor.LoadSessionState(statePath); err != nil {
		t.Fatalf("load initial state failed: %v", err)
	}

	now := time.Date(2026, 2, 20, 18, 0, 0, 0, time.UTC)
	sessionKey := "chat_id:oc_chat"
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, now)
	processor.mu.Lock()
	state := processor.sessions[sessionKey]
	state.LastIdleSummaryAnchor = now
	processor.sessions[sessionKey] = state
	processor.markStateChangedLocked()
	processor.mu.Unlock()

	if err := processor.FlushSessionState(); err != nil {
		t.Fatalf("flush state failed: %v", err)
	}

	loaded := NewProcessorWithMemory(
		codexStub{},
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		&memoryStub{},
	)
	if err := loaded.LoadSessionState(statePath); err != nil {
		t.Fatalf("load persisted state failed: %v", err)
	}
	if got := loaded.getThreadID(sessionKey); got != "thread_1" {
		t.Fatalf("unexpected thread id after reload: %s", got)
	}

	loaded.mu.Lock()
	loadedState := loaded.sessions[sessionKey]
	loaded.mu.Unlock()
	if !loadedState.LastMessageAt.Equal(now) {
		t.Fatalf("unexpected last message time after reload: %v", loadedState.LastMessageAt)
	}
	if !loadedState.LastIdleSummaryAnchor.Equal(now) {
		t.Fatalf("unexpected idle anchor after reload: %v", loadedState.LastIdleSummaryAnchor)
	}
}

func TestProcessor_IdleSummaryOncePerIdlePeriod(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"- 关键点1", "- 关键点2"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	mem := &memoryStub{}
	processor := NewProcessorWithMemory(
		fakeCodex,
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		mem,
	)

	sessionKey := "chat_id:oc_chat"
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	now := base.Add(9 * time.Hour)
	processor.now = func() time.Time { return now }
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, base)

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return mem.dailySummaryCalls == 1
	}, "idle summary should be written once")
	if mem.lastSummarySession != sessionKey {
		t.Fatalf("unexpected summary session key: %s", mem.lastSummarySession)
	}

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	time.Sleep(120 * time.Millisecond)
	if mem.dailySummaryCalls != 1 {
		t.Fatalf("same idle period should only write once, got %d", mem.dailySummaryCalls)
	}

	newMessageAt := base.Add(10 * time.Hour)
	processor.touchSessionMessage(sessionKey, newMessageAt)
	now = newMessageAt.Add(9 * time.Hour)
	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return mem.dailySummaryCalls == 2
	}, "new idle period should trigger another summary")
}

func TestProcessor_IdleSummaryAnchorChangedSkipsWriteAndNoParallelRun(t *testing.T) {
	fakeCodex := newBlockingResumableCodexStub()
	mem := &memoryStub{}
	processor := NewProcessorWithMemory(
		fakeCodex,
		&senderStub{},
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		mem,
	)

	sessionKey := "chat_id:oc_chat"
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	processor.now = func() time.Time { return base.Add(9 * time.Hour) }
	processor.setThreadID(sessionKey, "thread_1")
	processor.touchSessionMessage(sessionKey, base)

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	waitForCondition(t, 2*time.Second, func() bool {
		return fakeCodex.CallCount() == 1
	}, "summary codex call should start")

	processor.RunIdleSummaryScan(context.Background(), 8*time.Hour)
	time.Sleep(100 * time.Millisecond)
	if fakeCodex.CallCount() != 1 {
		t.Fatalf("same session should not run multiple summary tasks in parallel, got %d", fakeCodex.CallCount())
	}

	processor.touchSessionMessage(sessionKey, base.Add(10*time.Hour))
	fakeCodex.Release()

	time.Sleep(120 * time.Millisecond)
	if mem.dailySummaryCalls != 0 {
		t.Fatalf("summary should be skipped when anchor changes, got %d writes", mem.dailySummaryCalls)
	}
}

func TestProcessor_SavesFailureFallbackToMemory(t *testing.T) {
	fakeCodex := &codexCaptureStub{err: errors.New("boom")}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}

	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if memory.saveCalls != 1 {
		t.Fatalf("expected 1 memory save, got %d", memory.saveCalls)
	}
	if !memory.lastSaveFailed {
		t.Fatal("expected failed=true when codex returns error")
	}
	if memory.lastSaveReply != "Codex 暂时不可用，请稍后重试。" {
		t.Fatalf("unexpected fallback reply saved: %s", memory.lastSaveReply)
	}
}

func TestBuildProgressCardContent_UsesCardSchemaV2BodyElements(t *testing.T) {
	content := buildProgressCardContent("思考", "答案", false, false, 1250*time.Millisecond)

	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal card content failed: %v", err)
	}

	if payload["schema"] != "2.0" {
		t.Fatalf("expected schema 2.0, got %#v", payload["schema"])
	}
	if _, exists := payload["header"]; exists {
		t.Fatalf("card should not include header title")
	}
	if _, exists := payload["elements"]; exists {
		t.Fatalf("schema 2.0 card should not use top-level elements")
	}

	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body.elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	if first["tag"] != "markdown" {
		t.Fatalf("expected markdown element, got %#v", first["tag"])
	}

	var joined strings.Builder
	for _, element := range elements {
		elementMap, ok := element.(map[string]any)
		if !ok {
			t.Fatalf("expected element object, got %#v", element)
		}
		content, _ := elementMap["content"].(string)
		joined.WriteString(content)
		joined.WriteByte('\n')
	}

	all := joined.String()
	if strings.Contains(all, "Alice 助手") {
		t.Fatalf("card should not include assistant name: %s", all)
	}
	if strings.Contains(all, "你的消息") {
		t.Fatalf("card should not include user message block: %s", all)
	}
	if strings.Contains(all, "更新时间") {
		t.Fatalf("card should not include update timestamp: %s", all)
	}
	if !strings.Contains(all, "耗时：") && !strings.Contains(all, "已思考：") {
		t.Fatalf("card should include elapsed duration: %s", all)
	}
}

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

	replyCardCalls  int
	lastReplyCard   string
	patchCardCalls  int
	lastPatchedCard string
	patchCardErr    error

	getMessageTextCalls int
	getMessageTextErr   error
	messageTextByID     map[string]string

	downloadCalls     int
	downloadPathByKey map[string]string
	downloadErrByKey  map[string]error
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

func (s *senderStub) DownloadAttachment(_ context.Context, _ string, attachment *Attachment) error {
	s.downloadCalls++
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
