package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func (s *LarkSender) GetMessageText(ctx context.Context, messageID string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", errors.New("message id is empty")
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		Build()
	var msgType string
	var content string
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		if resp.Data == nil || len(resp.Data.Items) == 0 || resp.Data.Items[0] == nil {
			return errors.New("get message success but items is empty")
		}
		item := resp.Data.Items[0]
		msgType = strings.ToLower(strings.TrimSpace(deref(item.MsgType)))
		content = ""
		if item.Body != nil {
			content = deref(item.Body.Content)
		}
		content = strings.TrimSpace(content)
		if content == "" {
			return errors.New("get message success but content is empty")
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	switch msgType {
	case "text":
		text, err := extractText(&content)
		if err == nil {
			return text, nil
		}
	case "interactive":
		if text := extractReplyTextFromCard(content); text != "" {
			return text, nil
		}
	case "post":
		if text := extractTextFromPost(content); text != "" {
			return text, nil
		}
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		text := strings.TrimSpace(payload.Text)
		if text != "" {
			text = mentionPattern.ReplaceAllString(text, "")
			text = strings.TrimSpace(text)
			if text != "" {
				return text, nil
			}
		}
	}
	return "", nil
}
