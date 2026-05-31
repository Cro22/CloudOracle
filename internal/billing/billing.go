// Package billing abstracts where the v1 cost endpoints get their numbers.
//
// CloudOracle started with a single source: aggregated cost_snapshots, exposed
// as data_source "snapshots_approximation". Milestone 8.7 introduces this
// Source interface so a real billing integration (AWS Cost Explorer) can be
// swapped in by configuration without touching the HTTP handlers, which now
// consume a normalized []CostRecord regardless of where it came from.
package billing

import (
	"context"
	"time"
)

// CostRecord is one provider/service cost line for the requested period,
// already resolved to USD. The HTTP handlers group these by provider (for the
// summary) or by service within a provider (for the per-service breakdown).
type CostRecord struct {
	Provider  string
	Service   string
	AmountUSD float64
}

// Report is the result of a cost query: the records plus the data_source label
// and human note the API echoes so callers know how the numbers were produced.
type Report struct {
	Records    []CostRecord
	DataSource string
	Note       string
}

// Source produces a cost Report for an inclusive [start, end] period.
type Source interface {
	Costs(ctx context.Context, start, end time.Time) (Report, error)
}

// SourceError carries a machine-readable code so the HTTP layer can map a
// data-source failure to a stable error code (e.g. "snapshot_query_failed",
// "billing_query_failed") without knowing the concrete source type.
type SourceError struct {
	Code string
	Err  error
}

func (e *SourceError) Error() string { return e.Err.Error() }

func (e *SourceError) Unwrap() error { return e.Err }
