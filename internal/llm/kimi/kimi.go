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

	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/prompting"
)

type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
	Prompts      *prompting.Loader
}

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", "assistant", userText, "", "", "", nil, nil)
	return reply, err
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	agentName string,
	userText string,
	model string,
	personality string,
	noReplyToken string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	requestedThreadID := strings.TrimSpace(threadID)
	agentName = strings.TrimSpace(agentName)
	model = strings.TrimSpace(model)
	personality = strings.TrimSpace(personality)
	prompt, err := r.renderPrompt(requestedThreadID, userText, personality, noReplyToken)
	if err != nil {
		return "", requestedThreadID, err
	}
	if strings.TrimSpace(prompt) == "" {
		return "", requestedThreadID, errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 172800 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := r.resolvedWorkspaceDir()
	sessionEnv := mergeEnvMap(r.Env, env)
	execThreadID := effectiveThreadID(requestedThreadID, sessionEnv)
	cmdArgs := buildExecArgs(execThreadID, prompt, model)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = mergeEnv(mergeEnv(os.Environ(), r.Env), env)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", strings.TrimSpace(threadID), fmt.Errorf("start kimi process failed: %w", err)
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
	toolCalls := make([]string, 0, 2)
	emitTrace := func(runErr error) {
		logging.DebugAgentTrace(logging.AgentTrace{
			Provider:  "kimi",
			Agent:     agentName,
			ThreadID:  activeThreadID,
			Model:     model,
			Input:     prompt,
			Output:    finalMessage,
			ToolCalls: toolCalls,
			Error:     errorString(runErr),
		})
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')

		event := parseEventLine(line)
		if strings.TrimSpace(event.SessionID) != "" {
			activeThreadID = strings.TrimSpace(event.SessionID)
		}
		if strings.TrimSpace(event.ToolCall) != "" {
			toolCalls = append(toolCalls, strings.TrimSpace(event.ToolCall))
		}
		if strings.TrimSpace(event.Text) != "" {
			finalMessage = strings.TrimSpace(event.Text)
			if onThinking != nil {
				onThinking(finalMessage)
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
		runErr := fmt.Errorf("read kimi output failed: %w", scanErr)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	err = cmd.Wait()
	<-stderrDone
	if strings.TrimSpace(activeThreadID) == "" {
		activeThreadID = r.discoverThreadID(workDir, sessionEnv)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		timeoutErr := errors.New("kimi timeout")
		emitTrace(timeoutErr)
		return "", activeThreadID, timeoutErr
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		emitTrace(context.Canceled)
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
		runErr := fmt.Errorf("kimi exec failed: %w (%s)", err, detail)
		emitTrace(runErr)
		return "", activeThreadID, runErr
	}

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			emitTrace(parseErr)
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	emitTrace(nil)
	return finalMessage, activeThreadID, nil
}

func (r Runner) renderPrompt(threadID string, userText string, personality string, noReplyToken string) (string, error) {
	loader := r.Prompts
	if loader == nil {
		loader = prompting.DefaultLoader()
	}
	promptPrefix, err := prompting.ComposePromptPrefix(r.PromptPrefix, personality, noReplyToken)
	if err != nil {
		return "", err
	}
	return loader.RenderFile("llm/initial_prompt.md.tmpl", map[string]any{
		"Resume":       strings.TrimSpace(threadID) != "",
		"ThreadID":     strings.TrimSpace(threadID),
		"PromptPrefix": promptPrefix,
		"UserText":     strings.TrimSpace(userText),
	})
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := make([]string, len(base))
	copy(env, base)
	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		idx := strings.Index(item, "=")
		if idx <= 0 {
			continue
		}
		indexByKey[item[:idx]] = i
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := overrides[key]
		pair := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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

func effectiveThreadID(threadID string, env map[string]string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID != "" {
		return threadID
	}
	if sessionKey := strings.TrimSpace(env[mcpbridge.EnvSessionKey]); sessionKey != "" {
		return sessionKey
	}
	return ""
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
