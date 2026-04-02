package connector

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
)

const codeArmyTasksListLimit = 5

type codeArmyCampaignMatch struct {
	item  campaign.Campaign
	score int
}

func (p *Processor) buildCodeArmyTasksMarkdown(job Job, query string) string {
	snapshot := p.runtimeSnapshot()
	if snapshot.statusService == nil || !snapshot.statusService.IsAvailable() {
		return "当前还没有挂载 automation / code-army 状态存储，暂时无法执行 `/codearmy tasks`。"
	}

	result := snapshot.statusService.Query(job)
	if result.CampaignError != nil {
		return fmt.Sprintf("Code Army 查询失败：`%s`", sanitizeInlineCode(result.CampaignError.Error()))
	}
	if len(result.Campaigns) == 0 {
		return "当前 scope 下没有活跃的 Code Army campaign。"
	}

	selected, matches, resolution := resolveCodeArmyTaskCampaign(result.Campaigns, query)
	switch resolution {
	case "not_found":
		return buildCodeArmyCampaignSelectionMarkdown(
			"没有找到匹配的 Code Army campaign。",
			query,
			result.Campaigns,
			nil,
		)
	case "ambiguous":
		return buildCodeArmyCampaignSelectionMarkdown(
			"匹配到多个 Code Army campaign，请再给一个更具体的查询词。",
			query,
			result.Campaigns,
			matches,
		)
	case "missing_query":
		return buildCodeArmyCampaignSelectionMarkdown(
			"当前 scope 下有多个活跃 Code Army campaign，请补一个查询词。",
			query,
			result.Campaigns,
			result.Campaigns,
		)
	}

	if strings.TrimSpace(selected.CampaignRepoPath) == "" {
		return fmt.Sprintf("campaign `%s` 的 `campaign_repo_path` 为空，暂时无法读取 task 状态。", sanitizeInlineCode(selected.ID))
	}

	_, summary, err := campaignrepo.ScanFromPath(selected.CampaignRepoPath, p.now().Local(), selected.MaxParallelTrials)
	if err != nil {
		return fmt.Sprintf(
			"读取 campaign `%s` 的 repo 状态失败：`%s`",
			sanitizeInlineCode(selected.ID),
			sanitizeInlineCode(err.Error()),
		)
	}

	lines := []string{
		"## CodeArmy Tasks",
		"",
		fmt.Sprintf("- campaign: `%s`", sanitizeInlineCode(selected.ID)),
	}
	if title := strings.TrimSpace(selected.Title); title != "" {
		lines = append(lines, fmt.Sprintf("- title: %s", title))
	}
	lines = append(lines,
		fmt.Sprintf("- summary: total `%d` | draft `%d` | ready `%d` | active `%d` | review-pending `%d` | accepted `%d` | blocked `%d` | waiting `%d` | done `%d` | rejected `%d`",
			summary.TaskCount,
			summary.DraftCount,
			summary.ReadyCount,
			summary.ActiveCount,
			summary.ReviewPendingCount,
			summary.AcceptedCount,
			summary.BlockedCount,
			summary.WaitingCount,
			summary.DoneCount,
			summary.RejectedCount,
		),
	)
	if summary.CurrentPhase != "" {
		lines = append(lines, fmt.Sprintf("- current phase: `%s`", sanitizeInlineCode(summary.CurrentPhase)))
	}
	if summary.PlanStatus != "" {
		lines = append(lines, fmt.Sprintf("- plan: `%s` (round `%d`)", sanitizeInlineCode(summary.PlanStatus), summary.PlanRound))
	}
	if summary.RepositoryIssueCount > 0 {
		lines = append(lines, fmt.Sprintf("- repository issues: `%d`", summary.RepositoryIssueCount))
	}

	if len(summary.AllTasks) == 0 {
		lines = append(lines, "", "- 当前 campaign repo 里还没有 task package。")
		return strings.Join(lines, "\n")
	}

	phase := ""
	for _, task := range summary.AllTasks {
		taskPhase := strings.TrimSpace(task.Phase)
		if taskPhase == "" {
			taskPhase = "Unspecified"
		}
		if taskPhase != phase {
			phase = taskPhase
			lines = append(lines, "", "### "+phase, "")
		}
		lines = append(lines, formatCodeArmyTaskLine(task))
	}
	return strings.Join(lines, "\n")
}

func resolveCodeArmyTaskCampaign(items []campaign.Campaign, query string) (campaign.Campaign, []campaign.Campaign, string) {
	query = strings.TrimSpace(query)
	if len(items) == 0 {
		return campaign.Campaign{}, nil, "not_found"
	}
	if query == "" {
		if len(items) == 1 {
			return items[0], nil, "ok"
		}
		return campaign.Campaign{}, nil, "missing_query"
	}

	matches := make([]codeArmyCampaignMatch, 0, len(items))
	for _, item := range items {
		score := scoreCodeArmyCampaignMatch(item, query)
		if score <= 0 {
			continue
		}
		matches = append(matches, codeArmyCampaignMatch{item: item, score: score})
	}
	if len(matches) == 0 {
		return campaign.Campaign{}, nil, "not_found"
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		left := campaign.NormalizeCampaign(matches[i].item)
		right := campaign.NormalizeCampaign(matches[j].item)
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID < right.ID
	})

	if len(matches) == 1 || matches[0].score > matches[1].score {
		return matches[0].item, flattenCodeArmyCampaignMatches(matches), "ok"
	}
	return campaign.Campaign{}, flattenCodeArmyCampaignMatches(matches), "ambiguous"
}

func flattenCodeArmyCampaignMatches(matches []codeArmyCampaignMatch) []campaign.Campaign {
	if len(matches) == 0 {
		return nil
	}
	items := make([]campaign.Campaign, 0, len(matches))
	for _, match := range matches {
		items = append(items, match.item)
	}
	return items
}

func scoreCodeArmyCampaignMatch(item campaign.Campaign, query string) int {
	item = campaign.NormalizeCampaign(item)
	query = strings.TrimSpace(query)
	if query == "" {
		return 0
	}

	queryLower := strings.ToLower(query)
	queryCompact := compactCodeArmyMatchText(query)
	queryTokens := strings.Fields(queryLower)
	queryID := strings.TrimPrefix(queryLower, "camp_")

	best := 0
	for _, field := range []struct {
		value    string
		exact    int
		prefix   int
		contains int
	}{
		{value: item.ID, exact: 120, prefix: 110, contains: 100},
		{value: strings.TrimPrefix(item.ID, "camp_"), exact: 118, prefix: 108, contains: 98},
		{value: item.Title, exact: 96, prefix: 90, contains: 84},
		{value: item.Repo, exact: 88, prefix: 82, contains: 76},
		{value: filepath.Base(item.CampaignRepoPath), exact: 80, prefix: 74, contains: 68},
		{value: item.CampaignRepoPath, exact: 72, prefix: 66, contains: 60},
	} {
		best = maxCodeArmyMatchScore(best, scoreCodeArmyMatchField(field.value, queryLower, queryID, queryCompact, queryTokens, field.exact, field.prefix, field.contains))
	}

	combined := strings.ToLower(strings.Join([]string{
		item.ID,
		strings.TrimPrefix(item.ID, "camp_"),
		item.Title,
		item.Repo,
		filepath.Base(item.CampaignRepoPath),
		item.CampaignRepoPath,
	}, " "))
	if len(queryTokens) > 1 && allCodeArmyTokensContained(combined, queryTokens) {
		best = maxCodeArmyMatchScore(best, 58)
	}
	if queryCompact != "" && strings.Contains(compactCodeArmyMatchText(combined), queryCompact) {
		best = maxCodeArmyMatchScore(best, 56)
	}
	return best
}

func scoreCodeArmyMatchField(value, queryLower, queryID, queryCompact string, queryTokens []string, exact, prefix, contains int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	valueLower := strings.ToLower(value)
	valueCompact := compactCodeArmyMatchText(value)

	switch {
	case valueLower == queryLower || (queryID != "" && valueLower == queryID):
		return exact
	case strings.HasPrefix(valueLower, queryLower) || (queryID != "" && strings.HasPrefix(valueLower, queryID)):
		return prefix
	case strings.Contains(valueLower, queryLower) || (queryID != "" && strings.Contains(valueLower, queryID)):
		return contains
	case queryCompact != "" && valueCompact == queryCompact:
		return exact - 1
	case queryCompact != "" && strings.HasPrefix(valueCompact, queryCompact):
		return prefix - 1
	case queryCompact != "" && strings.Contains(valueCompact, queryCompact):
		return contains - 1
	case len(queryTokens) > 1 && allCodeArmyTokensContained(valueLower, queryTokens):
		return contains - 2
	default:
		return 0
	}
}

func allCodeArmyTokensContained(haystack string, tokens []string) bool {
	if strings.TrimSpace(haystack) == "" || len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func compactCodeArmyMatchText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r):
			return unicode.ToLower(r)
		case unicode.IsDigit(r):
			return r
		default:
			return -1
		}
	}, value)
}

func maxCodeArmyMatchScore(left, right int) int {
	if right > left {
		return right
	}
	return left
}

func buildCodeArmyCampaignSelectionMarkdown(lead, query string, all []campaign.Campaign, matches []campaign.Campaign) string {
	lines := []string{
		"## CodeArmy Tasks",
		"",
		lead,
	}
	if query = strings.TrimSpace(query); query != "" {
		lines = append(lines, fmt.Sprintf("- query: `%s`", sanitizeInlineCode(query)))
	}

	shortlist := matches
	if len(shortlist) == 0 {
		shortlist = all
	}
	if len(shortlist) > codeArmyTasksListLimit {
		shortlist = shortlist[:codeArmyTasksListLimit]
	}

	lines = append(lines, "", "可选 campaign：", "")
	for _, item := range shortlist {
		lines = append(lines, formatCodeArmyCampaignSelectionLine(item))
	}
	lines = append(lines, "", "可以用 `/codearmy tasks <部分 id / 标题 / repo>`。")
	return strings.Join(lines, "\n")
}

func formatCodeArmyCampaignSelectionLine(item campaign.Campaign) string {
	item = campaign.NormalizeCampaign(item)
	parts := []string{fmt.Sprintf("- `%s`", sanitizeInlineCode(item.ID))}
	if title := strings.TrimSpace(item.Title); title != "" {
		parts = append(parts, title)
	}
	parts = append(parts, fmt.Sprintf("status `%s`", sanitizeInlineCode(string(item.Status))))
	if repo := strings.TrimSpace(item.Repo); repo != "" {
		parts = append(parts, fmt.Sprintf("repo `%s`", sanitizeInlineCode(repo)))
	}
	return strings.Join(parts, " | ")
}

func formatCodeArmyTaskLine(task campaignrepo.TaskSummary) string {
	parts := []string{
		fmt.Sprintf("- `%s`", sanitizeInlineCode(task.TaskID)),
		fmt.Sprintf("status `%s`", sanitizeInlineCode(task.Status)),
	}
	if title := strings.TrimSpace(task.Title); title != "" {
		parts = append(parts, title)
	}
	switch task.Status {
	case campaignrepo.TaskStatusExecuting, campaignrepo.TaskStatusReviewing:
		if task.DispatchState != "" {
			parts = append(parts, fmt.Sprintf("dispatch `%s`", sanitizeInlineCode(task.DispatchState)))
		}
		if task.OwnerAgent != "" {
			parts = append(parts, fmt.Sprintf("owner `%s`", sanitizeInlineCode(task.OwnerAgent)))
		}
		if !task.LeaseUntil.IsZero() {
			parts = append(parts, fmt.Sprintf("lease `%s`", formatBuiltinStatusTime(task.LeaseUntil)))
		}
	case campaignrepo.TaskStatusReviewPending:
		if task.ReviewStatus != "" {
			parts = append(parts, fmt.Sprintf("review `%s`", sanitizeInlineCode(task.ReviewStatus)))
		}
	case campaignrepo.TaskStatusWaitingExternal:
		if !task.WakeAt.IsZero() {
			parts = append(parts, fmt.Sprintf("wake `%s`", task.WakeAt.Local().Format("2006-01-02 15:04:05")))
		}
	case campaignrepo.TaskStatusBlocked:
		if task.BlockedReason != "" {
			parts = append(parts, task.BlockedReason)
		}
	}
	if task.ExecutionRound > 0 {
		parts = append(parts, fmt.Sprintf("exec round `%d`", task.ExecutionRound))
	}
	if task.ReviewRound > 0 {
		parts = append(parts, fmt.Sprintf("review round `%d`", task.ReviewRound))
	}
	return strings.Join(parts, " | ")
}
