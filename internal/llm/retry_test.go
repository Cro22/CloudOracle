package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestRetryTransport builds a retryTransport with deterministic jitter
// (always 1.0, so the jittered value equals the full backoff) and tiny
// delays so tests run in milliseconds.
func newTestRetryTransport(maxRetries int, base http.RoundTripper) *retryTransport {
	t := newRetryTransport(base, maxRetries, time.Millisecond, 10*time.Millisecond)
	t.randFloat = func() float64 { return 1.0 } // no jitter for deterministic delay
	return t
}

func newClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: transport}
}

// TestRetry_RetriesUntilSuccess verifies the happy-path retry: server fails
// twice with 503, succeeds on the third attempt, client gets the success.
func TestRetry_RetriesUntilSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	rt := newTestRetryTransport(5, http.DefaultTransport)
	resp, err := newClient(rt).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server hit %d times, want 3", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", string(body))
	}
}

// TestRetry_RespectsMaxRetries verifies that after maxRetries failures the
// transport gives up and returns the final response (not an error). The
// caller's existing error-handling code stays unchanged.
func TestRetry_RespectsMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	rt := newTestRetryTransport(3, http.DefaultTransport)
	resp, err := newClient(rt).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get returned err = %v, want last response with 500", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (final attempt)", resp.StatusCode)
	}
	// maxRetries=3 means the transport tries: initial + 3 retries = 4 total calls.
	if got := calls.Load(); got != 4 {
		t.Errorf("server hit %d times, want 4 (1 initial + 3 retries)", got)
	}
}

// TestRetry_NonRetryableStatusReturnsImmediately confirms 4xx errors (other
// than 408/429) don't trigger retries. A 401 means "your key is wrong" — no
// amount of retrying fixes that.
func TestRetry_NonRetryableStatusReturnsImmediately(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	rt := newTestRetryTransport(5, http.DefaultTransport)
	resp, err := newClient(rt).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server hit %d times, want 1 (no retries on 401)", got)
	}
}

// TestRetry_HonorsRetryAfterDeltaSeconds is the test that distinguishes a
// "naive" retry from a "serious" one. When the server says "wait 1 second",
// we wait approximately 1 second — not the exponential backoff we'd compute.
func TestRetry_HonorsRetryAfterDeltaSeconds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// baseDelay tiny — if Retry-After were ignored, we'd see ~1ms delay.
	// Since Retry-After=1s is honored, total elapsed must be >= 1s.
	rt := newRetryTransport(http.DefaultTransport, 3, time.Millisecond, 5*time.Second)
	rt.randFloat = func() float64 { return 1.0 }

	start := time.Now()
	resp, err := newClient(rt).Get(srv.URL)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~1s (Retry-After ignored?)", elapsed)
	}
}

// TestRetry_ContextCancellation verifies that a context.Cancel during a
// backoff wait stops the loop immediately — no further server hits.
func TestRetry_ContextCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Long backoff so the context cancellation has time to fire mid-wait.
	rt := newRetryTransport(http.DefaultTransport, 5, 200*time.Millisecond, 1*time.Second)
	rt.randFloat = func() float64 { return 1.0 }

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond) // let the first call land
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	// First call should have happened; cancellation should prevent retries.
	if got := calls.Load(); got > 2 {
		t.Errorf("server hit %d times, want <= 2 after cancellation", got)
	}
}

// TestRetry_BodyIsReplayedOnEachAttempt is the subtle correctness test: when
// retrying a POST, every attempt must see the *full* request body, not an
// empty one (because the underlying transport consumes it on the first call).
func TestRetry_BodyIsReplayedOnEachAttempt(t *testing.T) {
	const wantBody = `{"prompt":"hello world"}`

	var calls atomic.Int32
	var lastBody atomic.Value // string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody.Store(string(body))
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := newTestRetryTransport(5, http.DefaultTransport)

	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(wantBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()

	if calls.Load() != 3 {
		t.Fatalf("server hit %d times, want 3", calls.Load())
	}
	got := lastBody.Load().(string)
	if got != wantBody {
		t.Errorf("body on third attempt = %q, want %q (body not replayed?)", got, wantBody)
	}
}

// TestRetry_NetworkErrorIsRetried verifies that transport-level errors
// (connection refused etc.) are retried, not just non-2xx statuses.
func TestRetry_NetworkErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	failingTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		n := calls.Add(1)
		if n < 3 {
			return nil, errors.New("connection refused")
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})

	rt := newTestRetryTransport(5, failingTransport)

	req, _ := http.NewRequest("GET", "http://example", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()

	if calls.Load() != 3 {
		t.Errorf("attempts = %d, want 3 (errors should retry)", calls.Load())
	}
}

func TestRetry_MaxRetriesZeroDisablesRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Note: in production newHTTPClient skips the retry transport entirely
	// when MaxRetries=0, but the transport itself should also degrade safely.
	rt := newTestRetryTransport(0, http.DefaultTransport)
	resp, err := newClient(rt).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no retries with maxRetries=0)", calls.Load())
	}
}

func TestRetryableStatus(t *testing.T) {
	retryable := []int{408, 429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !retryableStatus(code) {
			t.Errorf("status %d should be retryable", code)
		}
	}
	notRetryable := []int{200, 201, 301, 400, 401, 403, 404, 422}
	for _, code := range notRetryable {
		if retryableStatus(code) {
			t.Errorf("status %d should NOT be retryable", code)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d, ok := parseRetryAfter("5"); !ok || d != 5*time.Second {
		t.Errorf("delta-seconds: got d=%v ok=%v, want 5s/true", d, ok)
	}
	if d, ok := parseRetryAfter(""); ok || d != 0 {
		t.Errorf("empty: got d=%v ok=%v, want 0/false", d, ok)
	}
	if _, ok := parseRetryAfter("not-a-number-or-date"); ok {
		t.Error("garbage input should return ok=false")
	}
	// HTTP-date format
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	d, ok := parseRetryAfter(future)
	if !ok {
		t.Errorf("HTTP-date should parse: %q", future)
	}
	if d > 3*time.Second || d < 0 {
		t.Errorf("HTTP-date delta = %v, want ~2s", d)
	}
}

// roundTripperFunc adapts a function to http.RoundTripper for tests that need
// to simulate transport-level failures (vs HTTP-level failures from httptest).
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
