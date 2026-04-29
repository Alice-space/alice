package feishu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const feishuImageUploadMaxSize = 10 * 1024 * 1024
const feishuFileUploadMaxSize = 10 * 1024 * 1024

func (s *FeishuSender) SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) error {
	imageKey = strings.TrimSpace(imageKey)
	if imageKey == "" {
		return errors.New("image key is empty")
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("image").
			Content(imageMessageContent(imageKey)).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		return nil
	})
}

func (s *FeishuSender) ReplyImage(ctx context.Context, sourceMessageID, imageKey string) (string, error) {
	imageKey = strings.TrimSpace(imageKey)
	if imageKey == "" {
		return "", errors.New("image key is empty")
	}
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"image",
		imageMessageContent(imageKey),
		"reply image success but response message_id is empty",
	)
}

func (s *FeishuSender) ReplyImageDirect(ctx context.Context, sourceMessageID, imageKey string) (string, error) {
	imageKey = strings.TrimSpace(imageKey)
	if imageKey == "" {
		return "", errors.New("image key is empty")
	}
	return s.replyMessage(
		ctx,
		sourceMessageID,
		"image",
		imageMessageContent(imageKey),
		false,
		"reply image success but response message_id is empty",
	)
}

func (s *FeishuSender) SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) error {
	fileKey = strings.TrimSpace(fileKey)
	if fileKey == "" {
		return errors.New("file key is empty")
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("file").
			Content(fileMessageContent(fileKey)).
			Build()).
		Build()

	return s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Message.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		return nil
	})
}

func (s *FeishuSender) ReplyFile(ctx context.Context, sourceMessageID, fileKey string) (string, error) {
	fileKey = strings.TrimSpace(fileKey)
	if fileKey == "" {
		return "", errors.New("file key is empty")
	}
	return s.replyMessagePreferThread(
		ctx,
		sourceMessageID,
		"file",
		fileMessageContent(fileKey),
		"reply file success but response message_id is empty",
	)
}

func (s *FeishuSender) ReplyFileDirect(ctx context.Context, sourceMessageID, fileKey string) (string, error) {
	fileKey = strings.TrimSpace(fileKey)
	if fileKey == "" {
		return "", errors.New("file key is empty")
	}
	return s.replyMessage(
		ctx,
		sourceMessageID,
		"file",
		fileMessageContent(fileKey),
		false,
		"reply file success but response message_id is empty",
	)
}

func (s *FeishuSender) UploadImage(ctx context.Context, localPath string) (string, error) {
	resolvedPath, fileInfo, err := s.resolveUploadPath(localPath)
	if err != nil {
		return "", err
	}
	if fileInfo.Size() > feishuImageUploadMaxSize {
		return "", fmt.Errorf("image file exceeds 10MB limit: %s", resolvedPath)
	}

	// #nosec G304 -- localPath is explicitly provided by an authenticated runtime caller.
	file, err := os.Open(resolvedPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	req := larkim.NewCreateImageReqBuilder().
		Body(larkim.NewCreateImageReqBodyBuilder().
			ImageType("message").
			Image(file).
			Build()).
		Build()

	var imageKey string
	err = s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Image.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.Data == nil || resp.Data.ImageKey == nil {
			return errors.New("upload image success but image key is empty")
		}
		imageKey = strings.TrimSpace(*resp.Data.ImageKey)
		if imageKey == "" {
			return errors.New("upload image success but image key is blank")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return imageKey, nil
}

func (s *FeishuSender) UploadFile(ctx context.Context, localPath, fileName string) (string, error) {
	resolvedPath, fileInfo, err := s.resolveUploadPath(localPath)
	if err != nil {
		return "", err
	}
	if fileInfo.Size() > feishuFileUploadMaxSize {
		return "", fmt.Errorf("file exceeds 10MB limit: %s", resolvedPath)
	}

	// #nosec G304 -- localPath is explicitly provided by an authenticated runtime caller.
	file, err := os.Open(resolvedPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	normalizedFileName := strings.TrimSpace(fileName)
	if normalizedFileName == "" {
		normalizedFileName = filepath.Base(resolvedPath)
	}
	if normalizedFileName == "" || normalizedFileName == "." {
		return "", errors.New("file name is empty")
	}

	req := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType("stream").
			FileName(normalizedFileName).
			File(file).
			Build()).
		Build()

	var fileKey string
	err = s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.File.Create(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{
				Code:      resp.Code,
				Msg:       resp.Msg,
				RequestID: resp.RequestId(),
			}
		}
		if resp.Data == nil || resp.Data.FileKey == nil {
			return errors.New("upload file success but file key is empty")
		}
		fileKey = strings.TrimSpace(*resp.Data.FileKey)
		if fileKey == "" {
			return errors.New("upload file success but file key is blank")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fileKey, nil
}

func (s *FeishuSender) resolveUploadPath(localPath string) (string, os.FileInfo, error) {
	trimmedPath := strings.TrimSpace(localPath)
	if trimmedPath == "" {
		return "", nil, errors.New("local path is empty")
	}

	resolvedPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", nil, err
	}

	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		return "", nil, err
	}
	if fileInfo.IsDir() {
		return "", nil, fmt.Errorf("path is directory: %s", resolvedPath)
	}
	if fileInfo.Size() <= 0 {
		return "", nil, fmt.Errorf("file is empty: %s", resolvedPath)
	}
	return resolvedPath, fileInfo, nil
}
