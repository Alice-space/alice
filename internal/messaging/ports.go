package messaging

import "context"

type TextSender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
}

type CardSender interface {
	SendCard(ctx context.Context, receiveIDType, receiveID, cardContent string) error
}

type ReactionSender interface {
	AddReaction(ctx context.Context, messageID, emojiType string) error
}

type ImageSender interface {
	SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) error
}

type FileSender interface {
	SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) error
}

type ImageUploader interface {
	UploadImage(ctx context.Context, localPath string) (string, error)
}

type FileUploader interface {
	UploadFile(ctx context.Context, localPath, fileName string) (string, error)
}

type ReplyTextSender interface {
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
}

type ReplyTextDirectSender interface {
	ReplyTextDirect(ctx context.Context, sourceMessageID, text string) (string, error)
}

type ReplyRichTextSender interface {
	ReplyRichText(ctx context.Context, sourceMessageID string, lines []string) (string, error)
}

type ReplyRichTextMarkdownSender interface {
	ReplyRichTextMarkdown(ctx context.Context, sourceMessageID, markdown string) (string, error)
}

type ReplyRichTextMarkdownDirectSender interface {
	ReplyRichTextMarkdownDirect(ctx context.Context, sourceMessageID, markdown string) (string, error)
}

type ReplyCardSender interface {
	ReplyCard(ctx context.Context, sourceMessageID, cardContent string) (string, error)
}

type ReplyCardDirectSender interface {
	ReplyCardDirect(ctx context.Context, sourceMessageID, cardContent string) (string, error)
}

type ReplyImageSender interface {
	ReplyImage(ctx context.Context, sourceMessageID, imageKey string) (string, error)
}

type ReplyImageDirectSender interface {
	ReplyImageDirect(ctx context.Context, sourceMessageID, imageKey string) (string, error)
}

type ReplyFileSender interface {
	ReplyFile(ctx context.Context, sourceMessageID, fileKey string) (string, error)
}

type ReplyFileDirectSender interface {
	ReplyFileDirect(ctx context.Context, sourceMessageID, fileKey string) (string, error)
}

type AutomationSender interface {
	TextSender
	CardSender
}

type RuntimeSender interface {
	TextSender
	ImageSender
	FileSender
	ImageUploader
	FileUploader
}

type ConversationSender interface {
	TextSender
	CardSender
	ReactionSender
	ReplyTextSender
	ReplyTextDirectSender
	ReplyRichTextSender
	ReplyRichTextMarkdownSender
	ReplyRichTextMarkdownDirectSender
	ReplyCardSender
	ReplyCardDirectSender
}
