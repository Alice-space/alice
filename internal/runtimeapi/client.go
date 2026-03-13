package runtimeapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-resty/resty/v2"

	"github.com/Alice-space/alice/internal/mcpbridge"
)

type Client struct {
	http *resty.Client
}

func NewClient(baseURL, token string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	httpClient := resty.New().
		SetBaseURL(baseURL).
		SetAuthToken(strings.TrimSpace(token)).
		SetHeader("Accept", "application/json")
	return &Client{http: httpClient}
}

func (c *Client) IsEnabled() bool {
	return c != nil && c.http != nil
}

func (c *Client) SendText(ctx context.Context, session mcpbridge.SessionContext, req TextRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/messages/text", req)
}

func (c *Client) SendImage(ctx context.Context, session mcpbridge.SessionContext, req ImageRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/messages/image", req)
}

func (c *Client) SendFile(ctx context.Context, session mcpbridge.SessionContext, req FileRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/messages/file", req)
}

func (c *Client) post(ctx context.Context, session mcpbridge.SessionContext, path string, body any) (map[string]any, error) {
	if !c.IsEnabled() {
		return nil, fmt.Errorf("runtime api client is unavailable")
	}
	var result map[string]any
	var failure map[string]any
	resp, err := c.request(ctx, session).
		SetBody(body).
		SetResult(&result).
		SetError(&failure).
		Post(path)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		if message, ok := failure["error"].(string); ok && strings.TrimSpace(message) != "" {
			return nil, fmt.Errorf("runtime api %s failed: %s", path, message)
		}
		return nil, fmt.Errorf("runtime api %s failed: status=%d", path, resp.StatusCode())
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, session mcpbridge.SessionContext) *resty.Request {
	req := c.http.R().SetContext(ctx)
	headers := map[string]string{
		HeaderReceiveIDType:   strings.TrimSpace(session.ReceiveIDType),
		HeaderReceiveID:       strings.TrimSpace(session.ReceiveID),
		HeaderResourceRoot:    strings.TrimSpace(session.ResourceRoot),
		HeaderSourceMessageID: strings.TrimSpace(session.SourceMessageID),
		HeaderActorUserID:     strings.TrimSpace(session.ActorUserID),
		HeaderActorOpenID:     strings.TrimSpace(session.ActorOpenID),
		HeaderChatType:        strings.TrimSpace(session.ChatType),
		HeaderSessionKey:      strings.TrimSpace(session.SessionKey),
	}
	for key, value := range headers {
		if value == "" {
			continue
		}
		req.SetHeader(key, value)
	}
	return req
}
