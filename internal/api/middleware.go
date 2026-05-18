package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

var errInvalidInt = errors.New("invalid positive integer")

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

type ctxKey string

// ctxRequestID identifies the per-request ID stashed in context by
// requestIDMiddleware. Handlers can pull it out via requestIDFromContext.
const ctxRequestID ctxKey = "request_id"

func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// requestIDMiddleware honors an incoming X-Request-ID header when set
// (so a reverse proxy or upstream client can correlate logs) and falls
// back to a fresh random ID otherwise. Always echoes the chosen ID
// back in the response header so callers can quote it in bug reports.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// newRequestID returns 24 hex chars from crypto/rand. Short enough to read
// in logs, long enough to avoid collisions across a single process lifetime.
// crypto/rand never errors in practice on Linux/macOS/Windows; if it ever
// did we'd rather log a blank ID than crash the request path.
func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// authMiddleware enforces the X-API-Key header against a fixed key loaded
// at startup. subtle.ConstantTimeCompare avoids leaking the valid key length
// through timing — small thing, but cheap to do right since we're already
// gating the v1 endpoints. A missing or empty configured key is treated as
// "deny all"; the serve subcommand refuses to start in that state, so this
// path is defense-in-depth.
func authMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				writeAPIError(w, http.StatusUnauthorized, "API key not configured on server", "unauthorized")
				return
			}
			got := r.Header.Get("X-API-Key")
			if got == "" {
				writeAPIError(w, http.StatusUnauthorized, "missing X-API-Key header", "unauthorized")
				return
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(apiKey)) != 1 {
				writeAPIError(w, http.StatusUnauthorized, "invalid API key", "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, X-Request-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFromContext(r.Context()),
		)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// writeError keeps the legacy single-field shape used by the v0 dashboard
// endpoints. New v1 handlers use writeAPIError so clients have a stable
// machine-readable `code` to branch on.
func writeError(w http.ResponseWriter, status int, message string) {
	slog.Error("handler error", "status", status, "message", message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeAPIError is the v1 error shape: {"error": <human-readable>, "code":
// <machine-readable enum>}. The code values are documented in the README
// so the Python agent and any other client can branch deterministically.
func writeAPIError(w http.ResponseWriter, status int, message, code string) {
	slog.Error("v1 handler error", "status", status, "code", code, "message", message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
		"code":  code,
	})
}
