package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeAutomationCmd() *cobra.Command {
	automationCmd := &cobra.Command{
		Use:   "automation",
		Short: "Manage Alice automation tasks for the current conversation",
		Args:  cobra.NoArgs,
	}
	automationCmd.AddCommand(
		newRuntimeAutomationListCmd(),
		newRuntimeAutomationCreateCmd(),
		newRuntimeAutomationGetCmd(),
		newRuntimeAutomationPatchCmd(),
		newRuntimeAutomationDeleteCmd(),
	)
	return automationCmd
}

func newRuntimeAutomationListCmd() *cobra.Command {
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks in the current scope",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.ListTasks(ctx, session, status, limit)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&status, "status", "", "task status filter")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum tasks to return")
	return cmd
}

func newRuntimeAutomationCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [json]",
		Short: "Create a task from JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			body, err := readRuntimeBodyArgOrStdin(args)
			if err != nil {
				return err
			}
			var req runtimeapi.CreateTaskRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.CreateTask(ctx, session, req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeAutomationGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get TASK_ID",
		Short: "Get one task",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			result, err := client.GetTask(ctx, session, args[0])
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeAutomationPatchCmd() *cobra.Command {
	var contentType string

	cmd := &cobra.Command{
		Use:   "patch TASK_ID [json]",
		Short: "Patch one task with JSON or merge patch JSON",
		Args:  cobra.RangeArgs(1, 2),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			bodyArgs := []string{}
			if len(args) == 2 {
				bodyArgs = []string{args[1]}
			}
			body, err := readRuntimeBodyArgOrStdin(bodyArgs)
			if err != nil {
				return err
			}
			result, err := client.PatchTask(ctx, session, args[0], contentType, body)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&contentType, "content-type", "application/merge-patch+json", "HTTP content type for patch body")
	return cmd
}

func newRuntimeAutomationDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete TASK_ID",
		Short: "Delete one task",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			result, err := client.DeleteTask(ctx, session, args[0])
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}
