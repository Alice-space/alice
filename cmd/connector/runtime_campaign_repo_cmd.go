package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

func newRuntimeCampaignRepoScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "repo-scan CAMPAIGN_ID",
		Short: "Scan one campaign repo and return repo-native task summary",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			item, err := loadRuntimeCampaign(ctx, client, session, args[0])
			if err != nil {
				return err
			}
			_, summary, err := campaignrepo.ScanFromPath(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials)
			if err != nil {
				return err
			}
			return printRuntimeJSON(map[string]any{
				"status":   "ok",
				"campaign": item,
				"summary":  summary,
			})
		}),
	}
}

func newRuntimeCampaignRepoReconcileCmd() *cobra.Command {
	var writeReport bool
	var updateRuntime bool

	cmd := &cobra.Command{
		Use:   "repo-reconcile CAMPAIGN_ID",
		Short: "Reconcile one campaign repo and refresh live report",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			_ *cobra.Command,
			args []string,
		) error {
			item, err := loadRuntimeCampaign(ctx, client, session, args[0])
			if err != nil {
				return err
			}
			result, err := campaignrepo.ReconcileAndPrepare(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials, 0)
			if err != nil {
				return err
			}
			liveReportPath := ""
			if writeReport {
				liveReportPath, err = campaignrepo.WriteLiveReport(item.CampaignRepoPath, result.Summary)
				if err != nil {
					return err
				}
			}
			if updateRuntime && strings.TrimSpace(item.Summary) != result.Summary.SummaryLine() {
				patchBody, err := json.Marshal(map[string]string{"summary": result.Summary.SummaryLine()})
				if err != nil {
					return err
				}
				result, err := client.PatchCampaign(ctx, session, item.ID, "application/merge-patch+json", patchBody)
				if err != nil {
					return err
				}
				if updated, err := decodeRuntimeCampaign(result); err == nil {
					item = updated
				}
			}
			return printRuntimeJSON(map[string]any{
				"status":           "ok",
				"campaign":         item,
				"summary":          result.Summary,
				"dispatch_tasks":   result.DispatchTasks,
				"live_report_path": liveReportPath,
			})
		}),
	}
	cmd.Flags().BoolVar(&writeReport, "write-report", true, "rewrite reports/live-report.md from the reconciled summary")
	cmd.Flags().BoolVar(&updateRuntime, "update-runtime", true, "patch the runtime campaign summary after reconcile")
	return cmd
}

func loadRuntimeCampaign(
	ctx context.Context,
	client *runtimeapi.Client,
	session mcpbridge.SessionContext,
	campaignID string,
) (campaign.Campaign, error) {
	result, err := client.GetCampaign(ctx, session, campaignID)
	if err != nil {
		return campaign.Campaign{}, err
	}
	item, err := decodeRuntimeCampaign(result)
	if err != nil {
		return campaign.Campaign{}, err
	}
	if strings.TrimSpace(item.CampaignRepoPath) == "" {
		return campaign.Campaign{}, errors.New("campaign_repo_path is empty")
	}
	return item, nil
}

func decodeRuntimeCampaign(payload map[string]any) (campaign.Campaign, error) {
	raw, ok := payload["campaign"]
	if !ok {
		return campaign.Campaign{}, errors.New("runtime payload missing campaign")
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return campaign.Campaign{}, err
	}
	var item campaign.Campaign
	if err := json.Unmarshal(body, &item); err != nil {
		return campaign.Campaign{}, err
	}
	return campaign.NormalizeCampaign(item), nil
}

func currentTime() time.Time {
	return time.Now().Local()
}
