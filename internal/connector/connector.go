package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/logging"
)

var mentionPattern = regexp.MustCompile(`<at[^>]*>.*?</at>`)

var ErrIgnoreMessage = errors.New("ignore message")

type CodexRunner interface {
	Run(ctx context.Context, userText string) (string, error)
}

type StreamingCodexRunner interface {
	RunWithProgress(ctx context.Context, userText string, onThinking func(step string)) (string, error)
}

type MemoryManager interface {
	BuildPrompt(userText string) (string, error)
	SaveInteraction(userText, assistantText string, failed bool) error
}

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
	ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error)
	PatchCard(ctx context.Context, messageID, cardContent string) error
}

type Job struct {
	ReceiveID       string
	ReceiveIDType   string
	SourceMessageID string
	Text            string
	EventID         string
	ReceivedAt      time.Time
	SessionKey      string
	SessionVersion  uint64
}

type Processor struct {
	codex           CodexRunner
	memory          MemoryManager
	sender          Sender
	failureMessage  string
	thinkingMessage string
}

const interruptedReplyMessage = "已收到你的新消息，当前回复已中断并切换到最新输入。"

func NewProcessor(
	codexRunner CodexRunner,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	return NewProcessorWithMemory(codexRunner, sender, failureMessage, thinkingMessage, nil)
}

func NewProcessorWithMemory(
	codexRunner CodexRunner,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
	memoryManager MemoryManager,
) *Processor {
	return &Processor{
		codex:           codexRunner,
		memory:          memoryManager,
		sender:          sender,
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
	}
}

func (p *Processor) ProcessJob(ctx context.Context, job Job) {
	logging.Debugf(
		"process job start event_id=%s receive_id_type=%s receive_id=%s source_message_id=%s text=%q",
		job.EventID,
		job.ReceiveIDType,
		job.ReceiveID,
		job.SourceMessageID,
		job.Text,
	)
	if strings.TrimSpace(job.SourceMessageID) != "" {
		p.processReplyCard(ctx, job)
		return
	}

	promptText := p.buildPromptWithMemory(job)
	reply, err := p.runCodex(ctx, promptText, nil)
	if errors.Is(err, context.Canceled) {
		log.Printf("codex canceled event_id=%s", job.EventID)
		logging.Debugf("memory update skipped event_id=%s changed=false reason=codex_canceled", job.EventID)
		return
	}
	failed := err != nil
	if err != nil {
		log.Printf("codex failed event_id=%s: %v", job.EventID, err)
		reply = p.failureMessage
	}
	p.recordInteraction(job, reply, failed)

	if sendErr := p.sender.SendText(ctx, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		log.Printf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
}

func (p *Processor) processReplyCard(ctx context.Context, job Job) {
	startedAt := time.Now()
	thinkingParts := make([]string, 0, 8)
	initialThinking := strings.TrimSpace(p.thinkingMessage)
	if initialThinking == "" {
		initialThinking = "思考中..."
	}
	cardContent := buildProgressCardContent(initialThinking, "", false, false, 0)

	cardMessageID, err := p.sender.ReplyCard(ctx, job.SourceMessageID, cardContent)
	if err != nil {
		log.Printf("send card reply failed event_id=%s: %v", job.EventID, err)
		cardMessageID = ""
	}
	lastPatchTime := time.Time{}

	onThinking := func(step string) {
		normalized := normalizeReasoning(step)
		if normalized == "" {
			return
		}
		if len(thinkingParts) > 0 && thinkingParts[len(thinkingParts)-1] == normalized {
			return
		}
		thinkingParts = append(thinkingParts, normalized)
		thinkingText := strings.Join(thinkingParts, "\n")

		if cardMessageID == "" {
			return
		}
		// Feishu patch API recommends per-message frequency control; throttle incremental sync.
		if time.Since(lastPatchTime) < 350*time.Millisecond {
			return
		}
		progressCard := buildProgressCardContent(thinkingText, "", false, false, time.Since(startedAt))
		if patchErr := p.sender.PatchCard(ctx, cardMessageID, progressCard); patchErr != nil {
			log.Printf("patch card failed event_id=%s: %v", job.EventID, patchErr)
			return
		}
		lastPatchTime = time.Now()
	}

	promptText := p.buildPromptWithMemory(job)
	finalReply, runErr := p.runCodex(ctx, promptText, onThinking)
	if errors.Is(runErr, context.Canceled) {
		// Parent context cancellation usually means app shutdown.
		if ctx.Err() != nil {
			logging.Debugf("memory update skipped event_id=%s changed=false reason=context_canceled", job.EventID)
			return
		}

		finalThinking := strings.Join(thinkingParts, "\n")
		elapsed := time.Since(startedAt)
		if cardMessageID != "" {
			interruptedCard := buildProgressCardContent(finalThinking, interruptedReplyMessage, false, true, elapsed)
			if patchErr := p.sender.PatchCard(ctx, cardMessageID, interruptedCard); patchErr == nil {
				logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
				return
			}
		}
		if _, replyErr := p.sender.ReplyText(ctx, job.SourceMessageID, interruptedReplyMessage); replyErr != nil {
			log.Printf("fallback interrupted reply failed event_id=%s: %v", job.EventID, replyErr)
		}
		logging.Debugf("memory update skipped event_id=%s changed=false reason=job_interrupted", job.EventID)
		return
	}
	failed := runErr != nil
	if failed {
		log.Printf("codex failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.failureMessage
	}
	p.recordInteraction(job, finalReply, failed)

	finalThinking := strings.Join(thinkingParts, "\n")
	elapsed := time.Since(startedAt)

	if cardMessageID != "" {
		finalCard := buildProgressCardContent(finalThinking, finalReply, failed, false, elapsed)
		if patchErr := p.sender.PatchCard(ctx, cardMessageID, finalCard); patchErr == nil {
			return
		}
	}

	if _, replyErr := p.sender.ReplyText(ctx, job.SourceMessageID, finalReply); replyErr != nil {
		log.Printf("fallback reply text failed event_id=%s: %v", job.EventID, replyErr)
	}
}

func (p *Processor) runCodex(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	if runner, ok := p.codex.(StreamingCodexRunner); ok {
		return runner.RunWithProgress(ctx, userText, onThinking)
	}
	return p.codex.Run(ctx, userText)
}

func (p *Processor) buildPromptWithMemory(job Job) string {
	logging.Debugf("prompt assemble start event_id=%s memory_enabled=%t user_text=%q", job.EventID, p.memory != nil, job.Text)
	if p.memory == nil {
		logging.Debugf("prompt assemble event_id=%s strategy=direct final_prompt=%q", job.EventID, job.Text)
		return job.Text
	}

	prompt, err := p.memory.BuildPrompt(job.Text)
	if err != nil {
		log.Printf("build memory prompt failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("prompt assemble fallback event_id=%s strategy=direct reason=%v final_prompt=%q", job.EventID, err, job.Text)
		return job.Text
	}
	logging.Debugf("prompt assemble event_id=%s strategy=memory final_prompt=%q", job.EventID, prompt)
	return prompt
}

func (p *Processor) recordInteraction(job Job, reply string, failed bool) {
	if p.memory == nil {
		logging.Debugf("memory update skipped event_id=%s changed=false reason=no_memory_manager", job.EventID)
		return
	}
	if err := p.memory.SaveInteraction(job.Text, reply, failed); err != nil {
		log.Printf("save memory failed event_id=%s: %v", job.EventID, err)
		logging.Debugf("memory update result event_id=%s changed=unknown error=%v", job.EventID, err)
		return
	}
	logging.Debugf("memory update result event_id=%s changed=true failed=%t", job.EventID, failed)
}

type App struct {
	cfg       config.Config
	queue     chan Job
	processor *Processor
	mu        sync.Mutex
	latest    map[string]uint64
	active    map[string]activeSession
}

type activeSession struct {
	version uint64
	cancel  context.CancelFunc
	eventID string
}

func NewApp(cfg config.Config, processor *Processor) *App {
	return &App{
		cfg:       cfg,
		queue:     make(chan Job, cfg.QueueCapacity),
		processor: processor,
		latest:    make(map[string]uint64),
		active:    make(map[string]activeSession),
	}
}

func (a *App) Run(ctx context.Context) error {
	for i := 0; i < a.cfg.WorkerConcurrency; i++ {
		go a.workerLoop(ctx, i)
	}

	eventHandler := larkdispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(a.onMessageReceive)
	wsClient := larkws.NewClient(
		a.cfg.FeishuAppID,
		a.cfg.FeishuAppSecret,
		larkws.WithDomain(a.cfg.FeishuBaseURL),
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(parseLogLevel(a.cfg.LogLevel)),
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- wsClient.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("ws client stopped: %w", err)
		}
		return nil
	}
}

func (a *App) workerLoop(ctx context.Context, idx int) {
	log.Printf("worker started id=%d", idx)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			if !a.shouldProcessJob(job) {
				log.Printf("drop stale job event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				continue
			}

			jobCtx, cancel := context.WithCancel(ctx)
			if !a.markSessionActive(job, cancel) {
				cancel()
				log.Printf("skip stale job before run event_id=%s session=%s version=%d", job.EventID, job.SessionKey, job.SessionVersion)
				continue
			}

			a.processor.ProcessJob(jobCtx, job)
			cancel()
			a.clearSessionActive(job)
		}
	}
}

func (a *App) onMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	logIncomingEventDebug(event)

	job, err := BuildJob(event)
	if err != nil {
		if errors.Is(err, ErrIgnoreMessage) {
			logging.Debugf("incoming message ignored source=feishu_im event_id=%s", eventID(event))
			return nil
		}
		log.Printf("build job failed: %v", err)
		logging.Debugf("incoming message rejected source=feishu_im event_id=%s err=%v", eventID(event), err)
		return nil
	}

	queued, cancelActive, canceledEventID := a.enqueueJob(job)
	if !queued {
		log.Printf("queue full, drop event_id=%s", job.EventID)
		return nil
	}
	if cancelActive != nil {
		cancelActive()
		log.Printf("steer active job canceled old_event_id=%s new_event_id=%s session=%s", canceledEventID, job.EventID, job.SessionKey)
	}
	log.Printf(
		"job queued event_id=%s receive_id_type=%s session=%s version=%d",
		job.EventID,
		job.ReceiveIDType,
		job.SessionKey,
		job.SessionVersion,
	)
	logging.Debugf(
		"job accepted event_id=%s channel=%s receive_id_type=%s receive_id=%s source_message_id=%s normalized_text=%q",
		job.EventID,
		"feishu_im",
		job.ReceiveIDType,
		job.ReceiveID,
		job.SourceMessageID,
		job.Text,
	)

	return nil
}

func (a *App) enqueueJob(job *Job) (queued bool, cancelActive context.CancelFunc, canceledEventID string) {
	if job == nil {
		return false, nil, ""
	}

	if strings.TrimSpace(job.SessionKey) == "" {
		job.SessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	nextVersion := a.latest[job.SessionKey] + 1
	job.SessionVersion = nextVersion

	select {
	case a.queue <- *job:
		a.latest[job.SessionKey] = nextVersion
		if active, ok := a.active[job.SessionKey]; ok {
			cancelActive = active.cancel
			canceledEventID = active.eventID
		}
		return true, cancelActive, canceledEventID
	default:
		return false, nil, ""
	}
}

func (a *App) shouldProcessJob(job Job) bool {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" || job.SessionVersion == 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.latest[sessionKey] == job.SessionVersion
}

func (a *App) markSessionActive(job Job, cancel context.CancelFunc) bool {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" || job.SessionVersion == 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.latest[sessionKey] != job.SessionVersion {
		return false
	}
	a.active[sessionKey] = activeSession{
		version: job.SessionVersion,
		cancel:  cancel,
		eventID: job.EventID,
	}
	return true
}

func (a *App) clearSessionActive(job Job) {
	sessionKey := strings.TrimSpace(job.SessionKey)
	if sessionKey == "" {
		sessionKey = buildSessionKey(job.ReceiveIDType, job.ReceiveID)
	}
	if sessionKey == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	active, ok := a.active[sessionKey]
	if !ok {
		return
	}
	if active.version == job.SessionVersion {
		delete(a.active, sessionKey)
	}
}

func BuildJob(event *larkim.P2MessageReceiveV1) (*Job, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil, ErrIgnoreMessage
	}

	message := event.Event.Message
	if strings.ToLower(deref(message.MessageType)) != "text" {
		return nil, ErrIgnoreMessage
	}

	text, err := extractText(message.Content)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, ErrIgnoreMessage
	}

	receiveID := strings.TrimSpace(deref(message.ChatId))
	receiveIDType := "chat_id"
	if receiveID == "" {
		receiveID = strings.TrimSpace(extractOpenID(event))
		receiveIDType = "open_id"
	}
	if receiveID == "" {
		return nil, errors.New("missing receive target")
	}

	return &Job{
		ReceiveID:       receiveID,
		ReceiveIDType:   receiveIDType,
		SourceMessageID: strings.TrimSpace(deref(message.MessageId)),
		Text:            text,
		EventID:         eventID(event),
		ReceivedAt:      time.Now(),
		SessionKey:      buildSessionKey(receiveIDType, receiveID),
	}, nil
}

func logIncomingEventDebug(event *larkim.P2MessageReceiveV1) {
	if !logging.IsDebugEnabled() {
		return
	}
	if event == nil || event.Event == nil || event.Event.Message == nil {
		logging.Debugf("incoming message source=feishu_im event=<nil>")
		return
	}

	message := event.Event.Message
	logging.Debugf(
		"incoming message source=feishu_im event_id=%s message_id=%s message_type=%s chat_id=%s raw_content=%s",
		eventID(event),
		strings.TrimSpace(deref(message.MessageId)),
		strings.TrimSpace(deref(message.MessageType)),
		strings.TrimSpace(deref(message.ChatId)),
		deref(message.Content),
	)
}

func buildSessionKey(receiveIDType, receiveID string) string {
	idType := strings.TrimSpace(receiveIDType)
	if idType == "" {
		idType = "unknown"
	}

	id := strings.TrimSpace(receiveID)
	if id == "" {
		return ""
	}
	return idType + ":" + id
}

func extractText(content *string) (string, error) {
	if strings.TrimSpace(deref(content)) == "" {
		return "", ErrIgnoreMessage
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(deref(content)), &payload); err != nil {
		return "", fmt.Errorf("invalid text content json: %w", err)
	}

	text := mentionPattern.ReplaceAllString(payload.Text, "")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrIgnoreMessage
	}
	return text, nil
}

func extractOpenID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return deref(event.Event.Sender.SenderId.OpenId)
}

func eventID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil {
		return ""
	}
	return event.EventV2Base.Header.EventID
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parseLogLevel(level string) larkcore.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return larkcore.LogLevelDebug
	case "warn", "warning":
		return larkcore.LogLevelWarn
	case "error":
		return larkcore.LogLevelError
	default:
		return larkcore.LogLevelInfo
	}
}

func normalizeReasoning(step string) string {
	step = strings.TrimSpace(step)
	step = strings.Trim(step, "*")
	step = strings.TrimSpace(step)
	if step == "" {
		return ""
	}
	return clipText(step, 600)
}

func buildProgressCardContent(thinkingText, answerText string, failed bool, interrupted bool, elapsed time.Duration) string {
	status := "思考中"
	if interrupted {
		status = "已中断"
	} else if failed {
		status = "失败"
	} else if strings.TrimSpace(answerText) != "" {
		status = "已完成"
	}

	thinking := clipText(strings.TrimSpace(thinkingText), 4000)
	answer := clipText(strings.TrimSpace(answerText), 4000)
	if thinking == "" {
		thinking = "（暂无）"
	}
	durationLabel := "已思考：" + formatElapsed(elapsed)
	if interrupted || failed || strings.TrimSpace(answer) != "" {
		durationLabel = "总耗时：" + formatElapsed(elapsed)
	}

	elements := []any{
		cardMarkdown("**状态**：" + status + "（" + durationLabel + "）"),
		cardMarkdown("**Codex 思考**\n" + thinking),
	}
	if strings.TrimSpace(answer) != "" {
		elements = append(elements, cardMarkdown("**回复**\n"+answer))
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	raw, _ := json.Marshal(card)
	return string(raw)
}

func cardMarkdown(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func clipText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d / time.Minute)
		seconds := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}

	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
}

type LarkSender struct {
	client *lark.Client
}

func NewLarkSender(client *lark.Client) *LarkSender {
	return &LarkSender{client: client}
}

func (s *LarkSender) SendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	content := textMessageContent(text)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(content).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	return nil
}

func (s *LarkSender) ReplyText(ctx context.Context, sourceMessageID, text string) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(textMessageContent(text)).
			ReplyInThread(false).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", errors.New("reply success but response message_id is empty")
	}
	return strings.TrimSpace(*resp.Data.MessageId), nil
}

func (s *LarkSender) ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(cardContent).
			ReplyInThread(false).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", errors.New("reply card success but response message_id is empty")
	}
	return strings.TrimSpace(*resp.Data.MessageId), nil
}

func (s *LarkSender) PatchCard(ctx context.Context, messageID, cardContent string) error {
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardContent).
			Build()).
		Build()

	resp, err := s.client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("feishu api error code=%d msg=%s request_id=%s", resp.Code, resp.Msg, resp.RequestId())
	}
	return nil
}

func textMessageContent(text string) string {
	contentBytes, _ := json.Marshal(map[string]string{"text": text})
	return string(contentBytes)
}
