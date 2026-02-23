package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gitee.com/alicespace/alice/internal/logging"
)

func (p *Processor) enrichJobUserNames(ctx context.Context, job *Job) {
	if job == nil || p.sender == nil {
		return
	}

	resolver, ok := p.sender.(UserNameResolver)
	if !ok {
		return
	}
	chatResolver, _ := p.sender.(ChatMemberNameResolver)

	if strings.TrimSpace(job.SenderName) == "" {
		senderName, err := resolveUserNameWithChatMemberFallback(
			ctx,
			job,
			resolver,
			chatResolver,
			job.SenderOpenID,
			job.SenderUserID,
		)
		if err == nil {
			job.SenderName = senderName
		} else {
			logging.Debugf(
				"resolve sender name failed event_id=%s open_id=%s user_id=%s err=%v",
				job.EventID,
				job.SenderOpenID,
				job.SenderUserID,
				err,
			)
		}
	}

	for i := range job.MentionedUsers {
		if strings.TrimSpace(job.MentionedUsers[i].Name) != "" {
			continue
		}
		name, err := resolveUserNameWithChatMemberFallback(
			ctx,
			job,
			resolver,
			chatResolver,
			job.MentionedUsers[i].OpenID,
			job.MentionedUsers[i].UserID,
		)
		if err != nil {
			logging.Debugf(
				"resolve mentioned user name failed event_id=%s mention_index=%d open_id=%s user_id=%s err=%v",
				job.EventID,
				i,
				job.MentionedUsers[i].OpenID,
				job.MentionedUsers[i].UserID,
				err,
			)
			continue
		}
		job.MentionedUsers[i].Name = name
	}
}

func resolveUserNameWithChatMemberFallback(
	ctx context.Context,
	job *Job,
	resolver UserNameResolver,
	chatResolver ChatMemberNameResolver,
	openID string,
	userID string,
) (string, error) {
	openID = strings.TrimSpace(openID)
	userID = strings.TrimSpace(userID)

	name, err := resolver.ResolveUserName(ctx, openID, userID)
	name = strings.TrimSpace(name)
	if name != "" {
		return name, nil
	}

	lookupErr := err
	if lookupErr == nil {
		lookupErr = errors.New("empty user name from contact")
	}

	if chatResolver == nil || job == nil {
		return "", lookupErr
	}

	if !strings.EqualFold(strings.TrimSpace(job.ReceiveIDType), "chat_id") {
		return "", lookupErr
	}

	chatID := strings.TrimSpace(job.ReceiveID)
	if chatID == "" {
		return "", lookupErr
	}

	chatName, chatErr := chatResolver.ResolveChatMemberName(ctx, chatID, openID, userID)
	chatName = strings.TrimSpace(chatName)
	if chatName != "" {
		return chatName, nil
	}
	if chatErr != nil {
		return "", fmt.Errorf("contact lookup failed: %v; chat member lookup failed: %w", lookupErr, chatErr)
	}
	return "", lookupErr
}
