package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type LarkSender struct {
	client      *lark.Client
	resourceDir string
}

func NewLarkSender(client *lark.Client, resourceDir string) *LarkSender {
	return &LarkSender{
		client:      client,
		resourceDir: strings.TrimSpace(resourceDir),
	}
}

func (s *LarkSender) ResourceRootForScope(resourceScopeKey string) string {
	if s == nil {
		return ""
	}
	return resolveScopedResourceRoot(strings.TrimSpace(s.resourceDir), resourceScopeKey)
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

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		return nil
	})
}

func (s *LarkSender) AddReaction(ctx context.Context, messageID, emojiType string) error {
	messageID = strings.TrimSpace(messageID)
	emojiType = strings.ToUpper(strings.TrimSpace(emojiType))
	if messageID == "" {
		return errors.New("message id is empty")
	}
	if emojiType == "" {
		return errors.New("emoji type is empty")
	}

	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.MessageReaction.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		return nil
	})
}

func (s *LarkSender) SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("interactive").
			Content(cardContent).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		return nil
	})
}

func (s *LarkSender) ReplyText(ctx context.Context, sourceMessageID, text string) (string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "text", textMessageContent(text), "reply success but response message_id is empty")
}

func (s *LarkSender) ReplyTextDirect(ctx context.Context, sourceMessageID, text string) (string, error) {
	return s.replyMessage(ctx, sourceMessageID, "text", textMessageContent(text), false, "reply success but response message_id is empty")
}

func (s *LarkSender) ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "post", richTextMessageContent(lines), "reply rich text success but response message_id is empty")
}

func (s *LarkSender) ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "post", richTextMarkdownMessageContent(markdown), "reply markdown rich text success but response message_id is empty")
}

func (s *LarkSender) ReplyRichTextMarkdownDirect(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	return s.replyMessage(ctx, sourceMessageID, "post", richTextMarkdownMessageContent(markdown), false, "reply markdown rich text success but response message_id is empty")
}

func (s *LarkSender) ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "interactive", cardContent, "reply card success but response message_id is empty")
}

func (s *LarkSender) ReplyCardDirect(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	return s.replyMessage(ctx, sourceMessageID, "interactive", cardContent, false, "reply card success but response message_id is empty")
}

func (s *LarkSender) replyMessagePreferThread(
	ctx context.Context,
	sourceMessageID, msgType, content, emptyMessageIDErr string,
) (string, error) {
	messageID, err := s.replyMessage(ctx, sourceMessageID, msgType, content, true, emptyMessageIDErr)
	if err == nil {
		return messageID, nil
	}

	var apiErr *feishuAPIError
	if !errors.As(err, &apiErr) {
		return "", err
	}
	return s.replyMessage(ctx, sourceMessageID, msgType, content, false, emptyMessageIDErr)
}

func (s *LarkSender) replyMessage(
	ctx context.Context,
	sourceMessageID, msgType, content string,
	replyInThread bool,
	emptyMessageIDErr string,
) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(replyInThread).
			Build()).
		Build()

	var messageID string
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Reply(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		if resp.Data == nil || resp.Data.MessageId == nil {
			return errors.New(emptyMessageIDErr)
		}
		messageID = strings.TrimSpace(*resp.Data.MessageId)
		if messageID == "" {
			return errors.New(emptyMessageIDErr)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return messageID, nil
}

type feishuAPIError struct {
	Code      int
	Msg       string
	RequestID string
}

func (e *feishuAPIError) Error() string {
	if e == nil {
		return "feishu api error"
	}
	return fmt.Sprintf("feishu api error code=%d msg=%s request_id=%s", e.Code, e.Msg, e.RequestID)
}
