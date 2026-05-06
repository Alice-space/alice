package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

type lineRPCClient struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	notifications  chan rpcNotification
	closed         chan struct{}
	includeJSONRPC bool
	defaultHandler func(rpcRequest) any

	nextID  atomic.Uint64
	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan rpcResponse
	stderr  bytes.Buffer
}

type rpcNotification struct {
	Method string
	Params json.RawMessage
	Raw    string
}

type rpcRequest struct {
	ID     string
	Method string
	Params json.RawMessage
	Raw    string
}

type rpcResponse struct {
	Result json.RawMessage
	Err    error
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e rpcError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("json-rpc error code=%d", e.Code)
	}
	return fmt.Sprintf("json-rpc error code=%d message=%s", e.Code, e.Message)
}

func startLineRPCClient(ctx context.Context, command string, args []string, opts lineRPCOptions) (*lineRPCClient, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("command is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(command, args...)
	if strings.TrimSpace(opts.WorkspaceDir) != "" {
		cmd.Dir = strings.TrimSpace(opts.WorkspaceDir)
	}
	cmd.Env = mergeProcessEnv(mergeProcessEnv(os.Environ(), opts.BaseEnv), opts.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe failed: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s failed: %w", command, err)
	}

	c := &lineRPCClient{
		cmd:            cmd,
		stdin:          stdin,
		notifications:  make(chan rpcNotification, 128),
		closed:         make(chan struct{}),
		includeJSONRPC: opts.IncludeJSONRPC,
		defaultHandler: opts.DefaultHandler,
		pending:        make(map[string]chan rpcResponse),
	}
	go c.readStdout(stdout)
	go c.readStderr(stderr)
	return c, nil
}

type lineRPCOptions struct {
	WorkspaceDir   string
	BaseEnv        map[string]string
	Env            map[string]string
	IncludeJSONRPC bool
	DefaultHandler func(rpcRequest) any
}

func (c *lineRPCClient) Notifications() <-chan rpcNotification {
	return c.notifications
}

func (c *lineRPCClient) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrInteractiveClosed
	}
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	payload := map[string]any{
		"id":     id,
		"method": method,
	}
	if c.includeJSONRPC {
		payload["jsonrpc"] = "2.0"
	}
	if params != nil {
		payload["params"] = params
	}
	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(payload); err != nil {
		c.removePending(id)
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp.Result, resp.Err
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.closed:
		c.removePending(id)
		return nil, ErrInteractiveClosed
	}
}

func (c *lineRPCClient) Notify(method string, params any) error {
	payload := map[string]any{"method": method}
	if c.includeJSONRPC {
		payload["jsonrpc"] = "2.0"
	}
	if params != nil {
		payload["params"] = params
	}
	return c.write(payload)
}

func (c *lineRPCClient) Close() error {
	if c == nil || c.cmd == nil {
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

func (c *lineRPCClient) write(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.closed:
		return ErrInteractiveClosed
	default:
	}
	if _, err := c.stdin.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *lineRPCClient) readStdout(stdout io.Reader) {
	defer close(c.closed)
	defer close(c.notifications)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, shared.DefaultScannerBuf), shared.MaxScannerTokenSize10MB)
	for scanner.Scan() {
		line := scanner.Text()
		var msg map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if rawID, ok := msg["id"]; ok {
			id := trimJSONID(rawID)
			if _, hasMethod := msg["method"]; hasMethod {
				c.handleServerRequest(id, msg, line)
				continue
			}
			c.deliverResponse(id, msg)
			continue
		}
		if rawMethod, ok := msg["method"]; ok {
			var method string
			_ = json.Unmarshal(rawMethod, &method)
			c.notifications <- rpcNotification{Method: method, Params: msg["params"], Raw: line}
		}
	}
}

func (c *lineRPCClient) readStderr(stderr io.Reader) {
	_, _ = io.Copy(&c.stderr, stderr)
}

func (c *lineRPCClient) handleServerRequest(id string, msg map[string]json.RawMessage, raw string) {
	var method string
	_ = json.Unmarshal(msg["method"], &method)
	req := rpcRequest{ID: id, Method: method, Params: msg["params"], Raw: raw}
	result := any(map[string]any{})
	if c.defaultHandler != nil {
		result = c.defaultHandler(req)
	}
	resp := map[string]any{"id": id, "result": result}
	if c.includeJSONRPC {
		resp["jsonrpc"] = "2.0"
	}
	_ = c.write(resp)
}

func (c *lineRPCClient) deliverResponse(id string, msg map[string]json.RawMessage) {
	ch := c.removePending(id)
	if ch == nil {
		return
	}
	if rawErr, ok := msg["error"]; ok && len(rawErr) > 0 && string(rawErr) != "null" {
		var decoded rpcError
		if err := json.Unmarshal(rawErr, &decoded); err != nil {
			ch <- rpcResponse{Err: err}
			return
		}
		ch <- rpcResponse{Err: decoded}
		return
	}
	ch <- rpcResponse{Result: msg["result"]}
}

func (c *lineRPCClient) removePending(id string) chan rpcResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.pending[id]
	delete(c.pending, id)
	return ch
}

func trimJSONID(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatInt(n, 10)
	}
	return strings.Trim(string(raw), `"`)
}

func mergeProcessEnv(base []string, overrides map[string]string) []string {
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
	for key, value := range overrides {
		pair := key + "=" + value
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}
