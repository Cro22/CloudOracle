package api

import (
	"CloudOracle/internal/billing"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// fakeBillingSource is an injectable billing.Source for exercising the v1 cost
// handlers against a non-snapshot source (e.g. AWS Cost Explorer) without a DB.
type fakeBillingSource struct {
	report   billing.Report
	err      error
	gotStart time.Time
	gotEnd   time.Time
}

func (f *fakeBillingSource) Costs(
	_ context.Context, start, end time.Time,
) (billing.Report, error) {
	f.gotStart, f.gotEnd = start, end
	if f.err != nil {
		return billing.Report{}, f.err
	}
	return f.report, nil
}

func billingReport() billing.Report {
	return billing.Report{
		Records: []billing.CostRecord{
			{Provider: "aws", Service: "ec2", AmountUSD: 100},
			{Provider: "aws", Service: "rds", AmountUSD: 50},
			{Provider: "gcp", Service: "compute", AmountUSD: 200},
		},
		DataSource: billing.AWSCostExplorerDataSource,
		Note:       "real billed cost",
	}
}

func TestCostSummary_UsesInjectedBillingSource(t *testing.T) {
	src := &fakeBillingSource{report: billingReport()}
	srv := newTestServer(&fakeAPIData{}, testAPIKey, WithBillingSource(src))

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body costSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// data_source flows through from the source, not a hard-coded constant.
	if body.DataSource != billing.AWSCostExplorerDataSource {
		t.Errorf("data_source = %q, want %q", body.DataSource, billing.AWSCostExplorerDataSource)
	}
	if body.Providers["aws"].TotalUSD != 150 {
		t.Errorf("aws total = %v, want 150 (ec2 100 + rds 50)", body.Providers["aws"].TotalUSD)
	}
	if body.Providers["gcp"].TotalUSD != 200 {
		t.Errorf("gcp total = %v, want 200", body.Providers["gcp"].TotalUSD)
	}
	if body.GrandTotalUSD != 350 {
		t.Errorf("grand total = %v, want 350", body.GrandTotalUSD)
	}
	// The parsed range is forwarded to the source.
	if body.Period.Start != "2026-04-01" || src.gotStart.IsZero() {
		t.Errorf("source did not receive the parsed range: %+v", src)
	}
}

func TestCostSummary_ProvidersFilterWithBillingSource(t *testing.T) {
	src := &fakeBillingSource{report: billingReport()}
	srv := newTestServer(&fakeAPIData{}, testAPIKey, WithBillingSource(src))

	rec := doGet(t, srv,
		"/api/v1/cost-summary?start=2026-04-01&end=2026-04-30&providers=aws", true)
	var body costSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body.Providers["gcp"]; ok {
		t.Error("gcp should be filtered out")
	}
	if body.GrandTotalUSD != 150 {
		t.Errorf("grand total = %v, want 150 (aws only)", body.GrandTotalUSD)
	}
}

func TestCostByService_UsesInjectedBillingSource(t *testing.T) {
	src := &fakeBillingSource{report: billingReport()}
	srv := newTestServer(&fakeAPIData{}, testAPIKey, WithBillingSource(src))

	rec := doGet(t, srv,
		"/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=aws", true)
	var body costByServiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DataSource != billing.AWSCostExplorerDataSource {
		t.Errorf("data_source = %q, want %q", body.DataSource, billing.AWSCostExplorerDataSource)
	}
	if body.TotalUSD != 150 {
		t.Errorf("total = %v, want 150 (aws ec2+rds)", body.TotalUSD)
	}
	// Sorted by cost desc: ec2 (100) before rds (50).
	if len(body.Services) != 2 || body.Services[0].Name != "ec2" {
		t.Errorf("services = %+v, want ec2 first", body.Services)
	}
}

func TestCostSummary_BillingSourceErrorCode(t *testing.T) {
	src := &fakeBillingSource{
		err: &billing.SourceError{Code: "billing_query_failed", Err: errors.New("access denied")},
	}
	srv := newTestServer(&fakeAPIData{}, testAPIKey, WithBillingSource(src))

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if code := extractCode(t, rec); code != "billing_query_failed" {
		t.Errorf("code = %q, want billing_query_failed", code)
	}
}
