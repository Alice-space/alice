package connector

import (
	"context"
	"strings"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
)

type heartbeatBlockingBackend struct {
	started chan struct{}
	release chan struct{}
}

func newHeartbeatBlockingBackend() *heartbeatBlockingBackend {
	return &heartbeatBlockingBackend{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *heartbeatBlockingBackend) Run(ctx context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	if req.OnRawEvent != nil {
		req.OnRawEvent(agentbridge.RawEvent{Kind: "tool_use", Detail: "tool_use tool=`bash` command=`pwd`"})
	}
	if req.OnProgress != nil {
		req.OnProgress("[file_change] internal/connector/processor.go已更改，+23-34")
	}
	close(b.started)
	select {
	case <-ctx.Done():
		return agentbridge.RunResult{}, ctx.Err()
	case <-b.release:
		return agentbridge.RunResult{Reply: "最终答复"}, nil
	}
}

func TestProcessor_HeartbeatCardStopsBeforeDoneReaction(t *testing.T) {
	backend := newHeartbeatBlockingBackend()
	sender := &senderStub{}
	processor := NewProcessor(backend, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")
	processor.runtimeMu.Lock()
	processor.heartbeatConfig = llmHeartbeatConfig{
		Enabled:           true,
		FirstSilenceAfter: 10 * time.Millisecond,
		UpdateInterval:    10 * time.Millisecond,
		BackendStaleAfter: time.Minute,
	}
	processor.runtimeMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		processor.ProcessJob(context.Background(), Job{
			ReceiveID:       "oc_chat",
			ReceiveIDType:   "chat_id",
			SourceMessageID: "om_src",
			DisableAck:      true,
			Text:            "hello",
		})
	}()

	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for backend start")
	}
	waitForCondition(t, time.Second, func() bool {
		sender.mu.Lock()
		defer sender.mu.Unlock()
		for _, card := range sender.replyCards {
			if strings.Contains(card, "运行状态") {
				return true
			}
		}
		return false
	}, "timed out waiting for heartbeat card")

	close(backend.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for processor completion")
	}

	sender.mu.Lock()
	patchCount := sender.patchCardCalls
	reactionTypes := append([]string(nil), sender.reactionTypes...)
	patchCards := append([]string(nil), sender.patchCards...)
	sender.mu.Unlock()

	if patchCount == 0 {
		t.Fatal("expected heartbeat card to be patched to a terminal state")
	}
	if len(patchCards) == 0 ||
		!strings.Contains(patchCards[len(patchCards)-1], "已完成") ||
		!strings.Contains(patchCards[len(patchCards)-1], "internal/connector/processor.go已更改") {
		t.Fatalf("final heartbeat patch should include completion and file changes, got %#v", patchCards)
	}
	if len(reactionTypes) != 1 || reactionTypes[0] != finalReplyDoneEmoji {
		t.Fatalf("expected final DONE reaction only, got %#v", reactionTypes)
	}

	time.Sleep(3 * processor.heartbeatConfig.UpdateInterval)
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.patchCardCalls != patchCount {
		t.Fatalf("heartbeat patched after final DONE: before=%d after=%d", patchCount, sender.patchCardCalls)
	}
}
