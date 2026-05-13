// Package codex drives the codex CLI as a streaming backend.
package codex

// ExecPolicyConfig controls codex sandbox and approval settings.
type ExecPolicyConfig struct {
	Sandbox        string
	AskForApproval string
	AddDirs        []string
}
