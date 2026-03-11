// Package mcp provides embedded MCP server for Alice.
//
// This package uses the official Model Context Protocol Go SDK to provide
// an HTTP-based MCP server that enables structured tool calling for LLM agents.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server provides embedded MCP HTTP server for tool calling.
type Server struct {
	logger   *slog.Logger
	handler  http.Handler
	listener net.Listener
	addr     string
	mu       sync.RWMutex
}

// Config holds MCP server configuration.
type Config struct {
	// Logger for server logging. If nil, logging is disabled.
	Logger *slog.Logger

	// Host to bind to. Default: "127.0.0.1"
	Host string

	// Port to bind to. If 0, a random available port is chosen.
	Port int
}

// NewServer creates a new embedded MCP HTTP server with the specified tools.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Create MCP server with tools
	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "alice-mcp-server",
		Version: "v0.1.0",
	}, nil)

	// Add tools
	addTools(mcpServer)

	// Create HTTP handler
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{
			JSONResponse: true,
		},
	)

	// Create listener
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	s := &Server{
		logger:   logger,
		handler:  handler,
		listener: listener,
		addr:     listener.Addr().String(),
	}

	return s, nil
}

// Start starts the MCP HTTP server in a background goroutine.
func (s *Server) Start() error {
	go func() {
		s.logger.Info("mcp server started", "addr", s.addr)
		if err := http.Serve(s.listener, s.handler); err != nil && err != http.ErrServerClosed {
			s.logger.Error("mcp server error", "error", err)
		}
	}()
	return nil
}

// Stop stops the MCP HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.listener.Close()
}

// Addr returns the server address (host:port).
func (s *Server) Addr() string {
	return s.addr
}

// URL returns the full HTTP URL for connecting to this server.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s", s.addr)
}

// ConfigJSON returns the MCP configuration JSON for kimi CLI.
func (s *Server) ConfigJSON(serverName string) string {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			serverName: map[string]interface{}{
				"transport": "http",
				"url":       s.URL(),
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}

// addTools adds all Alice tools to the MCP server.
func addTools(server *mcp.Server) {
	// submit_promotion_decision tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_promotion_decision",
		Description: "Submit a promotion decision for the incoming request. This tool MUST be called to classify and route the request. Do not output JSON directly.",
	}, handlePromotionDecision)

	// submit_direct_answer tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_direct_answer",
		Description: "Submit a direct answer for simple queries. Use only when intent_kind is 'direct_query'.",
	}, handleDirectAnswer)

	// submit_tool_call tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_tool_call",
		Description: "Request to call a tool or MCP function.",
	}, handleToolCall)
}

// PromotionDecision represents the decision output from Reception
type PromotionDecision struct {
	IntentKind             string   `json:"intent_kind"`
	RiskLevel              string   `json:"risk_level"`
	ExternalWrite          bool     `json:"external_write"`
	CreatePersistentObject bool     `json:"create_persistent_object"`
	Async                  bool     `json:"async"`
	MultiStep              bool     `json:"multi_step"`
	MultiAgent             bool     `json:"multi_agent"`
	ApprovalRequired       bool     `json:"approval_required"`
	BudgetRequired         bool     `json:"budget_required"`
	RecoveryRequired       bool     `json:"recovery_required"`
	ProposedWorkflowIDs    []string `json:"proposed_workflow_ids"`
	ReasonCodes            []string `json:"reason_codes"`
	Confidence             float64  `json:"confidence"`
}

// DirectAnswer represents a direct answer output
type DirectAnswer struct {
	Answer    string   `json:"answer"`
	Citations []string `json:"citations,omitempty"`
}

// ToolCallRequest represents a tool call request
type ToolCallRequest struct {
	ToolName   string                 `json:"tool_name"`
	Parameters map[string]interface{} `json:"parameters"`
}

// OutputWrapper wraps the tool output for the agent to parse
type OutputWrapper struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func handlePromotionDecision(ctx context.Context, req *mcp.CallToolRequest, args PromotionDecision) (*mcp.CallToolResult, any, error) {
	wrapper := OutputWrapper{
		Type:    "promotion_decision",
		Payload: mustMarshal(args),
	}

	output, err := json.Marshal(wrapper)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal output: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(output)},
		},
	}, nil, nil
}

func handleDirectAnswer(ctx context.Context, req *mcp.CallToolRequest, args DirectAnswer) (*mcp.CallToolResult, any, error) {
	wrapper := OutputWrapper{
		Type:    "direct_answer",
		Payload: mustMarshal(args),
	}

	output, err := json.Marshal(wrapper)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal output: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(output)},
		},
	}, nil, nil
}

func handleToolCall(ctx context.Context, req *mcp.CallToolRequest, args ToolCallRequest) (*mcp.CallToolResult, any, error) {
	wrapper := OutputWrapper{
		Type:    "tool_call",
		Payload: mustMarshal(args),
	}

	output, err := json.Marshal(wrapper)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal output: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(output)},
		},
	}, nil, nil
}

func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return data
}
