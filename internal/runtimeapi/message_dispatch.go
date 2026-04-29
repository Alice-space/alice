package runtimeapi

import (
	"context"
	"errors"
	"strings"

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
