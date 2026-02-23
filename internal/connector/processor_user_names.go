package connector

import (
	"context"
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

	if strings.TrimSpace(job.SenderName) == "" {
		if senderName, err := resolver.ResolveUserName(ctx, job.SenderOpenID, job.SenderUserID); err == nil {
			job.SenderName = strings.TrimSpace(senderName)
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
		name, err := resolver.ResolveUserName(ctx, job.MentionedUsers[i].OpenID, job.MentionedUsers[i].UserID)
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
		job.MentionedUsers[i].Name = strings.TrimSpace(name)
	}
}
