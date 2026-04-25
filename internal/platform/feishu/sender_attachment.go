package feishu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Alice-space/alice/internal/connector"
)

func (s *FeishuSender) DownloadAttachment(ctx context.Context, resourceScopeKey, sourceMessageID string, attachment *connector.Attachment) error {
	if attachment == nil {
		return errors.New("attachment is nil")
	}
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return errors.New("source message id is empty")
	}
	if strings.TrimSpace(s.resourceDir) == "" {
		return errors.New("resource dir is empty")
	}
	resourceRoot := strings.TrimSpace(s.ResourceRootForScope(resourceScopeKey))
	if resourceRoot == "" {
		return errors.New("resource root is empty")
	}

	kind := strings.ToLower(strings.TrimSpace(attachment.Kind))
	switch kind {
	case "image":
		imageKey := strings.TrimSpace(attachment.ImageKey)
		fileKey := strings.TrimSpace(attachment.FileKey)
		if imageKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, imageKey, "image")
			if err == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
			}
			if fallbackName, fallbackReader, fallbackErr := s.downloadImage(ctx, imageKey); fallbackErr == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fallbackName, fallbackReader, attachment)
			}
			if fileKey == "" {
				return err
			}
		}
		if fileKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
			if err != nil {
				return err
			}
			return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
		}
		return errors.New("image attachment missing image_key and file_key")
	case "sticker":
		fileKey := strings.TrimSpace(attachment.FileKey)
		imageKey := strings.TrimSpace(attachment.ImageKey)
		if fileKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
			if err == nil {
				return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
			}
			if imageKey == "" {
				return err
			}
		}
		if imageKey != "" {
			fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, imageKey, "image")
			if err != nil {
				return err
			}
			return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, imageKey, fileName, fileReader, attachment)
		}
		return errors.New("sticker attachment missing file_key and image_key")
	case "audio", "file":
		fileKey := strings.TrimSpace(attachment.FileKey)
		if fileKey == "" {
			return fmt.Errorf("%s attachment missing file_key", kind)
		}
		fileName, fileReader, err := s.downloadMessageResource(ctx, sourceMessageID, fileKey, "file")
		if err != nil {
			return err
		}
		return s.writeAttachmentFile(resourceRoot, sourceMessageID, kind, fileKey, fileName, fileReader, attachment)
	default:
		return fmt.Errorf("unsupported attachment kind: %s", kind)
	}
}

func (s *FeishuSender) downloadImage(ctx context.Context, imageKey string) (string, io.Reader, error) {
	req := larkim.NewGetImageReqBuilder().
		ImageKey(imageKey).
		Build()
	var fileName string
	var fileReader io.Reader
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.Image.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		if resp.File == nil {
			return errors.New("download image success but file body is empty")
		}
		fileName = strings.TrimSpace(resp.FileName)
		fileReader = resp.File
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return fileName, fileReader, nil
}

func (s *FeishuSender) downloadMessageResource(ctx context.Context, messageID, resourceKey, resourceType string) (string, io.Reader, error) {
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(resourceKey).
		Type(resourceType).
		Build()
	var fileName string
	var fileReader io.Reader
	err := s.withFeishuRetry(ctx, func() error {
		resp, err := s.client.Im.V1.MessageResource.Get(ctx, req)
		if err != nil {
			return err
		}
		if !resp.Success() {
			return &feishuAPIError{Code: resp.Code, Msg: resp.Msg, RequestID: resp.RequestId()}
		}
		if resp.File == nil {
			return errors.New("download message resource success but file body is empty")
		}
		fileName = strings.TrimSpace(resp.FileName)
		fileReader = resp.File
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return fileName, fileReader, nil
}

const maxAttachmentDownloadSize = 50 * 1024 * 1024

func (s *FeishuSender) writeAttachmentFile(
	resourceRoot string,
	sourceMessageID, kind, key, suggestedFileName string,
	reader io.Reader,
	attachment *connector.Attachment,
) error {
	if reader == nil {
		return errors.New("attachment file reader is nil")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxAttachmentDownloadSize))
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("attachment file is empty")
	}
	if int64(len(raw)) >= maxAttachmentDownloadSize {
		return fmt.Errorf("attachment exceeds maximum download size of %d bytes", maxAttachmentDownloadSize)
	}

	subDir := filepath.Join(strings.TrimSpace(resourceRoot), time.Now().Format("2006-01-02"), sanitizePathToken(sourceMessageID))
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		return err
	}

	baseName := sanitizePathToken(strings.TrimSpace(suggestedFileName))
	if baseName == "" {
		baseName = sanitizePathToken(kind + "_" + key)
	}
	baseName = ensureAttachmentExtension(baseName, kind)

	targetPath := filepath.Join(subDir, baseName)
	if _, statErr := os.Stat(targetPath); statErr == nil {
		targetPath = filepath.Join(subDir, ensureAttachmentExtension(sanitizePathToken(kind+"_"+key+"_"+time.Now().Format("150405")), kind))
	}
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return err
	}

	attachment.LocalPath = targetPath
	if strings.TrimSpace(attachment.FileName) == "" {
		attachment.FileName = filepath.Base(targetPath)
	}
	return nil
}

func sanitizePathToken(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", "\n", "_", "\r", "_", "\t", "_", ":", "_")
	value = replacer.Replace(value)
	value = strings.Trim(value, "._")
	if value == "" {
		return "unknown"
	}
	return value
}

func ensureAttachmentExtension(fileName, kind string) string {
	if filepath.Ext(fileName) != "" {
		return fileName
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image", "sticker":
		return fileName + ".img"
	case "audio":
		return fileName + ".audio"
	default:
		return fileName + ".bin"
	}
}

func resolveScopedResourceRoot(baseResourceDir, resourceScopeKey string) string {
	baseResourceDir = strings.TrimSpace(baseResourceDir)
	if baseResourceDir == "" {
		return ""
	}

	scopeType, scopeID := splitResourceScopeKey(resourceScopeKey)
	return filepath.Join(baseResourceDir, "scopes", sanitizeResourcePathSegment(scopeType), sanitizeResourcePathSegment(scopeID))
}

func splitResourceScopeKey(resourceScopeKey string) (string, string) {
	key := strings.TrimSpace(resourceScopeKey)
	if key == "" {
		return "unknown", "unknown"
	}
	scopeType, scopeID, found := strings.Cut(key, ":")
	if !found {
		return "unknown", sanitizeResourcePathSegment(key)
	}
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if scopeType == "" {
		scopeType = "unknown"
	}
	if scopeID == "" {
		scopeID = "unknown"
	}
	return scopeType, scopeID
}

func sanitizeResourcePathSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return "unknown"
	}

	var builder strings.Builder
	builder.Grow(len(segment))
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}
