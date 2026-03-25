package runtimeapi

import (
	"context"
	"fmt"
	"net/http"
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
		SetHeader("Accept", "application/json")
	if token = strings.TrimSpace(token); token != "" {
		httpClient.SetAuthToken(token)
	}
	return &Client{http: httpClient}
}

func (c *Client) IsEnabled() bool {
	return c != nil && c.http != nil
}

func (c *Client) SendImage(ctx context.Context, session mcpbridge.SessionContext, req ImageRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/messages/image", req)
}

func (c *Client) SendFile(ctx context.Context, session mcpbridge.SessionContext, req FileRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/messages/file", req)
}

func (c *Client) ListTasks(ctx context.Context, session mcpbridge.SessionContext, status string, limit int) (map[string]any, error) {
	query := map[string]string{}
	if status = strings.TrimSpace(status); status != "" {
		query["status"] = status
	}
	if limit > 0 {
		query["limit"] = fmt.Sprintf("%d", limit)
	}
	return c.get(ctx, session, "/api/v1/automation/tasks", query)
}

func (c *Client) CreateTask(ctx context.Context, session mcpbridge.SessionContext, req CreateTaskRequest) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/automation/tasks", req)
}

func (c *Client) GetTask(ctx context.Context, session mcpbridge.SessionContext, taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	return c.get(ctx, session, "/api/v1/automation/tasks/"+taskID, nil)
}

func (c *Client) PatchTask(
	ctx context.Context,
	session mcpbridge.SessionContext,
	taskID string,
	contentType string,
	patchBody []byte,
) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/merge-patch+json"
	}
	return c.do(ctx, session, http.MethodPatch, "/api/v1/automation/tasks/"+taskID, patchBody, contentType, nil)
}

func (c *Client) DeleteTask(ctx context.Context, session mcpbridge.SessionContext, taskID string) (map[string]any, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	return c.delete(ctx, session, "/api/v1/automation/tasks/"+taskID, nil)
}

func (c *Client) ListCampaigns(
	ctx context.Context,
	session mcpbridge.SessionContext,
	status string,
	limit int,
) (map[string]any, error) {
	query := map[string]string{}
	if status = strings.TrimSpace(status); status != "" {
		query["status"] = status
	}
	if limit > 0 {
		query["limit"] = fmt.Sprintf("%d", limit)
	}
	return c.get(ctx, session, "/api/v1/campaigns", query)
}

func (c *Client) CreateCampaign(
	ctx context.Context,
	session mcpbridge.SessionContext,
	req CreateCampaignRequest,
) (map[string]any, error) {
	return c.post(ctx, session, "/api/v1/campaigns", req)
}

func (c *Client) GetCampaign(ctx context.Context, session mcpbridge.SessionContext, campaignID string) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	return c.get(ctx, session, "/api/v1/campaigns/"+campaignID, nil)
}

func (c *Client) PatchCampaign(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	contentType string,
	patchBody []byte,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/merge-patch+json"
	}
	return c.do(ctx, session, http.MethodPatch, "/api/v1/campaigns/"+campaignID, patchBody, contentType, nil)
}

func (c *Client) DeleteCampaign(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	deleteRepo bool,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	query := map[string]string{}
	if deleteRepo {
		query["delete_repo"] = "true"
	}
	return c.delete(ctx, session, "/api/v1/campaigns/"+campaignID, query)
}

func (c *Client) UpsertTrial(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	req UpsertTrialRequest,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	return c.post(ctx, session, "/api/v1/campaigns/"+campaignID+"/trials", req)
}

func (c *Client) AddGuidance(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	req AddGuidanceRequest,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	return c.post(ctx, session, "/api/v1/campaigns/"+campaignID+"/guidance", req)
}

func (c *Client) AddReview(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	req AddReviewRequest,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	return c.post(ctx, session, "/api/v1/campaigns/"+campaignID+"/reviews", req)
}

func (c *Client) AddPitfall(
	ctx context.Context,
	session mcpbridge.SessionContext,
	campaignID string,
	req AddPitfallRequest,
) (map[string]any, error) {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("campaign id is required")
	}
	return c.post(ctx, session, "/api/v1/campaigns/"+campaignID+"/pitfalls", req)
}

func (c *Client) get(
	ctx context.Context,
	session mcpbridge.SessionContext,
	path string,
	query map[string]string,
) (map[string]any, error) {
	return c.do(ctx, session, http.MethodGet, path, nil, "", query)
}

func (c *Client) post(ctx context.Context, session mcpbridge.SessionContext, path string, body any) (map[string]any, error) {
	return c.do(ctx, session, http.MethodPost, path, body, "application/json", nil)
}

func (c *Client) put(ctx context.Context, session mcpbridge.SessionContext, path string, body any) (map[string]any, error) {
	return c.do(ctx, session, http.MethodPut, path, body, "application/json", nil)
}

func (c *Client) delete(
	ctx context.Context,
	session mcpbridge.SessionContext,
	path string,
	query map[string]string,
) (map[string]any, error) {
	return c.do(ctx, session, http.MethodDelete, path, nil, "", query)
}

func (c *Client) do(
	ctx context.Context,
	session mcpbridge.SessionContext,
	method string,
	path string,
	body any,
	contentType string,
	query map[string]string,
) (map[string]any, error) {
	if !c.IsEnabled() {
		return nil, fmt.Errorf("runtime api client is unavailable")
	}
	var result map[string]any
	var failure map[string]any
	req := c.request(ctx, session).
		SetResult(&result).
		SetError(&failure)
	if strings.TrimSpace(contentType) != "" {
		req.SetHeader("Content-Type", contentType)
	}
	if len(query) > 0 {
		for key, value := range query {
			if strings.TrimSpace(value) == "" {
				continue
			}
			req.SetQueryParam(key, value)
		}
	}
	if body != nil {
		req.SetBody(body)
	}
	resp, err := req.Execute(method, path)
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
