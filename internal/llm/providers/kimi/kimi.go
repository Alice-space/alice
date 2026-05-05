// Package kimi drives the kimi CLI as a subprocess and parses its
// stream-json output into a plain text reply.
package kimi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm/internal/repodiff"
	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

// Runner executes the kimi CLI for a single request.
type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	WorkspaceDir string
}

// Run is a convenience wrapper that runs without thread resumption or progress
// callbacks.
func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, "", nil, nil, nil)
	return reply, err
}

// RunWithThreadAndProgress runs the kimi CLI and returns the final reply and
// next session ID.
//
//   - threadID: resume an existing session when non-empty.
//   - userText: the fully assembled prompt.
//   - model: overrides the CLI default when non-empty.
//   - env: merged over the process environment.
//   - onProgress: called with intermediate assistant messages; may be nil.
//   - onRawEvent: optional callback for raw stdout events (kind, line, detail);
//     nil disables raw event delivery.
func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	model string,
	env map[string]string,
	onProgress func(step string),
	onRawEvent func(kind, line, detail string),
) (string, string, error) {
	requestedThreadID := strings.TrimSpace(threadID)
	model = strings.TrimSpace(model)
	prompt := strings.TrimSpace(userText)
	if prompt == "" {
		return "", requestedThreadID, shared.ErrPromptEmpty
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = shared.DefaultLLMTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := r.resolvedWorkspaceDir()
	sessionEnv := mergeEnvMap(r.Env, env)
	execThreadID := strings.TrimSpace(requestedThreadID)
	cmdArgs := buildExecArgs(execThreadID, prompt, model)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = shared.MergeEnv(shared.MergeEnv(os.Environ(), r.Env), env)
	diffEmitter := repodiff.NewEmitter(tctx, cmd.Dir, onProgress)
	defer diffEmitter.Close()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", requestedThreadID, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", requestedThreadID, fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", requestedThreadID, fmt.Errorf("start kimi process failed: %w", err)
	}

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var stdout bytes.Buffer
	finalMessage := ""
	activeThreadID := execThreadID

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), shared.MaxScannerTokenSize)
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')

		if onRawEvent != nil {
			onRawEvent("stdout_line", line, "")
		}
		diffEmitter.Emit()
		event := parseEventLine(line)
		if strings.TrimSpace(event.SessionID) != "" {
			activeThreadID = strings.TrimSpace(event.SessionID)
		}
		if onRawEvent != nil && strings.TrimSpace(event.ToolCall) != "" {
			onRawEvent("tool_use", line, strings.TrimSpace(event.ToolCall))
		}
		if strings.TrimSpace(event.Text) != "" {
			finalMessage = strings.TrimSpace(event.Text)
			if onProgress != nil {
				onProgress(finalMessage)
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if strings.TrimSpace(activeThreadID) == "" {
			activeThreadID = r.discoverThreadID(workDir, sessionEnv)
		}
		return "", activeThreadID, fmt.Errorf("read kimi output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	diffEmitter.Emit()
	if strings.TrimSpace(activeThreadID) == "" {
		activeThreadID = r.discoverThreadID(workDir, sessionEnv)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", activeThreadID, errors.New("kimi timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		return "", activeThreadID, context.Canceled
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return "", activeThreadID, fmt.Errorf("kimi exec failed: %w (%s)", err, detail)
	}

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	return finalMessage, activeThreadID, nil
}

func mergeEnvMap(base map[string]string, overrides map[string]string) map[string]string {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(overrides))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func (r Runner) resolvedWorkspaceDir() string {
	workspaceDir := strings.TrimSpace(r.WorkspaceDir)
	if workspaceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		workspaceDir = cwd
	}
	absDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return filepath.Clean(workspaceDir)
	}
	return filepath.Clean(absDir)
}

func (r Runner) discoverThreadID(workDir string, env map[string]string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return ""
	}

	shareDir := strings.TrimSpace(env["KIMI_SHARE_DIR"])
	if shareDir == "" {
		shareDir = strings.TrimSpace(os.Getenv("KIMI_SHARE_DIR"))
	}
	if shareDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(homeDir) == "" {
			return ""
		}
		shareDir = filepath.Join(homeDir, ".kimi")
	}

	if threadID := discoverThreadIDFromMetadata(shareDir, workDir); threadID != "" {
		return threadID
	}
	return discoverThreadIDFromSessionDirs(shareDir, workDir)
}

func discoverThreadIDFromMetadata(shareDir string, workDir string) string {
	raw, err := os.ReadFile(filepath.Join(shareDir, "kimi.json"))
	if err != nil {
		return ""
	}

	var metadata struct {
		WorkDirs []struct {
			Path          string `json:"path"`
			LastSessionID string `json:"last_session_id"`
		} `json:"work_dirs"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return ""
	}

	for _, entry := range metadata.WorkDirs {
		if normalizePath(entry.Path) != normalizePath(workDir) {
			continue
		}
		if strings.TrimSpace(entry.LastSessionID) != "" {
			return strings.TrimSpace(entry.LastSessionID)
		}
	}
	return ""
}

func discoverThreadIDFromSessionDirs(shareDir string, workDir string) string {
	workDirHash := md5.Sum([]byte(normalizePath(workDir)))
	sessionRoot := filepath.Join(shareDir, "sessions", hex.EncodeToString(workDirHash[:]))

	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return ""
	}

	type candidate struct {
		name    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			name:    strings.TrimSpace(entry.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].name > candidates[j].name
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].name
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absPath)
}
