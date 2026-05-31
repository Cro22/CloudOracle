package api

import (
	"net/http"
	"strings"
	"time"
)

// Cost trends are derived from the same cost_snapshots the cost-summary
// endpoint uses, so they share the snapshots_approximation contract: the
// per-day totals reflect the latest snapshot's projected monthly rate on
// each day, not historical billed spend.

const (
	defaultTrendDays = 90
	maxTrendDays     = 365
)

type trendPointDTO struct {
	Date         string  `json:"date"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

type trendChangeDTO struct {
	// AbsoluteUSD is latest - first. PercentFromFirst is that delta over the
	// first point's total, or null when the first point is zero (growth from
	// nothing has no meaningful percentage). Direction collapses the change
	// into up/down/flat so the agent can phrase it without re-deriving sign.
	AbsoluteUSD      float64  `json:"absolute_usd"`
	PercentFromFirst *float64 `json:"percent_from_first"`
	Direction        string   `json:"direction"`
}

type costTrendsResponse struct {
	Days        int             `json:"days"`
	Provider    string          `json:"provider,omitempty"`
	Points      []trendPointDTO `json:"points"`
	First       *trendPointDTO  `json:"first"`
	Latest      *trendPointDTO  `json:"latest"`
	Change      *trendChangeDTO `json:"change"`
	GeneratedAt time.Time       `json:"generated_at"`
	DataSource  string          `json:"data_source"`
	Note        string          `json:"note"`
}

// handleCostTrends returns a per-day cost time series for the trailing
// `days` window, plus a precomputed first/latest/change summary so the agent
// can answer "is my spend growing?" without crunching the array itself.
//
//	days=N                   trailing window, default 90, clamped to 1..365
//	provider=aws|gcp|azure   restrict the per-day total to one cloud
//
// When provider is set, each day's total is recomputed from that day's
// per-service breakdown (only services mapping to the provider). Resource
// counts aren't exposed here because the underlying trend aggregates them per
// service without a per-provider split — cost is the signal that matters for
// a trend question.
func (s *Server) handleCostTrends(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	days := parseIntOr(q.Get("days"), defaultTrendDays)
	days = clampInt(days, 1, maxTrendDays)

	providerFilter, ok := parseOptionalProvider(q.Get("provider"))
	if !ok {
		writeAPIError(w, http.StatusBadRequest,
			"provider must be one of aws, gcp, azure", "invalid_provider")
		return
	}

	trends, err := s.data.ListTrends(r.Context(), days)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"failed to load trends: "+err.Error(), "trend_query_failed")
		return
	}

	points := make([]trendPointDTO, 0, len(trends))
	for _, t := range trends {
		total := t.TotalCost
		if providerFilter != "" {
			total = 0
			for service, cost := range t.BreakdownByService {
				if providerForServiceAccount(service, "") == providerFilter {
					total += cost
				}
			}
		}
		points = append(points, trendPointDTO{
			Date:         t.Date,
			TotalCostUSD: roundCents(total),
		})
	}

	resp := costTrendsResponse{
		Days:        days,
		Provider:    providerFilter,
		Points:      points,
		GeneratedAt: time.Now().UTC(),
		DataSource:  dataSourceLabel,
		Note:        dataSourceNote,
	}

	if len(points) > 0 {
		first := points[0]
		latest := points[len(points)-1]
		resp.First = &first
		resp.Latest = &latest
		resp.Change = buildTrendChange(first.TotalCostUSD, latest.TotalCostUSD)
	}

	writeJSON(w, http.StatusOK, resp)
}

// buildTrendChange computes the first→latest delta. percent_from_first is nil
// when first is zero so callers don't divide by zero or report an infinite
// percentage. The flat band (|delta| < 1 cent) absorbs floating-point noise.
func buildTrendChange(first, latest float64) *trendChangeDTO {
	delta := roundCents(latest - first)
	change := &trendChangeDTO{AbsoluteUSD: delta}

	switch {
	case delta > 0:
		change.Direction = "up"
	case delta < 0:
		change.Direction = "down"
	default:
		change.Direction = "flat"
	}

	if first != 0 {
		pct := roundCents(delta / first * 100)
		change.PercentFromFirst = &pct
	}
	return change
}

// parseOptionalProvider parses an optional provider query param. Empty means
// "no filter" (ok=true, empty string). An unrecognized value is rejected.
func parseOptionalProvider(raw string) (string, bool) {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return "", true
	}
	if !validProvider(p) {
		return "", false
	}
	return p, true
}
