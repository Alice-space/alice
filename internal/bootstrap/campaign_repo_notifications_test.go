package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/campaignrepo"
)

func TestCampaignEventCardTitle_UsesCampaignName(t *testing.T) {
	title := campaignEventCardTitle("Demo Campaign", "camp_demo", campaignrepo.ReconcileEvent{
		Kind:  campaignrepo.EventTaskDispatched,
		Title: "任务已派发执行",
	})
	if title != "Demo Campaign · 任务已派发执行" {
		t.Fatalf("unexpected campaign event title: %q", title)
	}
}

func TestBuildCampaignEventCard_HumanApprovalAddsButtons(t *testing.T) {
	cardContent, err := buildCampaignEventCard("Demo Campaign · 方案评审通过", campaignrepo.ReconcileEvent{
		Kind:       campaignrepo.EventHumanApprovalNeeded,
		CampaignID: "camp_demo",
		PlanRound:  3,
		Title:      "方案评审通过",
		Detail:     "等待人工批准",
		Severity:   "success",
	})
	if err != nil {
		t.Fatalf("build campaign event card failed: %v", err)
	}

	var card struct {
		Elements []map[string]any `json:"elements"`
	}
	if err := json.Unmarshal([]byte(cardContent), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v", err)
	}
	if len(card.Elements) != 2 {
		t.Fatalf("expected legacy markdown + action elements, got %d", len(card.Elements))
	}
	if got := card.Elements[0]["tag"]; got != "div" {
		t.Fatalf("expected legacy div element, got %#v", got)
	}
	action := card.Elements[1]
	if got := action["tag"]; got != "action" {
		t.Fatalf("expected action element, got %#v", got)
	}
	actions, ok := action["actions"].([]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("expected 2 action buttons, got %#v", action["actions"])
	}
	first, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first action payload: %#v", actions[0])
	}
	value, ok := first["value"].(map[string]any)
	if !ok {
		t.Fatalf("expected first action value map, got %#v", first["value"])
	}
	if value["alice_action"] != "campaign_plan_approval" {
		t.Fatalf("unexpected alice_action: %#v", value["alice_action"])
	}
	if value["campaign_id"] != "camp_demo" {
		t.Fatalf("unexpected campaign id: %#v", value["campaign_id"])
	}
	if value["decision"] != "approve" {
		t.Fatalf("unexpected first decision: %#v", value["decision"])
	}
	if value["plan_round"] != float64(3) {
		t.Fatalf("unexpected plan round: %#v", value["plan_round"])
	}
}

func TestBuildCampaignEventCard_NonApprovalKeepsSchemaV2(t *testing.T) {
	cardContent, err := buildCampaignEventCard("Demo Campaign · 任务派发", campaignrepo.ReconcileEvent{
		Kind:     campaignrepo.EventTaskDispatched,
		Title:    "任务已派发执行",
		Detail:   "任务已进入执行阶段",
		Severity: "info",
	})
	if err != nil {
		t.Fatalf("build campaign event card failed: %v", err)
	}

	var card struct {
		Schema string `json:"schema"`
		Body   struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(cardContent), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v", err)
	}
	if card.Schema != "2.0" {
		t.Fatalf("expected schema 2.0, got %q", card.Schema)
	}
	if len(card.Body.Elements) != 1 {
		t.Fatalf("expected single markdown element, got %d", len(card.Body.Elements))
	}
	if got := card.Body.Elements[0]["tag"]; got != "markdown" {
		t.Fatalf("expected markdown element, got %#v", got)
	}
}

func TestBuildCampaignPlanDecisionCard_ApprovedRemovesActions(t *testing.T) {
	cardContent, err := buildCampaignPlanDecisionCard("Demo Campaign", "camp_demo", true, 0)
	if err != nil {
		t.Fatalf("build campaign plan decision card failed: %v", err)
	}

	var card struct {
		Elements []map[string]any `json:"elements"`
	}
	if err := json.Unmarshal([]byte(cardContent), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v", err)
	}
	if len(card.Elements) != 2 {
		t.Fatalf("expected two legacy markdown elements, got %d", len(card.Elements))
	}
	for idx, element := range card.Elements {
		if got := element["tag"]; got != "div" {
			t.Fatalf("expected div element at %d, got %#v", idx, got)
		}
	}
}

func TestBuildCampaignPlanDecisionCard_RejectedShowsNextRound(t *testing.T) {
	cardContent, err := buildCampaignPlanDecisionCard("Demo Campaign", "camp_demo", false, 4)
	if err != nil {
		t.Fatalf("build campaign plan decision card failed: %v", err)
	}

	var card struct {
		Header struct {
			Template string `json:"template"`
		} `json:"header"`
		Elements []map[string]any `json:"elements"`
	}
	if err := json.Unmarshal([]byte(cardContent), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v", err)
	}
	if card.Header.Template != "orange" {
		t.Fatalf("expected orange template, got %q", card.Header.Template)
	}
	if len(card.Elements) == 0 {
		t.Fatalf("expected terminal card elements, got none")
	}
	text, ok := card.Elements[0]["text"].(map[string]any)
	if !ok {
		t.Fatalf("expected legacy text payload, got %#v", card.Elements[0]["text"])
	}
	if content, _ := text["content"].(string); content == "" || !containsRound(content, 4) {
		t.Fatalf("expected next round in content, got %#v", text["content"])
	}
}

func containsRound(content string, round int) bool {
	return content == fmt.Sprintf("Campaign **Demo Campaign** 当前方案未获人工批准，已回到第 %d 轮规划。", round)
}

func mustWriteBootstrapTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed for %s: %v", path, err)
	}
}

func TestLoadPreviousBlockedReasons_ParsesBlockerSection(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "reports", "live-report.md")
	mustWriteBootstrapTestFile(t, reportPath, "---\n"+
		"scope: live\n"+
		"---\n\n"+
		"# Live Report\n\n"+
		"## Blockers\n"+
		"- `T203` [P02] phases/P02/tasks/T203 Retry PMTSim | task T203 declared working_branches repo-a:feat but none exist in target_repos local_path\n"+
		"- `T201` [P02] phases/P02/tasks/T201 Waiting on T102 | dependency T102 not done yet\n\n"+
		"## Next\n"+
		"- no immediate next action\n")

	blocked := loadPreviousBlockedReasons(root)
	if got := blocked["T203"]; got == "" {
		t.Fatalf("expected T203 blocker to be parsed, got %+v", blocked)
	}
	if got := blocked["T201"]; got == "" {
		t.Fatalf("expected T201 blocker to be parsed, got %+v", blocked)
	}
}

func TestNewSummaryBlockedEvents_OnlyNotifiesNewExecutionChainBlockers(t *testing.T) {
	summary := campaignrepo.Summary{
		BlockedTasks: []campaignrepo.TaskSummary{
			{
				TaskID:        "T203",
				Title:         "Retry PMTSim",
				Status:        campaignrepo.TaskStatusReviewPending,
				BlockedReason: "task T203 declared working_branches repo-a:feat but none exist in target_repos local_path",
			},
			{
				TaskID:        "T201",
				Title:         "Waiting on T102",
				Status:        campaignrepo.TaskStatusReady,
				BlockedReason: "dependency `T102` not done yet",
			},
		},
	}

	events := newSummaryBlockedEvents("camp_demo", map[string]string{
		"T101": "old reason",
	}, summary)
	if len(events) != 1 {
		t.Fatalf("expected one new blocker event, got %+v", events)
	}
	if events[0].TaskID != "T203" {
		t.Fatalf("expected T203 event, got %+v", events[0])
	}
	if events[0].Kind != campaignrepo.EventTaskBlocked {
		t.Fatalf("expected task_blocked event, got %+v", events[0])
	}
}

func TestNewSummaryBlockedEvents_NotifiesTrueBlockedTasks(t *testing.T) {
	summary := campaignrepo.Summary{
		BlockedTasks: []campaignrepo.TaskSummary{
			{
				TaskID:        "T301",
				Title:         "Replay Run 9607",
				Status:        campaignrepo.TaskStatusBlocked,
				BlockedReason: "missing IHEP execution surface",
			},
		},
	}

	events := newSummaryBlockedEvents("camp_demo", nil, summary)
	if len(events) != 1 {
		t.Fatalf("expected one blocked event, got %+v", events)
	}
	if events[0].TaskID != "T301" {
		t.Fatalf("expected T301 event, got %+v", events[0])
	}
}

func TestNewSummaryBlockedEvents_SuppressesRepeatedReason(t *testing.T) {
	summary := campaignrepo.Summary{
		BlockedTasks: []campaignrepo.TaskSummary{
			{
				TaskID:        "T203",
				Title:         "Retry PMTSim",
				Status:        campaignrepo.TaskStatusReviewPending,
				BlockedReason: "task T203 declared working_branches repo-a:feat but none exist in target_repos local_path",
			},
		},
	}

	events := newSummaryBlockedEvents("camp_demo", map[string]string{
		"T203": "task T203 declared working_branches repo-a:feat but none exist in target_repos local_path",
	}, summary)
	if len(events) != 0 {
		t.Fatalf("expected repeated blocker reason to be suppressed, got %+v", events)
	}
}
