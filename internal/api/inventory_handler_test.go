package api

import (
	"CloudOracle/internal/shared"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// azureFunctionsAccount is a 36-char UUID with dashes at indices 8 and 13 —
// the shape providerForServiceAccount uses to disambiguate "functions" as
// Azure rather than the GCP default.
const azureFunctionsAccount = "12345678-1234-1234-1234-123456789012"

// inventoryFixtures spans all three providers, repeats a service (ec2) to
// exercise counting, and includes both flavours of the ambiguous "functions"
// service so the AccountID-based provider split is covered.
//
//	aws:   ec2 x2 (300), rds (50)          → count 3, cost 350
//	gcp:   compute (80), functions-gcp (10) → count 2, cost 90
//	azure: vm (40), functions-azure (20)    → count 2, cost 60
func inventoryFixtures() []shared.Resource {
	return []shared.Resource{
		{ID: "i-1", AccountID: "acc-aws", Service: "ec2", MonthlyCost: 100},
		{ID: "i-2", AccountID: "acc-aws", Service: "ec2", MonthlyCost: 200},
		{ID: "db-1", AccountID: "acc-aws", Service: "rds", MonthlyCost: 50},
		{ID: "gce-1", AccountID: "proj-gcp", Service: "compute", MonthlyCost: 80},
		{ID: "vm-1", AccountID: "sub-azure", Service: "vm", MonthlyCost: 40},
		{ID: "fn-gcp", AccountID: "proj-gcp", Service: "functions", MonthlyCost: 10},
		{ID: "fn-az", AccountID: azureFunctionsAccount, Service: "functions", MonthlyCost: 20},
	}
}

func decodeInventory(t *testing.T, body []byte) inventoryResponse {
	t.Helper()
	var resp inventoryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return resp
}

func TestInventory_HappyPath(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: inventoryFixtures()})
	rec := doGet(t, srv, "/api/v1/inventory", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	resp := decodeInventory(t, rec.Body.Bytes())

	if resp.TotalResources != 7 {
		t.Errorf("TotalResources = %d, want 7", resp.TotalResources)
	}
	if resp.TotalMonthlyCostUSD != 500 {
		t.Errorf("TotalMonthlyCostUSD = %v, want 500", resp.TotalMonthlyCostUSD)
	}
	if resp.TotalServices != 6 {
		t.Errorf("TotalServices = %d, want 6", resp.TotalServices)
	}
	if resp.DataSource != inventoryDataSource {
		t.Errorf("DataSource = %q, want %q", resp.DataSource, inventoryDataSource)
	}

	wantProviders := map[string]inventoryAggDTO{
		"aws":   {Count: 3, MonthlyCostUSD: 350},
		"gcp":   {Count: 2, MonthlyCostUSD: 90},
		"azure": {Count: 2, MonthlyCostUSD: 60},
	}
	for p, want := range wantProviders {
		got := resp.ByProvider[p]
		if got != want {
			t.Errorf("ByProvider[%q] = %+v, want %+v", p, got, want)
		}
	}

	// Top by cost is ec2 (aws, 2 resources, 300).
	top := resp.ByService[0]
	if top.Service != "ec2" || top.Provider != "aws" || top.Count != 2 || top.MonthlyCostUSD != 300 {
		t.Errorf("ByService[0] = %+v, want ec2/aws/2/300", top)
	}
}

func TestInventory_ProviderFilter(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: inventoryFixtures()})
	rec := doGet(t, srv, "/api/v1/inventory?provider=aws", true)

	resp := decodeInventory(t, rec.Body.Bytes())
	if resp.Provider != "aws" {
		t.Errorf("Provider = %q, want aws", resp.Provider)
	}
	if resp.TotalResources != 3 {
		t.Errorf("TotalResources = %d, want 3", resp.TotalResources)
	}
	if resp.TotalMonthlyCostUSD != 350 {
		t.Errorf("TotalMonthlyCostUSD = %v, want 350", resp.TotalMonthlyCostUSD)
	}
	if resp.TotalServices != 2 {
		t.Errorf("TotalServices = %d, want 2 (ec2, rds)", resp.TotalServices)
	}
	if len(resp.ByProvider) != 1 {
		t.Errorf("ByProvider has %d entries, want 1 (aws only)", len(resp.ByProvider))
	}
	for _, svc := range resp.ByService {
		if svc.Provider != "aws" {
			t.Errorf("ByService entry %+v not aws", svc)
		}
	}
}

func TestInventory_TopCap(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: inventoryFixtures()})
	rec := doGet(t, srv, "/api/v1/inventory?top=2", true)

	resp := decodeInventory(t, rec.Body.Bytes())
	if len(resp.ByService) != 2 {
		t.Errorf("ByService len = %d, want 2", len(resp.ByService))
	}
	// total_services still reports the full distinct count.
	if resp.TotalServices != 6 {
		t.Errorf("TotalServices = %d, want 6 (pre-cap)", resp.TotalServices)
	}
	if resp.TotalResources != 7 {
		t.Errorf("TotalResources = %d, want 7 (pre-cap)", resp.TotalResources)
	}
}

func TestInventory_BadProvider(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: inventoryFixtures()})
	rec := doGet(t, srv, "/api/v1/inventory?provider=oracle", true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestInventory_AuthRequired(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: inventoryFixtures()})
	rec := doGet(t, srv, "/api/v1/inventory", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestInventory_DataError(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resourcesErr: errors.New("boom")})
	rec := doGet(t, srv, "/api/v1/inventory", true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestInventory_Empty(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: nil})
	rec := doGet(t, srv, "/api/v1/inventory", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeInventory(t, rec.Body.Bytes())
	if resp.TotalResources != 0 || resp.TotalServices != 0 {
		t.Errorf("totals = %d/%d, want 0/0", resp.TotalResources, resp.TotalServices)
	}
	if resp.ByService == nil {
		t.Error("ByService should serialize as [] not null")
	}
	if resp.ByProvider == nil {
		t.Error("ByProvider should serialize as {} not null")
	}
}
