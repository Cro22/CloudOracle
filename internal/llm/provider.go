package llm

import (
	"CloudOracle/internal/config"
	"CloudOracle/internal/shared"
	"context"
	"errors"
)

var ErrNoProvider = errors.New("no LLM provider configured")

type Provider interface {
	// GenerateSummary builds the v1 executive-summary prompt from findings
	// and returns the LLM response. Convenience method; thin wrapper over
	// GenerateText that calls BuildPrompt for the caller.
	GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error)

	// GenerateText sends an arbitrary prompt and returns the LLM response.
	// The caller owns prompt construction. Used by v2 flows (e.g. PR
	// narrative in internal/diff) that build their own context-specific
	// prompts and do not want to go through findings shaping.
	GenerateText(ctx context.Context, prompt string) (string, error)

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
