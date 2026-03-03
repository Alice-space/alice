package connector

import (
	"context"
	"errors"
	"strings"
)

type sendCardCapable interface {
	SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error
}

// replyDispatcher owns the Feishu reply/send fallback policy so Processor can
// focus on job flow instead of transport-specific downgrade rules.
type replyDispatcher struct {
	sender Sender
}

func newReplyDispatcher(sender Sender) *replyDispatcher {
	return &replyDispatcher{sender: sender}
}

func (d *replyDispatcher) respond(ctx context.Context, job Job, markdown string) error {
	if strings.TrimSpace(job.SourceMessageID) != "" {
		_, err := d.reply(ctx, job, job.SourceMessageID, markdown)
		return err
	}
	return d.send(ctx, job, job.ReceiveIDType, job.ReceiveID, markdown)
}

func (d *replyDispatcher) reply(
	ctx context.Context,
	job Job,
	sourceMessageID,
	markdown string,
) (string, error) {
	if d == nil || d.sender == nil {
		return "", errors.New("reply dispatcher sender is nil")
	}

	normalized, forceText := normalizeOutgoingReplyWithMentions(markdown, job)
	if normalized == "" {
		return "", nil
	}
	if forceText {
		if messageID, textErr := d.sender.ReplyText(ctx, sourceMessageID, normalized); textErr == nil {
			return messageID, nil
		}
		normalized = strings.TrimSpace(markdown)
		if normalized == "" {
			return "", nil
		}
	}
	if messageID, cardErr := d.sender.ReplyCard(ctx, sourceMessageID, buildReplyCardContent(normalized)); cardErr == nil {
		return messageID, nil
	}
	return d.replyMarkdownPost(ctx, sourceMessageID, normalized, false)
}

func (d *replyDispatcher) send(
	ctx context.Context,
	job Job,
	receiveIDType,
	receiveID,
	markdown string,
) error {
	if d == nil || d.sender == nil {
		return errors.New("reply dispatcher sender is nil")
	}

	normalized, forceText := normalizeOutgoingReplyWithMentions(markdown, job)
	if normalized == "" {
		return nil
	}
	if forceText {
		if textErr := d.sender.SendText(ctx, receiveIDType, receiveID, normalized); textErr == nil {
			return nil
		}
		normalized = strings.TrimSpace(markdown)
		if normalized == "" {
			return nil
		}
	}
	if sender, ok := d.sender.(sendCardCapable); ok {
		if cardErr := sender.SendCard(ctx, receiveIDType, receiveID, buildReplyCardContent(normalized)); cardErr == nil {
			return nil
		}
	}
	return d.sender.SendText(ctx, receiveIDType, receiveID, normalized)
}

func (d *replyDispatcher) replyMarkdownPost(
	ctx context.Context,
	sourceMessageID,
	markdown string,
	forceText bool,
) (string, error) {
	if d == nil || d.sender == nil {
		return "", errors.New("reply dispatcher sender is nil")
	}

	normalized := strings.TrimSpace(markdown)
	if normalized == "" {
		return "", nil
	}
	if forceText {
		return d.sender.ReplyText(ctx, sourceMessageID, normalized)
	}
	if messageID, richErr := d.sender.ReplyRichTextMarkdown(ctx, sourceMessageID, normalized); richErr == nil {
		return messageID, nil
	}
	messageID, textErr := d.sender.ReplyText(ctx, sourceMessageID, normalized)
	if textErr != nil {
		return "", textErr
	}
	return messageID, nil
}
