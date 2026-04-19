package llm

import (
	"CloudOracle/internal/config"
	"CloudOracle/internal/shared"
	"context"
	"errors"
)

var ErrNoProvider = errors.New("no LLM provider configured")

type Provider interface {
	GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error)
	Name() string
}

func NewProvider(cfg config.LLMConfig) (Provider, error) {
	switch cfg.Provider {
	case "gemini":
		return newGemini(cfg)
	case "claude":
		return newClaude(cfg)
	case "openai":
		return newOpenAI(cfg)
	case "":
		if cfg.GeminiAPIKey != "" {
			return newGemini(cfg)
		}
		if cfg.ClaudeAPIKey != "" {
			return newClaude(cfg)
		}
		if cfg.OpenAIAPIKey != "" {
			return newOpenAI(cfg)
		}
		return nil, ErrNoProvider
	default:
		return nil, errors.New("unknown LLM_PROVIDER: " + cfg.Provider)
	}
}
