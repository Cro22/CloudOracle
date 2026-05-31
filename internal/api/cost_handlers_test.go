package api

import (
	"CloudOracle/internal/db"
	"CloudOracle/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAPIData is the in-memory stand-in for the apiData interface used
// across api unit tests. Each method has a corresponding *Err field so an
// individual test can simulate a failure from one data path without
// affecting the others.
type fakeAPIData struct {
	resources    []shared.Resource
	resourcesErr error

	trends    []db.Trend
	trendsErr error

	snapshots    []db.Snapshot
	snapshotsErr error

	gotStart time.Time
	gotEnd   time.Time
	gotDays  int
}

func (f *fakeAPIData) ListResources(_ context.Context) ([]shared.Resource, error) {
	return f.resources, f.resourcesErr
}

func (f *fakeAPIData) ListTrends(_ context.Context, days int) ([]db.Trend, error) {
	f.gotDays = days
	return f.trends, f.trendsErr
}

func (f *fakeAPIData) ListSnapshotsInRange(_ context.Context, start, end time.Time) ([]db.Snapshot, error) {
	f.gotStart = start
	f.gotEnd = end
	return f.snapshots, f.snapshotsErr
}

const testAPIKey = "test-key-abc123"

func newCostTestServer(data apiData) *Server {
	return newTestServer(data, testAPIKey)
}

func doGet(t *testing.T, srv *Server, path string, withAuth bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if withAuth {
		req.Header.Set("X-API-Key", testAPIKey)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// snapshotsAprilEightySplit fixture: two providers (AWS via ec2 + rds, GCP
// via compute) so we can assert per-provider aggregation. Two snapshots per
// (account, service) — slightly different costs so the average is meaningful
// rather than coincidental.
func snapshotsAprilEightySplit() []db.Snapshot {
	return []db.Snapshot{
		// AWS / ec2: avg 100. acc-aws.
		{TakenAt: mustTime("2026-04-05T10:00:00Z"), AccountID: "acc-aws", Service: "ec2", ResourceCount: 5, TotalMonthlyCost: 90},
		{TakenAt: mustTime("2026-04-20T10:00:00Z"), AccountID: "acc-aws", Service: "ec2", ResourceCount: 5, TotalMonthlyCost: 110},
		// AWS / rds: avg 50. acc-aws.
		{TakenAt: mustTime("2026-04-05T10:00:00Z"), AccountID: "acc-aws", Service: "rds", ResourceCount: 1, TotalMonthlyCost: 40},
		{TakenAt: mustTime("2026-04-20T10:00:00Z"), AccountID: "acc-aws", Service: "rds", ResourceCount: 1, TotalMonthlyCost: 60},
		// GCP / compute: avg 200. acc-gcp.
		{TakenAt: mustTime("2026-04-05T10:00:00Z"), AccountID: "acc-gcp", Service: "compute", ResourceCount: 3, TotalMonthlyCost: 180},
		{TakenAt: mustTime("2026-04-20T10:00:00Z"), AccountID: "acc-gcp", Service: "compute", ResourceCount: 3, TotalMonthlyCost: 220},
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCostSummary_HappyPath(t *testing.T) {
	reader := &fakeAPIData{snapshots: snapshotsAprilEightySplit()}
	srv := newCostTestServer(reader)

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body costSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, rec.Body.String())
	}

	if body.Period.Start != "2026-04-01" || body.Period.End != "2026-04-30" {
		t.Errorf("period = %+v", body.Period)
	}
	if body.DataSource != "snapshots_approximation" {
		t.Errorf("data_source = %q, want snapshots_approximation", body.DataSource)
	}
	if body.Note == "" {
		t.Errorf("note must be populated, got empty string")
	}
	if _, ok := body.Providers["aws"]; !ok {
		t.Errorf("missing aws in providers: %+v", body.Providers)
	}
	if _, ok := body.Providers["gcp"]; !ok {
		t.Errorf("missing gcp in providers: %+v", body.Providers)
	}
	if body.Providers["aws"].Currency != "USD" {
		t.Errorf("aws currency = %q, want USD", body.Providers["aws"].Currency)
	}
	// AWS expected: (100 + 50) * 30/30 = 150 (period is 30 days inclusive).
	// GCP expected: 200 * 30/30 = 200.
	if !floatNearlyEqual(body.Providers["aws"].TotalUSD, 150.0, 0.5) {
		t.Errorf("aws total = %v, want ~150", body.Providers["aws"].TotalUSD)
	}
	if !floatNearlyEqual(body.Providers["gcp"].TotalUSD, 200.0, 0.5) {
		t.Errorf("gcp total = %v, want ~200", body.Providers["gcp"].TotalUSD)
	}
	if !floatNearlyEqual(body.GrandTotalUSD, 350.0, 0.5) {
		t.Errorf("grand total = %v, want ~350", body.GrandTotalUSD)
	}
}

func floatNearlyEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestCostSummary_ProvidersFilter(t *testing.T) {
	reader := &fakeAPIData{snapshots: snapshotsAprilEightySplit()}
	srv := newCostTestServer(reader)

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30&providers=aws", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body costSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body.Providers["aws"]; !ok {
		t.Errorf("aws should be present after filter, got %+v", body.Providers)
	}
	if _, ok := body.Providers["gcp"]; ok {
		t.Errorf("gcp should be excluded by providers=aws, got %+v", body.Providers)
	}
}

func TestCostSummary_InvalidProvidersFilter(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{})

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30&providers=aws,oracle", true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if code := extractCode(t, rec); code != "invalid_provider" {
		t.Errorf("code = %q, want invalid_provider", code)
	}
}

func TestCostSummary_DateRangeErrors(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"missing both", "/api/v1/cost-summary"},
		{"missing end", "/api/v1/cost-summary?start=2026-04-01"},
		{"bad start format", "/api/v1/cost-summary?start=apr1&end=2026-04-30"},
		{"bad end format", "/api/v1/cost-summary?start=2026-04-01&end=tomorrow"},
		{"end before start", "/api/v1/cost-summary?start=2026-04-30&end=2026-04-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newCostTestServer(&fakeAPIData{})
			rec := doGet(t, srv, tc.path, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if code := extractCode(t, rec); code != "invalid_date_range" {
				t.Errorf("code = %q, want invalid_date_range", code)
			}
		})
	}
}

func TestCostSummary_RequiresAuth(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{})

	noKey := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", false)
	if noKey.Code != http.StatusUnauthorized {
		t.Errorf("missing key: status = %d, want 401", noKey.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", rec.Code)
	}
	if code := extractCode(t, rec); code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", code)
	}
}

func TestCostSummary_SnapshotQueryError(t *testing.T) {
	reader := &fakeAPIData{snapshotsErr: errors.New("connection refused")}
	srv := newCostTestServer(reader)

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if code := extractCode(t, rec); code != "snapshot_query_failed" {
		t.Errorf("code = %q, want snapshot_query_failed", code)
	}
}

func TestCostSummary_PassesParsedRangeToReader(t *testing.T) {
	reader := &fakeAPIData{}
	srv := newCostTestServer(reader)

	rec := doGet(t, srv, "/api/v1/cost-summary?start=2026-04-01&end=2026-04-30", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	wantStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !reader.gotStart.Equal(wantStart) {
		t.Errorf("start passed to reader = %v, want %v", reader.gotStart, wantStart)
	}
	// End is expanded to 23:59:59.999999999 of the closing day.
	if reader.gotEnd.Year() != 2026 || reader.gotEnd.Month() != 4 || reader.gotEnd.Day() != 30 || reader.gotEnd.Hour() != 23 {
		t.Errorf("end passed to reader = %v, want 2026-04-30T23:59:59.999999999Z", reader.gotEnd)
	}
}

func TestCostByService_HappyPath(t *testing.T) {
	reader := &fakeAPIData{snapshots: snapshotsAprilEightySplit()}
	srv := newCostTestServer(reader)

	rec := doGet(t, srv, "/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=aws", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body costByServiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Provider != "aws" {
		t.Errorf("provider = %q, want aws", body.Provider)
	}
	if body.DataSource != "snapshots_approximation" {
		t.Errorf("data_source = %q", body.DataSource)
	}
	if body.Note == "" {
		t.Errorf("note must be populated")
	}
	if len(body.Services) != 2 {
		t.Fatalf("services len = %d, want 2 (ec2 + rds)", len(body.Services))
	}
	// Sorted by cost desc — ec2 (100) before rds (50).
	if body.Services[0].Name != "ec2" || body.Services[1].Name != "rds" {
		t.Errorf("services not sorted as expected: %+v", body.Services)
	}
	if !floatNearlyEqual(body.Services[0].Percentage, 66.67, 0.05) {
		t.Errorf("ec2 percentage = %v, want ~66.67", body.Services[0].Percentage)
	}
	if !floatNearlyEqual(body.TotalUSD, 150.0, 0.5) {
		t.Errorf("total = %v, want ~150", body.TotalUSD)
	}
}

func TestCostByService_TopCap(t *testing.T) {
	snaps := []db.Snapshot{
		{TakenAt: mustTime("2026-04-10T00:00:00Z"), AccountID: "acc-aws", Service: "ec2", TotalMonthlyCost: 100},
		{TakenAt: mustTime("2026-04-10T00:00:00Z"), AccountID: "acc-aws", Service: "rds", TotalMonthlyCost: 50},
		{TakenAt: mustTime("2026-04-10T00:00:00Z"), AccountID: "acc-aws", Service: "ebs", TotalMonthlyCost: 25},
		{TakenAt: mustTime("2026-04-10T00:00:00Z"), AccountID: "acc-aws", Service: "lambda", TotalMonthlyCost: 10},
	}
	srv := newCostTestServer(&fakeAPIData{snapshots: snaps})

	rec := doGet(t, srv, "/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=aws&top=2", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body costByServiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Services) != 2 {
		t.Errorf("top=2 should cap services slice, got %d entries", len(body.Services))
	}
	if body.Services[0].Name != "ec2" {
		t.Errorf("top entry = %s, want ec2", body.Services[0].Name)
	}
}

func TestCostByService_InvalidProvider(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{})

	tests := []string{
		"/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30",
		"/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=",
		"/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=oracle",
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			rec := doGet(t, srv, path, true)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if code := extractCode(t, rec); code != "invalid_provider" {
				t.Errorf("code = %q, want invalid_provider", code)
			}
		})
	}
}

func TestCostByService_RequiresAuth(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{})
	rec := doGet(t, srv, "/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=aws", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCostByService_EmptyData(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{snapshots: nil})

	rec := doGet(t, srv, "/api/v1/cost-by-service?start=2026-04-01&end=2026-04-30&provider=aws", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body costByServiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Services) != 0 {
		t.Errorf("services should be empty for empty data, got %+v", body.Services)
	}
	if body.TotalUSD != 0 {
		t.Errorf("total should be 0 for empty data, got %v", body.TotalUSD)
	}
	// The contract still requires the disclaimer fields to surface so the
	// agent doesn't accidentally claim "no spend" without context.
	if body.DataSource != "snapshots_approximation" || body.Note == "" {
		t.Errorf("disclaimer fields missing on empty response: source=%q note=%q",
			body.DataSource, body.Note)
	}
}

func TestParseDateRange_SameDayOK(t *testing.T) {
	start, end, err := parseDateRange("2026-04-15", "2026-04-15")
	if err != nil {
		t.Fatalf("parseDateRange same day: %v", err)
	}
	if !start.Equal(time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %v", start)
	}
	if end.Day() != 15 || end.Hour() != 23 {
		t.Errorf("end = %v, want end-of-day 2026-04-15", end)
	}
	if periodDays(start, end) != 1 {
		t.Errorf("periodDays(same day) = %d, want 1", periodDays(start, end))
	}
}

func TestParseProvidersFilter(t *testing.T) {
	out, err := parseProvidersFilter("aws, GCP ,azure")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out["aws"] || !out["gcp"] || !out["azure"] {
		t.Errorf("expected aws/gcp/azure all set, got %+v", out)
	}

	if out, err := parseProvidersFilter(""); err != nil || out != nil {
		t.Errorf("empty input should return nil/nil, got %+v / %v", out, err)
	}

	if _, err := parseProvidersFilter("aws,banana"); err == nil {
		t.Error("expected error for invalid provider")
	}
}

func TestPeriodDays(t *testing.T) {
	d := periodDays(
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC),
	)
	if d != 30 {
		t.Errorf("periodDays(april) = %d, want 30", d)
	}
}

// TestV0DashboardEndpointsRemainUnauthenticated asserts the design decision
// from the pre-work: adding /api/v1/* auth must NOT regress the dashboard
// endpoints that the embedded React UI talks to. We hit /api/* (handled by
// the v0 catch-all 404 path since pool is nil in test) and confirm we don't
// get a 401 — the absence of auth wiring is the actual assertion.
func TestV0DashboardEndpointsRemainUnauthenticated(t *testing.T) {
	srv := newCostTestServer(&fakeAPIData{})

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("v0 path returned 401, which means auth middleware leaked outside /api/v1/*")
	}
}

// extractCode pulls the `code` field out of a v1 error body. Centralised so
// every "should return error code X" test reads the same way and a future
// change to the error envelope only has to touch this helper.
func extractCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := strings.TrimSpace(rec.Body.String())
	var m map[string]string
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("error body is not JSON: %s", body)
	}
	return m["code"]
}
