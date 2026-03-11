// Package llm provides LLM client implementations for Alice.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config holds the configuration for Kimi client.
type Config struct {
	APIKey      string
	BaseURL     string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	apiKey := os.Getenv("KIMI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	baseURL := os.Getenv("KIMI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.moonshot.cn/v1"
	}
	model := os.Getenv("KIMI_MODEL")
	if model == "" {
		model = "moonshot-v1-8k"
	}
	return Config{
		APIKey:      apiKey,
		BaseURL:     baseURL,
		Model:       model,
		Timeout:     60 * time.Second,
		MaxTokens:   4096,
		Temperature: 0.3,
	}
}

// Client is the Kimi LLM client.
type Client struct {
	config Config
	client *http.Client
}

// NewClient creates a new Kimi client.
func NewClient(cfg Config) *Client {
	if cfg.APIKey == "" {
		cfg = DefaultConfig()
	}
	return &Client{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// ChatResponse represents a chat completion response.
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message Message `json:"message"`
		Delta   *struct {
			Content string `json:"content"`
		} `json:"delta,omitempty"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat sends a chat completion request to Kimi API.
func (c *Client) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
		Stream:      false,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.config.BaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// ChatStream sends a streaming chat completion request.
func (c *Client) ChatStream(ctx context.Context, messages []Message, handler func(string)) error {
	reqBody := ChatRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
		Stream:      true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.config.BaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp ChatResponse
		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue // Skip invalid lines
		}

		if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta != nil {
			handler(streamResp.Choices[0].Delta.Content)
		}
	}

	return scanner.Err()
}

// SimpleChat is a convenience method for single-turn chat.
func (c *Client) SimpleChat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	resp, err := c.Chat(ctx, messages)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return resp.Choices[0].Message.Content, nil
}

// Health checks if the LLM service is available.
func (c *Client) Health(ctx context.Context) error {
	if c.config.APIKey == "" {
		return fmt.Errorf("KIMI_API_KEY not set")
	}
	// Try a minimal request
	_, err := c.SimpleChat(ctx, "You are a helpful assistant.", "Hi")
	return err
}
