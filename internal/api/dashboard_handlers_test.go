package api

import (
	"CloudOracle/internal/db"
	"CloudOracle/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The v0 dashboard handlers predate the v1 cost endpoints and historically
// only had unit tests for their helper functions (sortFindings, clampInt,
// parsePositiveInt, etc.). These tests cover the end-to-end handler flow
// through the fakeAPIData fake, which gives us the same coverage the v1
// endpoints have without spinning up Postgres.

func newDashboardTestServer(data apiData) *Server {
	return newTestServer(data, "test-key")
}

func dashGet(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func sampleResources() []shared.Resource {
	return []shared.Resource{
		{ID: "i-1", AccountID: "acc-aws", Service: "ec2", ResourceType: "t3.micro", Region: "us-east-2", MonthlyCost: 100, UsageMetric: 5},
		{ID: "i-2", AccountID: "acc-aws", Service: "ec2", ResourceType: "t3.small", Region: "us-east-2", MonthlyCost: 60, UsageMetric: 30},
		{ID: "db-1", AccountID: "acc-aws", Service: "rds", ResourceType: "db.t3.micro", Region: "us-east-2", MonthlyCost: 50, UsageMetric: 10},
	}
}

func TestHandleResources_Happy(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resources: sampleResources()})

	rec := dashGet(t, srv, "/api/resources")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body resourcesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TotalCount != 3 {
		t.Errorf("total_count = %d, want 3", body.TotalCount)
	}
	if body.TotalMonthlyCost != 210 {
		t.Errorf("total_monthly_cost = %v, want 210", body.TotalMonthlyCost)
	}
}

func TestHandleResources_DBError(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resourcesErr: errors.New("conn refused")})
	rec := dashGet(t, srv, "/api/resources")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleFindings_Pagination(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resources: sampleResources()})

	rec := dashGet(t, srv, "/api/findings?page=1&page_size=10&sort=savings&order=desc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body findingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Page != 1 || body.PageSize != 10 {
		t.Errorf("page/page_size = %d/%d", body.Page, body.PageSize)
	}
	if body.Sort != "savings" || body.Order != "desc" {
		t.Errorf("sort/order = %s/%s", body.Sort, body.Order)
	}
	if body.TotalCount < 0 {
		t.Errorf("total_count = %d", body.TotalCount)
	}
}

func TestHandleFindings_DBError(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resourcesErr: errors.New("boom")})
	rec := dashGet(t, srv, "/api/findings")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleFindings_PageBeyondTotalClamps(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resources: sampleResources()})

	// Asking for page 99 with 10 per page on a small dataset must clamp
	// back to the last valid page, not return a 5xx.
	rec := dashGet(t, srv, "/api/findings?page=99&page_size=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body findingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Page > body.TotalPages || body.Page < 1 {
		t.Errorf("page=%d, total_pages=%d — page should clamp into range", body.Page, body.TotalPages)
	}
}

func TestHandleTrends_Happy(t *testing.T) {
	trends := []db.Trend{
		{Date: "2026-04-01", TotalCost: 100, ResourceCount: 5, BreakdownByService: map[string]float64{"ec2": 80, "rds": 20}},
		{Date: "2026-04-15", TotalCost: 120, ResourceCount: 6, BreakdownByService: map[string]float64{"ec2": 100, "rds": 20}},
	}
	fake := &fakeAPIData{trends: trends}
	srv := newDashboardTestServer(fake)

	rec := dashGet(t, srv, "/api/trends?days=45")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if fake.gotDays != 45 {
		t.Errorf("days passed to ListTrends = %d, want 45", fake.gotDays)
	}
	var got []db.Trend
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("trends len = %d, want 2", len(got))
	}
}

func TestHandleTrends_BadDaysFallsBackToDefault(t *testing.T) {
	fake := &fakeAPIData{}
	srv := newDashboardTestServer(fake)

	rec := dashGet(t, srv, "/api/trends?days=garbage")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Default fallback is 90; documented in handleTrends.
	if fake.gotDays != 90 {
		t.Errorf("days passed to ListTrends = %d, want default 90", fake.gotDays)
	}
}

func TestHandleTrends_DBError(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{trendsErr: errors.New("boom")})
	rec := dashGet(t, srv, "/api/trends")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleSummary_Happy(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resources: sampleResources()})

	rec := dashGet(t, srv, "/api/summary")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body summaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TotalResources != 3 {
		t.Errorf("total_resources = %d, want 3", body.TotalResources)
	}
	if body.TotalMonthlyCost != 210 {
		t.Errorf("total_monthly_cost = %v, want 210", body.TotalMonthlyCost)
	}
	if body.ByService["ec2"].Count != 2 {
		t.Errorf("by_service[ec2].count = %d, want 2", body.ByService["ec2"].Count)
	}
	if body.ByProvider["aws"].Count != 3 {
		t.Errorf("by_provider[aws].count = %d, want 3", body.ByProvider["aws"].Count)
	}
}

func TestHandleSummary_DBError(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resourcesErr: errors.New("boom")})
	rec := dashGet(t, srv, "/api/summary")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandle_UnknownAPIPathReturns404(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{})
	rec := dashGet(t, srv, "/api/does-not-exist")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestRunGracefulShutdown spins the server on an ephemeral port, fires one
// request to confirm it serves, then cancels the context and verifies Run
// returns nil (clean shutdown) within the configured timeout. Pure stdlib —
// no external mocks. Covers the Run path the production runServe depends on.
func TestRunGracefulShutdown(t *testing.T) {
	srv := newDashboardTestServer(&fakeAPIData{resources: sampleResources()})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx, addr, 2*time.Second) }()

	// Wait briefly for the server to bind, then make a request.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/api/resources")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("request never succeeded: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("request status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned %v on clean shutdown, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within shutdown timeout")
	}
}
