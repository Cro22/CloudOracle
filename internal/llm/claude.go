package llm

import (
	"CloudOracle/internal/config"
	"CloudOracle/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type ClaudeProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func newClaude(cfg config.LLMConfig) (*ClaudeProvider, error) {
	if cfg.ClaudeAPIKey == "" {
		return nil, errors.New("ANTHROPIC_API_KEY environment variable is not set")
	}
	return &ClaudeProvider{
		apiKey: cfg.ClaudeAPIKey,
		model:  "claude-haiku-4-5",
		client: &http.Client{Timeout: cfg.RequestTimeout},
	}, nil
}

func (c *ClaudeProvider) Name() string {
	return "Claude (" + c.model + ")"
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"maxTokens"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *ClaudeProvider) GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error) {
	prompt := BuildPrompt(findings)

	reqBody := claudeRequest{
		Model:     c.model,
		MaxTokens: 1024,
		Messages: []claudeMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))

	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)

	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude API returned %d: %s", resp.StatusCode, string(body))
	}

	var claudeResp claudeResponse

	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal Claude response: %w", err)
	}

	if claudeResp.Error != nil {
		return "", fmt.Errorf("claude API error: %s - %s", claudeResp.Error.Type, claudeResp.Error.Message)
	}

	if len(claudeResp.Content) == 0 {
		return "", fmt.Errorf("claude API returned empty content")
	}

	return claudeResp.Content[0].Text, nil
}
