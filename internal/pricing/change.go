package pricing

import (
	"context"
	"fmt"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/iac/aws"
)

// EstimateChange returns the cost impact of a single resource change in
// a Terraform plan. It dispatches by rc.Type to the right Estimate*
// function, computes a before/after delta based on rc.Action(), and
// returns a uniform ChangeEstimate that downstream diff/comment code
// can render without caring about resource type.
//
// Action handling (see ChangeEstimate godoc for the sign convention):
//
//   - create:           BeforeMonthly = 0, AfterMonthly  = price(After),  delta = AfterMonthly
//   - delete:           BeforeMonthly = price(Before), AfterMonthly = 0,  delta = -BeforeMonthly
//   - update / replace: both states priced;            delta = After - Before
//   - no-op / read:     Skipped, all costs 0
//
// Behaviour for resource types we cannot price (aws_iam_role, S3,
// EKS, ...): EstimateChange returns Skipped=true with a SkipReason
// rather than an error. Callers iterate over the whole plan and want a
// per-resource result for every change, including the unpriced ones —
// silently dropping them would push "why didn't this show up?" lookups
// onto callers. No API call is made for unsupported types.
//
// Data sources (rc.IsManaged()==false) are also Skipped — they have no
// cost impact by definition.
//
// region is the AWS region for pricing queries (e.g. "us-east-2"); all
// resources in a plan should share a region. Multi-region plans need
// separate calls per region.
//
// Errors are returned only for genuine failures: API errors, malformed
// attributes that fail the per-type extractors, parser unit mismatches.
// "Unsupported type" and "no-op action" are NOT errors — they are
// Skipped results.
func EstimateChange(ctx context.Context, src productGetter, rc iac.ResourceChange, region string) (ChangeEstimate, error) {
	if region == "" {
		return ChangeEstimate{}, fmt.Errorf("EstimateChange: empty region")
	}

	action := rc.Action()
	out := ChangeEstimate{
		ResourceAddress: rc.Address,
		ResourceType:    rc.Type,
		Action:          action,
		Currency:        "USD",
		Confidence:      ConfidenceHigh,
	}

	if action == iac.ActionNoop || action == iac.ActionRead {
		out.Skipped = true
		out.SkipReason = "action has no cost impact"
		return out, nil
	}
	if !rc.IsManaged() {
		out.Skipped = true
		out.SkipReason = "data sources have no cost"
		return out, nil
	}

	switch action {
	case iac.ActionCreate:
		afterEst, afterSkip, err := estimateState(ctx, src, rc.Type, rc.Change.After, region)
		if err != nil {
			return ChangeEstimate{}, fmt.Errorf("EstimateChange: %s after: %w", rc.Address, err)
		}
		if afterSkip != "" {
			out.Skipped = true
			out.SkipReason = afterSkip
			return out, nil
		}
		out.AfterMonthly = afterEst.MonthlyUSD
		out.MonthlyDelta = afterEst.MonthlyUSD
		out.Confidence = afterEst.Confidence
		out.Notes = afterEst.Notes
		out.Breakdown = cloneBreakdown(afterEst.Breakdown)

	case iac.ActionDelete:
		beforeEst, beforeSkip, err := estimateState(ctx, src, rc.Type, rc.Change.Before, region)
		if err != nil {
			return ChangeEstimate{}, fmt.Errorf("EstimateChange: %s before: %w", rc.Address, err)
		}
		if beforeSkip != "" {
			out.Skipped = true
			out.SkipReason = beforeSkip
			return out, nil
		}
		out.BeforeMonthly = beforeEst.MonthlyUSD
		out.MonthlyDelta = -beforeEst.MonthlyUSD
		out.Confidence = beforeEst.Confidence
		out.Notes = beforeEst.Notes
		out.Breakdown = negateBreakdown(beforeEst.Breakdown)

	case iac.ActionUpdate, iac.ActionReplace:
		beforeEst, beforeSkip, err := estimateState(ctx, src, rc.Type, rc.Change.Before, region)
		if err != nil {
			return ChangeEstimate{}, fmt.Errorf("EstimateChange: %s before: %w", rc.Address, err)
		}
		afterEst, afterSkip, err := estimateState(ctx, src, rc.Type, rc.Change.After, region)
		if err != nil {
			return ChangeEstimate{}, fmt.Errorf("EstimateChange: %s after: %w", rc.Address, err)
		}
		// If the type is unsupported on either side (it'll be the same
		// type on both sides — Terraform doesn't change resource type
		// across a single change), skip the whole change.
		if beforeSkip != "" || afterSkip != "" {
			out.Skipped = true
			if beforeSkip != "" {
				out.SkipReason = beforeSkip
			} else {
				out.SkipReason = afterSkip
			}
			return out, nil
		}
		out.BeforeMonthly = beforeEst.MonthlyUSD
		out.AfterMonthly = afterEst.MonthlyUSD
		out.MonthlyDelta = afterEst.MonthlyUSD - beforeEst.MonthlyUSD
		out.Confidence = weakestConfidence(beforeEst.Confidence, afterEst.Confidence)
		out.Notes = mergeNotes(beforeEst.Notes, afterEst.Notes)
		out.Breakdown = mergeDeltaBreakdown(beforeEst.Breakdown, afterEst.Breakdown)

	default:
		// Unknown action (a hypothetical future value). Treat as Skipped
		// rather than crash — fail safely on unrecognised inputs.
		out.Skipped = true
		out.SkipReason = fmt.Sprintf("unsupported action %q", string(action))
		return out, nil
	}

	return out, nil
}

// estimateState extracts the typed attributes for resourceType and runs
// the appropriate per-resource estimator. The skipReason return is
// non-empty when the type is unsupported (Extract returned (nil, nil))
// or when the attribute map was empty — both produce a Skipped change
// rather than an error. Real errors propagate to the caller.
func estimateState(ctx context.Context, src productGetter, resourceType string, attrs map[string]interface{}, region string) (Estimate, string, error) {
	if len(attrs) == 0 {
		return Estimate{}, "no attributes for state", nil
	}
	ra, err := aws.Extract(resourceType, attrs)
	if err != nil {
		return Estimate{}, "", fmt.Errorf("extracting %s: %w", resourceType, err)
	}
	if ra == nil {
		return Estimate{}, "unsupported resource type: " + resourceType, nil
	}
	switch {
	case ra.EC2 != nil:
		est, err := EstimateEC2(ctx, src, ra.EC2, region)
		return est, "", err
	case ra.RDS != nil:
		est, err := EstimateRDS(ctx, src, ra.RDS, region)
		return est, "", err
	case ra.EBS != nil:
		est, err := EstimateEBS(ctx, src, ra.EBS, region)
		return est, "", err
	case ra.Lambda != nil:
		est, err := EstimateLambda(ctx, src, ra.Lambda, region)
		return est, "", err
	case ra.NATGateway != nil:
		est, err := EstimateNATGateway(ctx, src, ra.NATGateway, region)
		return est, "", err
	case ra.RDSClusterInstance != nil:
		est, err := EstimateRDSClusterInstance(ctx, src, ra.RDSClusterInstance, region)
		return est, "", err
	}
	// aws.Extract returned a non-nil ResourceAttributes with no inner
	// pointer set. Defensively treat as unsupported rather than panic —
	// this would only happen if SupportedTypes() and Extract drift apart.
	return Estimate{}, "unsupported resource type: " + resourceType, nil
}

// weakestConfidence returns whichever confidence is "weaker" — high is
// the strongest, low is the weakest. Used by EstimateChange to merge
// the before/after confidences of an update or replace into a single
// value: a high-confidence before paired with a low-confidence after
// produces a low-confidence change.
func weakestConfidence(a, b Confidence) Confidence {
	if confidenceRank(a) >= confidenceRank(b) {
		return a
	}
	return b
}

func confidenceRank(c Confidence) int {
	switch c {
	case ConfidenceHigh:
		return 0
	case ConfidenceMedium:
		return 1
	case ConfidenceLow:
		return 2
	}
	// Unknown values rank as the weakest so they propagate correctly
	// through merging instead of being silently dropped.
	return 3
}

// mergeNotes concatenates two note slices in (before, after) order,
// dropping duplicates so identical caveats from both states don't
// double up in the rendered output.
func mergeNotes(before, after []string) []string {
	seen := make(map[string]struct{}, len(before)+len(after))
	out := make([]string, 0, len(before)+len(after))
	for _, n := range before {
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	for _, n := range after {
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
}

// cloneBreakdown returns a fresh copy of the breakdown so the caller
// can't accidentally mutate the inner Estimate's slice through the
// returned ChangeEstimate.
func cloneBreakdown(in []LineItem) []LineItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]LineItem, len(in))
	copy(out, in)
	return out
}

// negateBreakdown returns a copy of the breakdown with every component's
// MonthlyUSD sign flipped, used for delete actions where the breakdown
// represents a removed cost.
func negateBreakdown(in []LineItem) []LineItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]LineItem, len(in))
	for i, li := range in {
		out[i] = LineItem{Component: li.Component, MonthlyUSD: -li.MonthlyUSD}
	}
	return out
}

// mergeDeltaBreakdown produces a per-component delta from a before and
// after breakdown for update/replace actions. Components in only the
// after appear with their full positive value; components in only the
// before appear with their negated value; components in both are
// after - before.
//
// The output preserves the order of components in `after`, then appends
// components seen only in `before`. Stable order matters because
// downstream comment renderers iterate the breakdown directly.
func mergeDeltaBreakdown(before, after []LineItem) []LineItem {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	idx := make(map[string]int, len(after))
	out := make([]LineItem, 0, len(after)+len(before))
	for _, li := range after {
		idx[li.Component] = len(out)
		out = append(out, LineItem{Component: li.Component, MonthlyUSD: li.MonthlyUSD})
	}
	for _, li := range before {
		if i, ok := idx[li.Component]; ok {
			out[i].MonthlyUSD -= li.MonthlyUSD
		} else {
			idx[li.Component] = len(out)
			out = append(out, LineItem{Component: li.Component, MonthlyUSD: -li.MonthlyUSD})
		}
	}
	return out
}
