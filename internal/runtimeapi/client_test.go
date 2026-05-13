package runtimeapi

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/sessionctx"
)

// shortSocketDir creates a temp directory with a deliberately short path to
// stay under the macOS Unix socket path limit (104 bytes).
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "a")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// startServer starts an already-constructed Server on its configured socket
// and waits for the socket to be ready. Returns the cancel function to shut it down.
func startServer(t *testing.T, server *Server, socketPath string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
	}()

	for i := 0; i < 100; i++ {
		if fi, err := os.Stat(socketPath); err == nil {
			if fi.Mode()&os.ModeSocket != 0 {
				return cancel
			}
		}
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited before socket was ready: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatalf("timed out waiting for Unix socket at %s", socketPath)
	return nil
}

// startTestServer starts a runtime API server on a Unix socket and waits for
// the socket to be ready. Returns the cancel function to shut it down.
func startTestServer(t *testing.T, socketPath string, token string, store *automation.Store) context.CancelFunc {
	t.Helper()
	server := NewServer(socketPath, token, nil, store, config.Config{})
	return startServer(t, server, socketPath)
}

func startTestServerWithCfg(t *testing.T, socketPath string, token string, store *automation.Store, cfg config.Config) context.CancelFunc {
	t.Helper()
	server := NewServer(socketPath, token, nil, store, cfg)
	return startServer(t, server, socketPath)
}

// newTestClient creates a client connected to a Unix socket, using the
// unix:// prefix to match production usage.
func newTestClient(t *testing.T, socketPath, token string) *Client {
	t.Helper()
	client := NewClient("unix://"+socketPath, token)
	if client == nil || !client.IsEnabled() {
		t.Fatal("client should be enabled for unix socket path")
	}
	return client
}

func TestNewClient_SocketTransport(t *testing.T) {
	client := NewClient("unix:///tmp/test.sock", "tok")
	if client == nil || !client.IsEnabled() {
		t.Fatal("client should be enabled for unix socket path")
	}
}

func TestNewClient_BarePath(t *testing.T) {
	client := NewClient("/tmp/test.sock", "tok")
	if client == nil || !client.IsEnabled() {
		t.Fatal("client should be enabled for bare socket path")
	}
}

func TestNewClient_EmptyPathReturnsNil(t *testing.T) {
	client := NewClient("", "")
	if client != nil {
		t.Fatal("client should be nil for empty path")
	}
}

func TestUnixSocketE2E_Healthz(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")
	cancel := startTestServer(t, socketPath, "test-token", nil)
	defer cancel()

	client := newTestClient(t, socketPath, "test-token")
	result, err := client.do(context.Background(),
		sessionctx.SessionContext{},
		http.MethodGet, "/healthz", nil, "", nil,
	)
	if err != nil {
		t.Fatalf("healthz failed: %v", err)
	}
	if status, ok := result["status"].(string); !ok || status != "ok" {
		t.Fatalf("unexpected healthz response: %#v", result)
	}
}

func TestUnixSocketE2E_GoalCRUD(t *testing.T) {
	socketDir := shortSocketDir(t)
	socketPath := filepath.Join(socketDir, "s")
	store := automation.NewStore(filepath.Join(socketDir, "automation.db"))

	cancel := startTestServer(t, socketPath, "test-token", store)
	defer cancel()

	client := newTestClient(t, socketPath, "test-token")

	workSession := sessionctx.SessionContext{
		ReceiveIDType:   "chat_id",
		ReceiveID:       "oc_test",
		ActorOpenID:     "ou_test",
		ChatType:        "group",
		SessionKey:      "chat_id:oc_test|work:om_work_seed",
		SourceMessageID: "om_msg",
	}

	result, err := client.CreateGoal(context.Background(), workSession, CreateGoalRequest{
		Objective:  "test socket goal",
		DeadlineIn: "1h",
	})
	if err != nil {
		t.Fatalf("create goal failed: %v", err)
	}
	goal, _ := result["goal"].(map[string]any)
	if goal == nil {
		t.Fatalf("unexpected create goal response: %#v", result)
	}

	getResult, err := client.GetGoal(context.Background(), workSession)
	if err != nil {
		t.Fatalf("get goal failed: %v", err)
	}
	if getGoal, _ := getResult["goal"].(map[string]any); getGoal == nil {
		t.Fatalf("expected goal in get response: %#v", getResult)
	}
}

func TestUnixSocketE2E_AuthRejected(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")
	cancel := startTestServer(t, socketPath, "correct-token", nil)
	defer cancel()

	badClient := newTestClient(t, socketPath, "wrong-token")
	_, err := badClient.do(context.Background(),
		sessionctx.SessionContext{},
		http.MethodGet, "/healthz", nil, "", nil,
	)
	if err == nil {
		t.Fatal("expected auth error with wrong token, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Fatalf("expected 401 error, got: %v", err)
	}
}

func TestUnixSocketE2E_StaleSocketCleanup(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")

	if err := os.WriteFile(socketPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("failed to create fake stale file: %v", err)
	}

	cancel := startTestServer(t, socketPath, "tok", nil)
	defer cancel()

	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket not found after server start: %v", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		t.Fatal("stale regular file was not replaced with a Unix socket")
	}
}

func TestUnixSocketE2E_SocketPermissions(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")
	cancel := startTestServer(t, socketPath, "tok", nil)
	defer cancel()

	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket not found: %v", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		t.Fatal("file is not a Unix socket")
	}
	if fi.Mode().Perm() != 0700 {
		t.Fatalf("expected socket permissions 0700, got %04o", fi.Mode().Perm())
	}
}

func TestUnixSocket_ListenErrorOnInvalidPath(t *testing.T) {
	socketPath := "/nonexistent/path/should/not/exist/runtime.sock"
	server := NewServer(socketPath, "tok", nil, nil, config.Config{})

	err := server.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid socket path, got nil")
	}
	t.Logf("error (expected): %v", err)
}

func TestUnixSocketE2E_ClientWithBareServer(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	go http.Serve(ln, mux)

	client := newTestClient(t, socketPath, "")
	result, err := client.do(context.Background(),
		sessionctx.SessionContext{},
		http.MethodGet, "/healthz", nil, "", nil,
	)
	if err != nil {
		t.Fatalf("healthz via Unix socket failed: %v", err)
	}
	if status, ok := result["status"].(string); !ok || status != "ok" {
		t.Fatalf("unexpected response: %#v", result)
	}
}

func TestUnixSocketE2E_ShutdownCleanup(t *testing.T) {
	socketPath := filepath.Join(shortSocketDir(t), "s")
	cancel := startTestServer(t, socketPath, "tok", nil)
	cancel()
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socketPath); err == nil {
		t.Fatal("socket file should be removed on shutdown")
	}
}

func TestBaseURL_SocketPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "absolute path", path: "/home/user/.alice/runtime.sock", want: "unix:///home/user/.alice/runtime.sock"},
		{name: "has unix already", path: "unix:///tmp/s", want: "unix:///tmp/s"},
		{name: "empty", path: "", want: ""},
		{name: "whitespace", path: "  ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BaseURL(tt.path)
			if got != tt.want {
				t.Fatalf("BaseURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
