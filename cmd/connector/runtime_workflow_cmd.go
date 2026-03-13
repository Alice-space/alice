package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeWorkflowCmd() *cobra.Command {
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Inspect Alice workflow state for the current conversation",
		Args:  cobra.NoArgs,
	}
	workflowCmd.AddCommand(
		&cobra.Command{
			Use:   "code-army-status [state_key]",
			Short: "Inspect code_army state in the current conversation",
			Args:  cobra.MaximumNArgs(1),
			RunE: withRuntimeClient(func(
				ctx context.Context,
				client *runtimeapi.Client,
				session mcpbridge.SessionContext,
				_ *cobra.Command,
				args []string,
			) error {
				stateKey := ""
				if len(args) > 0 {
					stateKey = args[0]
				}
				result, err := client.CodeArmyStatus(ctx, session, stateKey)
				if err != nil {
					return err
				}
				return printRuntimeJSON(result)
			}),
		},
	)
	return workflowCmd
}
