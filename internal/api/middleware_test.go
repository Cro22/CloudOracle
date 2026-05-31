package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddleware_GeneratesIDWhenAbsent(t *testing.T) {
	var inHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inHandler = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()
	requestIDMiddleware(next).ServeHTTP(rec, req)

	if inHandler == "" {
		t.Error("handler context did not receive a request ID")
	}
	echoed := rec.Header().Get("X-Request-ID")
	if echoed == "" {
		t.Error("response missing X-Request-ID header")
	}
	if echoed != inHandler {
		t.Errorf("header (%q) and context (%q) IDs should match", echoed, inHandler)
	}
	if len(echoed) != 24 {
		t.Errorf("generated ID should be 24 hex chars, got %d (%q)", len(echoed), echoed)
	}
}

func TestRequestIDMiddleware_HonorsIncomingHeader(t *testing.T) {
	const incoming = "client-supplied-id-001"
	var inHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inHandler = requestIDFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	req.Header.Set("X-Request-ID", incoming)
	rec := httptest.NewRecorder()
	requestIDMiddleware(next).ServeHTTP(rec, req)

	if inHandler != incoming {
		t.Errorf("incoming ID lost: got %q, want %q", inHandler, incoming)
	}
	if got := rec.Header().Get("X-Request-ID"); got != incoming {
		t.Errorf("echo header = %q, want %q", got, incoming)
	}
}

func TestAuthMiddleware_RejectsMissingKey(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	rec := httptest.NewRecorder()
	authMiddleware("server-key")(next).ServeHTTP(rec, req)

	if called {
		t.Error("handler must not be reached with missing key")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_RejectsWrongKey(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	authMiddleware("server-key")(next).ServeHTTP(rec, req)

	if called {
		t.Error("handler must not be reached with wrong key")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_AcceptsMatchingKey(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	req.Header.Set("X-API-Key", "server-key")
	rec := httptest.NewRecorder()
	authMiddleware("server-key")(next).ServeHTTP(rec, req)

	if !called {
		t.Error("handler must be reached when key matches")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddleware_RejectsWhenServerKeyEmpty(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cost-summary", nil)
	req.Header.Set("X-API-Key", "anything")
	rec := httptest.NewRecorder()
	authMiddleware("")(next).ServeHTTP(rec, req)

	if called {
		t.Error("server with empty key must not authenticate any request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestWriteAPIError_IncludesCodeField(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIError(rec, http.StatusBadRequest, "boom", "invalid_thing")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error":"boom"`) || !strings.Contains(body, `"code":"invalid_thing"`) {
		t.Errorf("body missing fields: %s", body)
	}
}
