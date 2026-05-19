package api

import (
	"CloudOracle/internal/db"
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// dataSourceLabel and dataSourceNote document the contract of the v1 cost
// endpoints. CloudOracle's `cost_snapshots` table records each provider's
// *projected monthly cost rate* at snapshot time — not the historical spend
// that a real Billing / Cost Explorer integration would surface. Until that
// integration lands (milestone 8.2+), the v1 endpoints expose this
// approximation explicitly in every response so downstream agents and
// dashboards can present the right disclaimer to the user.
const (
	dataSourceLabel = "snapshots_approximation"
	dataSourceNote  = "Costs are approximated from CloudOracle cost snapshots, " +
		"not from a billing / cost-explorer integration. Values reflect the " +
		"average projected monthly cost rate observed in the period, scaled " +
		"to the period length."
)

// apiData is the single data dependency the handlers reach through.
// Defining it here (rather than scattering db.* calls across handlers)
// keeps the test path simple — a fake apiData avoids spinning up Postgres
// just to verify request parsing, response shaping, and aggregation.
//
// The interface intentionally mirrors the methods the handlers actually
// need; widening it later is cheap because there is exactly one production
// implementation (pgxAdapter) and one test fake.
type apiData interface {
	ListResources(ctx context.Context) ([]shared.Resource, error)
	ListTrends(ctx context.Context, days int) ([]db.Trend, error)
	ListSnapshotsInRange(ctx context.Context, start, end time.Time) ([]db.Snapshot, error)
}

// pgxAdapter is the production implementation of apiData. It is a thin
// shim — every method delegates to its db.* counterpart. Tests use a
// fake instead, with the pool stayed nil; the production path is the only
// one that ever calls these methods.
type pgxAdapter struct{ pool *db.Pool }

func (p *pgxAdapter) ListResources(ctx context.Context) ([]shared.Resource, error) {
	return db.ListResources(ctx, p.pool)
}

func (p *pgxAdapter) ListTrends(ctx context.Context, days int) ([]db.Trend, error) {
	return db.ListTrends(ctx, p.pool, days)
}

func (p *pgxAdapter) ListSnapshotsInRange(ctx context.Context, start, end time.Time) ([]db.Snapshot, error) {
	return db.ListSnapshotsInRange(ctx, p.pool, start, end)
}

type periodDTO struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type providerSummaryDTO struct {
	TotalUSD float64 `json:"total_usd"`
	Currency string  `json:"currency"`
}

type costSummaryResponse struct {
	Period        periodDTO                     `json:"period"`
	Providers     map[string]providerSummaryDTO `json:"providers"`
	GrandTotalUSD float64                       `json:"grand_total_usd"`
	GeneratedAt   time.Time                     `json:"generated_at"`
	DataSource    string                        `json:"data_source"`
	Note          string                        `json:"note"`
}

func (s *Server) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, end, err := parseDateRange(q.Get("start"), q.Get("end"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error(), "invalid_date_range")
		return
	}

	filter, err := parseProvidersFilter(q.Get("providers"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error(), "invalid_provider")
		return
	}

	snapshots, err := s.data.ListSnapshotsInRange(r.Context(), start, end)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"failed to load snapshots: "+err.Error(), "snapshot_query_failed")
		return
	}

	days := periodDays(start, end)
	perProvider := aggregateByProvider(snapshots, days, filter)

	resp := costSummaryResponse{
		Period:      periodDTO{Start: start.Format(time.DateOnly), End: end.Format(time.DateOnly)},
		Providers:   make(map[string]providerSummaryDTO, len(perProvider)),
		GeneratedAt: time.Now().UTC(),
		DataSource:  dataSourceLabel,
		Note:        dataSourceNote,
	}

	var total float64
	for p, v := range perProvider {
		resp.Providers[p] = providerSummaryDTO{TotalUSD: roundCents(v), Currency: "USD"}
		total += v
	}
	resp.GrandTotalUSD = roundCents(total)

	writeJSON(w, http.StatusOK, resp)
}

type serviceCostDTO struct {
	Name       string  `json:"name"`
	TotalUSD   float64 `json:"total_usd"`
	Percentage float64 `json:"percentage"`
}

type costByServiceResponse struct {
	Period      periodDTO        `json:"period"`
	Provider    string           `json:"provider"`
	Services    []serviceCostDTO `json:"services"`
	TotalUSD    float64          `json:"total_usd"`
	GeneratedAt time.Time        `json:"generated_at"`
	DataSource  string           `json:"data_source"`
	Note        string           `json:"note"`
}

func (s *Server) handleCostByService(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, end, err := parseDateRange(q.Get("start"), q.Get("end"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error(), "invalid_date_range")
		return
	}

	provider := strings.ToLower(strings.TrimSpace(q.Get("provider")))
	if !validProvider(provider) {
		writeAPIError(w, http.StatusBadRequest,
			"provider query param is required and must be one of aws, gcp, azure",
			"invalid_provider")
		return
	}

	top := parseIntOr(q.Get("top"), 10)
	// Treat any non-positive or absurdly large top as the default. The cap
	// of 1000 is far above the number of services any provider has, but
	// bounds the response shape so a curious caller can't ask for 1B items.
	if top <= 0 || top > 1000 {
		top = 10
	}

	snapshots, err := s.data.ListSnapshotsInRange(r.Context(), start, end)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"failed to load snapshots: "+err.Error(), "snapshot_query_failed")
		return
	}

	days := periodDays(start, end)
	perService := aggregateByService(snapshots, days, provider)

	var total float64
	for _, v := range perService {
		total += v
	}

	services := make([]serviceCostDTO, 0, len(perService))
	for name, cost := range perService {
		pct := 0.0
		if total > 0 {
			pct = roundCents(cost / total * 100)
		}
		services = append(services, serviceCostDTO{
			Name:       name,
			TotalUSD:   roundCents(cost),
			Percentage: pct,
		})
	}
	// Sort by cost desc, tiebreak by name asc — deterministic order so
	// callers (and tests) don't have to deal with map iteration randomness.
	sort.Slice(services, func(i, j int) bool {
		if services[i].TotalUSD != services[j].TotalUSD {
			return services[i].TotalUSD > services[j].TotalUSD
		}
		return services[i].Name < services[j].Name
	})
	if len(services) > top {
		services = services[:top]
	}

	resp := costByServiceResponse{
		Period:      periodDTO{Start: start.Format(time.DateOnly), End: end.Format(time.DateOnly)},
		Provider:    provider,
		Services:    services,
		TotalUSD:    roundCents(total),
		GeneratedAt: time.Now().UTC(),
		DataSource:  dataSourceLabel,
		Note:        dataSourceNote,
	}
	writeJSON(w, http.StatusOK, resp)
}

// aggregateByProvider implements the snapshots approximation:
//
//  1. Group snapshots by (account, service).
//  2. For each group, compute the average total_monthly_cost across the
//     snapshots that fell in the period.
//  3. Map each (account, service) to a provider via providerForServiceAccount
//     (the same mapping the dashboard summary uses).
//  4. Scale the per-group monthly rate to the period length: avg × days / 30.
//
// The optional filter is treated as a whitelist when non-nil; an empty map
// is also "no filter" — see parseProvidersFilter.
func aggregateByProvider(snapshots []db.Snapshot, days int, filter map[string]bool) map[string]float64 {
	perAS := aggregateMonthlyByAccountService(snapshots)
	scale := float64(days) / 30.0
	result := make(map[string]float64)
	for k, avgMonthly := range perAS {
		provider := providerForServiceAccount(k.service, k.account)
		if len(filter) > 0 && !filter[provider] {
			continue
		}
		result[provider] += avgMonthly * scale
	}
	return result
}

// aggregateByService is the service-level counterpart: it returns per-service
// period totals for the requested provider. Unlike aggregateByProvider it
// hard-filters on the provider (the v1 endpoint requires `provider` to be
// set to a specific value), so no whitelist map is needed.
func aggregateByService(snapshots []db.Snapshot, days int, provider string) map[string]float64 {
	perAS := aggregateMonthlyByAccountService(snapshots)
	scale := float64(days) / 30.0
	result := make(map[string]float64)
	for k, avgMonthly := range perAS {
		if providerForServiceAccount(k.service, k.account) != provider {
			continue
		}
		result[k.service] += avgMonthly * scale
	}
	return result
}

type accountServiceKey struct {
	account string
	service string
}

// aggregateMonthlyByAccountService averages total_monthly_cost across every
// snapshot for each (account, service) tuple. Snapshots taken close together
// in time will have similar values, so the average is a reasonable point
// estimate of the period's monthly cost rate.
func aggregateMonthlyByAccountService(snapshots []db.Snapshot) map[accountServiceKey]float64 {
	type acc struct {
		sum   float64
		count int
	}
	totals := make(map[accountServiceKey]*acc)
	for _, s := range snapshots {
		k := accountServiceKey{s.AccountID, s.Service}
		if totals[k] == nil {
			totals[k] = &acc{}
		}
		totals[k].sum += s.TotalMonthlyCost
		totals[k].count++
	}
	out := make(map[accountServiceKey]float64, len(totals))
	for k, a := range totals {
		if a.count == 0 {
			continue
		}
		out[k] = a.sum / float64(a.count)
	}
	return out
}

// parseDateRange validates start/end query params. Both are required and
// must be ISO YYYY-MM-DD; end must not be before start. Returns the parsed
// times with `end` advanced to 23:59:59.999999999 of that day so the SQL
// BETWEEN includes the closing day in full — the v1 contract is inclusive.
func parseDateRange(startRaw, endRaw string) (time.Time, time.Time, error) {
	if startRaw == "" || endRaw == "" {
		return time.Time{}, time.Time{}, errors.New("start and end query params are required (YYYY-MM-DD)")
	}
	start, err := time.Parse(time.DateOnly, startRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start=%q is not a valid date (expected YYYY-MM-DD)", startRaw)
	}
	end, err := time.Parse(time.DateOnly, endRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end=%q is not a valid date (expected YYYY-MM-DD)", endRaw)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end=%s is before start=%s", endRaw, startRaw)
	}
	end = end.Add(24*time.Hour - time.Nanosecond)
	return start, end, nil
}

func parseProvidersFilter(raw string) (map[string]bool, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !validProvider(p) {
			return nil, fmt.Errorf("providers=%q contains invalid provider %q (must be aws, gcp, or azure)", raw, p)
		}
		out[p] = true
	}
	return out, nil
}

func validProvider(p string) bool {
	switch p {
	case "aws", "gcp", "azure":
		return true
	}
	return false
}

// periodDays returns the inclusive day span of the period. We use it to
// scale the monthly rate to the period length. A start/end on the same
// calendar day yields 1; the function never returns < 1, so callers can
// divide safely.
func periodDays(start, end time.Time) int {
	days := int(end.Sub(start).Hours()/24) + 1
	if days < 1 {
		return 1
	}
	return days
}

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}
