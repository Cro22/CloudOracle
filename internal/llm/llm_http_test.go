package llm

import (
	"CloudOracle/internal/shared"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testFindings() []shared.Finding {
	return []shared.Finding{
		{
			ResourceID:     "i-001",
			Service:        "ec2",
			Severity:       shared.SeverityHigh,
			MonthlyCost:    100,
			MonthlySavings: 100,
			Description:    "idle instance",
		},
	}
}

// --- Gemini HTTP Tests ---

func TestGeminiProvider_GenerateSummary_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-goog-api-key") != "test-gemini-key" {
			t.Error("missing or wrong API key header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}

		resp := geminiResponse{
			Candidates: []struct {
				Content struct {
					Parts []geminiPart `json:"parts"`
				} `json:"content"`
			}{
				{Content: struct {
					Parts []geminiPart `json:"parts"`
				}{Parts: []geminiPart{{Text: "Gemini executive summary"}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey: "test-gemini-key",
		model:  "gemini-2.5-flash",
		client: server.Client(),
	}
	// Override the URL by replacing the client transport
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	ctx := context.Background()
	result, err := p.GenerateSummary(ctx, testFindings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Gemini executive summary" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestGeminiProvider_GenerateSummary_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey: "test-key",
		model:  "gemini-2.5-flash",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGeminiProvider_GenerateSummary_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{Candidates: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &GeminiProvider{
		apiKey: "test-key",
		model:  "gemini-2.5-flash",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

// --- Claude HTTP Tests ---

func TestClaudeProvider_GenerateSummary_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-claude-key" {
			t.Error("missing or wrong API key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing anthropic-version header")
		}

		resp := claudeResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "Claude executive summary"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey: "test-claude-key",
		model:  "claude-haiku-4-5",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	result, err := p.GenerateSummary(context.Background(), testFindings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Claude executive summary" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestClaudeProvider_GenerateSummary_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey: "test-key",
		model:  "claude-haiku-4-5",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestClaudeProvider_GenerateSummary_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := claudeResponse{Content: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey: "test-key",
		model:  "claude-haiku-4-5",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestClaudeProvider_GenerateSummary_ErrorField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := claudeResponse{
			Error: &struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Type: "invalid_request", Message: "bad prompt"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &ClaudeProvider{
		apiKey: "test-key",
		model:  "claude-haiku-4-5",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error when API returns error field")
	}
}

// --- OpenAI HTTP Tests ---

func TestOpenAIProvider_GenerateSummary_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-openai-key" {
			t.Error("missing or wrong Authorization header")
		}

		resp := openAIResponse{
			Choices: []struct {
				Message openAIMessage `json:"message"`
			}{
				{Message: openAIMessage{Role: "assistant", Content: "OpenAI executive summary"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAPIProvider{
		apiKey: "test-openai-key",
		model:  "gpt-4o-mini",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	result, err := p.GenerateSummary(context.Background(), testFindings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "OpenAI executive summary" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestOpenAIProvider_GenerateSummary_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	p := &OpenAPIProvider{
		apiKey: "test-key",
		model:  "gpt-4o-mini",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestOpenAIProvider_GenerateSummary_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIResponse{Choices: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAPIProvider{
		apiKey: "test-key",
		model:  "gpt-4o-mini",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIProvider_GenerateSummary_ErrorField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIResponse{
			Error: &struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			}{Type: "invalid_api_key", Message: "bad key"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := &OpenAPIProvider{
		apiKey: "test-key",
		model:  "gpt-4o-mini",
		client: server.Client(),
	}
	p.client.Transport = &rewriteTransport{base: server.Client().Transport, targetURL: server.URL}

	_, err := p.GenerateSummary(context.Background(), testFindings())
	if err == nil {
		t.Fatal("expected error when API returns error field")
	}
}

// --- Context cancellation ---

func TestProviders_RespectContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Slow server
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	providers := []Provider{
		&GeminiProvider{apiKey: "k", model: "m", client: &http.Client{Transport: &rewriteTransport{targetURL: server.URL}}},
		&ClaudeProvider{apiKey: "k", model: "m", client: &http.Client{Transport: &rewriteTransport{targetURL: server.URL}}},
		&OpenAPIProvider{apiKey: "k", model: "m", client: &http.Client{Transport: &rewriteTransport{targetURL: server.URL}}},
	}

	for _, p := range providers {
		_, err := p.GenerateSummary(ctx, testFindings())
		if err == nil {
			t.Errorf("%s: expected context cancellation error", p.Name())
		}
	}
}

// rewriteTransport redirects all requests to the test server URL
type rewriteTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.targetURL[len("http://"):]
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
