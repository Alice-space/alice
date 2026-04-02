package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
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
			repo, summary, err := campaignrepo.ScanFromPath(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials)
			if err != nil {
				return err
			}
			return printRuntimeJSON(map[string]any{
				"status":            "ok",
				"campaign":          item,
				"summary":           summary,
				"repository_issues": repo.LoadIssues,
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
			cmd *cobra.Command,
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

func newRuntimeCampaignTaskSelfCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "task-self-check CAMPAIGN_ID TASK_ID KIND",
		Short: "Run post-run self-check for one executor/reviewer task round",
		Args:  cobra.ExactArgs(3),
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
			taskID := strings.TrimSpace(args[1])
			if taskID == "" {
				return errors.New("task id is required")
			}
			kind := campaignrepo.DispatchKind(strings.ToLower(strings.TrimSpace(args[2])))
			switch kind {
			case campaignrepo.DispatchKindExecutor, campaignrepo.DispatchKindReviewer:
			default:
				return fmt.Errorf("kind must be %q or %q", campaignrepo.DispatchKindExecutor, campaignrepo.DispatchKindReviewer)
			}

			validation, err := campaignrepo.RunTaskSelfCheck(item.CampaignRepoPath, taskID, kind, currentTime())
			if err != nil {
				return err
			}
			payload := map[string]any{
				"status":     "ok",
				"campaign":   item,
				"task_id":    taskID,
				"kind":       kind,
				"validation": validation,
			}
			if !validation.Valid {
				payload["status"] = "invalid"
				if printErr := printRuntimeJSON(payload); printErr != nil {
					return printErr
				}
				return validation.Error()
			}
			return printRuntimeJSON(payload)
		}),
	}
}

func newRuntimeCampaignPlanSelfCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan-self-check CAMPAIGN_ID planner|planner_reviewer PLAN_ROUND",
		Short: "Run post-run self-check for one planner/planner-reviewer round",
		Args:  cobra.ExactArgs(3),
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
			kind := campaignrepo.DispatchKind(strings.ToLower(strings.TrimSpace(args[1])))
			switch kind {
			case campaignrepo.DispatchKindPlanner, campaignrepo.DispatchKindPlannerReviewer:
			default:
				return fmt.Errorf("kind must be %q or %q", campaignrepo.DispatchKindPlanner, campaignrepo.DispatchKindPlannerReviewer)
			}
			round, err := strconv.Atoi(strings.TrimSpace(args[2]))
			if err != nil || round <= 0 {
				return errors.New("plan round must be a positive integer")
			}

			validation, err := campaignrepo.RunPlanSelfCheck(item.CampaignRepoPath, kind, round, currentTime())
			if err != nil {
				return err
			}
			payload := map[string]any{
				"status":     "ok",
				"campaign":   item,
				"kind":       kind,
				"plan_round": round,
				"validation": validation,
			}
			if !validation.Valid {
				payload["status"] = "invalid"
				if printErr := printRuntimeJSON(payload); printErr != nil {
					return printErr
				}
				return validation.Error()
			}
			return printRuntimeJSON(payload)
		}),
	}
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
			cmd *cobra.Command,
			args []string,
		) error {
			item, err := loadRuntimeCampaign(ctx, client, session, args[0])
			if err != nil {
				return err
			}
			_, validation, err := campaignrepo.ApprovePlan(item.CampaignRepoPath)
			if err != nil {
				return err
			}
			if !validation.Valid {
				return validation.Error()
			}
			roleDefaults, err := currentRuntimeCampaignRoleDefaults(cmd)
			if err != nil {
				return err
			}
			result, err := campaignrepo.ReconcileAndPrepare(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials, 0, roleDefaults)
			if err != nil {
				return err
			}
			liveReportPath, err := campaignrepo.WriteLiveReport(item.CampaignRepoPath, result.Summary)
			if err != nil {
				return err
			}
			if _, _, err := campaignrepo.CommitRepoChanges(item.CampaignRepoPath, "chore(campaign): approve plan and reconcile"); err != nil {
				return err
			}
			patchBody, err := json.Marshal(map[string]string{
				"status":  string(campaign.StatusRunning),
				"summary": result.Summary.SummaryLine(),
			})
			if err != nil {
				return err
			}
			patchResult, err := client.PatchCampaign(ctx, session, item.ID, "application/merge-patch+json", patchBody)
			if err != nil {
				return err
			}
			if updated, err := decodeRuntimeCampaign(patchResult); err == nil {
				item = updated
			}
			syncedDispatchTasks, err := syncRuntimeDispatchTasks(ctx, client, session, item, result.DispatchTasks)
			if err != nil {
				return err
			}
			return printRuntimeJSON(map[string]any{
				"status":                "ok",
				"campaign":              item,
				"repository":            result.Repository,
				"validation":            validation,
				"summary":               result.Summary,
				"dispatch_tasks":        result.DispatchTasks,
				"synced_dispatch_tasks": syncedDispatchTasks,
				"live_report_path":      liveReportPath,
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
			var commitResult campaignrepo.ReconcileCommitResult
			if writeReport {
				commitResult, err = campaignrepo.CommitReconcileSnapshot(item.CampaignRepoPath, &result.Summary)
			} else {
				commitResult, err = campaignrepo.CommitReconcileSnapshot(item.CampaignRepoPath, nil)
			}
			if err != nil {
				return err
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
				"live_report_path":      commitResult.LiveReportPath,
			})
		}),
	}
	cmd.Flags().BoolVar(&writeReport, "write-report", true, "rewrite reports/live-report.md from the reconciled summary")
	cmd.Flags().BoolVar(&updateRuntime, "update-runtime", true, "patch the runtime campaign summary after reconcile")
	cmd.Flags().BoolVar(&syncDispatch, "sync-dispatch", true, "create or update runtime automation tasks for planner/reviewer/executor dispatches")
	return cmd
}

func newRuntimeCampaignTaskGuidanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "task-guidance CAMPAIGN_ID TASK_ID accept|resume GUIDANCE",
		Short: "Apply human guidance to one repo-first task, then reconcile and sync runtime dispatch",
		Args:  cobra.ExactArgs(4),
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
			guidedTask, err := campaignrepo.ApplyTaskHumanGuidance(
				item.CampaignRepoPath,
				args[1],
				args[2],
				args[3],
				currentTime(),
			)
			if err != nil {
				return err
			}
			result, err := campaignrepo.ReconcileAndPrepare(item.CampaignRepoPath, currentTime(), item.MaxParallelTrials, 0, roleDefaults)
			if err != nil {
				return err
			}
			commitResult, err := campaignrepo.CommitReconcileSnapshot(item.CampaignRepoPath, &result.Summary)
			if err != nil {
				return err
			}
			if strings.TrimSpace(item.Summary) != result.Summary.SummaryLine() {
				patchBody, err := json.Marshal(map[string]string{"summary": result.Summary.SummaryLine()})
				if err != nil {
					return err
				}
				patchResult, err := client.PatchCampaign(ctx, session, item.ID, "application/merge-patch+json", patchBody)
				if err != nil {
					return err
				}
				if updated, err := decodeRuntimeCampaign(patchResult); err == nil {
					item = updated
				}
			}
			syncedDispatchTasks, err := syncRuntimeDispatchTasks(ctx, client, session, item, result.DispatchTasks)
			if err != nil {
				return err
			}
			taskAfterReconcile, ok := runtimeCampaignTaskByID(result.Repository, args[1])
			if !ok {
				taskAfterReconcile = guidedTask
			}
			return printRuntimeJSON(map[string]any{
				"status":                "ok",
				"campaign":              item,
				"task":                  taskAfterReconcile,
				"guided_task":           guidedTask,
				"summary":               result.Summary,
				"dispatch_tasks":        result.DispatchTasks,
				"synced_dispatch_tasks": syncedDispatchTasks,
				"live_report_path":      commitResult.LiveReportPath,
			})
		}),
	}
}

func runtimeCampaignTaskByID(repo campaignrepo.Repository, taskID string) (campaignrepo.TaskDocument, bool) {
	taskID = strings.TrimSpace(taskID)
	for _, task := range repo.Tasks {
		if strings.TrimSpace(task.Frontmatter.TaskID) == taskID {
			return task, true
		}
	}
	return campaignrepo.TaskDocument{}, false
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
