package diff

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/pricing"
)

// fakeEstimator is the test seam for analyzeWithEstimator. It looks up
// the response by rc.Address; tests build a results map and (optionally)
// an errors map keyed by address. nilSrc satisfies Source for callers
// that don't exercise the underlying call path.
type fakeEstimator struct {
	results map[string]pricing.ChangeEstimate
	errors  map[string]error
}

func (f *fakeEstimator) estimate(_ context.Context, _ Source, rc iac.ResourceChange, _ string) (pricing.ChangeEstimate, error) {
	if err, ok := f.errors[rc.Address]; ok {
		return pricing.ChangeEstimate{}, err
	}
	if r, ok := f.results[rc.Address]; ok {
		return r, nil
	}
	// Default: a no-op-ish skipped result so tests don't crash on
	// missing entries — they'll see the missing address in the diff.
	return pricing.ChangeEstimate{
		ResourceAddress: rc.Address,
		ResourceType:    rc.Type,
		Action:          rc.Action(),
		Skipped:         true,
		SkipReason:      "no result programmed for " + rc.Address,
	}, nil
}

// nilSource is a Source that should never be called (tests inject a
// fake estimator that ignores src). Calling GetProducts panics so a
// regression in the wiring is loud.
type nilSource struct{}

func (nilSource) GetProducts(_ context.Context, _ string, _ map[string]string) ([]string, error) {
	panic("nilSource.GetProducts called — fake estimator should bypass src entirely")
}

// rc is a small constructor for ResourceChange test fixtures.
func rc(addr, typ string, action string) iac.ResourceChange {
	return iac.ResourceChange{
		Address: addr,
		Mode:    "managed",
		Type:    typ,
		Change:  iac.Change{Actions: []string{action}},
	}
}

func plan(rcs ...iac.ResourceChange) *iac.Plan {
	return &iac.Plan{
		FormatVersion:   "1.2",
		ResourceChanges: rcs,
	}
}

func TestAnalyze_HappyPath(t *testing.T) {
	rcs := []iac.ResourceChange{
		rc("aws_instance.web", "aws_instance", "create"),
		rc("aws_ebs_volume.vol", "aws_ebs_volume", "delete"),
		rc("aws_db_instance.db", "aws_db_instance", "update"),
		rc("aws_lambda_function.fn", "aws_lambda_function", "create"),
	}
	// Replace = ["delete","create"]
	rcs = append(rcs, iac.ResourceChange{
		Address: "aws_lambda_function.replaced",
		Mode:    "managed",
		Type:    "aws_lambda_function",
		Change:  iac.Change{Actions: []string{"delete", "create"}},
	})
	p := plan(rcs...)

	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.web": {
			ResourceAddress: "aws_instance.web", ResourceType: "aws_instance",
			Action: iac.ActionCreate, Currency: "USD",
			AfterMonthly: 100, MonthlyDelta: 100, Confidence: pricing.ConfidenceLow,
		},
		"aws_ebs_volume.vol": {
			ResourceAddress: "aws_ebs_volume.vol", ResourceType: "aws_ebs_volume",
			Action: iac.ActionDelete, Currency: "USD",
			BeforeMonthly: 5, MonthlyDelta: -5, Confidence: pricing.ConfidenceMedium,
		},
		"aws_db_instance.db": {
			ResourceAddress: "aws_db_instance.db", ResourceType: "aws_db_instance",
			Action: iac.ActionUpdate, Currency: "USD",
			BeforeMonthly: 50, AfterMonthly: 75, MonthlyDelta: 25, Confidence: pricing.ConfidenceLow,
		},
		"aws_lambda_function.fn": {
			ResourceAddress: "aws_lambda_function.fn", ResourceType: "aws_lambda_function",
			Action: iac.ActionCreate, Currency: "USD",
			AfterMonthly: 0, MonthlyDelta: 0, Confidence: pricing.ConfidenceLow,
		},
		"aws_lambda_function.replaced": {
			ResourceAddress: "aws_lambda_function.replaced", ResourceType: "aws_lambda_function",
			Action: iac.ActionReplace, Currency: "USD",
			BeforeMonthly: 10, AfterMonthly: 30, MonthlyDelta: 20, Confidence: pricing.ConfidenceLow,
		},
	}}

	d, err := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if err != nil {
		t.Fatalf("analyzeWithEstimator: %v", err)
	}

	wantTotal := 100.0 - 5 + 25 + 0 + 20
	if math.Abs(d.TotalMonthlyDelta-wantTotal) > 1e-9 {
		t.Errorf("TotalMonthlyDelta = %v, want %v", d.TotalMonthlyDelta, wantTotal)
	}
	if d.Currency != "USD" {
		t.Errorf("Currency = %q", d.Currency)
	}

	// Sort: 100, 25, 20, -5, 0 (by abs)
	wantOrder := []string{
		"aws_instance.web", "aws_db_instance.db", "aws_lambda_function.replaced",
		"aws_ebs_volume.vol", "aws_lambda_function.fn",
	}
	for i, want := range wantOrder {
		if d.Changes[i].ResourceAddress != want {
			t.Errorf("Changes[%d] = %q, want %q", i, d.Changes[i].ResourceAddress, want)
		}
	}

	if len(d.Created) != 2 {
		t.Errorf("Created len = %d, want 2", len(d.Created))
	}
	if len(d.Deleted) != 1 {
		t.Errorf("Deleted len = %d, want 1", len(d.Deleted))
	}
	if len(d.Updated) != 1 {
		t.Errorf("Updated len = %d, want 1", len(d.Updated))
	}
	if len(d.Replaced) != 1 {
		t.Errorf("Replaced len = %d, want 1", len(d.Replaced))
	}
	if len(d.Skipped) != 0 {
		t.Errorf("Skipped len = %d, want 0", len(d.Skipped))
	}

	// TopMovers: first 5 non-skipped, in delta order.
	if len(d.TopMovers) != 5 {
		t.Errorf("TopMovers len = %d, want 5", len(d.TopMovers))
	}

	// Confidence is the weakest non-skipped: Low (4 Low + 1 Medium).
	if d.Confidence != pricing.ConfidenceLow {
		t.Errorf("Confidence = %q, want low", d.Confidence)
	}
}

func TestAnalyze_AllSkipped(t *testing.T) {
	p := plan(
		rc("aws_iam_role.r1", "aws_iam_role", "create"),
		rc("aws_iam_role.r2", "aws_iam_role", "create"),
		rc("aws_iam_role.r3", "aws_iam_role", "delete"),
	)
	mk := func(addr string) pricing.ChangeEstimate {
		return pricing.ChangeEstimate{
			ResourceAddress: addr, ResourceType: "aws_iam_role", Currency: "USD",
			Action: iac.ActionCreate, Skipped: true,
			SkipReason: "unsupported resource type: aws_iam_role",
			Confidence: pricing.ConfidenceHigh,
		}
	}
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_iam_role.r1": mk("aws_iam_role.r1"),
		"aws_iam_role.r2": mk("aws_iam_role.r2"),
		"aws_iam_role.r3": mk("aws_iam_role.r3"),
	}}
	d, err := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.TotalMonthlyDelta != 0 {
		t.Errorf("TotalMonthlyDelta = %v, want 0", d.TotalMonthlyDelta)
	}
	if d.Confidence != pricing.ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (no priceable changes)", d.Confidence)
	}
	if len(d.Skipped) != 3 {
		t.Errorf("Skipped len = %d, want 3", len(d.Skipped))
	}
	if len(d.TopMovers) != 0 {
		t.Errorf("TopMovers len = %d, want 0", len(d.TopMovers))
	}
	foundNote := false
	for _, n := range d.Notes {
		if strings.Contains(n, "No priceable resources") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("Notes missing 'No priceable resources': %v", d.Notes)
	}
}

func TestAnalyze_EstimationError(t *testing.T) {
	p := plan(
		rc("aws_instance.web", "aws_instance", "create"),
		rc("aws_instance.broken", "aws_instance", "create"),
	)
	fake := &fakeEstimator{
		results: map[string]pricing.ChangeEstimate{
			"aws_instance.web": {
				ResourceAddress: "aws_instance.web", ResourceType: "aws_instance",
				Action: iac.ActionCreate, AfterMonthly: 50, MonthlyDelta: 50,
				Confidence: pricing.ConfidenceLow, Currency: "USD",
			},
		},
		errors: map[string]error{
			"aws_instance.broken": errors.New("AccessDenied"),
		},
	}
	d, err := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// The successful one is in Created; the broken one is in Skipped.
	if len(d.Created) != 1 {
		t.Errorf("Created len = %d, want 1", len(d.Created))
	}
	if len(d.Skipped) != 1 {
		t.Errorf("Skipped len = %d, want 1", len(d.Skipped))
	}
	if got := d.Skipped[0].SkipReason; !strings.Contains(got, "estimation failed") || !strings.Contains(got, "AccessDenied") {
		t.Errorf("SkipReason = %q, missing 'estimation failed'/'AccessDenied'", got)
	}
	if d.Skipped[0].ResourceAddress != "aws_instance.broken" {
		t.Errorf("Skipped[0].Address = %q", d.Skipped[0].ResourceAddress)
	}

	// Plan-wide note breaks down the skip reasons.
	foundBreakdown := false
	for _, n := range d.Notes {
		if strings.Contains(n, "estimation failures") && strings.Contains(n, "1 estimation failures") {
			foundBreakdown = true
		}
	}
	if !foundBreakdown {
		t.Errorf("Notes missing skip-breakdown: %v", d.Notes)
	}
}

func TestAnalyze_NoOp(t *testing.T) {
	p := plan(rc("aws_instance.web", "aws_instance", "no-op"))
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.web": {
			ResourceAddress: "aws_instance.web", ResourceType: "aws_instance",
			Action: iac.ActionNoop, Currency: "USD",
			Skipped: true, SkipReason: "action has no cost impact",
			Confidence: pricing.ConfidenceHigh,
		},
	}}
	d, err := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(d.Created) != 0 || len(d.Updated) != 0 {
		t.Errorf("no-op should not appear in priced slices: created=%d updated=%d", len(d.Created), len(d.Updated))
	}
	if len(d.Skipped) != 1 {
		t.Errorf("Skipped len = %d, want 1", len(d.Skipped))
	}
	if d.Stats.NoOp != 1 {
		t.Errorf("Stats.NoOp = %d, want 1", d.Stats.NoOp)
	}
	if d.Stats.Skipped != 0 {
		t.Errorf("Stats.Skipped = %d, want 0 (no-op is counted under NoOp, not Skipped)", d.Stats.Skipped)
	}
}

func TestAnalyze_NetIncrease(t *testing.T) {
	p := plan(rc("aws_instance.web", "aws_instance", "create"))
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.web": {
			ResourceAddress: "aws_instance.web", Action: iac.ActionCreate,
			AfterMonthly: 50, MonthlyDelta: 50, Confidence: pricing.ConfidenceLow,
		},
	}}
	d, _ := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if !containsNote(d.Notes, "Net cost increase") {
		t.Errorf("expected 'Net cost increase' note, got: %v", d.Notes)
	}
}

func TestAnalyze_NetDecrease(t *testing.T) {
	p := plan(rc("aws_instance.web", "aws_instance", "delete"))
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.web": {
			ResourceAddress: "aws_instance.web", Action: iac.ActionDelete,
			BeforeMonthly: 50, MonthlyDelta: -50, Confidence: pricing.ConfidenceLow,
		},
	}}
	d, _ := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if !containsNote(d.Notes, "Net cost reduction") {
		t.Errorf("expected 'Net cost reduction' note, got: %v", d.Notes)
	}
}

func TestAnalyze_NetZero(t *testing.T) {
	p := plan(rc("aws_instance.web", "aws_instance", "update"))
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.web": {
			ResourceAddress: "aws_instance.web", Action: iac.ActionUpdate,
			BeforeMonthly: 50, AfterMonthly: 50, MonthlyDelta: 0, Confidence: pricing.ConfidenceLow,
		},
	}}
	d, _ := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if !containsNote(d.Notes, "Net zero cost change") {
		t.Errorf("expected 'Net zero cost change' note, got: %v", d.Notes)
	}
}

func TestAnalyze_TopMoversFiltersSkipped(t *testing.T) {
	rcs := make([]iac.ResourceChange, 0, 10)
	for i := 1; i <= 7; i++ {
		rcs = append(rcs, rc("aws_instance."+string(rune('a'+i-1)), "aws_instance", "create"))
	}
	rcs = append(rcs, rc("aws_iam_role.r1", "aws_iam_role", "create"))
	rcs = append(rcs, rc("aws_iam_role.r2", "aws_iam_role", "create"))
	rcs = append(rcs, rc("aws_iam_role.r3", "aws_iam_role", "create"))

	results := map[string]pricing.ChangeEstimate{}
	for i, r := range rcs {
		isSkipped := r.Type == "aws_iam_role"
		ce := pricing.ChangeEstimate{
			ResourceAddress: r.Address, ResourceType: r.Type,
			Action: iac.ActionCreate, Currency: "USD",
			Confidence: pricing.ConfidenceLow,
		}
		if isSkipped {
			ce.Skipped = true
			ce.SkipReason = "unsupported resource type: aws_iam_role"
		} else {
			ce.AfterMonthly = float64(i+1) * 10
			ce.MonthlyDelta = float64(i+1) * 10
		}
		results[r.Address] = ce
	}
	fake := &fakeEstimator{results: results}
	p := plan(rcs...)

	d, err := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(d.TopMovers) != TopMoversCount {
		t.Errorf("TopMovers len = %d, want %d", len(d.TopMovers), TopMoversCount)
	}
	for _, m := range d.TopMovers {
		if m.Skipped {
			t.Errorf("TopMovers contains skipped entry: %+v", m)
		}
	}
	// Ordered by abs delta descending: 70, 60, 50, 40, 30
	wantFirst := 70.0
	if math.Abs(d.TopMovers[0].MonthlyDelta-wantFirst) > 1e-9 {
		t.Errorf("TopMovers[0].MonthlyDelta = %v, want %v", d.TopMovers[0].MonthlyDelta, wantFirst)
	}
}

func TestAnalyze_TopMoversCountClampedDown(t *testing.T) {
	p := plan(
		rc("aws_instance.a", "aws_instance", "create"),
		rc("aws_instance.b", "aws_instance", "create"),
		rc("aws_instance.c", "aws_instance", "create"),
	)
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.a": {ResourceAddress: "aws_instance.a", Action: iac.ActionCreate, MonthlyDelta: 10, Confidence: pricing.ConfidenceLow},
		"aws_instance.b": {ResourceAddress: "aws_instance.b", Action: iac.ActionCreate, MonthlyDelta: 20, Confidence: pricing.ConfidenceLow},
		"aws_instance.c": {ResourceAddress: "aws_instance.c", Action: iac.ActionCreate, MonthlyDelta: 30, Confidence: pricing.ConfidenceLow},
	}}
	d, _ := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)
	if len(d.TopMovers) != 3 {
		t.Errorf("TopMovers len = %d, want 3 (only 3 changes exist)", len(d.TopMovers))
	}
}

func TestAnalyze_NilPlan(t *testing.T) {
	_, err := analyzeWithEstimator(context.Background(), nilSource{}, nil, "us-east-2", (&fakeEstimator{}).estimate)
	if err == nil || !strings.Contains(err.Error(), "nil plan") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyze_EmptyRegion(t *testing.T) {
	_, err := analyzeWithEstimator(context.Background(), nilSource{}, &iac.Plan{FormatVersion: "1.2"}, "", (&fakeEstimator{}).estimate)
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyze_NilSrc(t *testing.T) {
	_, err := Analyze(context.Background(), nil, &iac.Plan{FormatVersion: "1.2"}, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil src") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyze_EmptyPlan(t *testing.T) {
	d, err := analyzeWithEstimator(context.Background(), nilSource{}, &iac.Plan{FormatVersion: "1.2"}, "us-east-2", (&fakeEstimator{}).estimate)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.Stats.Total != 0 {
		t.Errorf("Stats.Total = %d, want 0", d.Stats.Total)
	}
	if d.Confidence != pricing.ConfidenceHigh {
		t.Errorf("Confidence = %q, want high (no analysis to doubt)", d.Confidence)
	}
	for _, slice := range [][]pricing.ChangeEstimate{
		d.Changes, d.Created, d.Deleted, d.Updated, d.Replaced, d.Skipped, d.TopMovers,
	} {
		if len(slice) != 0 {
			t.Errorf("expected empty slice, got %v", slice)
		}
	}
	if !containsNote(d.Notes, "No priceable resources") {
		t.Errorf("expected 'No priceable resources' note, got: %v", d.Notes)
	}
}

func TestStats_Counts(t *testing.T) {
	p := plan(
		rc("aws_instance.a", "aws_instance", "create"),
		rc("aws_instance.b", "aws_instance", "delete"),
		rc("aws_instance.c", "aws_instance", "update"),
		iac.ResourceChange{
			Address: "aws_instance.d", Mode: "managed", Type: "aws_instance",
			Change: iac.Change{Actions: []string{"delete", "create"}},
		},
		rc("aws_iam_role.r", "aws_iam_role", "create"),
		rc("aws_instance.noop", "aws_instance", "no-op"),
	)
	mkPriced := func(addr string, action iac.Action, delta float64) pricing.ChangeEstimate {
		return pricing.ChangeEstimate{
			ResourceAddress: addr, ResourceType: "aws_instance", Action: action,
			MonthlyDelta: delta, Confidence: pricing.ConfidenceLow,
		}
	}
	fake := &fakeEstimator{results: map[string]pricing.ChangeEstimate{
		"aws_instance.a": mkPriced("aws_instance.a", iac.ActionCreate, 100),
		"aws_instance.b": mkPriced("aws_instance.b", iac.ActionDelete, -50),
		"aws_instance.c": mkPriced("aws_instance.c", iac.ActionUpdate, 25),
		"aws_instance.d": mkPriced("aws_instance.d", iac.ActionReplace, 10),
		"aws_iam_role.r": {
			ResourceAddress: "aws_iam_role.r", ResourceType: "aws_iam_role",
			Action: iac.ActionCreate, Skipped: true,
			SkipReason: "unsupported resource type: aws_iam_role",
			Confidence: pricing.ConfidenceHigh,
		},
		"aws_instance.noop": {
			ResourceAddress: "aws_instance.noop", ResourceType: "aws_instance",
			Action: iac.ActionNoop, Skipped: true,
			SkipReason: "action has no cost impact", Confidence: pricing.ConfidenceHigh,
		},
	}}
	d, _ := analyzeWithEstimator(context.Background(), nilSource{}, p, "us-east-2", fake.estimate)

	want := Stats{Total: 6, Created: 1, Deleted: 1, Updated: 1, Replaced: 1, NoOp: 1, Skipped: 1, Priced: 4}
	if d.Stats != want {
		t.Errorf("Stats = %+v, want %+v", d.Stats, want)
	}
	// Disjoint partition invariant.
	if d.Stats.Priced+d.Stats.NoOp+d.Stats.Skipped != d.Stats.Total {
		t.Errorf("partition broken: priced=%d noop=%d skipped=%d total=%d",
			d.Stats.Priced, d.Stats.NoOp, d.Stats.Skipped, d.Stats.Total)
	}
}

func TestClassifyAction(t *testing.T) {
	cases := map[iac.Action]string{
		iac.ActionCreate:  "create",
		iac.ActionDelete:  "delete",
		iac.ActionUpdate:  "update",
		iac.ActionReplace: "replace",
		iac.ActionNoop:    "",
		iac.ActionRead:    "",
	}
	for a, want := range cases {
		if got := classifyAction(a); got != want {
			t.Errorf("classifyAction(%q) = %q, want %q", a, got, want)
		}
	}
}

func containsNote(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}
