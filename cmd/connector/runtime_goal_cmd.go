package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/runtimeapi"
	"github.com/Alice-space/alice/internal/sessionctx"
)

func newRuntimeGoalCmd() *cobra.Command {
	goalCmd := &cobra.Command{
		Use:   "goal",
		Short: "Manage the long-running goal for the current conversation",
		Args:  cobra.NoArgs,
	}
	goalCmd.AddCommand(
		newRuntimeGoalCreateCmd(),
		newRuntimeGoalGetCmd(),
		newRuntimeGoalPauseCmd(),
		newRuntimeGoalResumeCmd(),
		newRuntimeGoalCompleteCmd(),
		newRuntimeGoalDeleteCmd(),
	)
	return goalCmd
}

func newRuntimeGoalCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [json]",
		Short: "Create a goal from JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			body, err := readRuntimeBodyArgOrStdin(args)
			if err != nil {
				return err
			}
			var req runtimeapi.CreateGoalRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.CreateGoal(ctx, session, req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeGoalGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get the current goal",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.GetGoal(ctx, session)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeGoalPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the current goal",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.GoalPause(ctx, session)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeGoalResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume a paused goal",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.GoalResume(ctx, session)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeGoalCompleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "complete",
		Short: "Mark the current goal as complete",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.GoalComplete(ctx, session)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeGoalDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear the current goal",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session sessionctx.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.DeleteGoal(ctx, session)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}
