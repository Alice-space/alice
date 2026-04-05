package connector

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
)

type codexStub struct {
	resp string
	err  error
}

func (c codexStub) Run(_ context.Context, _ agentbridge.RunRequest) (agentbridge.RunResult, error) {
	return agentbridge.RunResult{Reply: c.resp}, c.err
}

type codexStreamingStub struct {
	resp          string
	err           error
	agentMessages []string
}

func (c codexStreamingStub) Run(_ context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	if req.OnProgress != nil {
		for _, step := range c.agentMessages {
			req.OnProgress(step)
		}
	}
	return agentbridge.RunResult{Reply: c.resp}, c.err
}

type codexCaptureStub struct {
	resp      string
	err       error
	lastInput string
	lastEnv   map[string]string
	lastReq   agentbridge.RunRequest
}

func (c *codexCaptureStub) Run(_ context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	c.lastReq = req
	c.lastInput = req.UserText
	if len(req.Env) == 0 {
		c.lastReq.Env = nil
		c.lastEnv = nil
	} else {
		c.lastReq.Env = make(map[string]string, len(req.Env))
		c.lastEnv = make(map[string]string, len(req.Env))
		for key, value := range req.Env {
			c.lastReq.Env[key] = value
			c.lastEnv[key] = value
		}
	}
	return agentbridge.RunResult{Reply: c.resp}, c.err
}

type codexResumableCaptureStub struct {
	respByCall   []string
	threadByCall []string

	receivedThreadIDs []string
	receivedInputs    []string
}

func (c *codexResumableCaptureStub) Run(_ context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	c.receivedThreadIDs = append(c.receivedThreadIDs, req.ThreadID)
	c.receivedInputs = append(c.receivedInputs, req.UserText)
	idx := len(c.receivedInputs) - 1
	return agentbridge.RunResult{
		Reply:        c.responseForCall(idx),
		NextThreadID: c.threadForCall(idx),
	}, nil
}

func (c *codexResumableCaptureStub) responseForCall(idx int) string {
	if idx >= 0 && idx < len(c.respByCall) {
		return c.respByCall[idx]
	}
	return "ok"
}

func (c *codexResumableCaptureStub) threadForCall(idx int) string {
	if idx >= 0 && idx < len(c.threadByCall) {
		return c.threadByCall[idx]
	}
	return ""
}

type blockingResumableCodexStub struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func newBlockingResumableCodexStub() *blockingResumableCodexStub {
	return &blockingResumableCodexStub{
		release: make(chan struct{}),
	}
}

func (c *blockingResumableCodexStub) Run(
	ctx context.Context,
	req agentbridge.RunRequest,
) (agentbridge.RunResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return agentbridge.RunResult{}, ctx.Err()
	case <-c.release:
		return agentbridge.RunResult{
			Reply:        "- summary",
			NextThreadID: req.ThreadID,
		}, nil
	}
}

func (c *blockingResumableCodexStub) Release() {
	select {
	case <-c.release:
		return
	default:
		close(c.release)
	}
}

func (c *blockingResumableCodexStub) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type interruptibleResumableCodexStub struct {
	mu sync.Mutex

	started       chan struct{}
	firstCallDone chan struct{}

	callCount    int
	threadByCall []string
	inputByCall  []string
}

func newInterruptibleResumableCodexStub() *interruptibleResumableCodexStub {
	return &interruptibleResumableCodexStub{
		started:       make(chan struct{}, 8),
		firstCallDone: make(chan struct{}),
	}
}

func (c *interruptibleResumableCodexStub) Run(
	ctx context.Context,
	req agentbridge.RunRequest,
) (agentbridge.RunResult, error) {
	c.mu.Lock()
	callIndex := c.callCount
	c.callCount++
	c.threadByCall = append(c.threadByCall, strings.TrimSpace(req.ThreadID))
	c.inputByCall = append(c.inputByCall, req.UserText)
	c.mu.Unlock()

	select {
	case c.started <- struct{}{}:
	default:
	}

	if callIndex == 0 {
		select {
		case <-ctx.Done():
			close(c.firstCallDone)
			return agentbridge.RunResult{NextThreadID: "thread_after_interrupt"}, ctx.Err()
		case <-c.firstCallDone:
			return agentbridge.RunResult{Reply: "unexpected release"}, nil
		}
	}

	return agentbridge.RunResult{
		Reply:        "latest answer",
		NextThreadID: strings.TrimSpace(req.ThreadID),
	}, nil
}

func (c *interruptibleResumableCodexStub) WaitForCall(t *testing.T, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-c.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for codex call %d", i+1)
		}
	}
}

func (c *interruptibleResumableCodexStub) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

func (c *interruptibleResumableCodexStub) ThreadIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.threadByCall...)
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
