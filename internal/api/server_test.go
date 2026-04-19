package api

import (
	"CloudOracle/internal/shared"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORSMiddleware_SetsHeaders(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("next handler was not called for GET")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected CORS origin '*', got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("expected GET in allowed methods, got %q", got)
	}
}

func TestCORSMiddleware_OptionsShortCircuits(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := corsMiddleware(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/resources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("next handler should not be called on OPTIONS preflight")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", rec.Code)
	}
}

func TestLoggingMiddleware_CapturesStatus(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := loggingMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("expected status 418 to pass through, got %d", rec.Code)
	}
}

func TestWriteJSON_SetsHeadersAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]int{"x": 1})

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("expected application/json, got %q", got)
	}

	var body map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body["x"] != 1 {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestWriteError_ReturnsErrorJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusInternalServerError, "boom")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body["error"] != "boom" {
		t.Errorf("unexpected error body: %v", body)
	}
}

func TestProviderFromResource(t *testing.T) {
	tests := []struct {
		name     string
		resource shared.Resource
		want     string
	}{
		{"EC2 is AWS", shared.Resource{Service: "ec2"}, "aws"},
		{"RDS is AWS", shared.Resource{Service: "rds"}, "aws"},
		{"EBS is AWS", shared.Resource{Service: "ebs"}, "aws"},
		{"Lambda is AWS", shared.Resource{Service: "lambda"}, "aws"},
		{"Compute is GCP", shared.Resource{Service: "compute"}, "gcp"},
		{"CloudSQL is GCP", shared.Resource{Service: "cloudsql"}, "gcp"},
		{"PersistentDisk is GCP", shared.Resource{Service: "persistent-disk"}, "gcp"},
		{"VM is Azure", shared.Resource{Service: "vm"}, "azure"},
		{"SQL is Azure", shared.Resource{Service: "sql"}, "azure"},
		{"ManagedDisk is Azure", shared.Resource{Service: "managed-disk"}, "azure"},
		{
			"Functions with Azure subscription GUID",
			shared.Resource{Service: "functions", AccountID: "12345678-1234-1234-1234-123456789012"},
			"azure",
		},
		{
			"Functions with non-GUID accountID -> gcp",
			shared.Resource{Service: "functions", AccountID: "my-gcp-project"},
			"gcp",
		},
		{"Unknown service", shared.Resource{Service: "magic"}, "other"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerFromResource(tc.resource); got != tc.want {
				t.Errorf("providerFromResource(%+v) = %s, want %s", tc.resource, got, tc.want)
			}
		})
	}
}

func TestParsePositiveInt(t *testing.T) {
	if n, err := parsePositiveInt("42"); err != nil || n != 42 {
		t.Errorf("expected 42/nil, got %d/%v", n, err)
	}
	if _, err := parsePositiveInt("0"); err == nil {
		t.Error("expected error for 0")
	}
	if _, err := parsePositiveInt("-5"); err == nil {
		t.Error("expected error for negative")
	}
	if _, err := parsePositiveInt("abc"); err == nil {
		t.Error("expected error for non-numeric")
	}
}

func TestParseIntOr_Defaults(t *testing.T) {
	if got := parseIntOr("", 7); got != 7 {
		t.Errorf("empty → default, got %d", got)
	}
	if got := parseIntOr("garbage", 7); got != 7 {
		t.Errorf("invalid → default, got %d", got)
	}
	if got := parseIntOr("42", 7); got != 42 {
		t.Errorf("valid → parsed, got %d", got)
	}
}

func TestClampInt(t *testing.T) {
	if got := clampInt(5, 1, 10); got != 5 {
		t.Errorf("in-range value kept, got %d", got)
	}
	if got := clampInt(-3, 1, 10); got != 1 {
		t.Errorf("below lo → lo, got %d", got)
	}
	if got := clampInt(999, 1, 10); got != 10 {
		t.Errorf("above hi → hi, got %d", got)
	}
}

func TestNormalizeSortColumn(t *testing.T) {
	cases := map[string]string{
		"severity": "severity",
		"Severity": "severity",
		"SERVICE":  "service",
		"cost":     "cost",
		"savings":  "savings",
		"":         "",
		"unknown":  "",
		"id":       "",
	}
	for in, want := range cases {
		if got := normalizeSortColumn(in); got != want {
			t.Errorf("normalizeSortColumn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeOrder(t *testing.T) {
	if got := normalizeOrder("asc"); got != "asc" {
		t.Errorf("asc should pass through, got %q", got)
	}
	if got := normalizeOrder("ASC"); got != "asc" {
		t.Errorf("case-insensitive asc, got %q", got)
	}
	if got := normalizeOrder(""); got != "desc" {
		t.Errorf("empty defaults to desc, got %q", got)
	}
	if got := normalizeOrder("anything-else"); got != "desc" {
		t.Errorf("unknown defaults to desc, got %q", got)
	}
}

func TestSortFindings_BySavingsDesc(t *testing.T) {
	findings := []shared.Finding{
		{ResourceID: "a", MonthlySavings: 10},
		{ResourceID: "b", MonthlySavings: 50},
		{ResourceID: "c", MonthlySavings: 30},
	}
	sortFindings(findings, "savings", "desc")
	want := []string{"b", "c", "a"}
	for i, f := range findings {
		if f.ResourceID != want[i] {
			t.Errorf("position %d: got %s, want %s", i, f.ResourceID, want[i])
		}
	}
}

func TestSortFindings_BySeverityAsc(t *testing.T) {
	findings := []shared.Finding{
		{ResourceID: "a", Severity: shared.SeverityHigh},
		{ResourceID: "b", Severity: shared.SeverityLow},
		{ResourceID: "c", Severity: shared.SeverityMedium},
	}
	sortFindings(findings, "severity", "asc")
	want := []string{"b", "c", "a"}
	for i, f := range findings {
		if f.ResourceID != want[i] {
			t.Errorf("position %d: got %s, want %s", i, f.ResourceID, want[i])
		}
	}
}

func TestSortFindings_ByService(t *testing.T) {
	findings := []shared.Finding{
		{ResourceID: "a", Service: "rds"},
		{ResourceID: "b", Service: "ebs"},
		{ResourceID: "c", Service: "ec2"},
	}
	sortFindings(findings, "service", "asc")
	want := []string{"b", "c", "a"}
	for i, f := range findings {
		if f.ResourceID != want[i] {
			t.Errorf("position %d: got %s, want %s", i, f.ResourceID, want[i])
		}
	}
}

func TestSortFindings_StableForEqualKeys(t *testing.T) {
	findings := []shared.Finding{
		{ResourceID: "a", Severity: shared.SeverityHigh},
		{ResourceID: "b", Severity: shared.SeverityHigh},
		{ResourceID: "c", Severity: shared.SeverityHigh},
	}
	sortFindings(findings, "severity", "desc")
	want := []string{"a", "b", "c"}
	for i, f := range findings {
		if f.ResourceID != want[i] {
			t.Errorf("stable sort lost order at %d: got %s, want %s", i, f.ResourceID, want[i])
		}
	}
}
