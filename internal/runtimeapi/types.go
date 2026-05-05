package runtimeapi

import (
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
)

const (
	EnvBaseURL = "ALICE_RUNTIME_API_BASE_URL"
	EnvToken   = "ALICE_RUNTIME_API_TOKEN"
	EnvBin     = "ALICE_RUNTIME_BIN"

	HeaderReceiveIDType   = "X-Alice-Receive-Id-Type"
	HeaderReceiveID       = "X-Alice-Receive-Id"
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
	Prompt         string                `json:"prompt"`
	EverySeconds   int                   `json:"every_seconds,omitempty"`
	CronExpr       string                `json:"cron,omitempty"`
	MaxRuns        int                   `json:"max_runs,omitempty"`
	Fresh          bool                  `json:"fresh,omitempty"`
	Title          string                `json:"title,omitempty"`
	ResumeThreadID string                `json:"resume_thread_id,omitempty"`
	ManageMode     automation.ManageMode `json:"manage_mode,omitempty"`
	NextRunAt      time.Time             `json:"next_run_at,omitempty"`
	Enabled        *bool                 `json:"enabled,omitempty"`
}

// CreateGoalRequest is the API payload for creating or replacing a long-running goal.
type CreateGoalRequest struct {
	Objective  string `json:"objective"`
	DeadlineIn string `json:"deadline_in,omitempty"`
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
