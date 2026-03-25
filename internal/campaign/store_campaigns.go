package campaign

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Alice-space/alice/internal/storeutil"
)

func (s *Store) ListCampaigns(visibilityKey, statusFilter string, limit int) ([]Campaign, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	visibilityKey = strings.TrimSpace(visibilityKey)
	if visibilityKey == "" {
		return nil, errors.New("visibility key is empty")
	}
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	var campaigns []Campaign
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		filtered := make([]Campaign, 0, len(snapshot.Campaigns))
		for _, raw := range snapshot.Campaigns {
			item := NormalizeCampaign(raw)
			if item.Session.VisibilityKey() != visibilityKey {
				continue
			}
			if statusFilter != "" && statusFilter != "all" && string(item.Status) != statusFilter {
				continue
			}
			filtered = append(filtered, item)
		}
		sort.Slice(filtered, func(i, j int) bool {
			left := filtered[i]
			right := filtered[j]
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.After(right.CreatedAt)
			}
			return left.ID > right.ID
		})
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		campaigns = filtered
		return nil
	})
	if err != nil {
		return nil, err
	}
	return campaigns, nil
}

func (s *Store) ListAllCampaigns(statusFilter string, limit int) ([]Campaign, error) {
	if s == nil {
		return nil, errors.New("store is nil")
	}
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	unlimited := limit < 0
	if !unlimited {
		if limit <= 0 {
			limit = defaultListLimit
		}
		if limit > maxListLimit {
			limit = maxListLimit
		}
	}

	var campaigns []Campaign
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		filtered := make([]Campaign, 0, len(snapshot.Campaigns))
		for _, raw := range snapshot.Campaigns {
			item := NormalizeCampaign(raw)
			if statusFilter != "" && statusFilter != "all" && string(item.Status) != statusFilter {
				continue
			}
			filtered = append(filtered, item)
		}
		sort.Slice(filtered, func(i, j int) bool {
			left := filtered[i]
			right := filtered[j]
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.After(right.CreatedAt)
			}
			return left.ID > right.ID
		})
		if !unlimited && len(filtered) > limit {
			filtered = filtered[:limit]
		}
		campaigns = filtered
		return nil
	})
	if err != nil {
		return nil, err
	}
	return campaigns, nil
}

func (s *Store) GetCampaign(campaignID string) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Campaign{}, errors.New("campaign id is empty")
	}

	var item Campaign
	err := s.viewSnapshot(func(snapshot Snapshot) error {
		idx := findCampaignIndex(snapshot.Campaigns, campaignID)
		if idx < 0 {
			return ErrCampaignNotFound
		}
		item = NormalizeCampaign(snapshot.Campaigns[idx])
		return nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return item, nil
}

func (s *Store) DeleteCampaign(campaignID string) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Campaign{}, errors.New("campaign id is empty")
	}

	var deleted Campaign
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findCampaignIndex(snapshot.Campaigns, campaignID)
		if idx < 0 {
			return false, ErrCampaignNotFound
		}
		deleted = NormalizeCampaign(snapshot.Campaigns[idx])
		snapshot.Campaigns = append(snapshot.Campaigns[:idx], snapshot.Campaigns[idx+1:]...)
		return true, nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return deleted, nil
}

func (s *Store) CreateCampaign(c Campaign) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	c = NormalizeCampaign(c)
	now := s.nowLocal()
	if c.ID == "" {
		c.ID = newID("camp", now)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if err := ValidateCampaign(c); err != nil {
		return Campaign{}, err
	}

	var created Campaign
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		if findCampaignIndex(snapshot.Campaigns, c.ID) >= 0 {
			return false, fmt.Errorf("campaign id already exists: %s", c.ID)
		}
		snapshot.Campaigns = append(snapshot.Campaigns, c)
		created = c
		return true, nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return created, nil
}

func (s *Store) PatchCampaign(campaignID string, mutate func(*Campaign) error) (Campaign, error) {
	if s == nil {
		return Campaign{}, errors.New("store is nil")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return Campaign{}, errors.New("campaign id is empty")
	}
	if mutate == nil {
		return Campaign{}, errors.New("mutate callback is nil")
	}

	var updated Campaign
	err := s.updateSnapshot(func(snapshot *Snapshot) (bool, error) {
		idx := findCampaignIndex(snapshot.Campaigns, campaignID)
		if idx < 0 {
			return false, ErrCampaignNotFound
		}
		item := NormalizeCampaign(snapshot.Campaigns[idx])
		if err := mutate(&item); err != nil {
			return false, err
		}
		item = NormalizeCampaign(item)
		item.UpdatedAt = s.nowLocal()
		item.Revision++
		if err := ValidateCampaign(item); err != nil {
			return false, err
		}
		snapshot.Campaigns[idx] = item
		updated = item
		return true, nil
	})
	if err != nil {
		return Campaign{}, err
	}
	return updated, nil
}

func (s *Store) UpsertTrial(campaignID string, trial Trial) (Campaign, Trial, error) {
	normalized := normalizeTrials([]Trial{trial})
	if len(normalized) == 0 {
		return Campaign{}, Trial{}, errors.New("trial is empty")
	}
	item := normalized[0]
	now := s.nowLocal()
	if item.ID == "" {
		item.ID = newID("trial", now)
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = TrialStatusPlanned
	}

	var persisted Trial
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		idx := findTrialIndex(campaign.Trials, item.ID)
		if idx >= 0 {
			item.CreatedAt = campaign.Trials[idx].CreatedAt
			campaign.Trials[idx] = item
		} else {
			campaign.Trials = append(campaign.Trials, item)
		}
		persisted = item
		return nil
	})
	if err != nil {
		return Campaign{}, Trial{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendGuidance(campaignID string, guidance Guidance) (Campaign, Guidance, error) {
	now := s.nowLocal()
	guidance.ID = strings.TrimSpace(guidance.ID)
	guidance.Source = strings.TrimSpace(guidance.Source)
	guidance.Command = strings.TrimSpace(guidance.Command)
	guidance.Summary = strings.TrimSpace(guidance.Summary)
	if guidance.ID == "" {
		guidance.ID = newID("guidance", now)
	}
	if guidance.CreatedAt.IsZero() {
		guidance.CreatedAt = now
	}
	if guidance.Command == "" && guidance.Summary == "" {
		return Campaign{}, Guidance{}, errors.New("guidance command and summary are both empty")
	}

	var persisted Guidance
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Guidance = append(campaign.Guidance, guidance)
		persisted = guidance
		return nil
	})
	if err != nil {
		return Campaign{}, Guidance{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendReview(campaignID string, review Review) (Campaign, Review, error) {
	now := s.nowLocal()
	review.ID = strings.TrimSpace(review.ID)
	review.ReviewerID = strings.TrimSpace(review.ReviewerID)
	review.Provider = strings.TrimSpace(review.Provider)
	review.Model = strings.TrimSpace(review.Model)
	review.Summary = strings.TrimSpace(review.Summary)
	review.Confidence = strings.TrimSpace(review.Confidence)
	if review.ID == "" {
		review.ID = newID("review", now)
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	}
	if review.Summary == "" && len(review.Findings) == 0 {
		return Campaign{}, Review{}, errors.New("review summary and findings are both empty")
	}

	var persisted Review
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Reviews = append(campaign.Reviews, review)
		persisted = review
		return nil
	})
	if err != nil {
		return Campaign{}, Review{}, err
	}
	return updated, persisted, nil
}

func (s *Store) AppendPitfall(campaignID string, pitfall Pitfall) (Campaign, Pitfall, error) {
	now := s.nowLocal()
	pitfall.ID = strings.TrimSpace(pitfall.ID)
	pitfall.Summary = strings.TrimSpace(pitfall.Summary)
	pitfall.Reason = strings.TrimSpace(pitfall.Reason)
	pitfall.RelatedTrialID = strings.TrimSpace(pitfall.RelatedTrialID)
	pitfall.RetryIf = strings.TrimSpace(pitfall.RetryIf)
	pitfall.Tags = storeutil.UniqueNonEmptyStrings(pitfall.Tags)
	if pitfall.ID == "" {
		pitfall.ID = newID("pitfall", now)
	}
	if pitfall.CreatedAt.IsZero() {
		pitfall.CreatedAt = now
	}
	if pitfall.Summary == "" {
		return Campaign{}, Pitfall{}, errors.New("pitfall summary is empty")
	}

	var persisted Pitfall
	updated, err := s.PatchCampaign(campaignID, func(campaign *Campaign) error {
		campaign.Pitfalls = append(campaign.Pitfalls, pitfall)
		persisted = pitfall
		return nil
	})
	if err != nil {
		return Campaign{}, Pitfall{}, err
	}
	return updated, persisted, nil
}
