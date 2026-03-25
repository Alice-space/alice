package connector

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

type senderStub struct {
	mu sync.Mutex

	sendCalls             int
	lastSendText          string
	sendImages            []string
	sendImageCalls        int
	sendFiles             []string
	sendFileCalls         int
	uploadImageErr        error
	uploadFileErr         error
	imageKeyByPath        map[string]string
	fileKeyByPath         map[string]string
	sendCardCalls         int
	lastSendCard          string
	sendCards             []string
	sendCardErr           error
	reactionCalls         int
	reactionErr           error
	reactionTypes         []string
	reactionTargets       []string
	replyTextCalls        int
	replyTextDirectCalls  int
	lastReplyText         string
	replyTexts            []string
	replyTargets          []string
	replyTextErr          error
	replyImageCalls       int
	replyImageDirectCalls int
	replyImages           []string
	replyImageErr         error
	replyFileCalls        int
	replyFileDirectCalls  int
	replyFiles            []string
	replyFileErr          error
	replyRichCalls        int
	lastReplyRich         []string
	replyRichLines        [][]string

	replyRichMarkdownCalls       int
	replyRichMarkdownDirectCalls int
	lastReplyMarkdown            string
	replyMarkdownTexts           []string
	replyRichMarkdownErr         error

	replyCardCalls       int
	replyCardDirectCalls int
	lastReplyCard        string
	replyCards           []string
	replyCardErr         error

	getMessageTextCalls int
	getMessageTextErr   error
	messageTextByID     map[string]string

	downloadCalls            int
	downloadScopeKeys        []string
	downloadSourceMessageIDs []string
	downloadPathByKey        map[string]string
	downloadErrByKey         map[string]error

	resourceRoot string

	resolveUserNameCalls int
	resolveUserNameErr   error
	userNameByIdentity   map[string]string

	resolveChatMemberNameCalls int
	resolveChatMemberNameErr   error
	chatMemberNameByIdentity   map[string]string
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCalls++
	s.lastSendText = text
	return nil
}

func (s *senderStub) SendImage(_ context.Context, _, _ string, imageKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendImageCalls++
	s.sendImages = append(s.sendImages, strings.TrimSpace(imageKey))
	return nil
}

func (s *senderStub) SendFile(_ context.Context, _, _ string, fileKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendFileCalls++
	s.sendFiles = append(s.sendFiles, strings.TrimSpace(fileKey))
	return nil
}

func (s *senderStub) UploadImage(_ context.Context, localPath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadImageErr != nil {
		return "", s.uploadImageErr
	}
	key := ""
	if s.imageKeyByPath != nil {
		key = strings.TrimSpace(s.imageKeyByPath[strings.TrimSpace(localPath)])
	}
	if key == "" {
		key = "img_uploaded"
	}
	return key, nil
}

func (s *senderStub) UploadFile(_ context.Context, localPath, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadFileErr != nil {
		return "", s.uploadFileErr
	}
	key := ""
	if s.fileKeyByPath != nil {
		key = strings.TrimSpace(s.fileKeyByPath[strings.TrimSpace(localPath)])
	}
	if key == "" {
		key = "file_uploaded"
	}
	return key, nil
}

func (s *senderStub) ResourceRootForScope(resourceScopeKey string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := strings.TrimSpace(s.resourceRoot)
	if root == "" {
		return ""
	}
	return resolveScopedResourceRoot(root, resourceScopeKey)
}

func (s *senderStub) SendCard(_ context.Context, _, _ string, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastSendCard = cardContent
	s.sendCards = append(s.sendCards, cardContent)
	return s.sendCardErr
}

func (s *senderStub) AddReaction(_ context.Context, messageID, emojiType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reactionCalls++
	s.reactionTargets = append(s.reactionTargets, strings.TrimSpace(messageID))
	s.reactionTypes = append(s.reactionTypes, strings.ToUpper(strings.TrimSpace(emojiType)))
	return s.reactionErr
}

func (s *senderStub) ReplyText(_ context.Context, sourceMessageID string, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyTextCalls++
	s.lastReplyText = text
	s.replyTexts = append(s.replyTexts, text)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyTextErr != nil {
		return "", s.replyTextErr
	}
	return "om_reply_text", nil
}

func (s *senderStub) ReplyTextDirect(_ context.Context, sourceMessageID string, text string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyTextCalls++
	s.replyTextDirectCalls++
	s.lastReplyText = text
	s.replyTexts = append(s.replyTexts, text)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyTextErr != nil {
		return "", s.replyTextErr
	}
	return "om_reply_text_direct", nil
}

func (s *senderStub) ReplyImage(_ context.Context, sourceMessageID, imageKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyImageCalls++
	s.replyImages = append(s.replyImages, strings.TrimSpace(imageKey))
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyImageErr != nil {
		return "", s.replyImageErr
	}
	return "om_reply_image", nil
}

func (s *senderStub) ReplyImageDirect(_ context.Context, sourceMessageID, imageKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyImageCalls++
	s.replyImageDirectCalls++
	s.replyImages = append(s.replyImages, strings.TrimSpace(imageKey))
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyImageErr != nil {
		return "", s.replyImageErr
	}
	return "om_reply_image_direct", nil
}

func (s *senderStub) ReplyFile(_ context.Context, sourceMessageID, fileKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyFileCalls++
	s.replyFiles = append(s.replyFiles, strings.TrimSpace(fileKey))
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyFileErr != nil {
		return "", s.replyFileErr
	}
	return "om_reply_file", nil
}

func (s *senderStub) ReplyFileDirect(_ context.Context, sourceMessageID, fileKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyFileCalls++
	s.replyFileDirectCalls++
	s.replyFiles = append(s.replyFiles, strings.TrimSpace(fileKey))
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyFileErr != nil {
		return "", s.replyFileErr
	}
	return "om_reply_file_direct", nil
}

func (s *senderStub) ReplyRichText(_ context.Context, sourceMessageID string, lines []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyRichCalls++
	cloned := append([]string(nil), lines...)
	s.lastReplyRich = cloned
	s.replyRichLines = append(s.replyRichLines, cloned)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	return "om_reply_rich", nil
}

func (s *senderStub) ReplyRichTextMarkdown(_ context.Context, sourceMessageID, markdown string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyRichMarkdownCalls++
	s.lastReplyMarkdown = markdown
	s.replyMarkdownTexts = append(s.replyMarkdownTexts, markdown)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyRichMarkdownErr != nil {
		return "", s.replyRichMarkdownErr
	}
	return "om_reply_rich_markdown", nil
}

func (s *senderStub) ReplyRichTextMarkdownDirect(_ context.Context, sourceMessageID, markdown string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyRichMarkdownCalls++
	s.replyRichMarkdownDirectCalls++
	s.lastReplyMarkdown = markdown
	s.replyMarkdownTexts = append(s.replyMarkdownTexts, markdown)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyRichMarkdownErr != nil {
		return "", s.replyRichMarkdownErr
	}
	return "om_reply_rich_markdown_direct", nil
}

func (s *senderStub) ReplyCard(_ context.Context, sourceMessageID string, cardContent string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyCardCalls++
	s.lastReplyCard = cardContent
	s.replyCards = append(s.replyCards, cardContent)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyCardErr != nil {
		return "", s.replyCardErr
	}
	return "om_reply_card", nil
}

func (s *senderStub) ReplyCardDirect(_ context.Context, sourceMessageID string, cardContent string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyCardCalls++
	s.replyCardDirectCalls++
	s.lastReplyCard = cardContent
	s.replyCards = append(s.replyCards, cardContent)
	s.replyTargets = append(s.replyTargets, sourceMessageID)
	if s.replyCardErr != nil {
		return "", s.replyCardErr
	}
	return "om_reply_card_direct", nil
}

func (s *senderStub) GetMessageText(_ context.Context, messageID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getMessageTextCalls++
	if s.getMessageTextErr != nil {
		return "", s.getMessageTextErr
	}
	if s.messageTextByID == nil {
		return "", nil
	}
	return s.messageTextByID[messageID], nil
}

func (s *senderStub) ResolveUserName(_ context.Context, openID, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveUserNameCalls++
	if s.resolveUserNameErr != nil {
		return "", s.resolveUserNameErr
	}
	if s.userNameByIdentity == nil {
		return "", nil
	}

	openKey := "open_id:" + strings.TrimSpace(openID)
	if name, ok := s.userNameByIdentity[openKey]; ok {
		return name, nil
	}
	userKey := "user_id:" + strings.TrimSpace(userID)
	if name, ok := s.userNameByIdentity[userKey]; ok {
		return name, nil
	}
	return "", nil
}

func (s *senderStub) ResolveChatMemberName(_ context.Context, chatID, openID, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveChatMemberNameCalls++
	if s.resolveChatMemberNameErr != nil {
		return "", s.resolveChatMemberNameErr
	}
	if s.chatMemberNameByIdentity == nil {
		return "", nil
	}

	chatKey := "chat_id:" + strings.TrimSpace(chatID) + "|"
	openKey := chatKey + "open_id:" + strings.TrimSpace(openID)
	if name, ok := s.chatMemberNameByIdentity[openKey]; ok {
		return name, nil
	}
	userKey := chatKey + "user_id:" + strings.TrimSpace(userID)
	if name, ok := s.chatMemberNameByIdentity[userKey]; ok {
		return name, nil
	}
	return "", nil
}

func (s *senderStub) DownloadAttachment(_ context.Context, resourceScopeKey, sourceMessageID string, attachment *Attachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.downloadCalls++
	s.downloadScopeKeys = append(s.downloadScopeKeys, strings.TrimSpace(resourceScopeKey))
	s.downloadSourceMessageIDs = append(s.downloadSourceMessageIDs, strings.TrimSpace(sourceMessageID))
	if attachment == nil {
		return errors.New("attachment is nil")
	}

	key := strings.TrimSpace(attachment.ImageKey)
	if key == "" {
		key = strings.TrimSpace(attachment.FileKey)
	}
	if s.downloadErrByKey != nil {
		if err, ok := s.downloadErrByKey[key]; ok && err != nil {
			return err
		}
	}

	localPath := ""
	if s.downloadPathByKey != nil {
		localPath = strings.TrimSpace(s.downloadPathByKey[key])
	}
	if localPath == "" {
		localPath = filepath.Join("/tmp", sanitizePathToken(key))
	}
	attachment.LocalPath = localPath
	if strings.TrimSpace(attachment.FileName) == "" {
		attachment.FileName = filepath.Base(localPath)
	}
	return nil
}

func (s *senderStub) SendCardCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendCardCalls
}

func (s *senderStub) LastSendCard() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSendCard
}
