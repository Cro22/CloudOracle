package diff

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/pricing"
)

// updateGoldens regenerates the testdata/markdown_*.md fixtures from
// the current renderer output. Run with `go test -update
// ./internal/diff/...` after an intentional template change. Without
// the flag, tests compare against the existing files.
var updateGoldens = flag.Bool("update", false, "update golden Markdown files")

// goldenCheck renders the diff with default config and compares against
// or rewrites the golden file. The comparison is byte-exact — Markdown
// whitespace matters because GitHub renders subtle differences (extra
// blank line collapses paragraphs into one).
func goldenCheck(t *testing.T, name string, d CostDiff) {
	t.Helper()
	got := RenderMarkdown(d)
	path := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing golden %q: %v", path, err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %q: %v (run with -update to create it)", path, err)
	}
	want := string(wantBytes)
	if got != want {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			path, got, want)
	}
}

// ce builds a ChangeEstimate test fixture. Keeps the test cases short.
func ce(addr, typ string, action iac.Action, delta float64, conf pricing.Confidence) pricing.ChangeEstimate {
	return pricing.ChangeEstimate{
		ResourceAddress: addr,
		ResourceType:    typ,
		Action:          action,
		MonthlyDelta:    delta,
		Currency:        "USD",
		Confidence:      conf,
	}
}

// withBreakdown wraps a ChangeEstimate adding line items so the full
// breakdown section has something interesting to render.
func withBreakdown(c pricing.ChangeEstimate, items ...pricing.LineItem) pricing.ChangeEstimate {
	c.Breakdown = append([]pricing.LineItem(nil), items...)
	return c
}

func withNotes(c pricing.ChangeEstimate, notes ...string) pricing.ChangeEstimate {
	c.Notes = append([]string(nil), notes...)
	return c
}

// happyPathDiff mimics the checkpoint-13.5 output: 6 resources, all
// creates, $389.35 total. This is the canonical PR-comment shape.
func happyPathDiff() CostDiff {
	web := withNotes(
		withBreakdown(
			ce("aws_instance.web", "aws_instance", iac.ActionCreate, 64.74, pricing.ConfidenceLow),
			pricing.LineItem{Component: "Compute", MonthlyUSD: 60.74},
			pricing.LineItem{Component: "RootEBS", MonthlyUSD: 4.00},
		),
		"Operating system assumed Linux (plan does not specify)",
		"Pricing assumes On-Demand (no Reserved Instances or Savings Plans)",
	)
	web.AfterMonthly = 64.74

	db := withNotes(
		withBreakdown(
			ce("aws_db_instance.db", "aws_db_instance", iac.ActionCreate, 71.36, pricing.ConfidenceLow),
			pricing.LineItem{Component: "Compute", MonthlyUSD: 59.86},
			pricing.LineItem{Component: "Storage", MonthlyUSD: 11.50},
		),
		"License: No license required (postgres/mysql/mariadb)",
	)
	db.AfterMonthly = 71.36

	disk := withNotes(
		withBreakdown(
			ce("aws_ebs_volume.disk", "aws_ebs_volume", iac.ActionCreate, 16.00, pricing.ConfidenceMedium),
			pricing.LineItem{Component: "Storage", MonthlyUSD: 16.00},
		),
		"IOPS-month and throughput-month charges not included for gp3 above defaults (3000 IOPS, 125 MB/s)",
	)
	disk.AfterMonthly = 16.00

	fn := withNotes(
		ce("aws_lambda_function.fn", "aws_lambda_function", iac.ActionCreate, 0, pricing.ConfidenceLow),
		"Standing cost is $0; per-invocation charges (requests + GB-seconds) not modeled",
	)

	nat := withNotes(
		withBreakdown(
			ce("aws_nat_gateway.nat", "aws_nat_gateway", iac.ActionCreate, 32.85, pricing.ConfidenceMedium),
			pricing.LineItem{Component: "Gateway", MonthlyUSD: 32.85},
		),
		"Hourly gateway charge only; per-GB data processing charges (~$0.045/GB) not modeled",
	)
	nat.AfterMonthly = 32.85

	aurora := withNotes(
		withBreakdown(
			ce("aws_rds_cluster_instance.aurora", "aws_rds_cluster_instance", iac.ActionCreate, 204.40, pricing.ConfidenceLow),
			pricing.LineItem{Component: "Compute", MonthlyUSD: 204.40},
		),
		"Cluster-level storage and I/O charges not included (priced at aws_rds_cluster)",
		"Aurora Multi-AZ is via reader replicas (multiple aws_rds_cluster_instance), not a per-instance flag",
		"Pricing assumes standard Aurora mode (storage=EBS Only); I/O Optimization Mode is not modeled",
	)
	aurora.AfterMonthly = 204.40

	all := []pricing.ChangeEstimate{aurora, db, web, nat, disk, fn} // sorted by abs delta desc

	return CostDiff{
		TotalMonthlyDelta: 389.35,
		Currency:          "USD",
		Changes:           all,
		Created:           all,
		TopMovers:         all[:5],
		Confidence:        pricing.ConfidenceLow,
		Notes:             []string{"Net cost increase this plan"},
		Stats: Stats{
			Total: 6, Created: 6, Priced: 6,
		},
	}
}

func TestRenderMarkdown_HappyPath(t *testing.T) {
	goldenCheck(t, "markdown_happy_path.md", happyPathDiff())
}

func TestRenderMarkdown_NetDecrease(t *testing.T) {
	web := withBreakdown(
		ce("aws_instance.web", "aws_instance", iac.ActionDelete, -64.74, pricing.ConfidenceLow),
		pricing.LineItem{Component: "Compute", MonthlyUSD: -60.74},
		pricing.LineItem{Component: "RootEBS", MonthlyUSD: -4.00},
	)
	web.BeforeMonthly = 64.74
	disk := withBreakdown(
		ce("aws_ebs_volume.disk", "aws_ebs_volume", iac.ActionDelete, -16.00, pricing.ConfidenceMedium),
		pricing.LineItem{Component: "Storage", MonthlyUSD: -16.00},
	)
	disk.BeforeMonthly = 16.00

	all := []pricing.ChangeEstimate{web, disk}
	d := CostDiff{
		TotalMonthlyDelta: -80.74,
		Currency:          "USD",
		Changes:           all,
		Deleted:           all,
		TopMovers:         all,
		Confidence:        pricing.ConfidenceLow,
		Notes:             []string{"Net cost reduction this plan"},
		Stats:             Stats{Total: 2, Deleted: 2, Priced: 2},
	}
	goldenCheck(t, "markdown_net_decrease.md", d)
}

func TestRenderMarkdown_NetZero(t *testing.T) {
	// Replace where before == after price. Common when changing tags
	// or other non-cost attributes.
	a := withBreakdown(
		ce("aws_instance.a", "aws_instance", iac.ActionReplace, 0, pricing.ConfidenceLow),
	)
	a.BeforeMonthly = 50
	a.AfterMonthly = 50

	all := []pricing.ChangeEstimate{a}
	d := CostDiff{
		TotalMonthlyDelta: 0,
		Currency:          "USD",
		Changes:           all,
		Replaced:          all,
		TopMovers:         all,
		Confidence:        pricing.ConfidenceLow,
		Notes:             []string{"Net zero cost change"},
		Stats:             Stats{Total: 1, Replaced: 1, Priced: 1},
	}
	goldenCheck(t, "markdown_net_zero.md", d)
}

func TestRenderMarkdown_EmptyPlan(t *testing.T) {
	d := CostDiff{
		Currency:   "USD",
		Confidence: pricing.ConfidenceHigh,
		Notes:      []string{"No priceable resources in plan"},
		Stats:      Stats{},
	}
	goldenCheck(t, "markdown_empty_plan.md", d)
}

func TestRenderMarkdown_AllSkipped(t *testing.T) {
	mk := func(addr string) pricing.ChangeEstimate {
		return pricing.ChangeEstimate{
			ResourceAddress: addr,
			ResourceType:    "aws_iam_role",
			Action:          iac.ActionCreate,
			Currency:        "USD",
			Skipped:         true,
			SkipReason:      "unsupported resource type: aws_iam_role",
			Confidence:      pricing.ConfidenceHigh,
		}
	}
	all := []pricing.ChangeEstimate{
		mk("aws_iam_role.r1"), mk("aws_iam_role.r2"), mk("aws_iam_role.r3"),
	}
	d := CostDiff{
		Currency:   "USD",
		Changes:    all,
		Skipped:    all,
		Confidence: pricing.ConfidenceHigh,
		Notes: []string{
			"3 resources skipped (3 unsupported types, 0 estimation failures)",
			"No priceable resources in plan",
		},
		Stats: Stats{Total: 3, Skipped: 3},
	}
	goldenCheck(t, "markdown_all_skipped.md", d)
}

func TestRenderMarkdown_WithEstimationErrors(t *testing.T) {
	web := withBreakdown(
		ce("aws_instance.web", "aws_instance", iac.ActionCreate, 64.74, pricing.ConfidenceLow),
		pricing.LineItem{Component: "Compute", MonthlyUSD: 60.74},
		pricing.LineItem{Component: "RootEBS", MonthlyUSD: 4.00},
	)
	web.AfterMonthly = 64.74

	broken := pricing.ChangeEstimate{
		ResourceAddress: "aws_instance.broken",
		ResourceType:    "aws_instance",
		Action:          iac.ActionCreate,
		Currency:        "USD",
		Skipped:         true,
		SkipReason:      "estimation failed: AccessDenied calling pricing API",
		Confidence:      pricing.ConfidenceHigh,
	}
	d := CostDiff{
		TotalMonthlyDelta: 64.74,
		Currency:          "USD",
		Changes:           []pricing.ChangeEstimate{web, broken},
		Created:           []pricing.ChangeEstimate{web},
		Skipped:           []pricing.ChangeEstimate{broken},
		TopMovers:         []pricing.ChangeEstimate{web},
		Confidence:        pricing.ConfidenceLow,
		Notes: []string{
			"1 resources skipped (0 unsupported types, 1 estimation failures)",
			"Net cost increase this plan",
		},
		Stats: Stats{Total: 2, Created: 1, Skipped: 1, Priced: 1},
	}
	goldenCheck(t, "markdown_with_estimation_errors.md", d)
}

func TestRenderMarkdown_MixedActions(t *testing.T) {
	cre := withBreakdown(
		ce("aws_instance.new", "aws_instance", iac.ActionCreate, 100, pricing.ConfidenceLow),
		pricing.LineItem{Component: "Compute", MonthlyUSD: 100},
	)
	del := withBreakdown(
		ce("aws_ebs_volume.old", "aws_ebs_volume", iac.ActionDelete, -16, pricing.ConfidenceMedium),
		pricing.LineItem{Component: "Storage", MonthlyUSD: -16},
	)
	upd := withBreakdown(
		ce("aws_db_instance.db", "aws_db_instance", iac.ActionUpdate, 25, pricing.ConfidenceLow),
		pricing.LineItem{Component: "Compute", MonthlyUSD: 25},
		pricing.LineItem{Component: "Storage", MonthlyUSD: 0},
	)
	rep := withBreakdown(
		ce("aws_lambda_function.fn", "aws_lambda_function", iac.ActionReplace, 5, pricing.ConfidenceLow),
		pricing.LineItem{Component: "ProvisionedConcurrency", MonthlyUSD: 5},
	)
	// Sorted by abs delta: 100, 25, -16, 5
	sorted := []pricing.ChangeEstimate{cre, upd, del, rep}
	d := CostDiff{
		TotalMonthlyDelta: 114,
		Currency:          "USD",
		Changes:           sorted,
		Created:           []pricing.ChangeEstimate{cre},
		Deleted:           []pricing.ChangeEstimate{del},
		Updated:           []pricing.ChangeEstimate{upd},
		Replaced:          []pricing.ChangeEstimate{rep},
		TopMovers:         sorted,
		Confidence:        pricing.ConfidenceLow,
		Notes:             []string{"Net cost increase this plan"},
		Stats: Stats{
			Total: 4, Created: 1, Deleted: 1, Updated: 1, Replaced: 1, Priced: 4,
		},
	}
	goldenCheck(t, "markdown_mixed_actions.md", d)
}

func TestFormatDelta(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{0.001, "$0.00"},
		{-0.001, "$0.00"},
		{0.004, "$0.00"},
		{0.005, "+$0.01"},
		{-0.005, "-$0.01"},
		{60.74, "+$60.74"},
		{-50.0, "-$50.00"},
		{1234.567, "+$1234.57"},
	}
	for _, c := range cases {
		if got := formatDelta(c.in); got != c.want {
			t.Errorf("formatDelta(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrendEmoji(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "⚪"},
		{0.001, "⚪"},
		{-0.001, "⚪"},
		{0.005, "🔴"},
		{50, "🔴"},
		{-50, "🟢"},
	}
	for _, c := range cases {
		if got := trendEmoji(c.in); got != c.want {
			t.Errorf("trendEmoji(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActionDisplay(t *testing.T) {
	cases := map[iac.Action]string{
		iac.ActionCreate:  "🆕 create",
		iac.ActionDelete:  "❌ delete",
		iac.ActionUpdate:  "♻️ update",
		iac.ActionReplace: "🔄 replace",
		iac.ActionNoop:    "⏭️ no-op",
		iac.ActionRead:    "⏭️ read",
	}
	for a, want := range cases {
		if got := actionDisplay(a); got != want {
			t.Errorf("actionDisplay(%q) = %q, want %q", a, got, want)
		}
	}
	// Unknown action passes through.
	if got := actionDisplay(iac.Action("import")); got != "import" {
		t.Errorf("actionDisplay(import) = %q, want literal pass-through", got)
	}
}

func TestMarkdownConfig_Defaults(t *testing.T) {
	d := happyPathDiff()
	a := RenderMarkdown(d)
	b := RenderMarkdownWithConfig(d, MarkdownConfig{})
	if a != b {
		t.Errorf("zero MarkdownConfig should produce same output as RenderMarkdown")
	}
}

func TestMarkdownConfig_CustomMarker(t *testing.T) {
	d := happyPathDiff()
	out := RenderMarkdownWithConfig(d, MarkdownConfig{CommentMarker: "my-custom-marker"})
	if !strings.Contains(out, "<!-- my-custom-marker -->") {
		t.Errorf("custom marker not in output")
	}
	if strings.Contains(out, "<!-- cloudoracle-pr-v1 -->") {
		t.Errorf("default marker leaked into output")
	}
}

func TestMarkdownConfig_HideBreakdown(t *testing.T) {
	d := happyPathDiff()
	out := RenderMarkdownWithConfig(d, MarkdownConfig{HideFullBreakdown: true})
	if strings.Contains(out, "Full breakdown") {
		t.Errorf("HideFullBreakdown=true did not omit the section")
	}
	// Caveats still present (only Breakdown was hidden).
	if !strings.Contains(out, "Assumptions and caveats") {
		t.Errorf("Caveats section unexpectedly missing")
	}
}

func TestMarkdownConfig_HideCaveats(t *testing.T) {
	d := happyPathDiff()
	out := RenderMarkdownWithConfig(d, MarkdownConfig{HideCaveats: true})
	if strings.Contains(out, "Assumptions and caveats") {
		t.Errorf("HideCaveats=true did not omit the section")
	}
	if !strings.Contains(out, "Full breakdown") {
		t.Errorf("Breakdown section unexpectedly missing")
	}
}

func TestMarkdownConfig_TopMoversCountClampsDown(t *testing.T) {
	d := happyPathDiff()
	out := RenderMarkdownWithConfig(d, MarkdownConfig{TopMoversCount: 2})
	// Count rows in the top-movers table by counting the table-row prefix.
	rows := strings.Count(out, "\n| `aws_")
	if rows != 2 {
		t.Errorf("got %d top-movers rows, want 2", rows)
	}
}

func TestRenderNarrative_AllShapes(t *testing.T) {
	cases := []struct {
		name string
		d    CostDiff
		want string
	}{
		{
			name: "no priceable",
			d:    CostDiff{Stats: Stats{Total: 3, Skipped: 3}},
			want: "No priceable resources in this plan.",
		},
		{
			name: "increase one create",
			d: CostDiff{
				TotalMonthlyDelta: 50,
				Stats:             Stats{Created: 1, Priced: 1},
			},
			want: "This plan adds 1 resource, with a net monthly cost increase of +$50.00.",
		},
		{
			name: "decrease one delete",
			d: CostDiff{
				TotalMonthlyDelta: -50,
				Stats:             Stats{Deleted: 1, Priced: 1},
			},
			want: "This plan removes 1, with a net monthly cost decrease of -$50.00.",
		},
		{
			name: "zero with replace",
			d: CostDiff{
				TotalMonthlyDelta: 0,
				Stats:             Stats{Replaced: 1, Priced: 1},
			},
			want: "This plan replaces 1, with no net cost change.",
		},
		{
			name: "with estimation failures",
			d: CostDiff{
				TotalMonthlyDelta: 50,
				Stats:             Stats{Created: 1, Priced: 1, Skipped: 1},
				Skipped: []pricing.ChangeEstimate{
					{SkipReason: "estimation failed: timeout"},
				},
			},
			want: "This plan adds 1 resource, with a net monthly cost increase of +$50.00. 1 resource could not be priced.",
		},
		{
			name: "mixed verbs all four",
			d: CostDiff{
				TotalMonthlyDelta: 100,
				Stats:             Stats{Created: 2, Deleted: 1, Updated: 3, Replaced: 1, Priced: 7},
			},
			want: "This plan adds 2 resources, removes 1, updates 3, and replaces 1, with a net monthly cost increase of +$100.00.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderNarrative(c.d); got != c.want {
				t.Errorf("got:\n  %q\nwant:\n  %q", got, c.want)
			}
		})
	}
}

func TestDeduplicateNotes(t *testing.T) {
	c1 := withNotes(ce("aws_instance.a", "aws_instance", iac.ActionCreate, 50, pricing.ConfidenceLow),
		"Operating system assumed Linux", "Common note")
	c2 := withNotes(ce("aws_instance.b", "aws_instance", iac.ActionCreate, 50, pricing.ConfidenceLow),
		"Operating system assumed Linux")
	c3 := withNotes(ce("aws_instance.c", "aws_instance", iac.ActionCreate, 50, pricing.ConfidenceLow),
		"Operating system assumed Linux", "Unique to c")

	got := deduplicateNotes([]pricing.ChangeEstimate{c1, c2, c3})

	if len(got) != 3 {
		t.Fatalf("got %d unique notes, want 3", len(got))
	}
	if got[0].Note != "Operating system assumed Linux" {
		t.Errorf("got[0].Note = %q", got[0].Note)
	}
	if len(got[0].Addresses) != 3 {
		t.Errorf("Linux note addresses = %v, want 3 addresses", got[0].Addresses)
	}
	if got[1].Note != "Common note" || len(got[1].Addresses) != 1 {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[2].Note != "Unique to c" || got[2].Addresses[0] != "aws_instance.c" {
		t.Errorf("got[2] = %+v", got[2])
	}
}
