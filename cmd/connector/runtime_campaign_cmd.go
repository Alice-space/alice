package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeCampaignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "campaigns",
		Short: "Manage Alice code-army campaigns for the current conversation",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		newRuntimeCampaignListCmd(),
		newRuntimeCampaignCreateCmd(),
		newRuntimeCampaignGetCmd(),
		newRuntimeCampaignPatchCmd(),
		newRuntimeCampaignRepoScanCmd(),
		newRuntimeCampaignRepoReconcileCmd(),
		newRuntimeCampaignTrialUpsertCmd(),
		newRuntimeCampaignGuidanceAddCmd(),
		newRuntimeCampaignReviewAddCmd(),
		newRuntimeCampaignPitfallAddCmd(),
	)
	return cmd
}

func newRuntimeCampaignListCmd() *cobra.Command {
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List campaigns in the current session scope",
		Args:  cobra.NoArgs,
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			_ []string,
		) error {
			result, err := client.ListCampaigns(ctx, session, status, limit)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&status, "status", "", "campaign status filter")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum campaigns to return")
	return cmd
}

func newRuntimeCampaignCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [json]",
		Short: "Create a campaign from JSON",
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
			var req runtimeapi.CreateCampaignRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.CreateCampaign(ctx, session, req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeCampaignGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get CAMPAIGN_ID",
		Short: "Get one campaign",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			result, err := client.GetCampaign(ctx, session, args[0])
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeCampaignPatchCmd() *cobra.Command {
	var contentType string

	cmd := &cobra.Command{
		Use:   "patch CAMPAIGN_ID [json]",
		Short: "Patch one campaign with JSON or merge patch JSON",
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
			result, err := client.PatchCampaign(ctx, session, args[0], contentType, body)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
	cmd.Flags().StringVar(&contentType, "content-type", "application/merge-patch+json", "HTTP content type for patch body")
	return cmd
}

func newRuntimeCampaignTrialUpsertCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upsert-trial CAMPAIGN_ID [json]",
		Short: "Create or update one trial under a campaign",
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
			var req runtimeapi.UpsertTrialRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.UpsertTrial(ctx, session, args[0], req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeCampaignGuidanceAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-guidance CAMPAIGN_ID [json]",
		Short: "Append one guidance record to a campaign",
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
			var req runtimeapi.AddGuidanceRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.AddGuidance(ctx, session, args[0], req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeCampaignReviewAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-review CAMPAIGN_ID [json]",
		Short: "Append one reviewer result to a campaign",
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
			var req runtimeapi.AddReviewRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.AddReview(ctx, session, args[0], req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}

func newRuntimeCampaignPitfallAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-pitfall CAMPAIGN_ID [json]",
		Short: "Append one pitfall record to a campaign",
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
			var req runtimeapi.AddPitfallRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return err
			}
			result, err := client.AddPitfall(ctx, session, args[0], req)
			if err != nil {
				return err
			}
			return printRuntimeJSON(result)
		}),
	}
}
