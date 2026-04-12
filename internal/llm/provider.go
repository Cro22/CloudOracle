package llm

import (
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"os"
)

var ErrNoProvider = errors.New("no LLM provider configured")

type Provider interface {
	GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error)

	Name() string
}

func NewProvider() (Provider, error) {
	explicit := os.Getenv("LLM_PROVIDER")

	switch explicit {
	case "gemini":
		return newGeminiFromEnv()
	case "claude":
		return newClaudeFromEnv()
	case "openai":
		return newOpenAIFromEnv()
	case "":
		if os.Getenv("GEMINI_API_KEY") != "" {
			return newGeminiFromEnv()
		}
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			return newClaudeFromEnv()
		}
		if os.Getenv("OPENAI_API_KEY") != "" {
			return newOpenAIFromEnv()
		}
		return nil, ErrNoProvider
	default:
		return nil, errors.New("unknown LLM_PROVIDER: " + explicit)
	}
}
