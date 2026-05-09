//go:build integration

// Package diff integration tests for the LLM-generated PR narrative.
//
// These tests hit a real LLM provider (Claude via ANTHROPIC_API_KEY) and
// therefore live behind the `integration` build tag. They are not part
// of the default `go test ./...` run; invoke them with
//
//	go test -tags=integration ./internal/diff/...
//
// If no API credentials are present, each test calls t.Skip — running
// with the tag but no creds yields a clean skip, not a failure.
package diff

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"CloudOracle/internal/config"
	"CloudOracle/internal/llm"
)

// claudeForIntegration returns a real Claude provider, or skips the
// test if ANTHROPIC_API_KEY is not set.
func claudeForIntegration(t *testing.T) llm.Provider {
	t.Helper()
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping integration test")
	}
	p, err := llm.NewProvider(config.LLMConfig{
		Provider:       "claude",
		ClaudeAPIKey:   key,
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("constructing Claude provider: %v", err)
	}
	return p
}

// TestIntegration_PRNarrative_Claude exercises the real LLM end-to-end
// with the canonical happy-path fixture. The LLM output is non-deterministic,
// so assertions are shape-based rather than content-equality:
//
//   - non-empty after our cleaning step
//   - within the 600-char ceiling we promised in the doc
//   - does NOT contain the exact total ("$389.35" / "+$389.35"), confirming
//     the model honored the "do not repeat the total" instruction
//   - mentions at least one of the top-mover types (rds / aurora /
//     instance / lambda / ebs / nat), confirming the model is grounded
//     in the data we passed
func TestIntegration_PRNarrative_Claude(t *testing.T) {
	provider := claudeForIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := RenderMarkdownWithLLM(ctx, happyPathDiff(), provider)

	// Pull out just the narrative line: between "**Net monthly change ..."
	// and the next blank-line separator before "### Top movers".
	narrative := extractNarrative(t, out)
	if narrative == "" {
		t.Fatalf("could not extract narrative from rendered output:\n%s", out)
	}
	t.Logf("LLM narrative: %s", narrative)

	if len(narrative) > 600 {
		t.Errorf("narrative exceeded 600 chars (got %d): %s", len(narrative), narrative)
	}

	// Sanity: the model must NOT echo the bold total verbatim.
	for _, forbidden := range []string{"$389.35", "+$389.35"} {
		if strings.Contains(narrative, forbidden) {
			t.Errorf("narrative repeats the total %q despite the explicit instruction:\n%s",
				forbidden, narrative)
		}
	}

	// Sanity: the narrative should be grounded in the resources we
	// supplied. Match against type substrings rather than full type
	// names so the LLM has wiggle room ("Aurora cluster" matches
	// "aurora", "RDS instance" matches "rds").
	wantAny := []string{"rds", "aurora", "instance", "lambda", "ebs", "nat", "database", "db"}
	hit := false
	lower := strings.ToLower(narrative)
	for _, w := range wantAny {
		if strings.Contains(lower, w) {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("narrative does not mention any top-mover type from %v:\n%s", wantAny, narrative)
	}
}

// TestIntegration_PRNarrative_FallbackOnRealError points the Claude
// provider at an obviously-invalid API key and asserts the renderer
// silently falls back to the templated narrative — no error surfaces
// in the rendered comment.
func TestIntegration_PRNarrative_FallbackOnRealError(t *testing.T) {
	// Skip if creds are present — this test is about the bad-key path
	// and would burn a real API call to no purpose. (Still requires the
	// integration tag because constructing the provider hits real net.)
	p, err := llm.NewProvider(config.LLMConfig{
		Provider:       "claude",
		ClaudeAPIKey:   "sk-ant-invalid-key-for-integration-test",
		RequestTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("constructing Claude provider with stub key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	d := happyPathDiff()
	got := RenderMarkdownWithLLM(ctx, d, p)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("invalid-key call should fall back to templated render byte-for-byte\n--- got ---\n%s\n--- want ---\n%s",
			got, want)
	}
}

// extractNarrative returns the prose paragraph between the bold net-change
// line and the next major section header in the rendered comment. Used
// by integration tests so we assert against just the LLM output, not
// the surrounding template.
func extractNarrative(t *testing.T, out string) string {
	t.Helper()
	const startMarker = "**Net monthly change:"
	startIdx := strings.Index(out, startMarker)
	if startIdx < 0 {
		return ""
	}
	// Skip past the bold line (its trailing newline) to the start of the narrative.
	lineEnd := strings.Index(out[startIdx:], "\n")
	if lineEnd < 0 {
		return ""
	}
	body := out[startIdx+lineEnd+1:]
	body = strings.TrimLeft(body, "\n")

	// The narrative ends at the next blank line before the table heading
	// (`### Top movers`) or the breakdown block.
	for _, end := range []string{"\n### ", "\n<details>", "\n---\n"} {
		if i := strings.Index(body, end); i >= 0 {
			body = body[:i]
		}
	}
	return strings.TrimSpace(body)
}
