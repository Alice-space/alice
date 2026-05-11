package feishu

import (
	"context"
	"errors"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type FeishuSender struct {
	client      *lark.Client
	resourceDir string
}

func NewFeishuSender(client *lark.Client, resourceDir string) *FeishuSender {
	return &FeishuSender{
		client:      client,
		resourceDir: strings.TrimSpace(resourceDir),
	}
}

func (s *FeishuSender) Client() *lark.Client {
	if s == nil {
		return nil
	}
	return s.client
}

func (s *FeishuSender) ResourceRootForScope(resourceScopeKey string) string {
	if s == nil {
		return ""
	}
	return resolveScopedResourceRoot(strings.TrimSpace(s.resourceDir), resourceScopeKey)
}

func (s *FeishuSender) SendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	_, err := s.SendTextMessage(ctx, receiveIDType, receiveID, text)
	return err
}

func (s *FeishuSender) SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error) {
	return s.createMessage(ctx, receiveIDType, receiveID, "text", textMessageContent(text), "send text success but response message_id is empty")
}

func (s *FeishuSender) AddReaction(ctx context.Context, messageID, emojiType string) error {
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

func (s *FeishuSender) SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error {
	_, err := s.SendCardMessage(ctx, receiveIDType, receiveID, cardContent)
	return err
}

func (s *FeishuSender) SendCardMessage(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error) {
	return s.createMessage(ctx, receiveIDType, receiveID, "interactive", cardContent, "send card success but response message_id is empty")
}

func (s *FeishuSender) PatchCard(ctx context.Context, messageID, cardContent string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return errors.New("message id is empty")
	}

	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().
			Content(cardContent).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Patch(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		return nil
	})
}

func (s *FeishuSender) UrgentApp(ctx context.Context, messageID, userIDType string, userIDs []string) error {
	messageID = strings.TrimSpace(messageID)
	userIDType = strings.TrimSpace(userIDType)
	if messageID == "" {
		return errors.New("message id is empty")
	}
	if userIDType == "" {
		return errors.New("user id type is empty")
	}
	receivers := make([]string, 0, len(userIDs))
	seen := make(map[string]struct{}, len(userIDs))
	for _, raw := range userIDs {
		userID := strings.TrimSpace(raw)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		receivers = append(receivers, userID)
	}
	if len(receivers) == 0 {
		return errors.New("urgent receivers are empty")
	}

	req := larkim.NewUrgentAppMessageReqBuilder().
		MessageId(messageID).
		UserIdType(userIDType).
		UrgentReceivers(larkim.NewUrgentReceiversBuilder().UserIdList(receivers).Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.UrgentApp(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		return nil
	})
}

func (s *FeishuSender) ReplyText(ctx context.Context, sourceMessageID, text string) (string, error) {
	messageID, _, err := s.ReplyTextWithThread(ctx, sourceMessageID, text)
	return messageID, err
}

func (s *FeishuSender) ReplyTextWithThread(ctx context.Context, sourceMessageID, text string) (string, string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "text", textMessageContent(text), "reply success but response message_id is empty")
}

func (s *FeishuSender) ReplyTextDirect(ctx context.Context, sourceMessageID, text string) (string, error) {
	messageID, _, err := s.ReplyTextDirectWithThread(ctx, sourceMessageID, text)
	return messageID, err
}

func (s *FeishuSender) ReplyTextDirectWithThread(ctx context.Context, sourceMessageID, text string) (string, string, error) {
	return s.replyMessage(ctx, sourceMessageID, "text", textMessageContent(text), false, "reply success but response message_id is empty")
}

func (s *FeishuSender) ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error) {
	messageID, _, err := s.replyMessagePreferThread(ctx, sourceMessageID, "post", richTextMessageContent(lines), "reply rich text success but response message_id is empty")
	return messageID, err
}

func (s *FeishuSender) ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	messageID, _, err := s.ReplyRichTextMarkdownWithThread(ctx, sourceMessageID, markdown)
	return messageID, err
}

func (s *FeishuSender) ReplyRichTextMarkdownWithThread(ctx context.Context, sourceMessageID, markdown string) (string, string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "post", richTextMarkdownMessageContent(markdown), "reply markdown rich text success but response message_id is empty")
}

func (s *FeishuSender) ReplyRichTextMarkdownDirect(ctx context.Context, sourceMessageID, markdown string) (string, error) {
	messageID, _, err := s.ReplyRichTextMarkdownDirectWithThread(ctx, sourceMessageID, markdown)
	return messageID, err
}

func (s *FeishuSender) ReplyRichTextMarkdownDirectWithThread(ctx context.Context, sourceMessageID, markdown string) (string, string, error) {
	return s.replyMessage(ctx, sourceMessageID, "post", richTextMarkdownMessageContent(markdown), false, "reply markdown rich text success but response message_id is empty")
}

func (s *FeishuSender) ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	messageID, _, err := s.ReplyCardWithThread(ctx, sourceMessageID, cardContent)
	return messageID, err
}

func (s *FeishuSender) ReplyCardWithThread(ctx context.Context, sourceMessageID, cardContent string) (string, string, error) {
	return s.replyMessagePreferThread(ctx, sourceMessageID, "interactive", cardContent, "reply card success but response message_id is empty")
}

func (s *FeishuSender) ReplyCardDirect(ctx context.Context, sourceMessageID, cardContent string) (string, error) {
	messageID, _, err := s.ReplyCardDirectWithThread(ctx, sourceMessageID, cardContent)
	return messageID, err
}

func (s *FeishuSender) ReplyCardDirectWithThread(ctx context.Context, sourceMessageID, cardContent string) (string, string, error) {
	return s.replyMessage(ctx, sourceMessageID, "interactive", cardContent, false, "reply card success but response message_id is empty")
}

func (s *FeishuSender) replyMessagePreferThread(
	ctx context.Context,
	sourceMessageID, msgType, content, emptyMessageIDErr string,
) (string, string, error) {
	messageID, threadID, err := s.replyMessage(ctx, sourceMessageID, msgType, content, true, emptyMessageIDErr)
	if err == nil {
		return messageID, threadID, nil
	}
	if !shouldFallbackThreadReply(err) {
		return "", "", err
	}
	return s.replyMessage(ctx, sourceMessageID, msgType, content, false, emptyMessageIDErr)
}

func shouldFallbackThreadReply(err error) bool {
	var apiErr *feishuAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return isThreadReplyUnsupportedFeishuError(apiErr)
}

func isThreadReplyUnsupportedFeishuError(err *feishuAPIError) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Msg))
	switch {
	case strings.Contains(msg, "reply in thread") && strings.Contains(msg, "not support"):
		return true
	case strings.Contains(msg, "reply in thread") && strings.Contains(msg, "unsupported"):
		return true
	case strings.Contains(msg, "thread") && strings.Contains(msg, "not support"):
		return true
	default:
		return false
	}
}

func (s *FeishuSender) replyMessage(
	ctx context.Context,
	sourceMessageID, msgType, content string,
	replyInThread bool,
	emptyMessageIDErr string,
) (string, string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(sourceMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(replyInThread).
			Build()).
		Build()

	var messageID, threadID string
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
		if resp.Data.ThreadId != nil {
			threadID = strings.TrimSpace(*resp.Data.ThreadId)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return messageID, threadID, nil
}

func (s *FeishuSender) createMessage(
	ctx context.Context,
	receiveIDType, receiveID, msgType, content, emptyMessageIDErr string,
) (string, error) {
	// "source_message_id" is an internal routing type used by the automation
	// engine for thread delivery: receiveID is an om_xxx Feishu message ID and
	// the message should be posted as a reply in the same thread.
	if receiveIDType == "source_message_id" {
		messageID, _, err := s.replyMessagePreferThread(ctx, receiveID, msgType, content, emptyMessageIDErr)
		return messageID, err
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	var messageID string
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
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
