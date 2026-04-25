package runtimeapi

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"

	"github.com/Alice-space/alice/internal/sessionctx"
)

func (s *Server) dispatchText(ctx context.Context, session sessionctx.SessionContext, text string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session.SessionKey, session.ChatType) {
			if replySender, ok := s.sender.(replyTextDirectSender); ok {
				_, err := replySender.ReplyTextDirect(ctx, sourceMessageID, text)
				return err
			}
		}
		if replySender, ok := s.sender.(replyTextSender); ok {
			_, err := replySender.ReplyText(ctx, sourceMessageID, text)
			return err
		}
		return errors.New("sender does not support text replying; cannot dispatch runtime text message as reply")
	}
	return s.sender.SendText(ctx, session.ReceiveIDType, session.ReceiveID, text)
}

func (s *Server) dispatchImage(ctx context.Context, session sessionctx.SessionContext, imageKey string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session.SessionKey, session.ChatType) {
			if replySender, ok := s.sender.(replyImageDirectSender); ok {
				_, err := replySender.ReplyImageDirect(ctx, sourceMessageID, imageKey)
				return err
			}
		}
		if replySender, ok := s.sender.(replyImageSender); ok {
			_, err := replySender.ReplyImage(ctx, sourceMessageID, imageKey)
			return err
		}
		return errors.New("sender does not support image replying; cannot dispatch runtime image message as reply")
	}
	return s.sender.SendImage(ctx, session.ReceiveIDType, session.ReceiveID, imageKey)
}

func (s *Server) dispatchFile(ctx context.Context, session sessionctx.SessionContext, fileKey string) error {
	if sourceMessageID := strings.TrimSpace(session.SourceMessageID); sourceMessageID != "" {
		if !s.prefersThreadReply(session.SessionKey, session.ChatType) {
			if replySender, ok := s.sender.(replyFileDirectSender); ok {
				_, err := replySender.ReplyFileDirect(ctx, sourceMessageID, fileKey)
				return err
			}
		}
		if replySender, ok := s.sender.(replyFileSender); ok {
			_, err := replySender.ReplyFile(ctx, sourceMessageID, fileKey)
			return err
		}
		return errors.New("sender does not support file replying; cannot dispatch runtime file message as reply")
	}
	return s.sender.SendFile(ctx, session.ReceiveIDType, session.ReceiveID, fileKey)
}

func validatePathUnderRoot(path string, root string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path is empty")
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("resource root is empty")
	}
	pathAbs := filepath.Clean(path)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	resolvedPath, err := securejoin.SecureJoin(rootAbs, rel)
	if err != nil {
		return errors.New("path out of allowed root")
	}
	if filepath.Clean(resolvedPath) != pathAbs {
		return errors.New("path out of allowed root")
	}
	return nil
}
