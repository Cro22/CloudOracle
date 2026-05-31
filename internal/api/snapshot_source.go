package api

import (
	"CloudOracle/internal/billing"
	"context"
	"fmt"
	"time"
)

// snapshotSource is the default billing.Source: it derives cost from the
// aggregated cost_snapshots, reproducing the original v1 behavior exactly
// (data_source "snapshots_approximation"). It carries the same monthly-rate
// averaging and days/30 scaling the cost handlers used before milestone 8.7
// moved this logic behind the billing.Source abstraction.
type snapshotSource struct {
	data apiData
}

func newSnapshotSource(data apiData) snapshotSource {
	return snapshotSource{data: data}
}

func (s snapshotSource) Costs(
	ctx context.Context, start, end time.Time,
) (billing.Report, error) {
	snapshots, err := s.data.ListSnapshotsInRange(ctx, start, end)
	if err != nil {
		return billing.Report{}, &billing.SourceError{
			Code: "snapshot_query_failed",
			Err:  fmt.Errorf("failed to load snapshots: %w", err),
		}
	}

	days := periodDays(start, end)
	scale := float64(days) / 30.0
	perAS := aggregateMonthlyByAccountService(snapshots)

	type key struct{ provider, service string }
	agg := make(map[key]float64)
	for k, avgMonthly := range perAS {
		provider := providerForServiceAccount(k.service, k.account)
		agg[key{provider, k.service}] += avgMonthly * scale
	}

	// Amounts stay unrounded; the handlers round the final aggregates once,
	// so summing records per provider matches the pre-8.7 numbers to the cent.
	records := make([]billing.CostRecord, 0, len(agg))
	for k, amount := range agg {
		records = append(records, billing.CostRecord{
			Provider:  k.provider,
			Service:   k.service,
			AmountUSD: amount,
		})
	}
	return billing.Report{
		Records:    records,
		DataSource: dataSourceLabel,
		Note:       dataSourceNote,
	}, nil
}
