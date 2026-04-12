package llm

import (
	"errors"
	"os"
	"testing"
)

func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"LLM_PROVIDER", "GEMINI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func TestNewProvider_NoKeysReturnsErrNoProvider(t *testing.T) {
	clearLLMEnv(t)
	_, err := NewProvider()
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestNewProvider_ExplicitGemini(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "gemini")
	t.Setenv("GEMINI_API_KEY", "test-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*GeminiProvider); !ok {
		t.Errorf("expected GeminiProvider, got %T", p)
	}
}

func TestNewProvider_ExplicitClaude(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "claude")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*ClaudeProvider); !ok {
		t.Errorf("expected ClaudeProvider, got %T", p)
	}
}

func TestNewProvider_ExplicitOpenAI(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "openai")
	t.Setenv("OPENAI_API_KEY", "test-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*OpenAPIProvider); !ok {
		t.Errorf("expected OpenAPIProvider, got %T", p)
	}
}

func TestNewProvider_UnknownProviderReturnsError(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "grok")
	_, err := NewProvider()
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if errors.Is(err, ErrNoProvider) {
		t.Error("should not be ErrNoProvider for unknown provider")
	}
}

func TestNewProvider_AutoDetectGeminiFirst(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("GEMINI_API_KEY", "gemini-key")
	t.Setenv("ANTHROPIC_API_KEY", "claude-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*GeminiProvider); !ok {
		t.Errorf("expected GeminiProvider (first in auto-detect), got %T", p)
	}
}

func TestNewProvider_AutoDetectClaudeSecond(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "claude-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*ClaudeProvider); !ok {
		t.Errorf("expected ClaudeProvider (second in auto-detect), got %T", p)
	}
}

func TestNewProvider_AutoDetectOpenAIThird(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("OPENAI_API_KEY", "openai-key")
	p, err := NewProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*OpenAPIProvider); !ok {
		t.Errorf("expected OpenAPIProvider (third in auto-detect), got %T", p)
	}
}

func TestNewProvider_ExplicitWithoutKeyReturnsError(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("LLM_PROVIDER", "gemini")
	// GEMINI_API_KEY not set
	_, err := NewProvider()
	if err == nil {
		t.Fatal("expected error when explicit provider key is missing")
	}
}

func TestGeminiProvider_Name(t *testing.T) {
	p := &GeminiProvider{model: "gemini-2.5-flash"}
	if p.Name() != "Gemini (gemini-2.5-flash)" {
		t.Errorf("unexpected name: %s", p.Name())
	}
}

func TestClaudeProvider_Name(t *testing.T) {
	p := &ClaudeProvider{model: "claude-haiku-4-5"}
	if p.Name() != "Claude (claude-haiku-4-5)" {
		t.Errorf("unexpected name: %s", p.Name())
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := &OpenAPIProvider{model: "gpt-4o-mini"}
	if p.Name() != "OpenAI (gpt-4o-mini)" {
		t.Errorf("unexpected name: %s", p.Name())
	}
}
