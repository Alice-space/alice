package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeMessageCmd() *cobra.Command {
	messageCmd := &cobra.Command{
		Use:   "message",
		Short: "Send text, images, or files into the current Alice conversation",
		Args:  cobra.NoArgs,
	}
	messageCmd.AddCommand(
		newRuntimeMessageTextCmd(),
		newRuntimeMessageImageCmd(),
		newRuntimeMessageFileCmd(),
	)
	return messageCmd
}

func newRuntimeMessageTextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "text [text]",
		Short: "Send text to the current conversation",
		Args:  cobra.MaximumNArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			text, err := readRuntimeTextArgOrStdin(args)
			if err != nil {
				return err
			}
			result, err := client.SendText(ctx, session, runtimeapi.TextRequest{Text: text})
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
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
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			if strings.TrimSpace(imageKey) == "" && strings.TrimSpace(path) == "" {
				return fmt.Errorf("image_key or path is required")
			}
			result, err := client.SendImage(ctx, session, runtimeapi.ImageRequest{
				ImageKey: strings.TrimSpace(imageKey),
				Path:     strings.TrimSpace(path),
				Caption:  strings.TrimSpace(caption),
			})
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&imageKey, "image-key", "", "existing Feishu image_key")
	cmd.Flags().StringVar(&path, "path", "", "local absolute file path to upload")
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
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			if strings.TrimSpace(fileKey) == "" && strings.TrimSpace(path) == "" {
				return fmt.Errorf("file_key or path is required")
			}
			result, err := client.SendFile(ctx, session, runtimeapi.FileRequest{
				FileKey:  strings.TrimSpace(fileKey),
				Path:     strings.TrimSpace(path),
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
	cmd.Flags().StringVar(&path, "path", "", "local absolute file path to upload")
	cmd.Flags().StringVar(&fileName, "file-name", "", "optional file name used when uploading")
	cmd.Flags().StringVar(&caption, "caption", "", "optional text sent after the file")
	return cmd
}
