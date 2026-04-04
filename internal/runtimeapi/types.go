package runtimeapi

import (
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
)

const (
	EnvBaseURL = "ALICE_RUNTIME_API_BASE_URL"
	EnvToken   = "ALICE_RUNTIME_API_TOKEN"
	EnvBin     = "ALICE_RUNTIME_BIN"

	HeaderReceiveIDType   = "X-Alice-Receive-Id-Type"
	HeaderReceiveID       = "X-Alice-Receive-Id"
	HeaderResourceRoot    = "X-Alice-Resource-Root"
	HeaderSourceMessageID = "X-Alice-Source-Message-Id"
	HeaderActorUserID     = "X-Alice-Actor-User-Id"
	HeaderActorOpenID     = "X-Alice-Actor-Open-Id"
	HeaderChatType        = "X-Alice-Chat-Type"
	HeaderSessionKey      = "X-Alice-Session-Key"
)

type ImageRequest struct {
	ImageKey string `json:"image_key,omitempty"`
	Path     string `json:"path,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

type FileRequest struct {
	FileKey  string `json:"file_key,omitempty"`
	Path     string `json:"path,omitempty"`
	FileName string `json:"file_name,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

type CreateTaskRequest struct {
	Title            string                `json:"title,omitempty"`
	Schedule         automation.Schedule   `json:"schedule"`
	Action           automation.Action     `json:"action"`
	ManageMode       automation.ManageMode `json:"manage_mode,omitempty"`
	MaxRuns          int                   `json:"max_runs,omitempty"`
	NextRunAt        time.Time             `json:"next_run_at,omitempty"`
	Enabled          *bool                 `json:"enabled,omitempty"`
	ResumeSessionKey string                `json:"resume_session_key,omitempty"`
}

type CreateCampaignRequest struct {
	Title             string              `json:"title,omitempty"`
	Objective         string              `json:"objective"`
	Repo              string              `json:"repo,omitempty"`
	CampaignRepoPath  string              `json:"campaign_repo_path,omitempty"`
	ManageMode        campaign.ManageMode `json:"manage_mode,omitempty"`
	MaxParallelTrials int                 `json:"max_parallel_trials,omitempty"`
	Summary           string              `json:"summary,omitempty"`
	Baseline          []campaign.Metric   `json:"baseline,omitempty"`
	Gates             []campaign.Gate     `json:"gates,omitempty"`
	Tags              []string            `json:"tags,omitempty"`
}

func BaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}
