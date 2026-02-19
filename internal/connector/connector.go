package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"gitee.com/alicespace/alice/internal/config"
)

var mentionPattern = regexp.MustCompile(`<at[^>]*>.*?</at>`)

var ErrIgnoreMessage = errors.New("ignore message")

type CodexRunner interface {
	Run(ctx context.Context, userText string) (string, error)
}

type StreamingCodexRunner interface {
	RunWithProgress(ctx context.Context, userText string, onThinking func(step string)) (string, error)
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
}

type Processor struct {
	codex           CodexRunner
	sender          Sender
	failureMessage  string
	thinkingMessage string
}

func NewProcessor(
	codexRunner CodexRunner,
	sender Sender,
	failureMessage string,
	thinkingMessage string,
) *Processor {
	return &Processor{
		codex:           codexRunner,
		sender:          sender,
		failureMessage:  failureMessage,
		thinkingMessage: thinkingMessage,
	}
}

func (p *Processor) ProcessJob(ctx context.Context, job Job) {
	if strings.TrimSpace(job.SourceMessageID) != "" {
		p.processReplyCard(ctx, job)
		return
	}

	reply, err := p.runCodex(ctx, job.Text, nil)
	if err != nil {
		log.Printf("codex failed event_id=%s: %v", job.EventID, err)
		reply = p.failureMessage
	}
	if sendErr := p.sender.SendText(ctx, job.ReceiveIDType, job.ReceiveID, reply); sendErr != nil {
		log.Printf("send message failed event_id=%s: %v", job.EventID, sendErr)
	}
}

func (p *Processor) processReplyCard(ctx context.Context, job Job) {
	thinkingParts := make([]string, 0, 8)
	initialThinking := strings.TrimSpace(p.thinkingMessage)
	if initialThinking == "" {
		initialThinking = "思考中..."
	}
	cardContent := buildProgressCardContent(job.Text, initialThinking, "", false)

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
		progressCard := buildProgressCardContent(job.Text, thinkingText, "", false)
		if patchErr := p.sender.PatchCard(ctx, cardMessageID, progressCard); patchErr != nil {
			log.Printf("patch card failed event_id=%s: %v", job.EventID, patchErr)
			return
		}
		lastPatchTime = time.Now()
	}

	finalReply, runErr := p.runCodex(ctx, job.Text, onThinking)
	failed := runErr != nil
	if failed {
		log.Printf("codex failed event_id=%s: %v", job.EventID, runErr)
		finalReply = p.failureMessage
	}
	finalThinking := strings.Join(thinkingParts, "\n")

	if cardMessageID != "" {
		finalCard := buildProgressCardContent(job.Text, finalThinking, finalReply, failed)
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

type App struct {
	cfg       config.Config
	queue     chan Job
	processor *Processor
}

func NewApp(cfg config.Config, processor *Processor) *App {
	return &App{
		cfg:       cfg,
		queue:     make(chan Job, cfg.QueueCapacity),
		processor: processor,
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
			a.processor.ProcessJob(ctx, job)
		}
	}
}

func (a *App) onMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	job, err := BuildJob(event)
	if err != nil {
		if errors.Is(err, ErrIgnoreMessage) {
			return nil
		}
		log.Printf("build job failed: %v", err)
		return nil
	}

	select {
	case a.queue <- *job:
		log.Printf("job queued event_id=%s receive_id_type=%s", job.EventID, job.ReceiveIDType)
	default:
		log.Printf("queue full, drop event_id=%s", job.EventID)
	}

	return nil
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
	}, nil
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

func buildProgressCardContent(userText, thinkingText, answerText string, failed bool) string {
	status := "思考中"
	if failed {
		status = "失败"
	} else if strings.TrimSpace(answerText) != "" {
		status = "已完成"
	}

	question := clipText(strings.TrimSpace(userText), 1200)
	thinking := clipText(strings.TrimSpace(thinkingText), 4000)
	answer := clipText(strings.TrimSpace(answerText), 4000)
	if thinking == "" {
		thinking = "（暂无）"
	}
	if answer == "" {
		answer = "（等待中）"
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"enable_forward": true,
			"update_multi":   true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": "Alice 助手",
			},
		},
		"body": map[string]any{
			"elements": []any{
				cardMarkdown("**状态**：" + status),
				cardMarkdown("**你的消息**\n" + question),
				cardMarkdown("**Codex 思考**\n" + thinking),
				cardMarkdown("**回复**\n" + answer),
				cardMarkdown("_更新时间：" + time.Now().In(time.Local).Format("2006-01-02 15:04:05") + "_"),
			},
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
