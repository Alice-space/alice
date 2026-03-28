package campaignrepo

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	PlanStatusIdle              = "idle"
	PlanStatusPlanning          = "planning"
	PlanStatusPlanReviewPending = "plan_review_pending"
	PlanStatusPlanReviewing     = "plan_reviewing"
	PlanStatusPlanApproved      = "plan_approved"
	PlanStatusHumanApproved     = "human_approved"
)

func normalizePlanStatus(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "", "idle":
		return PlanStatusIdle
	case "planning":
		return PlanStatusPlanning
	case "plan_review_pending", "plan_review":
		return PlanStatusPlanReviewPending
	case "plan_reviewing":
		return PlanStatusPlanReviewing
	case "plan_approved", "approved":
		return PlanStatusPlanApproved
	case "human_approved":
		return PlanStatusHumanApproved
	default:
		return value
	}
}

func isPlanningPhase(raw string) bool {
	switch normalizePlanStatus(raw) {
	case PlanStatusPlanning, PlanStatusPlanReviewPending, PlanStatusPlanReviewing, PlanStatusPlanApproved:
		return true
	default:
		return false
	}
}

func reconcilePlanPhase(repo *Repository, now time.Time, _ time.Duration) (bool, error) {
	if repo == nil {
		return false, nil
	}
	planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)

	switch planStatus {
	case PlanStatusHumanApproved:
		promoted, err := promoteDraftTasksToReady(repo)
		if err != nil {
			return false, err
		}
		return promoted > 0, nil

	case PlanStatusIdle:
		// If tasks already exist beyond draft, assume planning was done externally
		// (backward compat with campaigns created before the planning phase feature).
		if hasNonDraftTasks(repo.Tasks) {
			repo.Campaign.Frontmatter.PlanStatus = PlanStatusHumanApproved
			return persistCampaignDocument(repo)
		}
		repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanning
		repo.Campaign.Frontmatter.PlanRound = 1
		return persistCampaignDocument(repo)

	case PlanStatusPlanning:
		if hasSubmittedProposal(repo.PlanProposals, repo.Campaign.Frontmatter.PlanRound) {
			repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanReviewPending
			return persistCampaignDocument(repo)
		}
		return false, nil

	case PlanStatusPlanReviewPending, PlanStatusPlanReviewing:
		review, ok := latestPlanReviewForRound(repo.PlanReviews, repo.Campaign.Frontmatter.PlanRound)
		if !ok {
			return false, nil
		}
		return applyPlanVerdict(repo, review)

	case PlanStatusPlanApproved:
		return false, nil
	}

	return false, nil
}

func applyPlanVerdict(repo *Repository, review PlanReviewDocument) (bool, error) {
	verdict := normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking)
	switch verdict {
	case "approve":
		repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanApproved
		if err := promotePlanToMasterPlan(repo); err != nil {
			return false, err
		}
		return persistCampaignDocument(repo)

	case "concern":
		if err := markCurrentProposalSuperseded(repo); err != nil {
			return false, err
		}
		repo.Campaign.Frontmatter.PlanRound++
		repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanning
		return persistCampaignDocument(repo)

	case "blocking", "reject":
		if err := markCurrentProposalSuperseded(repo); err != nil {
			return false, err
		}
		repo.Campaign.Frontmatter.PlanRound++
		repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanning
		return persistCampaignDocument(repo)
	}
	return false, nil
}

func hasNonDraftTasks(tasks []TaskDocument) bool {
	for _, t := range tasks {
		status := normalizeTaskStatus(t.Frontmatter.Status)
		if status != TaskStatusDraft && status != "" {
			return true
		}
	}
	return false
}

func hasSubmittedProposal(proposals []PlanProposalDocument, round int) bool {
	for _, p := range proposals {
		if p.Frontmatter.PlanRound == round && p.Frontmatter.Status == "submitted" {
			return true
		}
	}
	return false
}

func latestPlanReviewForRound(reviews []PlanReviewDocument, round int) (PlanReviewDocument, bool) {
	var chosen PlanReviewDocument
	found := false
	for _, r := range reviews {
		if r.Frontmatter.PlanRound != round {
			continue
		}
		verdict := normalizeReviewVerdict(r.Frontmatter.Verdict, r.Frontmatter.Blocking)
		if verdict == "" {
			continue
		}
		if !found || r.CreatedAt.After(chosen.CreatedAt) {
			chosen = r
			found = true
		}
	}
	return chosen, found
}

func latestProposalForRound(proposals []PlanProposalDocument, round int) (PlanProposalDocument, bool) {
	for i := len(proposals) - 1; i >= 0; i-- {
		if proposals[i].Frontmatter.PlanRound == round {
			return proposals[i], true
		}
	}
	return PlanProposalDocument{}, false
}

func previousProposalAndReview(repo Repository) (proposalPath, reviewPath string) {
	round := repo.Campaign.Frontmatter.PlanRound
	if round <= 1 {
		return "", ""
	}
	prevRound := round - 1
	if p, ok := latestProposalForRound(repo.PlanProposals, prevRound); ok {
		proposalPath = filepath.Join(repo.Root, filepath.FromSlash(p.Path))
	}
	if r, ok := latestPlanReviewForRound(repo.PlanReviews, prevRound); ok {
		reviewPath = filepath.Join(repo.Root, filepath.FromSlash(r.Path))
	}
	return proposalPath, reviewPath
}

func currentProposalPath(repo Repository) string {
	if p, ok := latestProposalForRound(repo.PlanProposals, repo.Campaign.Frontmatter.PlanRound); ok {
		return filepath.Join(repo.Root, filepath.FromSlash(p.Path))
	}
	return ""
}

func promotePlanToMasterPlan(repo *Repository) error {
	proposal, ok := latestProposalForRound(repo.PlanProposals, repo.Campaign.Frontmatter.PlanRound)
	if !ok {
		return nil
	}
	srcPath := filepath.Join(repo.Root, filepath.FromSlash(proposal.Path))
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	dstPath := filepath.Join(repo.Root, "plans", "merged", "master-plan.md")
	return writeFileIfChanged(dstPath, raw)
}

func markCurrentProposalSuperseded(repo *Repository) error {
	round := repo.Campaign.Frontmatter.PlanRound
	for i := range repo.PlanProposals {
		p := &repo.PlanProposals[i]
		if p.Frontmatter.PlanRound != round {
			continue
		}
		if p.Frontmatter.Status == "submitted" || p.Frontmatter.Status == "draft" {
			p.Frontmatter.Status = "superseded"
			if err := writePlanProposalDocument(repo.Root, *p); err != nil {
				return err
			}
		}
	}
	return nil
}

func promoteDraftTasksToReady(repo *Repository) (int, error) {
	promoted := 0
	for i := range repo.Tasks {
		if normalizeTaskStatus(repo.Tasks[i].Frontmatter.Status) != TaskStatusDraft {
			continue
		}
		repo.Tasks[i].Frontmatter.Status = TaskStatusReady
		if err := persistTaskDocument(repo, i); err != nil {
			return promoted, err
		}
		promoted++
	}
	return promoted, nil
}

func persistCampaignDocument(repo *Repository) (bool, error) {
	if repo == nil {
		return false, nil
	}
	repo.Campaign.Frontmatter.PlanStatus = normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	return true, writeCampaignDocument(repo.Root, repo.Campaign)
}

func writeCampaignDocument(root string, doc CampaignDocument) error {
	frontmatter, err := yaml.Marshal(doc.Frontmatter)
	if err != nil {
		return err
	}
	content := strings.TrimRight(string(frontmatter), "\n")
	body := strings.TrimSpace(doc.Body)
	rendered := "---\n" + content + "\n---\n"
	if body != "" {
		rendered += "\n" + body + "\n"
	}
	path := filepath.Join(root, filepath.FromSlash(doc.Path))
	return writeFileIfChanged(path, []byte(rendered))
}

func writePlanProposalDocument(root string, doc PlanProposalDocument) error {
	frontmatter, err := yaml.Marshal(doc.Frontmatter)
	if err != nil {
		return err
	}
	content := strings.TrimRight(string(frontmatter), "\n")
	body := strings.TrimSpace(doc.Body)
	rendered := "---\n" + content + "\n---\n"
	if body != "" {
		rendered += "\n" + body + "\n"
	}
	path := filepath.Join(root, filepath.FromSlash(doc.Path))
	return writeFileIfChanged(path, []byte(rendered))
}
