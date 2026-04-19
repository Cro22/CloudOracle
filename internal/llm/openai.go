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

type OpenAPIProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func newOpenAI(cfg config.LLMConfig) (*OpenAPIProvider, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, errors.New("OPENAI_API_KEY environment variable is not set")
	}

	return &OpenAPIProvider{
		apiKey: cfg.OpenAIAPIKey,
		model:  "gpt-4o-mini",
		client: &http.Client{Timeout: cfg.RequestTimeout},
	}, nil
}

func (o *OpenAPIProvider) Name() string {
	return "OpenAI (" + o.model + ")"
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (o *OpenAPIProvider) GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error) {

	prompt := BuildPrompt(findings)

	reqBody := openAIRequest{
		Model: o.model,
		Messages: []openAIMessage{
			{Role: "system", Content: "You are a senior cloud cost optimization consultant."},
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)

	if err != nil {
		return "", fmt.Errorf("OpenAI API request failed: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API returned %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal OpenAI response: %w", err)
	}

	if openAIResp.Error != nil {
		return "", fmt.Errorf("OpenAI API error: %s - %s", openAIResp.Error.Type, openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		return "", errors.New("OpenAI API returned no choices")
	}

	return openAIResp.Choices[0].Message.Content, nil
}
