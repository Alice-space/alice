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
		Short: "Run a one-shot LLM task via streaming backend",
		Long: `Execute a single prompt against a configured LLM provider and print the reply.

The prompt is taken from --prompt or stdin (stdin takes precedence when both are provided).

Supported providers: codex | claude | kimi | opencode`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider == "" {
				return fmt.Errorf("--provider is required (codex | claude | kimi | opencode)")
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
				Kimi: llm.KimiConfig{
					Command: "kimi", Timeout: timeout,
					WorkspaceDir: workspaceDir,
				},
				OpenCode: llm.OpenCodeConfig{
					Command: "opencode", Timeout: timeout,
					WorkspaceDir: workspaceDir, Model: model,
				},
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+5*time.Second)
			defer cancel()

			session, err := llm.NewInteractiveProviderSession(cfg)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			defer session.Close()

			req := llm.RunRequest{
				UserText:     promptText,
				Model:        model,
				WorkspaceDir: workspaceDir,
			}

			result, err := runDelegateTurn(ctx, session, req)
			if err != nil {
				return fmt.Errorf("run: %w", err)
			}

			fmt.Fprint(cmd.OutOrStdout(), result.Reply)
			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider: codex | claude | kimi | opencode")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Task prompt (or pipe via stdin)")
	cmd.Flags().StringVar(&model, "model", "", "Model override")
	cmd.Flags().StringVar(&workspaceDir, "workspace-dir", "", "Working directory")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Timeout")
	_ = cmd.MarkFlagRequired("provider")

	return cmd
}

func runDelegateTurn(ctx context.Context, session *llm.InteractiveSession, req llm.RunRequest) (llm.RunResult, error) {
	submitted, err := session.Submit(ctx, req)
	if err != nil {
		return llm.RunResult{}, err
	}

	reply := ""
	nextThreadID := strings.TrimSpace(submitted.ThreadID)
	var usage llm.Usage

	for {
		select {
		case <-ctx.Done():
			return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, ctx.Err()
		case event, ok := <-session.Events():
			if !ok {
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, nil
			}
			if event.TurnID != "" && submitted.TurnID != "" && event.TurnID != submitted.TurnID {
				continue
			}
			if threadID := strings.TrimSpace(event.ThreadID); threadID != "" {
				nextThreadID = threadID
			}
			if event.Usage.HasUsage() {
				usage = event.Usage
			}
			switch event.Kind {
			case llm.TurnEventAssistantText:
				if text := strings.TrimSpace(event.Text); text != "" {
					reply = text
				}
			case llm.TurnEventCompleted:
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, nil
			case llm.TurnEventInterrupted:
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, context.Canceled
			case llm.TurnEventError:
				if event.Err != nil {
					return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, event.Err
				}
				return llm.RunResult{Reply: reply, NextThreadID: nextThreadID, Usage: usage}, fmt.Errorf("turn failed")
			}
		}
	}
}
