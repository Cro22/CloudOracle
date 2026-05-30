package api

import (
	"CloudOracle/internal/db"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// trendFixtures: three ascending days. AWS via ec2+rds, GCP via compute, so a
// provider filter recomputes the per-day total from the service breakdown.
//
//	day1: aws 150 (ec2 100 + rds 50), gcp 50  → total 200
//	day2: aws 180 (ec2 120 + rds 60), gcp 60  → total 240
//	day3: aws 220 (ec2 150 + rds 70), gcp 80  → total 300
func trendFixtures() []db.Trend {
	return []db.Trend{
		{Date: "2026-03-01", TotalCost: 200, ResourceCount: 9, BreakdownByService: map[string]float64{"ec2": 100, "rds": 50, "compute": 50}},
		{Date: "2026-03-15", TotalCost: 240, ResourceCount: 9, BreakdownByService: map[string]float64{"ec2": 120, "rds": 60, "compute": 60}},
		{Date: "2026-03-30", TotalCost: 300, ResourceCount: 9, BreakdownByService: map[string]float64{"ec2": 150, "rds": 70, "compute": 80}},
	}
}

func decodeTrends(t *testing.T, body []byte) costTrendsResponse {
	t.Helper()
	var resp costTrendsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return resp
}

func TestCostTrends_HappyPath(t *testing.T) {
	fake := &fakeAPIData{trends: trendFixtures()}
	srv := newCostTestServer(fake)
	rec := doGet(t, srv, "/api/v1/cost-trends", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	resp := decodeTrends(t, rec.Body.Bytes())

	if fake.gotDays != defaultTrendDays {
		t.Errorf("ListTrends days = %d, want default %d", fake.gotDays, defaultTrendDays)
	}
	if len(resp.Points) != 3 {
		t.Fatalf("Points len = %d, want 3", len(resp.Points))
	}
	if resp.First == nil || resp.First.TotalCostUSD != 200 {
		t.Errorf("First = %+v, want total 200", resp.First)
	}
	if resp.Latest == nil || resp.Latest.TotalCostUSD != 300 {
		t.Errorf("Latest = %+v, want total 300", resp.Latest)
	}
	if resp.Change == nil {
		t.Fatal("Change is nil")
	}
	if resp.Change.AbsoluteUSD != 100 {
		t.Errorf("Change.AbsoluteUSD = %v, want 100", resp.Change.AbsoluteUSD)
	}
	if resp.Change.PercentFromFirst == nil || *resp.Change.PercentFromFirst != 50 {
		t.Errorf("Change.PercentFromFirst = %v, want 50", resp.Change.PercentFromFirst)
	}
	if resp.Change.Direction != "up" {
		t.Errorf("Change.Direction = %q, want up", resp.Change.Direction)
	}
	if resp.DataSource != dataSourceLabel {
		t.Errorf("DataSource = %q, want %q", resp.DataSource, dataSourceLabel)
	}
}

func TestCostTrends_ProviderFilter(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{trends: trendFixtures()})
	rec := doGet(t, srv, "/api/v1/cost-trends?provider=aws", true)

	resp := decodeTrends(t, rec.Body.Bytes())
	if resp.Provider != "aws" {
		t.Errorf("Provider = %q, want aws", resp.Provider)
	}
	// AWS-only per-day totals: 150, 180, 220.
	wantTotals := []float64{150, 180, 220}
	for i, want := range wantTotals {
		if resp.Points[i].TotalCostUSD != want {
			t.Errorf("Points[%d].TotalCostUSD = %v, want %v", i, resp.Points[i].TotalCostUSD, want)
		}
	}
	if resp.Change.AbsoluteUSD != 70 {
		t.Errorf("Change.AbsoluteUSD = %v, want 70", resp.Change.AbsoluteUSD)
	}
	// 70 / 150 * 100 = 46.666... → 46.67
	if resp.Change.PercentFromFirst == nil || *resp.Change.PercentFromFirst != 46.67 {
		t.Errorf("Change.PercentFromFirst = %v, want 46.67", resp.Change.PercentFromFirst)
	}
}

func TestCostTrends_DaysClampUpper(t *testing.T) {
	fake := &fakeAPIData{trends: trendFixtures()}
	srv := newCostTestServer(fake)
	doGet(t, srv, "/api/v1/cost-trends?days=9999", true)
	if fake.gotDays != maxTrendDays {
		t.Errorf("ListTrends days = %d, want clamped to %d", fake.gotDays, maxTrendDays)
	}
}

func TestCostTrends_BadProvider(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{trends: trendFixtures()})
	rec := doGet(t, srv, "/api/v1/cost-trends?provider=oracle", true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestCostTrends_AuthRequired(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{trends: trendFixtures()})
	rec := doGet(t, srv, "/api/v1/cost-trends", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCostTrends_DataError(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{trendsErr: errors.New("boom")})
	rec := doGet(t, srv, "/api/v1/cost-trends", true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestCostTrends_EmptySeries(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{trends: nil})
	rec := doGet(t, srv, "/api/v1/cost-trends", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeTrends(t, rec.Body.Bytes())
	if resp.Points == nil {
		t.Error("Points should serialize as [] not null")
	}
	if resp.First != nil || resp.Latest != nil || resp.Change != nil {
		t.Errorf("first/latest/change should be null for empty series; got %+v/%+v/%+v",
			resp.First, resp.Latest, resp.Change)
	}
}

func TestCostTrends_GrowthFromZeroHasNilPercent(t *testing.T) {
	// First day zero, later non-zero: percentage from zero is undefined, so
	// percent_from_first must be null but direction still "up".
	trends := []db.Trend{
		{Date: "2026-03-01", TotalCost: 0, BreakdownByService: map[string]float64{}},
		{Date: "2026-03-30", TotalCost: 120, BreakdownByService: map[string]float64{"ec2": 120}},
	}
	srv := newCostTestServer(&fakeAPIData{trends: trends})
	rec := doGet(t, srv, "/api/v1/cost-trends", true)

	resp := decodeTrends(t, rec.Body.Bytes())
	if resp.Change == nil {
		t.Fatal("Change is nil")
	}
	if resp.Change.PercentFromFirst != nil {
		t.Errorf("PercentFromFirst = %v, want nil (growth from zero)", *resp.Change.PercentFromFirst)
	}
	if resp.Change.Direction != "up" {
		t.Errorf("Direction = %q, want up", resp.Change.Direction)
	}
	if resp.Change.AbsoluteUSD != 120 {
		t.Errorf("AbsoluteUSD = %v, want 120", resp.Change.AbsoluteUSD)
	}
}

func TestCostTrends_FlatDirection(t *testing.T) {
	trends := []db.Trend{
		{Date: "2026-03-01", TotalCost: 100, BreakdownByService: map[string]float64{"ec2": 100}},
		{Date: "2026-03-30", TotalCost: 100, BreakdownByService: map[string]float64{"ec2": 100}},
	}
	srv := newCostTestServer(&fakeAPIData{trends: trends})
	rec := doGet(t, srv, "/api/v1/cost-trends", true)

	resp := decodeTrends(t, rec.Body.Bytes())
	if resp.Change.Direction != "flat" {
		t.Errorf("Direction = %q, want flat", resp.Change.Direction)
	}
	if resp.Change.PercentFromFirst == nil || *resp.Change.PercentFromFirst != 0 {
		t.Errorf("PercentFromFirst = %v, want 0", resp.Change.PercentFromFirst)
	}
}
