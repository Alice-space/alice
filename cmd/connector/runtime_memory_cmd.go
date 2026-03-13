package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeMemoryCmd() *cobra.Command {
	memoryCmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect or update Alice memory for the current conversation",
		Args:  cobra.NoArgs,
	}
	memoryCmd.AddCommand(
		&cobra.Command{
			Use:   "context",
			Short: "Show current memory context",
			Args:  cobra.NoArgs,
			RunE: withRuntimeClient(func(
				ctx context.Context,
				client *runtimeapi.Client,
				session mcpbridge.SessionContext,
				_ *cobra.Command,
				_ []string,
			) error {
				result, err := client.MemoryContext(ctx, session)
				if err != nil {
					return err
				}
				return printRuntimeJSON(result)
			}),
		},
		&cobra.Command{
			Use:   "write-session [content]",
			Short: "Overwrite session-scoped long-term memory",
			Args:  cobra.MaximumNArgs(1),
			RunE: withRuntimeClient(func(
				ctx context.Context,
				client *runtimeapi.Client,
				session mcpbridge.SessionContext,
				_ *cobra.Command,
				args []string,
			) error {
				content, err := readRuntimeTextArgOrStdin(args)
				if err != nil {
					return err
				}
				result, err := client.WriteLongTerm(ctx, session, runtimeapi.MemoryWriteRequest{
					ScopeType: "session",
					Content:   content,
				})
				if err != nil {
					return err
				}
				return printRuntimeJSON(result)
			}),
		},
		&cobra.Command{
			Use:   "write-global [content]",
			Short: "Overwrite global long-term memory",
			Args:  cobra.MaximumNArgs(1),
			RunE: withRuntimeClient(func(
				ctx context.Context,
				client *runtimeapi.Client,
				session mcpbridge.SessionContext,
				_ *cobra.Command,
				args []string,
			) error {
				content, err := readRuntimeTextArgOrStdin(args)
				if err != nil {
					return err
				}
				result, err := client.WriteLongTerm(ctx, session, runtimeapi.MemoryWriteRequest{
					ScopeType: "global",
					Content:   content,
				})
				if err != nil {
					return err
				}
				return printRuntimeJSON(result)
			}),
		},
		&cobra.Command{
			Use:   "daily-summary [summary]",
			Short: "Append a daily summary entry",
			Args:  cobra.MaximumNArgs(1),
			RunE: withRuntimeClient(func(
				ctx context.Context,
				client *runtimeapi.Client,
				session mcpbridge.SessionContext,
				_ *cobra.Command,
				args []string,
			) error {
				summary, err := readRuntimeTextArgOrStdin(args)
				if err != nil {
					return err
				}
				result, err := client.AppendDailySummary(ctx, session, runtimeapi.DailySummaryRequest{
					Summary: summary,
				})
				if err != nil {
					return err
				}
				return printRuntimeJSON(result)
			}),
		},
	)
	return memoryCmd
}
