package api

import (
	"CloudOracle/internal/shared"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// recommendationFixtures returns four resources: three trip an analyzer rule
// (ec2-idle/High, rds-oversized/Medium, ebs-orphan/High) and one healthy ec2
// trips nothing — so a test can assert that non-findings are excluded.
func recommendationFixtures() []shared.Resource {
	old := mustTime("2025-01-01T00:00:00Z") // well over the 7-day idle threshold
	return []shared.Resource{
		// ec2 idle: High, savings == cost == 300.
		{ID: "i-aaa", AccountID: "acc-aws", Service: "ec2", ResourceType: "t3.large", Region: "us-east-1", MonthlyCost: 300, UsageMetric: 1.0, CreatedAt: old},
		// rds oversized: Medium, savings == cost*0.5 == 50.
		{ID: "db-bbb", AccountID: "acc-aws", Service: "rds", ResourceType: "db.m5.large", Region: "us-east-1", MonthlyCost: 100, UsageMetric: 5.0, CreatedAt: old},
		// ebs orphan: High, savings == cost == 30.
		{ID: "vol-ccc", AccountID: "acc-aws", Service: "ebs", ResourceType: "gp3", Region: "us-east-1", MonthlyCost: 30, UsageMetric: 0, CreatedAt: old},
		// Healthy ec2: no finding.
		{ID: "i-ddd", AccountID: "acc-aws", Service: "ec2", ResourceType: "t3.micro", Region: "us-east-1", MonthlyCost: 20, UsageMetric: 80, CreatedAt: old},
	}
}

func decodeRecs(t *testing.T, body []byte) recommendationsResponse {
	t.Helper()
	var resp recommendationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
	return resp
}

func TestRecommendations_HappyPath(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	rec := doGet(t, srv, "/api/v1/recommendations", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	resp := decodeRecs(t, rec.Body.Bytes())

	if resp.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", resp.TotalCount)
	}
	if resp.ReturnedCount != 3 {
		t.Errorf("ReturnedCount = %d, want 3", resp.ReturnedCount)
	}
	if resp.TotalMonthlySavingsUSD != 380 {
		t.Errorf("TotalMonthlySavingsUSD = %v, want 380", resp.TotalMonthlySavingsUSD)
	}
	if resp.BySeverity["High"] != 2 || resp.BySeverity["Medium"] != 1 {
		t.Errorf("BySeverity = %v, want High:2 Medium:1", resp.BySeverity)
	}
	if resp.DataSource != recommendationsDataSource {
		t.Errorf("DataSource = %q, want %q", resp.DataSource, recommendationsDataSource)
	}
	// Sorted by savings desc: ec2(300) > rds(50) > ebs(30).
	got := []string{
		resp.Recommendations[0].ResourceID,
		resp.Recommendations[1].ResourceID,
		resp.Recommendations[2].ResourceID,
	}
	want := []string{"i-aaa", "db-bbb", "vol-ccc"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if resp.Recommendations[0].Provider != "aws" {
		t.Errorf("Provider = %q, want aws", resp.Recommendations[0].Provider)
	}
}

func TestRecommendations_TopCap(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	rec := doGet(t, srv, "/api/v1/recommendations?top=1", true)

	resp := decodeRecs(t, rec.Body.Bytes())
	if resp.ReturnedCount != 1 {
		t.Errorf("ReturnedCount = %d, want 1", resp.ReturnedCount)
	}
	// total_count and savings still describe the full filtered set.
	if resp.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3 (pre-cap)", resp.TotalCount)
	}
	if resp.TotalMonthlySavingsUSD != 380 {
		t.Errorf("TotalMonthlySavingsUSD = %v, want 380 (pre-cap)", resp.TotalMonthlySavingsUSD)
	}
	if resp.Recommendations[0].ResourceID != "i-aaa" {
		t.Errorf("top item = %q, want i-aaa", resp.Recommendations[0].ResourceID)
	}
}

func TestRecommendations_SeverityFilter(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	rec := doGet(t, srv, "/api/v1/recommendations?severity=high", true)

	resp := decodeRecs(t, rec.Body.Bytes())
	if resp.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2 (only High)", resp.TotalCount)
	}
	for _, r := range resp.Recommendations {
		if r.Severity != "High" {
			t.Errorf("got severity %q, want only High", r.Severity)
		}
	}
	if resp.Filters.Severity != "high" {
		t.Errorf("Filters.Severity = %q, want high", resp.Filters.Severity)
	}
}

func TestRecommendations_ProviderFilterEmptyForGCP(t *testing.T) {
	// All analyzer rules target AWS services, so provider=gcp yields none.
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	rec := doGet(t, srv, "/api/v1/recommendations?provider=gcp", true)

	resp := decodeRecs(t, rec.Body.Bytes())
	if resp.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0 for gcp", resp.TotalCount)
	}
	if len(resp.Recommendations) != 0 {
		t.Errorf("Recommendations = %v, want empty", resp.Recommendations)
	}
}

func TestRecommendations_BadFilters(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	cases := []struct{ name, path string }{
		{"bad provider", "/api/v1/recommendations?provider=oracle"},
		{"bad severity", "/api/v1/recommendations?severity=critical"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGet(t, srv, tc.path, true)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestRecommendations_AuthRequired(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: recommendationFixtures()})
	rec := doGet(t, srv, "/api/v1/recommendations", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRecommendations_DataError(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resourcesErr: errors.New("conn refused")})
	rec := doGet(t, srv, "/api/v1/recommendations", true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestRecommendations_EmptyFindings(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{resources: nil})
	rec := doGet(t, srv, "/api/v1/recommendations", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decodeRecs(t, rec.Body.Bytes())
	if resp.TotalCount != 0 || resp.ReturnedCount != 0 {
		t.Errorf("counts = %d/%d, want 0/0", resp.TotalCount, resp.ReturnedCount)
	}
	if resp.Recommendations == nil {
		t.Error("Recommendations should serialize as [] not null")
	}
	// Filters.Top should still report the effective default.
	if resp.Filters.Top != defaultRecommendationsTop {
		t.Errorf("Filters.Top = %d, want %d", resp.Filters.Top, defaultRecommendationsTop)
	}
}
