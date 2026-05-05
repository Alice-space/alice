// Package codex drives the codex CLI as a subprocess and parses its
// JSON-lines output into a plain text reply with optional file-change events.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

const (
	fileChangeCallbackPrefix = "[file_change] "

	defaultChatSandbox   = "workspace-write"
	defaultApprovalMode  = "never"
	envAliceResourceRoot = "ALICE_MCP_RESOURCE_ROOT"
	defaultIdleTimeout   = 15 * time.Minute
	highIdleTimeout      = 30 * time.Minute
	xhighIdleTimeout     = time.Hour
)

var errCodexIdleTimeout = errors.New("codex idle timeout")

// ExecPolicyConfig controls codex sandbox and approval settings.
type ExecPolicyConfig struct {
	Sandbox        string
	AskForApproval string
	AddDirs        []string
}

// Usage holds token counts reported by the codex CLI.
type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

// TotalTokens returns InputTokens + OutputTokens.
func (u Usage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

// HasUsage reports whether any token counts were captured.
func (u Usage) HasUsage() bool {
	return u.InputTokens != 0 || u.CachedInputTokens != 0 || u.OutputTokens != 0
}

type fileDiffStat struct {
	Additions int
	Deletions int
}

type repoDiffSnapshot map[string]fileDiffStat

// Runner executes the codex CLI for a single request.
type Runner struct {
	shared.RunnerBase

	IdleTimeout            time.Duration
	DefaultIdleTimeout     time.Duration
	HighIdleTimeout        time.Duration
	XHighIdleTimeout       time.Duration
	DefaultModel           string
	DefaultReasoningEffort string
	SyntheticDiffGuard     *syntheticDiffRunGuard
}

// Run is a convenience wrapper that runs without thread resumption or progress
// callbacks.
func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, ExecPolicyConfig{}, "", "", "", "", nil, nil)
	return reply, err
}

// RunWithThreadAndProgress runs the codex CLI and returns the final reply and
// next thread ID. It is a convenience wrapper around RunWithThreadAndProgressAndUsage.
func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	policy ExecPolicyConfig,
	model string,
	profile string,
	reasoningEffort string,
	personality string,
	env map[string]string,
	onThinking func(step string),
) (string, string, error) {
	reply, nextThreadID, _, err := r.RunWithThreadAndProgressAndUsage(
		ctx, threadID, userText, policy, model, profile, reasoningEffort, personality, env, onThinking, nil,
	)
	return reply, nextThreadID, err
}

// RunWithThreadAndProgressAndUsage runs the codex CLI and returns the final
// reply, next thread ID, and token usage.
//
//   - threadID: resume an existing session when non-empty.
//   - userText: the fully assembled prompt.
//   - policy: sandbox and approval settings.
//   - model, profile, reasoningEffort, personality: forwarded as CLI flags.
//   - env: merged over the process environment.
//   - onThinking: receives intermediate messages and file-change notifications.
//   - onRawEvent: optional callback for raw stdout events (kind, line, detail);
//     nil disables raw event delivery.
func (r Runner) RunWithThreadAndProgressAndUsage(
	ctx context.Context,
	threadID string,
	userText string,
	policy ExecPolicyConfig,
	model string,
	profile string,
	reasoningEffort string,
	personality string,
	env map[string]string,
	onThinking func(step string),
	onRawEvent func(kind, line, detail string),
) (string, string, Usage, error) {
	reply, nextThreadID, usage, err := r.runAttempt(
		ctx, threadID, userText, policy, model, profile, reasoningEffort, personality, env, onThinking, onRawEvent,
	)
	if err == nil || !errors.Is(err, errCodexIdleTimeout) || strings.TrimSpace(nextThreadID) == "" {
		return reply, nextThreadID, usage, err
	}

	// On idle timeout, retry once by resuming the thread.
	retryReply, retryThreadID, retryUsage, retryErr := r.runAttempt(
		ctx, nextThreadID, idleResumePrompt(), policy, model, profile, reasoningEffort, personality, env, onThinking, onRawEvent,
	)
	usage = mergeUsage(usage, retryUsage)
	if retryErr != nil {
		return "", retryThreadID, usage, fmt.Errorf("codex idle timeout and resume failed: %w", retryErr)
	}
	return retryReply, retryThreadID, usage, nil
}

func (r Runner) runAttempt(
	ctx context.Context,
	threadID string,
	userText string,
	policy ExecPolicyConfig,
	model string,
	profile string,
	reasoningEffort string,
	personality string,
	env map[string]string,
	onThinking func(step string),
	onRawEvent func(kind, line, detail string),
) (string, string, Usage, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(r.DefaultModel)
	}
	profile = strings.TrimSpace(profile)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(r.DefaultReasoningEffort)
	}
	prompt := strings.TrimSpace(userText)
	if prompt == "" {
		return "", "", Usage{}, shared.ErrPromptEmpty
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = shared.DefaultLLMTimeout
	}
	idleTimeout := r.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = r.idleTimeoutForReasoningEffort(reasoningEffort)
	}
	if idleTimeout > timeout {
		idleTimeout = timeout
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := buildExecArgs(threadID, prompt, model, profile, reasoningEffort, personality, r.execPolicy(policy, env))
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	configureInterruptibleCommand(cmd, "codex")
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	cmd.Env = shared.MergeEnv(shared.MergeEnv(os.Environ(), r.Env), env)

	watchedRepos := discoverWatchRepos(cmd.Dir)
	diffGuard := r.SyntheticDiffGuard
	if diffGuard == nil {
		diffGuard = newSyntheticDiffRunGuard()
	}
	repoLease := diffGuard.Acquire(watchedRepos)
	defer diffGuard.Release(repoLease)
	repoSnapshots := captureRepoSnapshots(tctx, watchedRepos)
	activeThreadID := strings.TrimSpace(threadID)
	finalMessage := ""
	sawNativeFileChange := false
	usage := Usage{}

	tryEmitSyntheticFileChanges := func() {
		if onThinking == nil {
			return
		}
		if !diffGuard.CanEmit(repoLease) {
			repoSnapshots = captureRepoSnapshots(tctx, watchedRepos)
			return
		}
		diffMessages, nextSnapshots := collectRepoDiffMessages(tctx, watchedRepos, repoSnapshots)
		repoSnapshots = nextSnapshots
		for _, message := range diffMessages {
			onThinking(fileChangeCallbackPrefix + strings.TrimSpace(message))
		}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", Usage{}, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", Usage{}, fmt.Errorf("create stderr pipe failed: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", Usage{}, fmt.Errorf("start codex process failed: %w", err)
	}

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var stdout bytes.Buffer
	type stdoutEvent struct {
		line string
		err  error
	}
	stdoutEvents := make(chan stdoutEvent, 128)
	stopCh := make(chan struct{})
	defer close(stopCh)
	go func() {
		defer close(stdoutEvents)
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), shared.MaxScannerTokenSize)
		for scanner.Scan() {
			select {
			case stdoutEvents <- stdoutEvent{line: scanner.Text()}:
			case <-stopCh:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case stdoutEvents <- stdoutEvent{err: err}:
			case <-stopCh:
			}
		}
	}()

	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()
	resetIdleTimer := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(idleTimeout)
	}

	var scanErr error
loop:
	for {
		select {
		case <-tctx.Done():
			_ = cmd.Cancel()
			_ = cmd.Wait()
			<-stderrDone
			if errors.Is(tctx.Err(), context.DeadlineExceeded) {
				return "", activeThreadID, usage, shared.ErrLLMTimeout
			}
			return "", activeThreadID, usage, context.Canceled
		case <-idleTimer.C:
			_ = cmd.Cancel()
			_ = cmd.Wait()
			<-stderrDone
			return "", activeThreadID, usage, errCodexIdleTimeout
		case event, ok := <-stdoutEvents:
			if !ok {
				break loop
			}
			if event.err != nil {
				scanErr = event.err
				break loop
			}
			resetIdleTimer()
			line := event.line
			stdout.WriteString(line)
			stdout.WriteByte('\n')

			if onRawEvent != nil {
				onRawEvent("stdout_line", line, "")
			}
			reasoning, agentMessage, fileChangeMessage, parsedThreadID := parseEventLine(line)
			if strings.TrimSpace(parsedThreadID) != "" {
				activeThreadID = strings.TrimSpace(parsedThreadID)
			}
			if parsedUsage := parseUsageLine(line); parsedUsage.HasUsage() {
				usage = parsedUsage
			}
			if onRawEvent != nil && strings.TrimSpace(reasoning) != "" {
				onRawEvent("reasoning", line, strings.TrimSpace(reasoning))
			}
			if onRawEvent != nil {
				if toolCall := parseToolCallLine(line); strings.TrimSpace(toolCall) != "" {
					onRawEvent("tool_call", line, strings.TrimSpace(toolCall))
				}
			}
			_ = reasoning
			if strings.TrimSpace(fileChangeMessage) != "" {
				resolvedMsg := enrichFileChangeMessageStats(tctx, fileChangeMessage, watchedRepos)
				if strings.TrimSpace(resolvedMsg) == "" {
					resolvedMsg = strings.TrimSpace(fileChangeMessage)
				}
				sawNativeFileChange = true
				if onThinking != nil {
					onThinking(fileChangeCallbackPrefix + strings.TrimSpace(resolvedMsg))
				}
			}
			if strings.TrimSpace(agentMessage) != "" {
				finalMessage = strings.TrimSpace(agentMessage)
				if onThinking != nil {
					onThinking(finalMessage)
				}
			}
			if onThinking != nil && !sawNativeFileChange && isSuccessfulCommandExecutionCompleted(line) {
				tryEmitSyntheticFileChanges()
			}
		}
	}

	if scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			return "", activeThreadID, usage, shared.ErrLLMTimeout
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			return "", activeThreadID, usage, context.Canceled
		}
		return "", activeThreadID, usage, fmt.Errorf("read codex output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		return "", activeThreadID, usage, shared.ErrLLMTimeout
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		return "", activeThreadID, usage, context.Canceled
	}
	if err != nil {
		detail := stderrText
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		return "", activeThreadID, usage, fmt.Errorf("codex exec failed: %w (%s)", err, detail)
	}

	if onThinking != nil && !sawNativeFileChange {
		tryEmitSyntheticFileChanges()
	}
	_ = repoSnapshots

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			return "", activeThreadID, usage, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	return finalMessage, activeThreadID, usage, nil
}

func idleResumePrompt() string {
	return "Resume the current thread from its existing state and finish the task. Do not repeat completed analysis. If required repo or file updates are still pending, perform them now, then emit the required final assistant completion message."
}

func (r Runner) idleTimeoutForReasoningEffort(reasoningEffort string) time.Duration {
	defaultTimeout := r.DefaultIdleTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = defaultIdleTimeout
	}
	highTimeout := r.HighIdleTimeout
	if highTimeout <= 0 {
		highTimeout = highIdleTimeout
	}
	xhighTimeout := r.XHighIdleTimeout
	if xhighTimeout <= 0 {
		xhighTimeout = xhighIdleTimeout
	}
	switch strings.ToLower(strings.TrimSpace(reasoningEffort)) {
	case "xhigh":
		return xhighTimeout
	case "high":
		return highTimeout
	default:
		return defaultTimeout
	}
}

func mergeUsage(base Usage, extra Usage) Usage {
	base.InputTokens += extra.InputTokens
	base.CachedInputTokens += extra.CachedInputTokens
	base.OutputTokens += extra.OutputTokens
	return base
}

func (r Runner) execPolicy(policy ExecPolicyConfig, env map[string]string) ExecPolicyConfig {
	policy.Sandbox = strings.TrimSpace(policy.Sandbox)
	if policy.Sandbox == "" {
		policy.Sandbox = defaultChatSandbox
	}
	policy.AskForApproval = strings.TrimSpace(policy.AskForApproval)
	if policy.AskForApproval == "" {
		policy.AskForApproval = defaultApprovalMode
	}
	policy.AddDirs = uniqueAddDirs(policy.AddDirs)
	if resourceRoot := strings.TrimSpace(env[envAliceResourceRoot]); resourceRoot != "" {
		policy.AddDirs = appendUniqueAddDir(policy.AddDirs, resourceRoot)
	}
	return policy
}

func uniqueAddDirs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		out = appendUniqueAddDir(out, raw)
	}
	return out
}

func appendUniqueAddDir(out []string, raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return out
	}
	for _, existing := range out {
		if strings.TrimSpace(existing) == trimmed {
			return out
		}
	}
	return append(out, trimmed)
}
