package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

type Client interface {
	Action(ctx context.Context, req *MCPActionRequest) (*MCPActionResponse, error)
	Query(ctx context.Context, req *MCPQueryRequest) (*MCPQueryResponse, error)
	ActionStatus(ctx context.Context, actionID string) (*MCPActionStatusResponse, error)
	Lookup(ctx context.Context, req *MCPActionLookupRequest) (*MCPActionStatusResponse, error)
	Health(ctx context.Context) error
}

// HTTPClient implements MCP Client using go-resty/resty/v2.
type HTTPClient struct {
	client *resty.Client
}

// NewHTTPClient creates a new MCP HTTP client using resty.
func NewHTTPClient(baseURL string) *HTTPClient {
	client := resty.New().
		SetBaseURL(strings.TrimRight(baseURL, "/")).
		SetTimeout(15*time.Second).
		SetHeader("Content-Type", "application/json").
		SetRetryCount(3).
		SetRetryWaitTime(100 * time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Second)

	return &HTTPClient{client: client}
}

// NewHTTPClientWithResty creates a client from an existing resty client.
func NewHTTPClientWithResty(client *resty.Client) *HTTPClient {
	return &HTTPClient{client: client}
}

func (c *HTTPClient) Action(ctx context.Context, req *MCPActionRequest) (*MCPActionResponse, error) {
	var out MCPActionResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/actions")

	if err != nil {
		return nil, fmt.Errorf("mcp action: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("mcp action status %d: %s", resp.StatusCode(), resp.String())
	}
	return &out, nil
}

func (c *HTTPClient) Query(ctx context.Context, req *MCPQueryRequest) (*MCPQueryResponse, error) {
	var out MCPQueryResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/queries")

	if err != nil {
		return nil, fmt.Errorf("mcp query: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("mcp query status %d: %s", resp.StatusCode(), resp.String())
	}
	return &out, nil
}

func (c *HTTPClient) ActionStatus(ctx context.Context, actionID string) (*MCPActionStatusResponse, error) {
	var out MCPActionStatusResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetResult(&out).
		Get("/v1/actions/" + actionID)

	if err != nil {
		return nil, fmt.Errorf("mcp action status: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("mcp action status %d: %s", resp.StatusCode(), resp.String())
	}
	return &out, nil
}

func (c *HTTPClient) Lookup(ctx context.Context, req *MCPActionLookupRequest) (*MCPActionStatusResponse, error) {
	var out MCPActionStatusResponse
	resp, err := c.client.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&out).
		Post("/v1/actions/lookup")

	if err != nil {
		return nil, fmt.Errorf("mcp lookup: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("mcp lookup status %d: %s", resp.StatusCode(), resp.String())
	}
	return &out, nil
}

func (c *HTTPClient) Health(ctx context.Context) error {
	resp, err := c.client.R().
		SetContext(ctx).
		Get("/healthz")

	if err != nil {
		return fmt.Errorf("mcp health: %w", err)
	}
	if resp.IsError() {
		return fmt.Errorf("mcp unhealthy: status %d", resp.StatusCode())
	}
	return nil
}
