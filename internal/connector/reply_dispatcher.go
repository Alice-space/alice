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
	_, err := d.send(ctx, job, job.ReceiveIDType, job.ReceiveID, markdown)
	return err
}

func (d *replyDispatcher) respondCardWithTitle(ctx context.Context, job Job, title, markdown string) error {
	if strings.TrimSpace(job.SourceMessageID) != "" {
		_, err := d.replyCardWithTitle(ctx, job, job.SourceMessageID, title, markdown)
		return err
	}
	return d.sendCardWithTitle(ctx, job, job.ReceiveIDType, job.ReceiveID, title, markdown)
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
		plainText := sanitizeMarkdownForPlainText(normalized)
		if messageID, textErr := d.replyText(ctx, sourceMessageID, plainText, preferThread); textErr == nil {
			return messageID, nil
		}
		normalized = stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
		if normalized == "" {
			return "", nil
		}
	}
	if jobAllowsCards(job) {
		if messageID, cardErr := d.replyCard(ctx, sourceMessageID, buildReplyCardContent(normalized), preferThread); cardErr == nil {
			return messageID, nil
		}
	}
	return d.replyMarkdownPost(ctx, job, sourceMessageID, normalized, false, preferThread)
}

func (d *replyDispatcher) send(
	ctx context.Context,
	job Job,
	receiveIDType,
	receiveID,
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
		plainText := sanitizeMarkdownForPlainText(normalized)
		if messageID, textErr := d.sendText(ctx, receiveIDType, receiveID, plainText); textErr == nil {
			return messageID, nil
		}
		normalized = stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
		if normalized == "" {
			return "", nil
		}
	}
	if jobAllowsCards(job) {
		if messageID, cardErr := d.sendCard(ctx, receiveIDType, receiveID, buildReplyCardContent(normalized)); cardErr == nil {
			return messageID, nil
		}
	}
	return d.sendText(ctx, receiveIDType, receiveID, normalized)
}

func (d *replyDispatcher) replyCardWithTitle(
	ctx context.Context,
	job Job,
	sourceMessageID,
	title,
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
		plainText := sanitizeMarkdownForPlainText(normalized)
		if messageID, textErr := d.replyText(ctx, sourceMessageID, plainText, preferThread); textErr == nil {
			return messageID, nil
		}
		normalized = stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
		if normalized == "" {
			return "", nil
		}
	}
	if messageID, cardErr := d.replyCard(ctx, sourceMessageID, buildTitledReplyCardContent(title, normalized), preferThread); cardErr == nil {
		return messageID, nil
	}
	return d.replyMarkdownPost(ctx, job, sourceMessageID, normalized, false, preferThread)
}

func (d *replyDispatcher) sendCardWithTitle(
	ctx context.Context,
	job Job,
	receiveIDType,
	receiveID,
	title,
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
		plainText := sanitizeMarkdownForPlainText(normalized)
		if _, textErr := d.sendText(ctx, receiveIDType, receiveID, plainText); textErr == nil {
			return nil
		}
		normalized = stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
		if normalized == "" {
			return nil
		}
	}
	if _, cardErr := d.sendCard(ctx, receiveIDType, receiveID, buildTitledReplyCardContent(title, normalized)); cardErr == nil {
		return nil
	}
	_, err := d.sendText(ctx, receiveIDType, receiveID, normalized)
	return err
}

func (d *replyDispatcher) replyMarkdownPost(
	ctx context.Context,
	job Job,
	sourceMessageID,
	markdown string,
	forceText bool,
	preferThread bool,
) (string, error) {
	if d == nil || d.sender == nil {
		return "", errors.New("reply dispatcher sender is nil")
	}

	normalized := stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
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

type sendTextMessageSender interface {
	SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error)
}

type sendCardMessageSender interface {
	SendCardMessage(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error)
}

func (d *replyDispatcher) sendText(
	ctx context.Context,
	receiveIDType,
	receiveID,
	text string,
) (string, error) {
	if sender, ok := d.sender.(sendTextMessageSender); ok {
		return sender.SendTextMessage(ctx, receiveIDType, receiveID, text)
	}
	if err := d.sender.SendText(ctx, receiveIDType, receiveID, text); err != nil {
		return "", err
	}
	return "", nil
}

func (d *replyDispatcher) sendCard(
	ctx context.Context,
	receiveIDType,
	receiveID,
	cardContent string,
) (string, error) {
	if sender, ok := d.sender.(sendCardMessageSender); ok {
		return sender.SendCardMessage(ctx, receiveIDType, receiveID, cardContent)
	}
	if err := d.sender.SendCard(ctx, receiveIDType, receiveID, cardContent); err != nil {
		return "", err
	}
	return "", nil
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
