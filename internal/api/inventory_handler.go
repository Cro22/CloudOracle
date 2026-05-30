package api

import (
	"net/http"
	"sort"
	"time"
)

// The inventory endpoint reports the *current scanned resource inventory* —
// the same data the dashboard's /api/resources serves — aggregated into
// counts and projected monthly cost per provider and per service. Unlike the
// cost endpoints it doesn't go through the snapshot approximation; each
// resource's MonthlyCost is the per-resource projected monthly rate recorded
// at scan time, so the data_source is labelled distinctly.
const (
	inventoryDataSource = "live_inventory"
	inventoryNote       = "Inventory reflects the latest CloudOracle resource scan. " +
		"monthly_cost_usd is the sum of per-resource projected monthly cost rates, " +
		"not billed spend."
)

// defaultInventoryTop caps the by_service list by default. Real inventories
// have a handful of service types, so this rarely bites — but it bounds the
// response and is overridable up to maxPageSize (200).
const defaultInventoryTop = 50

type inventoryAggDTO struct {
	Count          int     `json:"count"`
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
}

type serviceInventoryDTO struct {
	Service        string  `json:"service"`
	Provider       string  `json:"provider"`
	Count          int     `json:"count"`
	MonthlyCostUSD float64 `json:"monthly_cost_usd"`
}

type inventoryResponse struct {
	Provider            string                     `json:"provider,omitempty"`
	TotalResources      int                        `json:"total_resources"`
	TotalMonthlyCostUSD float64                    `json:"total_monthly_cost_usd"`
	TotalServices       int                        `json:"total_services"`
	ByProvider          map[string]inventoryAggDTO `json:"by_provider"`
	ByService           []serviceInventoryDTO      `json:"by_service"`
	GeneratedAt         time.Time                  `json:"generated_at"`
	DataSource          string                     `json:"data_source"`
	Note                string                     `json:"note"`
}

// handleInventory answers "what do I have?" — how many resources, of which
// services, where the cost concentrates. Optional filters:
//
//	provider=aws|gcp|azure   restrict to one cloud
//	top=N                    cap the by_service list (default 50, max 200)
//
// total_resources / total_monthly_cost_usd / total_services / by_provider all
// describe the full filtered set; only by_service is subject to the top cap,
// so a truncated list still reports accurate totals.
func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	providerFilter, ok := parseOptionalProvider(q.Get("provider"))
	if !ok {
		writeAPIError(w, http.StatusBadRequest,
			"provider must be one of aws, gcp, azure", "invalid_provider")
		return
	}

	top := parseIntOr(q.Get("top"), defaultInventoryTop)
	top = clampInt(top, 1, maxPageSize)

	resources, err := s.data.ListResources(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"failed to list resources: "+err.Error(), "resource_query_failed")
		return
	}

	byProvider := make(map[string]inventoryAggDTO)
	type svcKey struct{ provider, service string }
	byService := make(map[svcKey]*serviceInventoryDTO)

	var totalResources int
	var totalCost float64
	for _, res := range resources {
		provider := providerForServiceAccount(res.Service, res.AccountID)
		if providerFilter != "" && provider != providerFilter {
			continue
		}

		totalResources++
		totalCost += res.MonthlyCost

		pAgg := byProvider[provider]
		pAgg.Count++
		pAgg.MonthlyCostUSD += res.MonthlyCost
		byProvider[provider] = pAgg

		k := svcKey{provider, res.Service}
		svc := byService[k]
		if svc == nil {
			svc = &serviceInventoryDTO{Service: res.Service, Provider: provider}
			byService[k] = svc
		}
		svc.Count++
		svc.MonthlyCostUSD += res.MonthlyCost
	}

	// Round the accumulated provider totals once, at the end, to avoid
	// compounding rounding across many resources.
	for p, agg := range byProvider {
		agg.MonthlyCostUSD = roundCents(agg.MonthlyCostUSD)
		byProvider[p] = agg
	}

	services := make([]serviceInventoryDTO, 0, len(byService))
	for _, svc := range byService {
		svc.MonthlyCostUSD = roundCents(svc.MonthlyCostUSD)
		services = append(services, *svc)
	}
	// Cost desc, tiebreak by service name for a deterministic order.
	sort.Slice(services, func(i, j int) bool {
		if services[i].MonthlyCostUSD != services[j].MonthlyCostUSD {
			return services[i].MonthlyCostUSD > services[j].MonthlyCostUSD
		}
		return services[i].Service < services[j].Service
	})

	totalServices := len(services)
	if len(services) > top {
		services = services[:top]
	}

	writeJSON(w, http.StatusOK, inventoryResponse{
		Provider:            providerFilter,
		TotalResources:      totalResources,
		TotalMonthlyCostUSD: roundCents(totalCost),
		TotalServices:       totalServices,
		ByProvider:          byProvider,
		ByService:           services,
		GeneratedAt:         time.Now().UTC(),
		DataSource:          inventoryDataSource,
		Note:                inventoryNote,
	})
}
