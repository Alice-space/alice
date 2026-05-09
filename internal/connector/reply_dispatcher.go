package connector

import (
	"context"
	"errors"
	"strings"

	"github.com/Alice-space/alice/internal/messaging"
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
	messageID, _, err := d.replyWithThread(ctx, job, sourceMessageID, markdown)
	return messageID, err
}

// replyWithThread is the same as reply but additionally returns the Feishu
// thread_id captured from the underlying API response when the sender
// supports it (Feishu's Reply API surfaces the thread_id of the resulting
// message). Senders that do not implement the *WithThread interfaces return
// an empty thread_id; callers must treat that as "unknown".
func (d *replyDispatcher) replyWithThread(
	ctx context.Context,
	job Job,
	sourceMessageID,
	markdown string,
) (string, string, error) {
	if d == nil || d.sender == nil {
		return "", "", errors.New("reply dispatcher sender is nil")
	}

	normalized, forceText := normalizeOutgoingReplyWithMentions(markdown, job)
	if normalized == "" {
		return "", "", nil
	}
	preferThread := jobPrefersThreadReply(job)
	if forceText {
		plainText := sanitizeMarkdownForPlainText(normalized)
		if messageID, threadID, textErr := d.replyTextWithThread(ctx, sourceMessageID, plainText, preferThread); textErr == nil {
			return messageID, threadID, nil
		}
		normalized = stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
		if normalized == "" {
			return "", "", nil
		}
	}
	if jobAllowsCards(job) {
		if messageID, threadID, cardErr := d.replyCardWithThread(ctx, sourceMessageID, buildReplyCardContent(normalized), preferThread); cardErr == nil {
			return messageID, threadID, nil
		}
	}
	return d.replyMarkdownPostWithThread(ctx, job, sourceMessageID, normalized, false, preferThread)
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
	messageID, _, err := d.replyMarkdownPostWithThread(ctx, job, sourceMessageID, markdown, forceText, preferThread)
	return messageID, err
}

func (d *replyDispatcher) replyMarkdownPostWithThread(
	ctx context.Context,
	job Job,
	sourceMessageID,
	markdown string,
	forceText bool,
	preferThread bool,
) (string, string, error) {
	if d == nil || d.sender == nil {
		return "", "", errors.New("reply dispatcher sender is nil")
	}

	normalized := stripHiddenReplyMetadata(markdown, job.SoulDoc.OutputContract)
	if normalized == "" {
		return "", "", nil
	}
	if forceText {
		return d.replyTextWithThread(ctx, sourceMessageID, normalized, preferThread)
	}
	if messageID, threadID, richErr := d.replyRichTextMarkdownWithThread(ctx, sourceMessageID, normalized, preferThread); richErr == nil {
		return messageID, threadID, nil
	}
	messageID, threadID, textErr := d.replyTextWithThread(ctx, sourceMessageID, normalized, preferThread)
	if textErr != nil {
		return "", "", textErr
	}
	return messageID, threadID, nil
}

func (d *replyDispatcher) replyText(
	ctx context.Context,
	sourceMessageID string,
	text string,
	preferThread bool,
) (string, error) {
	messageID, _, err := d.replyTextWithThread(ctx, sourceMessageID, text, preferThread)
	return messageID, err
}

func (d *replyDispatcher) replyTextWithThread(
	ctx context.Context,
	sourceMessageID string,
	text string,
	preferThread bool,
) (string, string, error) {
	if preferThread {
		if sender, ok := d.sender.(messaging.ReplyTextWithThreadSender); ok {
			return sender.ReplyTextWithThread(ctx, sourceMessageID, text)
		}
		messageID, err := d.sender.ReplyText(ctx, sourceMessageID, text)
		return messageID, "", err
	}
	if sender, ok := d.sender.(messaging.ReplyTextDirectWithThreadSender); ok {
		return sender.ReplyTextDirectWithThread(ctx, sourceMessageID, text)
	}
	messageID, err := d.sender.ReplyTextDirect(ctx, sourceMessageID, text)
	return messageID, "", err
}

func (d *replyDispatcher) replyRichTextMarkdown(
	ctx context.Context,
	sourceMessageID string,
	markdown string,
	preferThread bool,
) (string, error) {
	messageID, _, err := d.replyRichTextMarkdownWithThread(ctx, sourceMessageID, markdown, preferThread)
	return messageID, err
}

func (d *replyDispatcher) replyRichTextMarkdownWithThread(
	ctx context.Context,
	sourceMessageID string,
	markdown string,
	preferThread bool,
) (string, string, error) {
	if preferThread {
		if sender, ok := d.sender.(messaging.ReplyRichTextMarkdownWithThreadSender); ok {
			return sender.ReplyRichTextMarkdownWithThread(ctx, sourceMessageID, markdown)
		}
		messageID, err := d.sender.ReplyRichTextMarkdown(ctx, sourceMessageID, markdown)
		return messageID, "", err
	}
	if sender, ok := d.sender.(messaging.ReplyRichTextMarkdownDirectWithThreadSender); ok {
		return sender.ReplyRichTextMarkdownDirectWithThread(ctx, sourceMessageID, markdown)
	}
	messageID, err := d.sender.ReplyRichTextMarkdownDirect(ctx, sourceMessageID, markdown)
	return messageID, "", err
}

func (d *replyDispatcher) replyCard(
	ctx context.Context,
	sourceMessageID string,
	cardContent string,
	preferThread bool,
) (string, error) {
	messageID, _, err := d.replyCardWithThread(ctx, sourceMessageID, cardContent, preferThread)
	return messageID, err
}

func (d *replyDispatcher) replyCardWithThread(
	ctx context.Context,
	sourceMessageID string,
	cardContent string,
	preferThread bool,
) (string, string, error) {
	if preferThread {
		if sender, ok := d.sender.(messaging.ReplyCardWithThreadSender); ok {
			return sender.ReplyCardWithThread(ctx, sourceMessageID, cardContent)
		}
		messageID, err := d.sender.ReplyCard(ctx, sourceMessageID, cardContent)
		return messageID, "", err
	}
	if sender, ok := d.sender.(messaging.ReplyCardDirectWithThreadSender); ok {
		return sender.ReplyCardDirectWithThread(ctx, sourceMessageID, cardContent)
	}
	messageID, err := d.sender.ReplyCardDirect(ctx, sourceMessageID, cardContent)
	return messageID, "", err
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
