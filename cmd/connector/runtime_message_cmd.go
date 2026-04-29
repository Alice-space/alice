package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/runtimeapi"
	"github.com/Alice-space/alice/internal/sessionctx"
)

func newRuntimeMessageCmd() *cobra.Command {
	messageCmd := &cobra.Command{
		Use:   "message",
		Short: "Send images or files into the current Alice conversation",
		Args:  cobra.NoArgs,
	}
	messageCmd.AddCommand(
		newRuntimeMessageImageCmd(),
		newRuntimeMessageFileCmd(),
	)
	return messageCmd
}

func newRuntimeMessageImageCmd() *cobra.Command {
	var imageKey string
	var path string
	var caption string

	cmd := &cobra.Command{
		Use:   "image",
		Short: "Send an image to the current conversation",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			normalizedPath, err := normalizeRuntimeMessagePath(path)
			if err != nil {
				return err
			}
			if strings.TrimSpace(imageKey) == "" && normalizedPath == "" {
				return fmt.Errorf("image_key or path is required")
			}
			result, err := client.SendImage(ctx, session, runtimeapi.ImageRequest{
				ImageKey: strings.TrimSpace(imageKey),
				Path:     normalizedPath,
				Caption:  strings.TrimSpace(caption),
			})
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&imageKey, "image-key", "", "existing Feishu image_key")
	cmd.Flags().StringVar(&path, "path", "", "local file path to upload")
	cmd.Flags().StringVar(&caption, "caption", "", "optional text sent after the image")
	return cmd
}

func newRuntimeMessageFileCmd() *cobra.Command {
	var fileKey string
	var path string
	var fileName string
	var caption string

	cmd := &cobra.Command{
		Use:   "file",
		Short: "Send a file to the current conversation",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			normalizedPath, err := normalizeRuntimeMessagePath(path)
			if err != nil {
				return err
			}
			if strings.TrimSpace(fileKey) == "" && normalizedPath == "" {
				return fmt.Errorf("file_key or path is required")
			}
			result, err := client.SendFile(ctx, session, runtimeapi.FileRequest{
				FileKey:  strings.TrimSpace(fileKey),
				Path:     normalizedPath,
				FileName: strings.TrimSpace(fileName),
				Caption:  strings.TrimSpace(caption),
			})
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&fileKey, "file-key", "", "existing Feishu file_key")
	cmd.Flags().StringVar(&path, "path", "", "local file path to upload")
	cmd.Flags().StringVar(&fileName, "file-name", "", "optional file name used when uploading")
	cmd.Flags().StringVar(&caption, "caption", "", "optional text sent after the file")
	return cmd
}

func normalizeRuntimeMessagePath(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
