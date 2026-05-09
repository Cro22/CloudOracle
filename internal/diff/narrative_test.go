package diff

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/llm"
	"CloudOracle/internal/pricing"
	"CloudOracle/internal/shared"
)

// fakeProvider is a minimal llm.Provider stub used to drive
// RenderMarkdownWithLLM through every branch (success, error, empty,
// oversize, slow, ...) without an HTTP server.
type fakeProvider struct {
	name     string
	response string
	err      error
	delay    time.Duration

	// observed prompt — set by GenerateText for assertions
	gotPrompt string
}

func (f *fakeProvider) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f *fakeProvider) GenerateSummary(ctx context.Context, _ []shared.Finding) (string, error) {
	return f.GenerateText(ctx, "")
}

func (f *fakeProvider) GenerateText(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// captureLogs swaps slog's default logger for one that writes to a
// buffer, restoring the previous default on test cleanup. Used to
// assert that the silent fallback still leaves a debugging trail.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// --- BuildPRNarrativePrompt tests ---

func TestBuildPRNarrativePrompt_HappyPath(t *testing.T) {
	d := happyPathDiff()
	prompt := BuildPRNarrativePrompt(d)

	// Total monthly delta appears verbatim with the formatting used by
	// the renderer. The model should see exactly what the comment shows.
	if !strings.Contains(prompt, "+$389.35") {
		t.Errorf("prompt missing total delta '+$389.35'; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Direction: increase") {
		t.Errorf("prompt missing direction word 'increase'")
	}

	// Top three movers should be present by address — the fixture sorts
	// aurora ($204.40), db ($71.36), web ($64.74) into the leading slots.
	for _, addr := range []string{
		"aws_rds_cluster_instance.aurora",
		"aws_db_instance.db",
		"aws_instance.web",
	} {
		if !strings.Contains(prompt, addr) {
			t.Errorf("prompt missing top mover %q", addr)
		}
	}

	// Lower-ranked movers must NOT appear (we cap at three to keep the
	// model focused on the dominant items).
	if strings.Contains(prompt, "aws_lambda_function.fn") {
		t.Errorf("prompt unexpectedly includes 4th+ mover (lambda fn)")
	}

	// Notable caveats should surface at least one of the per-resource
	// note categories from the fixture.
	for _, want := range []string{
		"Operating system assumed Linux",
		"Aurora Multi-AZ",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing caveat %q", want)
		}
	}

	// The three task instructions must be present.
	for _, want := range []string{
		"1-3 sentences",
		"PRIMARY DRIVER",
		"if X, consider Y",
		"Output only the prose",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing task instruction %q", want)
		}
	}
}

func TestBuildPRNarrativePrompt_EmptyPlan(t *testing.T) {
	d := CostDiff{
		Currency:   "USD",
		Confidence: pricing.ConfidenceHigh,
	}
	prompt := BuildPRNarrativePrompt(d)
	if !strings.Contains(prompt, "No priceable changes") {
		t.Errorf("empty-plan prompt missing 'No priceable changes' phrasing; got:\n%s", prompt)
	}
	// Even on an empty plan the task block should still tell the model
	// what to do — so it produces *something* rather than nothing.
	if !strings.Contains(prompt, "Output only the prose") {
		t.Errorf("empty-plan prompt missing task block")
	}
}

func TestBuildPRNarrativePrompt_AllSkipped(t *testing.T) {
	mk := func(addr string) pricing.ChangeEstimate {
		return pricing.ChangeEstimate{
			ResourceAddress: addr,
			ResourceType:    "aws_iam_role",
			Action:          iac.ActionCreate,
			Currency:        "USD",
			Skipped:         true,
			SkipReason:      "unsupported resource type: aws_iam_role",
		}
	}
	all := []pricing.ChangeEstimate{mk("aws_iam_role.r1"), mk("aws_iam_role.r2")}
	d := CostDiff{
		Changes: all,
		Skipped: all,
		Notes:   []string{"2 resources skipped (2 unsupported types, 0 estimation failures)"},
		Stats:   Stats{Total: 2, Skipped: 2},
	}
	prompt := BuildPRNarrativePrompt(d)
	if !strings.Contains(prompt, "No priced resources") {
		t.Errorf("all-skipped prompt missing 'No priced resources' phrasing; got:\n%s", prompt)
	}
}

func TestBuildPRNarrativePrompt_NetDecrease(t *testing.T) {
	d := CostDiff{
		TotalMonthlyDelta: -120.50,
		Changes: []pricing.ChangeEstimate{
			ce("aws_instance.old", "aws_instance", iac.ActionDelete, -120.50, pricing.ConfidenceLow),
		},
		Deleted:    []pricing.ChangeEstimate{ce("aws_instance.old", "aws_instance", iac.ActionDelete, -120.50, pricing.ConfidenceLow)},
		TopMovers:  []pricing.ChangeEstimate{ce("aws_instance.old", "aws_instance", iac.ActionDelete, -120.50, pricing.ConfidenceLow)},
		Confidence: pricing.ConfidenceLow,
		Stats:      Stats{Total: 1, Deleted: 1, Priced: 1},
	}
	prompt := BuildPRNarrativePrompt(d)

	// "decrease" (or "savings", per the spec) must appear in the
	// direction line so the model frames the narrative correctly.
	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "decrease") && !strings.Contains(lower, "savings") {
		t.Errorf("net-decrease prompt missing 'decrease' or 'savings' wording; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "-$120.50") {
		t.Errorf("net-decrease prompt missing signed delta '-$120.50'")
	}
}

// TestBuildPRNarrativePrompt_GoldenSnapshot writes / compares a stable
// reference file under testdata/narrative_prompts/. The aim isn't strict
// regression: it's to give a human-readable record of what the prompt
// shape looks like for the canonical happy-path case. Run with -update
// to refresh after intentional changes.
func TestBuildPRNarrativePrompt_GoldenSnapshot(t *testing.T) {
	prompt := BuildPRNarrativePrompt(happyPathDiff())
	path := filepath.Join("testdata", "narrative_prompts", "happy_path.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(prompt), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v (run `go test -update ./internal/diff/...` to create it)", path, err)
	}
	if string(want) != prompt {
		t.Errorf("prompt drifted from %s\n--- got ---\n%s\n--- want ---\n%s", path, prompt, string(want))
	}
}

// --- RenderMarkdownWithLLM tests ---

func TestRenderMarkdownWithLLM_HappyPath(t *testing.T) {
	narrative := "The Aurora cluster instance dominates this change at ~$204/month, roughly half the total; if this is a non-prod environment, an aws_db_instance running db.t3.medium would land around $60/mo for similar functional coverage."
	provider := &fakeProvider{response: narrative}

	out := RenderMarkdownWithLLM(context.Background(), happyPathDiff(), provider)

	if !strings.Contains(out, narrative) {
		t.Errorf("rendered output missing LLM narrative:\n%s", out)
	}
	// Templated narrative must NOT be in the output — the LLM version
	// replaces it.
	if strings.Contains(out, "This plan adds 6 resources") {
		t.Errorf("templated narrative leaked into LLM render")
	}
	if provider.gotPrompt == "" {
		t.Errorf("provider was not called")
	}
}

func TestRenderMarkdownWithLLM_NilProvider(t *testing.T) {
	d := happyPathDiff()
	want := RenderMarkdown(d)
	got := RenderMarkdownWithLLM(context.Background(), d, nil)
	if got != want {
		t.Errorf("nil-provider output should equal RenderMarkdown(d) byte-for-byte\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderMarkdownWithLLM_LLMError(t *testing.T) {
	logs := captureLogs(t)
	provider := &fakeProvider{
		name: "claude",
		err:  errors.New("rate limit exceeded"),
	}
	d := happyPathDiff()
	got := RenderMarkdownWithLLM(context.Background(), d, provider)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("LLM error should fall back to templated render byte-for-byte")
	}
	logged := logs.String()
	if !strings.Contains(logged, "LLM provider returned error") {
		t.Errorf("expected slog.Warn for LLM error; got logs:\n%s", logged)
	}
	if !strings.Contains(logged, "rate limit exceeded") {
		t.Errorf("expected underlying error to appear in logs; got:\n%s", logged)
	}
}

func TestRenderMarkdownWithLLM_EmptyResponse(t *testing.T) {
	logs := captureLogs(t)
	provider := &fakeProvider{response: ""}
	d := happyPathDiff()
	got := RenderMarkdownWithLLM(context.Background(), d, provider)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("empty LLM response should fall back to templated render")
	}
	if !strings.Contains(logs.String(), "empty/whitespace") {
		t.Errorf("expected slog.Warn for empty response; got:\n%s", logs.String())
	}
}

func TestRenderMarkdownWithLLM_WhitespaceOnly(t *testing.T) {
	provider := &fakeProvider{response: "   \n  \t\n  "}
	d := happyPathDiff()
	got := RenderMarkdownWithLLM(context.Background(), d, provider)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("whitespace-only LLM response should fall back to templated render")
	}
}

func TestRenderMarkdownWithLLM_TooLongResponse(t *testing.T) {
	logs := captureLogs(t)
	// Build a 1500-char response — the spec caps at ~500.
	provider := &fakeProvider{response: strings.Repeat("a", 1500)}
	d := happyPathDiff()
	got := RenderMarkdownWithLLM(context.Background(), d, provider)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("oversize LLM response should fall back to templated render")
	}
	if !strings.Contains(logs.String(), "exceeded max length") {
		t.Errorf("expected slog.Warn for oversize response; got:\n%s", logs.String())
	}
}

func TestRenderMarkdownWithLLM_TrimsResponse(t *testing.T) {
	clean := "The Aurora instance dominates at $204/mo."
	provider := &fakeProvider{response: "\n\n  " + clean + "\n\n   "}
	out := RenderMarkdownWithLLM(context.Background(), happyPathDiff(), provider)

	// The cleaned narrative is what the template should render — which
	// means the line containing the narrative is exactly `clean` with no
	// surrounding extra blank lines (the template inserts its own
	// surrounding blank lines).
	if !strings.Contains(out, clean) {
		t.Errorf("cleaned narrative not in output")
	}
	if strings.Contains(out, "  "+clean) {
		t.Errorf("leading whitespace was not trimmed before insertion")
	}
}

func TestRenderMarkdownWithLLM_StripsPreamble(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{"here-is-narrative", "Here is the narrative: The Aurora cluster is the driver.", "The Aurora cluster is the driver."},
		{"heres-narrative", "Here's the narrative: The Aurora cluster is the driver.", "The Aurora cluster is the driver."},
		{"sure-comma", "Sure, the Aurora cluster is the driver.", "the Aurora cluster is the driver."},
		{"of-course", "Of course! The Aurora cluster is the driver.", "The Aurora cluster is the driver."},
		{"certainly", "Certainly. The Aurora cluster is the driver.", "The Aurora cluster is the driver."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &fakeProvider{response: c.raw}
			out := RenderMarkdownWithLLM(context.Background(), happyPathDiff(), provider)
			if !strings.Contains(out, c.wantSub) {
				t.Errorf("expected stripped narrative %q in output; got:\n%s", c.wantSub, out)
			}
			// The preamble itself should not leak through.
			if strings.Contains(out, "Here is the narrative:") || strings.Contains(out, "Here's the narrative:") {
				t.Errorf("preamble leaked into output:\n%s", out)
			}
		})
	}
}

func TestRenderMarkdownWithLLM_ContextCancellation(t *testing.T) {
	logs := captureLogs(t)
	provider := &fakeProvider{response: "should never be returned", delay: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	d := happyPathDiff()
	got := RenderMarkdownWithLLM(ctx, d, provider)
	want := RenderMarkdown(d)
	if got != want {
		t.Errorf("cancelled context should fall back to templated render")
	}
	logged := logs.String()
	if !strings.Contains(logged, "context already cancelled") && !strings.Contains(logged, "context cancelled") {
		t.Errorf("expected slog.Warn for context cancellation; got:\n%s", logged)
	}
}

func TestRenderMarkdownWithLLMConfig_RespectsConfig(t *testing.T) {
	provider := &fakeProvider{response: "Aurora drives this."}
	d := happyPathDiff()
	out := RenderMarkdownWithLLMConfig(context.Background(), d, provider, MarkdownConfig{
		HideFullBreakdown: true,
		HideCaveats:       true,
		CommentMarker:     "test-marker",
	})
	if strings.Contains(out, "Full breakdown") {
		t.Errorf("HideFullBreakdown=true was ignored")
	}
	if strings.Contains(out, "Assumptions and caveats") {
		t.Errorf("HideCaveats=true was ignored")
	}
	if !strings.Contains(out, "<!-- test-marker -->") {
		t.Errorf("custom CommentMarker missing")
	}
	if !strings.Contains(out, "Aurora drives this.") {
		t.Errorf("LLM narrative missing under custom config")
	}
}

// --- Compile-time check that fakeProvider satisfies llm.Provider ---

var _ llm.Provider = (*fakeProvider)(nil)

// --- cleanNarrative direct unit tests ---

func TestCleanNarrative(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"\n\n\t  \n", ""},
		{"clean text", "clean text"},
		{"  padded  ", "padded"},
		{"Here is the narrative: ACTUAL", "ACTUAL"},
		{"here's the narrative: ACTUAL", "ACTUAL"},
		{"Sure, ACTUAL", "ACTUAL"},
		{"Of course. ACTUAL", "ACTUAL"},
		// Preamble matching is anchored at the start; an inline "Sure" doesn't strip.
		{"Pricing is sure to surprise.", "Pricing is sure to surprise."},
	}
	for _, c := range cases {
		if got := cleanNarrative(c.in); got != c.want {
			t.Errorf("cleanNarrative(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirectionWord(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "neutral"},
		{0.001, "neutral"},
		{-0.001, "neutral"},
		{50, "increase"},
		{-50, "decrease (savings)"},
	}
	for _, c := range cases {
		if got := directionWord(c.in); got != c.want {
			t.Errorf("directionWord(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStatsSummary(t *testing.T) {
	cases := []struct {
		in   Stats
		want string
	}{
		{Stats{}, "no changes"},
		{Stats{Created: 1}, "1 created"},
		{Stats{Created: 2, Deleted: 1, Skipped: 3}, "2 created, 1 deleted, 3 skipped"},
		{Stats{Updated: 5, Replaced: 2}, "5 updated, 2 replaced"},
	}
	for _, c := range cases {
		if got := statsSummary(c.in); got != c.want {
			t.Errorf("statsSummary(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCollectCaveats_DedupesAndCaps(t *testing.T) {
	d := CostDiff{
		Notes: []string{"plan-wide A", "plan-wide B"},
		Changes: []pricing.ChangeEstimate{
			withNotes(ce("a", "t", iac.ActionCreate, 1, pricing.ConfidenceLow), "plan-wide A", "per-res X"),
			withNotes(ce("b", "t", iac.ActionCreate, 1, pricing.ConfidenceLow), "per-res X", "per-res Y"),
		},
	}
	got := collectCaveats(d, 10)
	want := []string{"plan-wide A", "plan-wide B", "per-res X", "per-res Y"}
	if len(got) != len(want) {
		t.Fatalf("got %d caveats, want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("caveat[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if capped := collectCaveats(d, 2); len(capped) != 2 {
		t.Errorf("cap=2 returned %d items: %v", len(capped), capped)
	}
}
