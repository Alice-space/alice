package connector

import (
	"context"
	"errors"
	"strings"
)

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
	preferThread := jobPrefersThreadReply(job)
	if forceText {
		if messageID, textErr := d.replyText(ctx, sourceMessageID, normalized, preferThread); textErr == nil {
			return messageID, nil
		}
		normalized = stripHiddenReplyMetadata(markdown)
		if normalized == "" {
			return "", nil
		}
	}
	if jobAllowsCards(job) {
		if messageID, cardErr := d.replyCard(ctx, sourceMessageID, buildReplyCardContent(normalized), preferThread); cardErr == nil {
			return messageID, nil
		}
	}
	return d.replyMarkdownPost(ctx, sourceMessageID, normalized, false, preferThread)
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
		normalized = stripHiddenReplyMetadata(markdown)
		if normalized == "" {
			return nil
		}
	}
	if jobAllowsCards(job) {
		if cardErr := d.sender.SendCard(ctx, receiveIDType, receiveID, buildReplyCardContent(normalized)); cardErr == nil {
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
	preferThread bool,
) (string, error) {
	if d == nil || d.sender == nil {
		return "", errors.New("reply dispatcher sender is nil")
	}

	normalized := stripHiddenReplyMetadata(markdown)
	if normalized == "" {
		return "", nil
	}
	if forceText {
		return d.replyText(ctx, sourceMessageID, normalized, preferThread)
	}
	if messageID, richErr := d.replyRichTextMarkdown(ctx, sourceMessageID, normalized, preferThread); richErr == nil {
		return messageID, nil
	}
	messageID, textErr := d.replyText(ctx, sourceMessageID, normalized, preferThread)
	if textErr != nil {
		return "", textErr
	}
	return messageID, nil
}

func (d *replyDispatcher) replyText(
	ctx context.Context,
	sourceMessageID string,
	text string,
	preferThread bool,
) (string, error) {
	if preferThread {
		return d.sender.ReplyText(ctx, sourceMessageID, text)
	}
	return d.sender.ReplyTextDirect(ctx, sourceMessageID, text)
}

func (d *replyDispatcher) replyRichTextMarkdown(
	ctx context.Context,
	sourceMessageID string,
	markdown string,
	preferThread bool,
) (string, error) {
	if preferThread {
		return d.sender.ReplyRichTextMarkdown(ctx, sourceMessageID, markdown)
	}
	return d.sender.ReplyRichTextMarkdownDirect(ctx, sourceMessageID, markdown)
}

func (d *replyDispatcher) replyCard(
	ctx context.Context,
	sourceMessageID string,
	cardContent string,
	preferThread bool,
) (string, error) {
	if preferThread {
		return d.sender.ReplyCard(ctx, sourceMessageID, cardContent)
	}
	return d.sender.ReplyCardDirect(ctx, sourceMessageID, cardContent)
}

func jobAllowsCards(job Job) bool {
	return strings.TrimSpace(job.Scene) != jobSceneChat
}

func jobPrefersThreadReply(job Job) bool {
	switch strings.TrimSpace(job.Scene) {
	case jobSceneChat:
		return false
	case jobSceneWork:
		return job.CreateFeishuThread
	default:
		return true
	}
}
