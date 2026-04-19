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
