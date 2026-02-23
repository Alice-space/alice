package connector

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var mentionPattern = regexp.MustCompile(`<at[^>]*>.*?</at>`)
var mentionUserIDPattern = regexp.MustCompile(`<at[^>]*\buser_id="([^"]+)"[^>]*>`)

var ErrIgnoreMessage = errors.New("ignore message")

type MemoryManager interface {
	BuildPrompt(userText string) (string, error)
	SaveInteraction(userText, assistantText string, failed bool) (changed bool, err error)
	AppendDailySummary(sessionKey, summary string, at time.Time) error
}

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
	ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error)
	ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error)
	ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error)
	PatchCard(ctx context.Context, messageID, cardContent string) error
}

type ReplyContextProvider interface {
	GetMessageText(ctx context.Context, messageID string) (string, error)
}

type AttachmentDownloader interface {
	DownloadAttachment(ctx context.Context, sourceMessageID string, attachment *Attachment) error
}

type UserNameResolver interface {
	ResolveUserName(ctx context.Context, openID, userID string) (string, error)
}

type ChatMemberNameResolver interface {
	ResolveChatMemberName(ctx context.Context, chatID, openID, userID string) (string, error)
}

type Attachment struct {
	SourceMessageID string
	Kind            string
	FileKey         string
	ImageKey        string
	FileName        string
	LocalPath       string
	DownloadError   string
}

type MentionedUser struct {
	Key     string
	Name    string
	OpenID  string
	UserID  string
	UnionID string
}

type Job struct {
	ReceiveID            string
	ReceiveIDType        string
	ChatType             string
	BotOpenID            string
	BotUserID            string
	SenderName           string
	SenderOpenID         string
	SenderUserID         string
	SenderUnionID        string
	MentionedUsers       []MentionedUser
	SourceMessageID      string
	ReplyParentMessageID string
	MessageType          string
	Text                 string
	Attachments          []Attachment
	RawContent           string
	EventID              string
	ReceivedAt           time.Time
	SessionKey           string
	SessionVersion       uint64
	WorkflowPhase        string
}

type JobProcessState string

const (
	JobProcessCompleted           JobProcessState = "completed"
	JobProcessRetryAfterRestart   JobProcessState = "retry_after_restart"
	JobProcessPostRestartFinalize JobProcessState = "post_restart_finalize"
)

const (
	jobWorkflowPhaseNormal              = "normal"
	jobWorkflowPhasePostRestartFinalize = "post_restart_finalize"
	jobWorkflowPhaseRestartNotification = "restart_notification"
)

func normalizeJobWorkflowPhase(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", jobWorkflowPhaseNormal:
		return jobWorkflowPhaseNormal
	case jobWorkflowPhasePostRestartFinalize:
		return jobWorkflowPhasePostRestartFinalize
	case jobWorkflowPhaseRestartNotification:
		return jobWorkflowPhaseRestartNotification
	default:
		return jobWorkflowPhaseNormal
	}
}
