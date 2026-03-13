package runtimeapi

import (
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
)

const (
	EnvBaseURL = "ALICE_RUNTIME_API_BASE_URL"
	EnvToken   = "ALICE_RUNTIME_API_TOKEN"

	HeaderReceiveIDType   = "X-Alice-Receive-Id-Type"
	HeaderReceiveID       = "X-Alice-Receive-Id"
	HeaderResourceRoot    = "X-Alice-Resource-Root"
	HeaderSourceMessageID = "X-Alice-Source-Message-Id"
	HeaderActorUserID     = "X-Alice-Actor-User-Id"
	HeaderActorOpenID     = "X-Alice-Actor-Open-Id"
	HeaderChatType        = "X-Alice-Chat-Type"
	HeaderSessionKey      = "X-Alice-Session-Key"
)

type TextRequest struct {
	Text string `json:"text"`
}

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

type MemoryWriteRequest struct {
	ScopeType string `json:"scope_type"`
	Content   string `json:"content"`
}

type DailySummaryRequest struct {
	SessionKey string    `json:"session_key,omitempty"`
	Summary    string    `json:"summary"`
	At         time.Time `json:"at,omitempty"`
}

type CreateTaskRequest struct {
	Title      string                `json:"title,omitempty"`
	Schedule   automation.Schedule   `json:"schedule"`
	Action     automation.Action     `json:"action"`
	ManageMode automation.ManageMode `json:"manage_mode,omitempty"`
	MaxRuns    int                   `json:"max_runs,omitempty"`
	Enabled    *bool                 `json:"enabled,omitempty"`
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
