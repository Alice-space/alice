package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client interface {
	Action(ctx context.Context, req *MCPActionRequest) (*MCPActionResponse, error)
	Query(ctx context.Context, req *MCPQueryRequest) (*MCPQueryResponse, error)
	ActionStatus(ctx context.Context, actionID string) (*MCPActionStatusResponse, error)
	Lookup(ctx context.Context, req *MCPActionLookupRequest) (*MCPActionStatusResponse, error)
	Health(ctx context.Context) error
}

type HTTPClient struct {
	baseURL string
	client  *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *HTTPClient) Action(ctx context.Context, req *MCPActionRequest) (*MCPActionResponse, error) {
	var out MCPActionResponse
	if err := c.postJSON(ctx, "/v1/actions", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) Query(ctx context.Context, req *MCPQueryRequest) (*MCPQueryResponse, error) {
	var out MCPQueryResponse
	if err := c.postJSON(ctx, "/v1/queries", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) ActionStatus(ctx context.Context, actionID string) (*MCPActionStatusResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/actions/"+actionID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp status %s", resp.Status)
	}
	var out MCPActionStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) Lookup(ctx context.Context, req *MCPActionLookupRequest) (*MCPActionStatusResponse, error) {
	var out MCPActionStatusResponse
	if err := c.postJSON(ctx, "/v1/actions/lookup", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *HTTPClient) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mcp unhealthy: %s", resp.Status)
	}
	return nil
}

func (c *HTTPClient) postJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("mcp %s status %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
