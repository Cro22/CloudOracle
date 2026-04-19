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

type GeminiProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func newGemini(cfg config.LLMConfig) (*GeminiProvider, error) {
	if cfg.GeminiAPIKey == "" {
		return nil, errors.New("GEMINI_API_KEY is not set")
	}

	return &GeminiProvider{
		apiKey: cfg.GeminiAPIKey,
		model:  "gemini-2.5-flash",
		client: &http.Client{Timeout: cfg.RequestTimeout},
	}, nil
}

func (g *GeminiProvider) Name() string {
	return "Gemini (" + g.model + ")"
}

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (g *GeminiProvider) GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error) {

	prompt := BuildPrompt(findings)

	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Gemini request: %w", err)
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", g.model)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini request: %w", err)
	}
	req.Header.Set("X-goog-api-key", g.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}
	var response geminiResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal Gemini response: %w", err)
	}
	if len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini response has no content")
	}
	return response.Candidates[0].Content.Parts[0].Text, nil
}
