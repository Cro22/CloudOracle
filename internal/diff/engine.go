package diff

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/pricing"
)

// TopMoversCount is the default number of top-by-absolute-delta items
// surfaced in CostDiff.TopMovers. Five fits in a PR comment header
// without crowding it; the renderer in 14.2 may show more on demand.
const TopMoversCount = 5

// Analyze runs pricing.EstimateChange on every resource change in the
// plan and aggregates the results into a CostDiff.
//
// Failures on individual changes are NOT fatal: the orchestrator logs
// them at slog.Warn and the change is added to Skipped with
// SkipReason="estimation failed: <error>". This way callers can always
// render a CostDiff — even one where every resource failed to price —
// rather than refusing to produce a comment because of a single
// transient API hiccup.
//
// Analyze returns a non-nil error only when the input itself is
// malformed: nil plan, nil src, empty region. Multi-region plans are
// out of scope; all changes are priced against `region`.
func Analyze(ctx context.Context, src Source, plan *iac.Plan, region string) (CostDiff, error) {
	if src == nil {
		return CostDiff{}, fmt.Errorf("Analyze: nil src")
	}
	return analyzeWithEstimator(ctx, src, plan, region, defaultEstimator)
}

// estimator is the function shape Analyze uses internally to produce
// per-change estimates. Defaulted to pricing.EstimateChange via
// defaultEstimator; tests inject their own to avoid building a fake
// Source plus fixture JSON for every assertion.
type estimator func(ctx context.Context, src Source, rc iac.ResourceChange, region string) (pricing.ChangeEstimate, error)

// defaultEstimator is the production wiring: a thin pass-through to
// pricing.EstimateChange. Lives as a named function rather than a
// closure so the test seam (analyzeWithEstimator) is easy to read.
func defaultEstimator(ctx context.Context, src Source, rc iac.ResourceChange, region string) (pricing.ChangeEstimate, error) {
	return pricing.EstimateChange(ctx, src, rc, region)
}

// analyzeWithEstimator is the test seam. Public Analyze always supplies
// defaultEstimator; tests pass a fake to avoid simulating Pricing API
// JSON. Mirrors the pattern pricing.newClientWithAPI uses.
func analyzeWithEstimator(ctx context.Context, src Source, plan *iac.Plan, region string, est estimator) (CostDiff, error) {
	if plan == nil {
		return CostDiff{}, fmt.Errorf("Analyze: nil plan")
	}
	if region == "" {
		return CostDiff{}, fmt.Errorf("Analyze: empty region")
	}

	out := CostDiff{
		Currency: "USD",
		Changes:  make([]pricing.ChangeEstimate, 0, len(plan.ResourceChanges)),
	}

	for _, rc := range plan.ResourceChanges {
		ce, err := est(ctx, src, rc, region)
		if err != nil {
			slog.Warn("diff: estimation failed; recording as skipped",
				"address", rc.Address,
				"type", rc.Type,
				"error", err,
			)
			ce = pricing.ChangeEstimate{
				ResourceAddress: rc.Address,
				ResourceType:    rc.Type,
				Action:          rc.Action(),
				Currency:        "USD",
				Confidence:      pricing.ConfidenceHigh,
				Skipped:         true,
				SkipReason:      "estimation failed: " + err.Error(),
			}
		}
		out.Changes = append(out.Changes, ce)
	}

	// Sort by absolute MonthlyDelta descending. Skipped items have
	// delta=0 and naturally trail the priced ones; ties keep input order
	// (sort.SliceStable would matter only if we cared about secondary
	// ordering, which we don't here).
	sort.SliceStable(out.Changes, func(i, j int) bool {
		return math.Abs(out.Changes[i].MonthlyDelta) > math.Abs(out.Changes[j].MonthlyDelta)
	})

	out.Confidence = pricing.ConfidenceHigh
	for i := range out.Changes {
		ce := &out.Changes[i]
		out.TotalMonthlyDelta += ce.MonthlyDelta
		categorize(ce, &out)
		if !ce.Skipped {
			out.Confidence = weakestConfidence(out.Confidence, ce.Confidence)
		}
	}

	out.TopMovers = topMovers(out.Changes, TopMoversCount)
	out.Stats = computeStats(&out)
	out.Notes = buildNotes(&out)

	return out, nil
}

// categorize files a change into the appropriate action-keyed slice on
// out, falling back to Skipped when ce.Skipped is set regardless of
// action. The action-keyed slices intentionally exclude skipped items
// so a renderer iterating "Created" sees only successfully-priced
// creates.
func categorize(ce *pricing.ChangeEstimate, out *CostDiff) {
	if ce.Skipped {
		out.Skipped = append(out.Skipped, *ce)
		return
	}
	switch classifyAction(ce.Action) {
	case "create":
		out.Created = append(out.Created, *ce)
	case "delete":
		out.Deleted = append(out.Deleted, *ce)
	case "update":
		out.Updated = append(out.Updated, *ce)
	case "replace":
		out.Replaced = append(out.Replaced, *ce)
	}
}

// classifyAction returns the canonical category string for an action,
// or "" for actions that don't get their own slice (no-op, read,
// unknown). The empty string is the cue to NOT add the change to any
// action-specific slice — those changes are still represented in
// Changes, and if Skipped=true they show up in Skipped.
func classifyAction(a iac.Action) string {
	switch a {
	case iac.ActionCreate:
		return "create"
	case iac.ActionDelete:
		return "delete"
	case iac.ActionUpdate:
		return "update"
	case iac.ActionReplace:
		return "replace"
	}
	return ""
}

// topMovers returns the first n non-skipped entries from changes.
// Skipped items have delta=0 and would dilute the "biggest changes"
// summary, so they are filtered out. n is clamped against the
// available non-skipped count.
func topMovers(changes []pricing.ChangeEstimate, n int) []pricing.ChangeEstimate {
	if n <= 0 || len(changes) == 0 {
		return nil
	}
	out := make([]pricing.ChangeEstimate, 0, n)
	for _, c := range changes {
		if c.Skipped {
			continue
		}
		out = append(out, c)
		if len(out) == n {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// computeStats counts each disjoint category. The partition rule is in
// the Stats godoc: NoOp captures no-op/read actions; Skipped captures
// items with Skipped=true that AREN'T no-op/read; Priced is the four
// action slices summed.
func computeStats(out *CostDiff) Stats {
	s := Stats{
		Total:    len(out.Changes),
		Created:  len(out.Created),
		Deleted:  len(out.Deleted),
		Updated:  len(out.Updated),
		Replaced: len(out.Replaced),
	}
	s.Priced = s.Created + s.Deleted + s.Updated + s.Replaced
	for _, c := range out.Changes {
		switch c.Action {
		case iac.ActionNoop, iac.ActionRead:
			s.NoOp++
		default:
			if c.Skipped {
				s.Skipped++
			}
		}
	}
	return s
}

// buildNotes produces the plan-wide notes appended to CostDiff.Notes.
// The order is fixed: skip-count breakdown first (if any skips), then
// either the "no priceable" diagnostic or the net-direction note.
// Calling code can rely on this order when rendering.
func buildNotes(out *CostDiff) []string {
	var notes []string

	if len(out.Skipped) > 0 {
		var unsupported, estFail int
		for _, c := range out.Skipped {
			switch {
			case strings.Contains(c.SkipReason, "unsupported"):
				unsupported++
			case strings.Contains(c.SkipReason, "estimation failed"):
				estFail++
			}
		}
		notes = append(notes,
			fmt.Sprintf("%d resources skipped (%d unsupported types, %d estimation failures)",
				len(out.Skipped), unsupported, estFail))
	}

	allSkipped := len(out.Changes) == 0 || len(out.Skipped) == len(out.Changes)
	switch {
	case allSkipped:
		notes = append(notes, "No priceable resources in plan")
	case out.TotalMonthlyDelta > 0:
		notes = append(notes, "Net cost increase this plan")
	case out.TotalMonthlyDelta < 0:
		notes = append(notes, "Net cost reduction this plan")
	default:
		notes = append(notes, "Net zero cost change")
	}

	return notes
}

// weakestConfidence returns whichever of a/b is "weaker": low dominates
// medium dominates high. Duplicated from pricing/change.go intentionally
// — the function is five lines and importing it would couple the diff
// package to pricing's confidenceRank, which is unexported and could
// change shape independently.
func weakestConfidence(a, b pricing.Confidence) pricing.Confidence {
	rank := func(c pricing.Confidence) int {
		switch c {
		case pricing.ConfidenceHigh:
			return 0
		case pricing.ConfidenceMedium:
			return 1
		case pricing.ConfidenceLow:
			return 2
		}
		return 3
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}
