package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alice/internal/platform"

	"github.com/gin-gonic/gin"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var ErrDisabled = errors.New("feishu is disabled")

type replyAPI interface {
	Reply(ctx context.Context, req *larkim.ReplyMessageReq, options ...larkcore.RequestOptionFunc) (*larkim.ReplyMessageResp, error)
}

// Service owns the Feishu SDK integration and its local state.
type Service struct {
	cfg        Config
	logger     platform.Logger
	state      *StateStore
	dispatcher *larkdispatcher.EventDispatcher
	replyAPI   replyAPI
	replyFunc  func(context.Context, ReplyTarget, string, string) (string, error)
}

func NewService(cfg Config, storageRoot string, logger platform.Logger) (*Service, error) {
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	state, err := OpenStateStore(storageRoot)
	if err != nil {
		return nil, err
	}
	service := &Service{
		cfg:    cfg,
		logger: logger.WithComponent("feishu"),
		state:  state,
	}
	if !cfg.Enabled {
		return service, nil
	}
	client := lark.NewClient(
		cfg.AppID,
		cfg.AppSecret,
		lark.WithReqTimeout(10*time.Second),
		lark.WithLogLevel(larkcore.LogLevelError),
	)
	service.replyAPI = client.Im.V1.Message
	service.dispatcher = larkdispatcher.NewEventDispatcher(cfg.VerificationToken, cfg.EncryptKey)
	return service, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *Service) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	return s.state.Close()
}

func (s *Service) RegisterWebhookRoutes(rg *gin.RouterGroup, ingest func(context.Context, InboundMessage) error) {
	rg.POST("/ingress/im/feishu", s.WebhookHandler(ingest))
}

func (s *Service) WebhookHandler(ingest func(context.Context, InboundMessage) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.Enabled() || s.dispatcher == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "feishu is not configured"})
			return
		}

		dispatcher := larkdispatcher.NewEventDispatcher(s.cfg.VerificationToken, s.cfg.EncryptKey)
		dispatcher.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			if ingest == nil {
				return nil
			}
			msg := inboundFromEvent(event)
			if strings.TrimSpace(msg.Metadata.MessageID) == "" {
				s.logger.Warn("feishu_message_missing_message_id")
				return nil
			}
			return ingest(ctx, msg)
		})

		resp := dispatcher.Handle(c.Request.Context(), &larkevent.EventReq{
			Header:     c.Request.Header,
			Body:       mustReadBody(c),
			RequestURI: c.Request.URL.RequestURI(),
		})
		if resp == nil {
			c.Status(http.StatusOK)
			return
		}
		for key, values := range resp.Header {
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}
		c.Data(resp.StatusCode, detectContentType(resp.Header), resp.Body)
	}
}

func mustReadBody(c *gin.Context) []byte {
	if c.Request == nil || c.Request.Body == nil {
		return nil
	}
	body, err := c.GetRawData()
	if err != nil {
		return nil
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func detectContentType(header http.Header) string {
	if header == nil {
		return larkevent.ContentTypeJson
	}
	values := header.Values("Content-Type")
	if len(values) == 0 {
		return larkevent.ContentTypeJson
	}
	return values[0]
}

func inboundFromEvent(event *larkim.P2MessageReceiveV1) InboundMessage {
	msg := InboundMessage{}
	if event == nil || event.Event == nil {
		return msg
	}
	meta := MessageMetadata{}
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		meta.EventID = strings.TrimSpace(event.EventV2Base.Header.EventID)
		meta.TenantKey = strings.TrimSpace(event.EventV2Base.Header.TenantKey)
	}
	if event.Event.Message != nil {
		meta.MessageID = safeString(event.Event.Message.MessageId)
		meta.RootID = safeString(event.Event.Message.RootId)
		meta.ParentID = safeString(event.Event.Message.ParentId)
		meta.ThreadID = safeString(event.Event.Message.ThreadId)
		meta.ChatID = safeString(event.Event.Message.ChatId)
		meta.ChatType = safeString(event.Event.Message.ChatType)
		meta.MessageType = safeString(event.Event.Message.MessageType)
		meta.RawContent = safeString(event.Event.Message.Content)
	}
	if event.Event.Sender != nil {
		if event.Event.Sender.SenderId != nil {
			meta.SenderOpenID = safeString(event.Event.Sender.SenderId.OpenId)
			meta.SenderUserID = safeString(event.Event.Sender.SenderId.UserId)
			meta.SenderUnionID = safeString(event.Event.Sender.SenderId.UnionId)
		}
		if meta.TenantKey == "" {
			meta.TenantKey = safeString(event.Event.Sender.TenantKey)
		}
	}
	msg.Metadata = meta
	msg.Text = extractUserText(meta.MessageType, meta.RawContent)
	return msg
}

func extractUserText(messageType, rawContent string) string {
	messageType = strings.TrimSpace(messageType)
	rawContent = strings.TrimSpace(rawContent)
	if rawContent == "" {
		return syntheticText(messageType)
	}
	if messageType == "text" {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(rawContent), &payload); err == nil && strings.TrimSpace(payload.Text) != "" {
			return strings.TrimSpace(payload.Text)
		}
	}
	return syntheticText(messageType)
}

func syntheticText(messageType string) string {
	messageType = strings.TrimSpace(messageType)
	if messageType == "" {
		messageType = "unknown"
	}
	return fmt.Sprintf("[feishu %s message]", messageType)
}

func safeString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func (s *Service) ReplyText(ctx context.Context, target ReplyTarget, text, uuid string) (string, error) {
	if !s.Enabled() || s.replyAPI == nil {
		return "", ErrDisabled
	}
	if !target.Valid() {
		return "", fmt.Errorf("invalid feishu reply target")
	}
	if s.replyFunc != nil {
		return s.replyFunc(ctx, target, text, uuid)
	}
	content := larkim.NewTextMsgBuilder().Text(text).Build()
	body := larkim.NewReplyMessageReqBodyBuilder().
		Content(content).
		MsgType(larkim.MsgTypeText).
		ReplyInThread(target.ReplyInThread)
	if strings.TrimSpace(uuid) != "" {
		body = body.Uuid(strings.TrimSpace(uuid))
	}
	resp, err := s.replyAPI.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(target.MessageID).
		Body(body.Build()).
		Build())
	if err != nil {
		return "", fmt.Errorf("feishu reply message: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("feishu reply message failed: empty response")
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu reply message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return "", nil
	}
	return safeString(resp.Data.MessageId), nil
}
