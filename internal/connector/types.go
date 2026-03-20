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
var errSessionInterrupted = errors.New("session interrupted by newer message")

func wasInterruptedByNewMessage(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errSessionInterrupted)
}

type MemoryManager interface {
	BuildPrompt(memoryScopeKey, userText string) (string, error)
	SaveInteraction(memoryScopeKey, userText, assistantText string, failed bool) (changed bool, err error)
	AppendDailySummary(memoryScopeKey, sessionKey, summary string, at time.Time) error
}

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	AddReaction(ctx context.Context, messageID, emojiType string) error
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
	ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error)
	ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error)
	ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error)
}

type ReplyContextProvider interface {
	GetMessageText(ctx context.Context, messageID string) (string, error)
}

type AttachmentDownloader interface {
	DownloadAttachment(ctx context.Context, memoryScopeKey, sourceMessageID string, attachment *Attachment) error
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
	ThreadID             string
	RootID               string
	MessageType          string
	Text                 string
	Attachments          []Attachment
	RawContent           string
	EventID              string
	ReceivedAt           time.Time
	MemoryScopeKey       string
	SessionKey           string
	SessionVersion       uint64
	Scene                string
	ResponseMode         string
	LLMModel             string
	LLMProfile           string
	LLMReasoningEffort   string
	LLMPersonality       string
	NoReplyToken         string
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
