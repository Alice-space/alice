package connector

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/messaging"
)

var mentionPattern = regexp.MustCompile(`<at[^>]*>.*?</at>`)
var mentionUserIDPattern = regexp.MustCompile(`<at[^>]*\buser_id="([^"]+)"[^>]*>`)

var ErrIgnoreMessage = errors.New("ignore message")
var errSessionInterrupted = errors.New("session interrupted by newer message")
var errSessionStopped = errors.New("session stopped by slash command")

func wasInterruptedByNewMessage(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errSessionInterrupted)
}

func wasStoppedByCommand(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errSessionStopped)
}

type Sender = messaging.ConversationSender

type ReplyContextProvider interface {
	GetMessageText(ctx context.Context, messageID string) (string, error)
}

type AttachmentDownloader interface {
	DownloadAttachment(ctx context.Context, resourceScopeKey, sourceMessageID string, attachment *Attachment) error
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
	BotID                string
	BotName              string
	BotOpenID            string
	BotUserID            string
	SoulPath             string
	SenderName           string
	SenderOpenID         string
	SenderUserID         string
	SenderUnionID        string
	MentionedUsers       []MentionedUser
	SourceMessageID      string
	ReplyParentMessageID string
	ThreadID             string
	RootID               string
	MessageType          string
	Text                 string
	Attachments          []Attachment
	RawContent           string
	EventID              string
	ReceivedAt           time.Time
	ResourceScopeKey     string
	SessionKey           string
	SessionVersion       uint64
	Scene                string
	ResponseMode         string
	CreateFeishuThread   bool
	LLMProvider          string
	LLMModel             string
	LLMProfile           string
	LLMReasoningEffort   string
	LLMVariant           string
	LLMPersonality       string
	LLMPromptPrefix      string
	NoReplyToken         string
	SoulDoc              soulDocument
	DisableAck           bool
	WorkflowPhase        string
}

type JobProcessState string

const (
	JobProcessCompleted         JobProcessState = "completed"
	JobProcessRetryAfterRestart JobProcessState = "retry_after_restart"
)

const (
	jobWorkflowPhaseNormal = "normal"
	jobSceneChat           = "chat"
	jobSceneWork           = "work"
	jobResponseModeReply   = "reply"
	jobResponseModeSend    = "send"
)

func normalizeJobWorkflowPhase(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", jobWorkflowPhaseNormal:
		return jobWorkflowPhaseNormal
	default:
		return jobWorkflowPhaseNormal
	}
}
