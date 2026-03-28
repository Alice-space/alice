package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/config"
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

func newRuntimeCampaignRepoLintCmd() *cobra.Command {
	var forApproval bool

	cmd := &cobra.Command{
		Use:   "repo-lint CAMPAIGN_ID",
		Short: "Validate one campaign repo against the repo-first contract",
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
			repo, validation, err := validateRuntimeCampaignRepo(item, forApproval)
			if err != nil {
				return err
			}
			if !validation.Valid {
				return validation.Error()
			}
			return printRuntimeJSON(map[string]any{
				"status":     "ok",
				"campaign":   item,
				"repository": repo,
				"validation": validation,
			})
		}),
	}
	cmd.Flags().BoolVar(&forApproval, "for-approval", false, "require plan review/master plan/refined task tree approval gates")
	return cmd
}

func newRuntimeCampaignApprovePlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve-plan CAMPAIGN_ID",
		Short: "Approve a plan only after repo lint, plan review, and merged plan checks all pass",
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
			repo, validation, err := campaignrepo.ApprovePlan(item.CampaignRepoPath)
			if err != nil {
				return err
			}
			if !validation.Valid {
				return validation.Error()
			}
			_, summary, err := campaignrepo.ScanFromPath(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials)
			if err != nil {
				return err
			}
			patchBody, err := json.Marshal(map[string]string{
				"status":  string(campaign.StatusRunning),
				"summary": summary.SummaryLine(),
			})
			if err != nil {
				return err
			}
			result, err := client.PatchCampaign(ctx, session, item.ID, "application/merge-patch+json", patchBody)
			if err != nil {
				return err
			}
			return printRuntimeJSON(map[string]any{
				"status":     "ok",
				"campaign":   result["campaign"],
				"repository": repo,
				"validation": validation,
			})
		}),
	}
}

func newRuntimeCampaignRepoReconcileCmd() *cobra.Command {
	var writeReport bool
	var updateRuntime bool
	var syncDispatch bool

	cmd := &cobra.Command{
		Use:   "repo-reconcile CAMPAIGN_ID",
		Short: "Reconcile one campaign repo and refresh live report",
		Args:  cobra.ExactArgs(1),
		RunE: withRuntimeClient(func(
			ctx context.Context,
			client *runtimeapi.Client,
			session mcpbridge.SessionContext,
			cmd *cobra.Command,
			args []string,
		) error {
			item, err := loadRuntimeCampaign(ctx, client, session, args[0])
			if err != nil {
				return err
			}
			roleDefaults, err := currentRuntimeCampaignRoleDefaults(cmd)
			if err != nil {
				return err
			}
			result, err := campaignrepo.ReconcileAndPrepare(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials, 0, roleDefaults)
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
			syncedDispatchTasks := 0
			if syncDispatch {
				syncedDispatchTasks, err = syncRuntimeDispatchTasks(ctx, client, session, item, result.DispatchTasks)
				if err != nil {
					return err
				}
			}
			return printRuntimeJSON(map[string]any{
				"status":                "ok",
				"campaign":              item,
				"summary":               result.Summary,
				"dispatch_tasks":        result.DispatchTasks,
				"synced_dispatch_tasks": syncedDispatchTasks,
				"live_report_path":      liveReportPath,
			})
		}),
	}
	cmd.Flags().BoolVar(&writeReport, "write-report", true, "rewrite reports/live-report.md from the reconciled summary")
	cmd.Flags().BoolVar(&updateRuntime, "update-runtime", true, "patch the runtime campaign summary after reconcile")
	cmd.Flags().BoolVar(&syncDispatch, "sync-dispatch", true, "create or update runtime automation tasks for planner/reviewer/executor dispatches")
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

func validateRuntimeCampaignRepo(item campaign.Campaign, forApproval bool) (campaignrepo.Repository, campaignrepo.ValidationResult, error) {
	if forApproval {
		return campaignrepo.ValidateForApproval(item.CampaignRepoPath)
	}
	return campaignrepo.Validate(item.CampaignRepoPath)
}

func currentRuntimeCampaignRoleDefaults(cmd *cobra.Command) (campaignrepo.CampaignRoleDefaults, error) {
	runtimeCfg, err := currentRuntimeConfig(cmd)
	if err != nil {
		return campaignrepo.CampaignRoleDefaults{}, err
	}
	return bootstrap.CampaignRoleDefaultsFromConfig(runtimeCfg), nil
}

func currentRuntimeConfig(cmd *cobra.Command) (config.Config, error) {
	configPath := config.DefaultConfigPath()
	if cmd != nil {
		if value, err := cmd.Flags().GetString("config"); err == nil && strings.TrimSpace(value) != "" {
			configPath = value
		}
	}
	cfg, err := config.LoadFromFile(bootstrap.ResolveConfigPath(configPath))
	if err != nil {
		return config.Config{}, err
	}
	runtimes, err := cfg.RuntimeConfigs()
	if err != nil {
		return config.Config{}, err
	}
	return matchRuntimeConfigByBaseURL(runtimes, strings.TrimSpace(os.Getenv(runtimeapi.EnvBaseURL)))
}

func matchRuntimeConfigByBaseURL(runtimes []config.Config, baseURL string) (config.Config, error) {
	if len(runtimes) == 1 {
		return runtimes[0], nil
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, runtimeCfg := range runtimes {
		if strings.TrimRight(runtimeapi.BaseURL(runtimeCfg.RuntimeHTTPAddr), "/") == baseURL {
			return runtimeCfg, nil
		}
	}
	return config.Config{}, fmt.Errorf("no runtime config matches %q", baseURL)
}
