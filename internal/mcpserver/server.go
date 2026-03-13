package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/codearmy"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

const (
	ToolSendImage = "send_image"
	ToolSendFile  = "send_file"
)

type Sender interface {
	SendText(ctx context.Context, receiveIDType, receiveID, text string) error
	SendImage(ctx context.Context, receiveIDType, receiveID, imageKey string) error
	SendFile(ctx context.Context, receiveIDType, receiveID, fileKey string) error
	UploadImage(ctx context.Context, localPath string) (string, error)
	UploadFile(ctx context.Context, localPath, fileName string) (string, error)
}

type replyTextSender interface {
	ReplyText(ctx context.Context, sourceMessageID, text string) (string, error)
}

type replyImageSender interface {
	ReplyImage(ctx context.Context, sourceMessageID, imageKey string) (string, error)
}

type replyFileSender interface {
	ReplyFile(ctx context.Context, sourceMessageID, fileKey string) (string, error)
}

type service struct {
	sender          Sender
	getenv          func(string) string
	getppid         func() int
	readFile        func(string) ([]byte, error)
	runtimeClient   *runtimeapi.Client
	automationStore *automation.Store
	codeArmyStatus  *codearmy.Inspector
}

func New(sender Sender, getenv func(string) string, automationStore *automation.Store, codeArmyStateDir string) (*server.MCPServer, error) {
	if sender == nil {
		return nil, errors.New("sender is nil")
	}
	if getenv == nil {
		getenv = os.Getenv
	}

	svc := &service{
		sender:          sender,
		getenv:          getenv,
		runtimeClient:   runtimeapi.NewClient(getenv(runtimeapi.EnvBaseURL), getenv(runtimeapi.EnvToken)),
		automationStore: automationStore,
		codeArmyStatus:  codearmy.NewInspector(codeArmyStateDir),
	}
	svc.getppid = os.Getppid
	svc.readFile = os.ReadFile
	mcpServer := server.NewMCPServer(
		"alice-feishu-tools",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	mcpServer.AddTool(mcp.NewTool(
		ToolSendImage,
		mcp.WithDescription("向当前会话发送图片。发送目标由会话上下文自动决定：私聊发私聊，群聊有 thread 时优先回复到该 thread。"),
		mcp.WithString("image_key", mcp.Description("已存在的飞书 image_key")),
		mcp.WithString("path", mcp.Description("本地绝对路径，仅允许资源目录白名单路径")),
		mcp.WithString("caption", mcp.Description("可选文字说明，发送在图片之后")),
	), svc.handleSendImage)

	mcpServer.AddTool(mcp.NewTool(
		ToolSendFile,
		mcp.WithDescription("向当前会话发送文件。发送目标由会话上下文自动决定：私聊发私聊，群聊有 thread 时优先回复到该 thread。"),
		mcp.WithString("file_key", mcp.Description("已存在的飞书 file_key")),
		mcp.WithString("path", mcp.Description("本地绝对路径，仅允许资源目录白名单路径")),
		mcp.WithString("file_name", mcp.Description("可选文件名，path上传时生效")),
		mcp.WithString("caption", mcp.Description("可选文字说明，发送在文件之后")),
	), svc.handleSendFile)
	svc.registerAutomationTools(mcpServer)

	return mcpServer, nil
}

func (s *service) handleSendImage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionContext, err := s.loadSessionContext()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	imageKey := strings.TrimSpace(request.GetString("image_key", ""))
	path := strings.TrimSpace(request.GetString("path", ""))
	caption := strings.TrimSpace(request.GetString("caption", ""))
	if imageKey == "" && path == "" {
		return mcp.NewToolResultError("send_image requires image_key or path"), nil
	}
	if s.runtimeClient != nil && s.runtimeClient.IsEnabled() {
		result, runtimeErr := s.runtimeClient.SendImage(ctx, sessionContext, runtimeapi.ImageRequest{
			ImageKey: imageKey,
			Path:     path,
			Caption:  caption,
		})
		if runtimeErr != nil {
			return mcp.NewToolResultError(runtimeErr.Error()), nil
		}
		return mcp.NewToolResultStructured(result, "image sent"), nil
	}
	if imageKey == "" {
		if err := validatePathUnderRoot(path, sessionContext.ResourceRoot); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		uploadedImageKey, uploadErr := s.sender.UploadImage(ctx, path)
		if uploadErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("upload image failed: %v", uploadErr)), nil
		}
		imageKey = strings.TrimSpace(uploadedImageKey)
	}

	if sendErr := s.dispatchImage(ctx, sessionContext, imageKey); sendErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("send image failed: %v", sendErr)), nil
	}
	if caption != "" {
		if captionErr := s.dispatchText(ctx, sessionContext, caption); captionErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("send image caption failed: %v", captionErr)), nil
		}
	}

	return mcp.NewToolResultStructured(map[string]any{
		"status":    "ok",
		"type":      "image",
		"image_key": imageKey,
		"caption":   caption,
	}, "image sent"), nil
}

func (s *service) handleSendFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionContext, err := s.loadSessionContext()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	fileKey := strings.TrimSpace(request.GetString("file_key", ""))
	path := strings.TrimSpace(request.GetString("path", ""))
	fileName := strings.TrimSpace(request.GetString("file_name", ""))
	caption := strings.TrimSpace(request.GetString("caption", ""))
	if fileKey == "" && path == "" {
		return mcp.NewToolResultError("send_file requires file_key or path"), nil
	}
	if s.runtimeClient != nil && s.runtimeClient.IsEnabled() {
		result, runtimeErr := s.runtimeClient.SendFile(ctx, sessionContext, runtimeapi.FileRequest{
			FileKey:  fileKey,
			Path:     path,
			FileName: fileName,
			Caption:  caption,
		})
		if runtimeErr != nil {
			return mcp.NewToolResultError(runtimeErr.Error()), nil
		}
		return mcp.NewToolResultStructured(result, "file sent"), nil
	}
	if fileKey == "" {
		if err := validatePathUnderRoot(path, sessionContext.ResourceRoot); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		uploadedFileKey, uploadErr := s.sender.UploadFile(ctx, path, fileName)
		if uploadErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("upload file failed: %v", uploadErr)), nil
		}
		fileKey = strings.TrimSpace(uploadedFileKey)
	}

	if sendErr := s.dispatchFile(ctx, sessionContext, fileKey); sendErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("send file failed: %v", sendErr)), nil
	}
	if caption != "" {
		if captionErr := s.dispatchText(ctx, sessionContext, caption); captionErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("send file caption failed: %v", captionErr)), nil
		}
	}

	return mcp.NewToolResultStructured(map[string]any{
		"status":   "ok",
		"type":     "file",
		"file_key": fileKey,
		"caption":  caption,
	}, "file sent"), nil
}

func (s *service) loadSessionContext() (mcpbridge.SessionContext, error) {
	sessionContext := mcpbridge.SessionContextFromEnv(s.getenv)
	if err := sessionContext.Validate(); err != nil {
		if s.getppid != nil && s.readFile != nil {
			fallbackContext := mcpbridge.SessionContextFromProcessTree(s.getppid(), s.readFile, 8)
			sessionContext = mcpbridge.MergeSessionContext(sessionContext, fallbackContext)
		}
		if err := sessionContext.Validate(); err != nil {
			return mcpbridge.SessionContext{}, fmt.Errorf(
				"mcp session context invalid: %w (send_image/send_file must run in connector session context)",
				err,
			)
		}
	}
	return sessionContext, nil
}

func (s *service) dispatchText(ctx context.Context, sessionContext mcpbridge.SessionContext, text string) error {
	sourceMessageID := strings.TrimSpace(sessionContext.SourceMessageID)
	if sourceMessageID != "" {
		if replySender, ok := s.sender.(replyTextSender); ok {
			_, err := replySender.ReplyText(ctx, sourceMessageID, text)
			return err
		}
	}
	return s.sender.SendText(ctx, sessionContext.ReceiveIDType, sessionContext.ReceiveID, text)
}

func (s *service) dispatchImage(ctx context.Context, sessionContext mcpbridge.SessionContext, imageKey string) error {
	sourceMessageID := strings.TrimSpace(sessionContext.SourceMessageID)
	if sourceMessageID != "" {
		if replySender, ok := s.sender.(replyImageSender); ok {
			_, err := replySender.ReplyImage(ctx, sourceMessageID, imageKey)
			return err
		}
	}
	return s.sender.SendImage(ctx, sessionContext.ReceiveIDType, sessionContext.ReceiveID, imageKey)
}

func (s *service) dispatchFile(ctx context.Context, sessionContext mcpbridge.SessionContext, fileKey string) error {
	sourceMessageID := strings.TrimSpace(sessionContext.SourceMessageID)
	if sourceMessageID != "" {
		if replySender, ok := s.sender.(replyFileSender); ok {
			_, err := replySender.ReplyFile(ctx, sourceMessageID, fileKey)
			return err
		}
	}
	return s.sender.SendFile(ctx, sessionContext.ReceiveIDType, sessionContext.ReceiveID, fileKey)
}

func validatePathUnderRoot(path string, root string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path is empty")
	}
	if strings.TrimSpace(root) == "" {
		return nil
	}

	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	pathAbs = filepath.Clean(pathAbs)
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path out of allowed root: %s", rootAbs)
	}
	return nil
}
