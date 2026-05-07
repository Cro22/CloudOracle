package llm

import (
	"bytes"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// retryTransport wraps an http.RoundTripper so transient failures (5xx, 429,
// network blips) get retried with exponential-backoff-with-jitter. All three
// LLM clients share this — they construct an *http.Client with this transport
// underneath instead of the zero-value DefaultTransport.
//
// We do this at the transport layer (RoundTripper) rather than wrapping each
// client.Do call for two reasons:
//
//  1. Composition. Any code path that builds an http.Request — even paths we
//     add later — gets retries for free. We don't have to remember to wrap
//     every call site.
//  2. Testability. A RoundTripper is the standard mocking seam in net/http.
//     Tests can stub the *base* transport with httptest, and the retry logic
//     runs against it without any plumbing changes.
//
// The trade-off: a transport must buffer the request body to be able to retry,
// because the underlying transport consumes req.Body on Do. We do this once
// per request on entry and replace req.Body / req.GetBody before every attempt.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration

	// randFloat is overridden in tests to make jitter deterministic.
	// Production wires it to rand.Float64.
	randFloat func() float64
}

// retryableStatus is the set of HTTP statuses we consider worth retrying.
// Anthropic and OpenAI both return 429 with a Retry-After header on rate
// limit; 5xx are transient by definition; 408 is the rare "client took too
// long" but still worth a second shot.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

func newRetryTransport(base http.RoundTripper, maxRetries int, baseDelay, maxDelay time.Duration) *retryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryTransport{
		base:       base,
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
		randFloat:  rand.Float64,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body once so we can replay it on each attempt. POST bodies
	// to LLMs are tiny (a JSON prompt), so the memory cost is negligible.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	resetBody := func() {
		if bodyBytes == nil {
			return
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
		req.ContentLength = int64(len(bodyBytes))
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		resetBody()

		resp, err := t.base.RoundTrip(req)
		lastResp, lastErr = resp, err

		if err == nil && !retryableStatus(resp.StatusCode) {
			return resp, nil
		}

		if attempt == t.maxRetries {
			break
		}

		// Drain and close any prior response body before we discard it,
		// otherwise the underlying connection can't be reused.
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		delay := t.computeDelay(attempt, resp)

		slog.Warn("llm http retry",
			"attempt", attempt+1,
			"max", t.maxRetries,
			"delay", delay,
			"status", statusOrZero(resp),
			"error", err,
		)

		// Wait, but bail early if the caller's context is cancelled.
		select {
		case <-time.After(delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	return lastResp, lastErr
}

// computeDelay picks the wait duration for the next attempt.
//
// Priority order:
//  1. If the server sent Retry-After (per-spec on 429 and 503), honor it —
//     this is what distinguishes a serious retry from a naive one. Anthropic
//     and OpenAI both send delta-seconds; we also accept HTTP-date format.
//  2. Otherwise, exponential backoff (baseDelay * 2^attempt) with full jitter,
//     capped at maxDelay. Full jitter (uniform random in [0, backoff]) is the
//     AWS-recommended algorithm for distributed clients hitting the same API.
func (t *retryTransport) computeDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			if d > t.maxDelay {
				return t.maxDelay
			}
			return d
		}
	}

	backoff := t.baseDelay << attempt // baseDelay * 2^attempt
	if backoff <= 0 || backoff > t.maxDelay {
		backoff = t.maxDelay
	}

	jittered := time.Duration(t.randFloat() * float64(backoff))
	// Floor of 1ms so we don't spin in a hot loop on a degenerate base delay.
	if jittered < time.Millisecond {
		jittered = time.Millisecond
	}
	return jittered
}

func parseRetryAfter(h string) (time.Duration, bool) {
	if h == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0, true // server says "now"
		}
		return d, true
	}
	return 0, false
}

func statusOrZero(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
