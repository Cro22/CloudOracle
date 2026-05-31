package api

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/shared"
	"net/http"
	"sort"
	"strings"
	"time"
)

// recommendationsDataSource and recommendationsNote document the contract of
// the /api/v1/recommendations endpoint. Unlike the cost endpoints (which
// approximate spend from cost_snapshots), recommendations come from the
// rule-based analyzer run over the *current* resource inventory — so they
// carry a distinct data_source so the agent doesn't conflate the two and
// surfaces the right caveat: these are heuristic estimates, not guaranteed
// savings.
const (
	recommendationsDataSource = "heuristic_rules"
	recommendationsNote       = "Recommendations come from CloudOracle's rule-based analyzer " +
		"applied to the current resource inventory (not historical billing). " +
		"Estimated savings are heuristic upper bounds — validate against real " +
		"usage before acting."
)

// defaultRecommendationsTop bounds how many recommendations the endpoint
// returns by default. maxPageSize (200, shared with the findings handler)
// is the hard ceiling so a caller can't ask for an unbounded list.
const defaultRecommendationsTop = 20

type recommendationDTO struct {
	ResourceID        string  `json:"resource_id"`
	Provider          string  `json:"provider"`
	Service           string  `json:"service"`
	ResourceType      string  `json:"resource_type"`
	Region            string  `json:"region"`
	Rule              string  `json:"rule"`
	Severity          string  `json:"severity"`
	MonthlyCostUSD    float64 `json:"monthly_cost_usd"`
	MonthlySavingsUSD float64 `json:"monthly_savings_usd"`
	Description       string  `json:"description"`
	Recommendation    string  `json:"recommendation"`
}

type recommendationsFiltersDTO struct {
	Provider string `json:"provider,omitempty"`
	Severity string `json:"severity,omitempty"`
	Top      int    `json:"top"`
}

type recommendationsResponse struct {
	Recommendations        []recommendationDTO       `json:"recommendations"`
	TotalCount             int                       `json:"total_count"`
	ReturnedCount          int                       `json:"returned_count"`
	TotalMonthlySavingsUSD float64                   `json:"total_monthly_savings_usd"`
	BySeverity             map[string]int            `json:"by_severity"`
	Filters                recommendationsFiltersDTO `json:"filters"`
	GeneratedAt            time.Time                 `json:"generated_at"`
	DataSource             string                    `json:"data_source"`
	Note                   string                    `json:"note"`
}

// handleRecommendations exposes the analyzer findings as agent-friendly
// savings recommendations. It answers questions like "where can I save
// money?" or "what are my top AWS optimizations?". Optional filters:
//
//	provider=aws|gcp|azure   restrict to one cloud
//	severity=high|medium|low restrict to one severity band
//	top=N                    cap the list (default 20, max 200)
//
// total_count / total_monthly_savings_usd / by_severity describe the full
// filtered set *before* the top cap, so a truncated list still reports the
// real opportunity size.
func (s *Server) handleRecommendations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var providerFilter string
	if raw := strings.ToLower(strings.TrimSpace(q.Get("provider"))); raw != "" {
		if !validProvider(raw) {
			writeAPIError(w, http.StatusBadRequest,
				"provider must be one of aws, gcp, azure", "invalid_provider")
			return
		}
		providerFilter = raw
	}

	severityFilter, ok := parseSeverityFilter(q.Get("severity"))
	if !ok {
		writeAPIError(w, http.StatusBadRequest,
			"severity must be one of high, medium, low", "invalid_severity")
		return
	}

	top := parseIntOr(q.Get("top"), defaultRecommendationsTop)
	top = clampInt(top, 1, maxPageSize)

	resources, err := s.data.ListResources(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"failed to list resources: "+err.Error(), "resource_query_failed")
		return
	}

	findings := analyzer.Analyze(resources)

	items := make([]recommendationDTO, 0, len(findings))
	bySeverity := make(map[string]int)
	var totalSavings float64
	for _, f := range findings {
		provider := providerForServiceAccount(f.Service, "")
		if providerFilter != "" && provider != providerFilter {
			continue
		}
		if severityFilter != "" && f.Severity != severityFilter {
			continue
		}
		bySeverity[string(f.Severity)]++
		totalSavings += f.MonthlySavings
		items = append(items, recommendationDTO{
			ResourceID:        f.ResourceID,
			Provider:          provider,
			Service:           f.Service,
			ResourceType:      f.ResourceType,
			Region:            f.Region,
			Rule:              f.Rule,
			Severity:          string(f.Severity),
			MonthlyCostUSD:    roundCents(f.MonthlyCost),
			MonthlySavingsUSD: roundCents(f.MonthlySavings),
			Description:       f.Description,
			Recommendation:    f.Recommendation,
		})
	}

	// Highest savings first, tiebreak by resource id for a deterministic
	// order independent of analyzer internals.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MonthlySavingsUSD != items[j].MonthlySavingsUSD {
			return items[i].MonthlySavingsUSD > items[j].MonthlySavingsUSD
		}
		return items[i].ResourceID < items[j].ResourceID
	})

	totalCount := len(items)
	if len(items) > top {
		items = items[:top]
	}

	writeJSON(w, http.StatusOK, recommendationsResponse{
		Recommendations:        items,
		TotalCount:             totalCount,
		ReturnedCount:          len(items),
		TotalMonthlySavingsUSD: roundCents(totalSavings),
		BySeverity:             bySeverity,
		Filters: recommendationsFiltersDTO{
			Provider: providerFilter,
			Severity: strings.ToLower(string(severityFilter)),
			Top:      top,
		},
		GeneratedAt: time.Now().UTC(),
		DataSource:  recommendationsDataSource,
		Note:        recommendationsNote,
	})
}

// parseSeverityFilter maps an optional severity query param to a
// shared.Severity. An empty string means "no filter" (ok=true, empty
// Severity). An unrecognized value is rejected (ok=false).
func parseSeverityFilter(raw string) (shared.Severity, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", true
	case "high":
		return shared.SeverityHigh, true
	case "medium":
		return shared.SeverityMedium, true
	case "low":
		return shared.SeverityLow, true
	default:
		return "", false
	}
}
