package connector

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var mentionPattern = regexp.MustCompile(`<at[^>]*>.*?</at>`)
var mentionUserIDPattern = regexp.MustCompile(`<at[^>]*\buser_id="([^"]+)"[^>]*>`)

var ErrIgnoreMessage = errors.New("ignore message")

type CodexRunner interface {
	Run(ctx context.Context, userText string) (string, error)
}

type StreamingCodexRunner interface {
	RunWithProgress(ctx context.Context, userText string, onThinking func(step string)) (string, error)
}

type ResumableCodexRunner interface {
	RunWithThread(ctx context.Context, threadID, userText string) (reply string, nextThreadID string, err error)
}

type ResumableStreamingCodexRunner interface {
	RunWithThreadAndProgress(
		ctx context.Context,
		threadID string,
		userText string,
		onThinking func(step string),
	) (reply string, nextThreadID string, err error)
}

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

type Attachment struct {
	Kind          string
	FileKey       string
	ImageKey      string
	FileName      string
	LocalPath     string
	DownloadError string
}

type Job struct {
	ReceiveID            string
	ReceiveIDType        string
	ChatType             string
	SenderOpenID         string
	SenderUserID         string
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
}
