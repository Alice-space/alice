package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/bootstrap"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
	bolt "go.etcd.io/bbolt"
)

const localRuntimeStoreOpenTimeout = 10 * time.Second

func deleteRuntimeCampaign(
	ctx context.Context,
	client *runtimeapi.Client,
	session mcpbridge.SessionContext,
	campaignID string,
	deleteRepo bool,
) (map[string]any, error) {
	result, err := client.DeleteCampaign(ctx, session, campaignID, deleteRepo)
	if err == nil {
		return result, nil
	}
	if !shouldFallbackToLocalCampaignDelete(err, campaignID) {
		return nil, err
	}
	return deleteRuntimeCampaignLocally(session, campaignID, deleteRepo)
}

func shouldFallbackToLocalCampaignDelete(err error, campaignID string) bool {
	if err == nil {
		return false
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return false
	}
	path := "/api/v1/campaigns/" + strings.TrimSpace(campaignID)
	return strings.Contains(message, path) && strings.Contains(message, "status=404")
}

func deleteRuntimeCampaignLocally(
	session mcpbridge.SessionContext,
	campaignID string,
	deleteRepo bool,
) (map[string]any, error) {
	campaignStorePath, automationStorePath := localRuntimeStorePaths()

	item, err := loadLocalCampaign(campaignStorePath, campaignID)
	if err != nil {
		return nil, err
	}
	if !localCampaignVisibleToSession(item, session) {
		return nil, errors.New("campaign not found in current scope")
	}
	if !localCanManageCampaign(item, sessionActorID(session)) {
		return nil, errors.New("permission denied for campaign delete")
	}

	deletedRepoPath := ""
	if deleteRepo && strings.TrimSpace(item.CampaignRepoPath) != "" {
		if err := os.RemoveAll(item.CampaignRepoPath); err != nil {
			return nil, err
		}
		deletedRepoPath = item.CampaignRepoPath
	}

	deletedTaskIDs, err := deleteLocalCampaignAutomationTasks(automationStorePath, session, item.ID)
	if err != nil {
		return nil, err
	}
	if err := deleteLocalCampaignRecord(campaignStorePath, item.ID); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":                      "ok",
		"campaign":                    item,
		"deleted_automation_task_ids": deletedTaskIDs,
		"deleted_campaign_repo_path":  deletedRepoPath,
		"delete_mode":                 "local_fallback",
	}, nil
}

func loadLocalCampaign(path, campaignID string) (campaign.Campaign, error) {
	if !localFileExists(path) {
		return campaign.Campaign{}, campaign.ErrCampaignNotFound
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: localRuntimeStoreOpenTimeout})
	if err != nil {
		return campaign.Campaign{}, err
	}
	defer db.Close()

	var item campaign.Campaign
	err = db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("campaigns"))
		if bucket == nil {
			return campaign.ErrCampaignNotFound
		}
		raw := bucket.Get([]byte(strings.TrimSpace(campaignID)))
		if len(raw) == 0 {
			return campaign.ErrCampaignNotFound
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		item = campaign.NormalizeCampaign(item)
		if item.ID == "" {
			return campaign.ErrCampaignNotFound
		}
		return nil
	})
	if err != nil {
		return campaign.Campaign{}, err
	}
	return item, nil
}

func deleteLocalCampaignRecord(path, campaignID string) error {
	if !localFileExists(path) {
		return campaign.ErrCampaignNotFound
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: localRuntimeStoreOpenTimeout})
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("campaigns"))
		if bucket == nil {
			return campaign.ErrCampaignNotFound
		}
		key := []byte(strings.TrimSpace(campaignID))
		if len(bucket.Get(key)) == 0 {
			return campaign.ErrCampaignNotFound
		}
		return bucket.Delete(key)
	})
}

func localRuntimeStorePaths() (string, string) {
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
			root := filepath.Join(dir, "run", "connector")
			campaignPath := filepath.Join(root, "campaigns.db")
			automationPath := filepath.Join(root, "automation.db")
			if localFileExists(campaignPath) || localFileExists(automationPath) {
				return campaignPath, automationPath
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	root := bootstrap.ResolveRuntimeStateRoot(config.AliceHomeDir())
	return filepath.Join(root, "campaigns.db"), filepath.Join(root, "automation.db")
}

func localFileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sessionActorID(session mcpbridge.SessionContext) string {
	if actorID := strings.TrimSpace(session.ActorUserID); actorID != "" {
		return actorID
	}
	return strings.TrimSpace(session.ActorOpenID)
}

func localCampaignVisibleToSession(item campaign.Campaign, session mcpbridge.SessionContext) bool {
	scopeSession := campaign.SessionRoute{
		ScopeKey:      session.SessionKey,
		ReceiveIDType: session.ReceiveIDType,
		ReceiveID:     session.ReceiveID,
		ChatType:      session.ChatType,
	}
	visibilityKey := scopeSession.VisibilityKey()
	return visibilityKey != "" && item.Session.VisibilityKey() == visibilityKey
}

func localCanManageCampaign(item campaign.Campaign, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == item.Creator.PreferredID() || item.ManageMode == campaign.ManageModeScopeAll
}

func localAutomationScope(session mcpbridge.SessionContext) automation.Scope {
	if strings.EqualFold(strings.TrimSpace(session.ChatType), "group") {
		return automation.Scope{Kind: automation.ScopeKindChat, ID: strings.TrimSpace(session.ReceiveID)}
	}
	return automation.Scope{Kind: automation.ScopeKindUser, ID: sessionActorID(session)}
}

func deleteLocalCampaignAutomationTasks(
	path string,
	session mcpbridge.SessionContext,
	campaignID string,
) ([]string, error) {
	if !localFileExists(path) {
		return nil, nil
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: localRuntimeStoreOpenTimeout})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	scope := localAutomationScope(session)
	actorID := sessionActorID(session)
	now := time.Now().Local()
	deleted := make([]string, 0)
	err = db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("tasks"))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(key, value []byte) error {
			var task automation.Task
			if err := json.Unmarshal(value, &task); err != nil {
				return err
			}
			task = automation.NormalizeTask(task)
			if task.Scope != scope || !localCampaignMatchesAutomationTask(task, campaignID) {
				return nil
			}
			if !localCanManageTask(task, actorID) {
				return errors.New("permission denied for campaign-linked task delete")
			}
			task.Status = automation.TaskStatusDeleted
			task.NextRunAt = time.Time{}
			task.Running = false
			task.UpdatedAt = now
			task.Revision++
			if task.DeletedAt.IsZero() {
				task.DeletedAt = now
			}
			raw, err := json.Marshal(automation.NormalizeTask(task))
			if err != nil {
				return err
			}
			if err := bucket.Put(key, raw); err != nil {
				return err
			}
			deleted = append(deleted, task.ID)
			return nil
		})
	})
	return deleted, err
}

func localCanManageTask(task automation.Task, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	return actorID == task.Creator.PreferredID() || task.ManageMode == automation.ManageModeScopeAll
}

func localCampaignMatchesAutomationTask(task automation.Task, campaignID string) bool {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return false
	}
	task = automation.NormalizeTask(task)
	return strings.Contains(task.ID, campaignID) ||
		strings.Contains(task.Title, campaignID) ||
		strings.Contains(task.Action.Prompt, campaignID) ||
		strings.Contains(task.Action.StateKey, campaignID)
}
