package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestLineRPCClientProcessOutlivesStartContext(t *testing.T) {
	startCtx, cancelStart := context.WithCancel(context.Background())
	client, err := startLineRPCClient(
		startCtx,
		os.Args[0],
		[]string{"-test.run=TestLineRPCClientFakeServer"},
		lineRPCOptions{Env: map[string]string{"AGENTBRIDGE_FAKE_RPC": "1"}},
	)
	if err != nil {
		t.Fatalf("startLineRPCClient failed: %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	cancelStart()
	time.Sleep(50 * time.Millisecond)

	reqCtx, cancelReq := context.WithTimeout(context.Background(), time.Second)
	defer cancelReq()
	raw, err := client.Request(reqCtx, "ping", nil)
	if err != nil {
		t.Fatalf("request after start context cancellation failed: %v", err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected result: %s", raw)
	}
}

func TestLineRPCClientFakeServer(t *testing.T) {
	if os.Getenv("AGENTBRIDGE_FAKE_RPC") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		id, ok := msg["id"]
		if !ok {
			continue
		}
		_ = encoder.Encode(map[string]any{
			"id":     id,
			"result": map[string]any{"ok": true},
		})
	}
	os.Exit(0)
}
