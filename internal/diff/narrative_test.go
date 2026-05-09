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

	// Lower-ranked movers must NOT appear in the Top resources block
	// (we cap at three to keep the model focused on the dominant items).
	// fn's caveat may legitimately appear in the "Other" caveat block
	// so we narrow the search to the top-resources section.
	topStart := strings.Index(prompt, "# Top resources by impact")
	topEnd := strings.Index(prompt[topStart:], "\n# ")
	if topStart < 0 || topEnd <= 0 {
		t.Fatalf("could not locate '# Top resources' section in prompt:\n%s", prompt)
	}
	topBlock := prompt[topStart : topStart+topEnd]
	if strings.Contains(topBlock, "aws_lambda_function.fn") {
		t.Errorf("Top resources block unexpectedly includes 4th+ mover (lambda fn):\n%s", topBlock)
	}

	// Caveats are grouped by resource. The Aurora primary-driver block
	// names the address explicitly; "Aurora Multi-AZ" is in its bullets.
	if !strings.Contains(prompt, "# Notable assumptions for the primary driver (aws_rds_cluster_instance.aurora)") {
		t.Errorf("primary-driver caveat heading missing or wrong; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Aurora Multi-AZ") {
		t.Errorf("Aurora Multi-AZ caveat missing")
	}

	// Other resources' caveats appear under "Other ..." with explicit
	// address prefixes — this is the line that prevents the LLM from
	// attributing a NAT or web caveat to the RDS driver.
	if !strings.Contains(prompt, "- aws_instance.web: Operating system assumed Linux") {
		t.Errorf("web's Linux caveat missing or not attributed to its address; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "- aws_nat_gateway.nat: Hourly gateway charge only") {
		t.Errorf("NAT gateway caveat must be address-prefixed in the 'Other' block")
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

// --- Caveat grouping (hotfix to prevent caveat hallucination) ---

// caveatGroupedDiff returns a deterministic CostDiff used by the
// grouping tests below. Primary driver = aws_instance.web (top mover);
// "other" = aws_db_instance.db with one note; one plan-wide note.
func caveatGroupedDiff() CostDiff {
	web := withNotes(
		ce("aws_instance.web", "aws_instance", iac.ActionCreate, 100, pricing.ConfidenceLow),
		"Operating system assumed Linux",
		"Pricing assumes On-Demand",
	)
	db := withNotes(
		ce("aws_db_instance.db", "aws_db_instance", iac.ActionCreate, 50, pricing.ConfidenceLow),
		"License: No license required",
	)
	all := []pricing.ChangeEstimate{web, db}
	return CostDiff{
		TotalMonthlyDelta: 150,
		Currency:          "USD",
		Changes:           all,
		Created:           all,
		TopMovers:         all,
		Confidence:        pricing.ConfidenceLow,
		Notes:             []string{"Net cost increase this plan"},
		Stats:             Stats{Total: 2, Created: 2, Priced: 2},
	}
}

func TestBuildPRNarrativePrompt_CaveatsAreGroupedByResource(t *testing.T) {
	prompt := BuildPRNarrativePrompt(caveatGroupedDiff())

	// Sub-block 1: primary header names the address explicitly.
	if !strings.Contains(prompt, "# Notable assumptions for the primary driver (aws_instance.web)") {
		t.Errorf("primary-driver heading missing or wrong; got:\n%s", prompt)
	}
	// Primary's own notes appear without an address prefix (the heading
	// already binds them).
	for _, want := range []string{"- Operating system assumed Linux", "- Pricing assumes On-Demand"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("primary note %q missing", want)
		}
	}

	// Sub-block 2: other resources are explicitly attributed via prefix.
	if !strings.Contains(prompt, "# Other notable assumptions (do NOT attribute to the primary driver)") {
		t.Errorf("'other' heading missing")
	}
	if !strings.Contains(prompt, "- aws_db_instance.db: License: No license required") {
		t.Errorf("'other' note missing its address prefix; got:\n%s", prompt)
	}

	// Critical anti-hallucination check: the primary driver's address
	// must not appear inside the "Other" sub-block (would defeat the
	// purpose of the split).
	otherIdx := strings.Index(prompt, "# Other notable assumptions")
	planIdx := strings.Index(prompt, "# Plan-wide notes")
	if otherIdx < 0 || planIdx < 0 || planIdx < otherIdx {
		t.Fatalf("section ordering wrong (other=%d plan=%d)", otherIdx, planIdx)
	}
	otherBlock := prompt[otherIdx:planIdx]
	if strings.Contains(otherBlock, "aws_instance.web:") {
		t.Errorf("primary driver leaked into 'Other' block:\n%s", otherBlock)
	}

	// Sub-block 3: plan-wide.
	if !strings.Contains(prompt, "# Plan-wide notes\n- Net cost increase this plan") {
		t.Errorf("plan-wide note missing or in wrong format")
	}
}

func TestBuildPRNarrativePrompt_NoNotesOmitsSection(t *testing.T) {
	web := ce("aws_instance.web", "aws_instance", iac.ActionCreate, 100, pricing.ConfidenceLow)
	all := []pricing.ChangeEstimate{web}
	d := CostDiff{
		TotalMonthlyDelta: 100,
		Changes:           all,
		Created:           all,
		TopMovers:         all,
		Confidence:        pricing.ConfidenceLow,
		Stats:             Stats{Total: 1, Created: 1, Priced: 1},
	}
	prompt := BuildPRNarrativePrompt(d)
	for _, marker := range []string{
		"# Notable assumptions for the primary driver",
		"# Other notable assumptions",
		"# Plan-wide notes",
	} {
		if strings.Contains(prompt, marker) {
			t.Errorf("expected %q to be omitted when there are no notes; got:\n%s", marker, prompt)
		}
	}
}

func TestBuildPRNarrativePrompt_OnlyPrimaryHasNotes(t *testing.T) {
	web := withNotes(
		ce("aws_instance.web", "aws_instance", iac.ActionCreate, 100, pricing.ConfidenceLow),
		"Linux assumed",
	)
	db := ce("aws_db_instance.db", "aws_db_instance", iac.ActionCreate, 50, pricing.ConfidenceLow)
	all := []pricing.ChangeEstimate{web, db}
	d := CostDiff{
		TotalMonthlyDelta: 150,
		Changes:           all,
		Created:           all,
		TopMovers:         all,
		Stats:             Stats{Total: 2, Created: 2, Priced: 2},
	}
	prompt := BuildPRNarrativePrompt(d)
	if !strings.Contains(prompt, "# Notable assumptions for the primary driver (aws_instance.web)") {
		t.Errorf("primary heading missing")
	}
	if strings.Contains(prompt, "# Other notable assumptions") {
		t.Errorf("'other' block should be omitted when no other resource has notes")
	}
	if strings.Contains(prompt, "# Plan-wide notes") {
		t.Errorf("plan-wide block should be omitted when d.Notes is empty")
	}
}

func TestBuildPRNarrativePrompt_OnlyOthersHaveNotes(t *testing.T) {
	web := ce("aws_instance.web", "aws_instance", iac.ActionCreate, 100, pricing.ConfidenceLow)
	db := withNotes(
		ce("aws_db_instance.db", "aws_db_instance", iac.ActionCreate, 50, pricing.ConfidenceLow),
		"License assumed",
	)
	all := []pricing.ChangeEstimate{web, db}
	d := CostDiff{
		TotalMonthlyDelta: 150,
		Changes:           all,
		Created:           all,
		TopMovers:         all,
		Stats:             Stats{Total: 2, Created: 2, Priced: 2},
	}
	prompt := BuildPRNarrativePrompt(d)
	if strings.Contains(prompt, "# Notable assumptions for the primary driver") {
		t.Errorf("primary block should be omitted when primary has no notes")
	}
	if !strings.Contains(prompt, "# Other notable assumptions (do NOT attribute to the primary driver)") {
		t.Errorf("'other' heading missing")
	}
	if !strings.Contains(prompt, "- aws_db_instance.db: License assumed") {
		t.Errorf("'other' note missing its address prefix")
	}
}

func TestBuildPRNarrativePrompt_NoTopMovers(t *testing.T) {
	// All-skipped plan: no TopMovers, so no primary driver. Only
	// plan-wide notes should render.
	mk := func(addr string) pricing.ChangeEstimate {
		return pricing.ChangeEstimate{
			ResourceAddress: addr,
			ResourceType:    "aws_iam_role",
			Action:          iac.ActionCreate,
			Skipped:         true,
			SkipReason:      "unsupported",
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
	if strings.Contains(prompt, "# Notable assumptions for the primary driver") {
		t.Errorf("primary block should be omitted when there is no primary")
	}
	if strings.Contains(prompt, "# Other notable assumptions") {
		t.Errorf("'other' block should be omitted when no resource carries per-resource notes")
	}
	if !strings.Contains(prompt, "# Plan-wide notes\n- 2 resources skipped") {
		t.Errorf("plan-wide notes missing or malformed; got:\n%s", prompt)
	}
}

func TestBuildPRNarrativePrompt_ContainsBillingModelDoNot(t *testing.T) {
	prompt := BuildPRNarrativePrompt(happyPathDiff())
	if !strings.Contains(prompt, "Suggest billing-model alternatives") {
		t.Errorf("new billing-model DO NOT rule missing from prompt")
	}
	if !strings.Contains(prompt, "Reserved Instances") {
		t.Errorf("billing-model rule should name the things it forbids (RI/SP/Spot)")
	}
}
