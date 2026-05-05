package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/llm"
)

func newDelegateCmd() *cobra.Command {
	var (
		provider     string
		prompt       string
		model        string
		workspaceDir string
		timeout      time.Duration
	)

	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Run a one-shot LLM task via configured CLI backend",
		Long: `Execute a single prompt against a configured LLM provider CLI and print the reply.

The prompt is taken from --prompt or stdin (stdin takes precedence when both are provided).

Supported providers: codex | claude | gemini | kimi | opencode`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider == "" {
				return fmt.Errorf("--provider is required (codex | claude | gemini | kimi | opencode)")
			}

			promptText := strings.TrimSpace(prompt)
			if promptText == "" {
				data, readErr := io.ReadAll(cmd.InOrStdin())
				if readErr != nil {
					return fmt.Errorf("read stdin: %w", readErr)
				}
				if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
					promptText = trimmed
				}
			} else {
				// If --prompt is set but stdin is non-terminal (pipe/file),
				// prefer stdin content over --prompt.
				stat, statErr := os.Stdin.Stat()
				if statErr == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
					data, readErr := io.ReadAll(cmd.InOrStdin())
					if readErr != nil {
						return fmt.Errorf("read stdin: %w", readErr)
					}
					if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
						promptText = trimmed
					}
				}
			}
			if promptText == "" {
				return fmt.Errorf("--prompt is required (or pipe via stdin)")
			}

			cfg := llm.FactoryConfig{
				Provider: provider,
				Codex: llm.CodexConfig{
					Command: "codex", Timeout: timeout,
					WorkspaceDir: workspaceDir, Model: model,
				},
				Claude: llm.ClaudeConfig{
					Command: "claude", Timeout: timeout,
					WorkspaceDir: workspaceDir,
				},
				Gemini: llm.GeminiConfig{
					Command: "gemini", Timeout: timeout,
					WorkspaceDir: workspaceDir,
				},
				Kimi: llm.KimiConfig{
					Command: "kimi", Timeout: timeout,
					WorkspaceDir: workspaceDir,
				},
				OpenCode: llm.OpenCodeConfig{
					Command: "opencode", Timeout: timeout,
					WorkspaceDir: workspaceDir, Model: model,
				},
			}

			backend, err := llm.NewBackend(cfg)
			if err != nil {
				return fmt.Errorf("create backend: %w", err)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+5*time.Second)
			defer cancel()

			result, err := backend.Run(ctx, llm.RunRequest{
				UserText:     promptText,
				Model:        model,
				WorkspaceDir: workspaceDir,
			})
			if err != nil {
				return fmt.Errorf("run: %w", err)
			}

			fmt.Fprint(cmd.OutOrStdout(), result.Reply)
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider: codex | claude | gemini | kimi | opencode")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Task prompt (or pipe via stdin)")
	cmd.Flags().StringVar(&model, "model", "", "Model override")
	cmd.Flags().StringVar(&workspaceDir, "workspace-dir", "", "Working directory")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout")
	_ = cmd.MarkFlagRequired("provider")

	return cmd
}
