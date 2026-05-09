package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureLogs swaps slog's default logger for one that writes to a
// buffer, so tests can assert on warnings emitted by the silent
// fallbacks (multi-match, body truncation, pagination cap). Restored
// on test cleanup.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// makeComment is a small constructor used to keep test fixtures terse.
func makeComment(id int64, body string, updated time.Time) Comment {
	return Comment{ID: id, Body: body, UpdatedAt: updated}
}

func mustParse(t *testing.T, ts string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("bad timestamp %q: %v", ts, err)
	}
	return v
}

// repo is the canonical test repo. The values are unimportant; the
// server validates them so a bug that swaps owner/name is loud.
var testRepo = Repo{Owner: "Cro22", Name: "CloudOracle"}

// --- listComments ---

func TestListComments_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repos/Cro22/CloudOracle/issues/42/comments") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]Comment{
			makeComment(1, "first", mustParse(t, "2026-05-01T10:00:00Z")),
			makeComment(2, "second", mustParse(t, "2026-05-02T10:00:00Z")),
		})
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	got, err := c.listComments(context.Background(), testRepo, 42)
	if err != nil {
		t.Fatalf("listComments: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d comments, want 2", len(got))
	}
}

func TestListComments_TwoPages(t *testing.T) {
	var pagesSeen []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pagesSeen = append(pagesSeen, page)
		switch page {
		case "1":
			_ = json.NewEncoder(w).Encode(makeBatch(100, 1))
		case "2":
			_ = json.NewEncoder(w).Encode(makeBatch(50, 101))
		default:
			t.Errorf("unexpected page request: %s", page)
		}
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	got, err := c.listComments(context.Background(), testRepo, 1)
	if err != nil {
		t.Fatalf("listComments: %v", err)
	}
	if len(got) != 150 {
		t.Errorf("got %d comments, want 150", len(got))
	}
	if len(pagesSeen) != 2 || pagesSeen[0] != "1" || pagesSeen[1] != "2" {
		t.Errorf("pages seen = %v, want [1 2]", pagesSeen)
	}
}

func TestListComments_PaginationCap(t *testing.T) {
	logs := captureLogs(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return a full page so the function never sees a
		// short page (which would naturally terminate the loop).
		_ = json.NewEncoder(w).Encode(makeBatch(100, 1))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	got, err := c.listComments(context.Background(), testRepo, 1)
	if err != nil {
		t.Fatalf("listComments: %v", err)
	}
	if len(got) != maxPagesGitHub*perPage {
		t.Errorf("collected %d comments, want %d (cap)", len(got), maxPagesGitHub*perPage)
	}
	if !strings.Contains(logs.String(), "pagination cap hit") {
		t.Errorf("expected pagination-cap warn; logs:\n%s", logs.String())
	}
}

func TestListComments_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.listComments(context.Background(), testRepo, 99)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want substring 'not found'", err)
	}
	if !strings.Contains(err.Error(), "PR #99") {
		t.Errorf("error should reference the PR number; got %q", err)
	}
}

func TestListComments_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.listComments(context.Background(), testRepo, 1)
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %q, want substring 'authentication failed'", err)
	}
}

func TestListComments_403WrappedAsAuth(t *testing.T) {
	// 403 (rate-limited / forbidden) shares the auth-error mapping
	// because there's no useful user-facing distinction at the call
	// site — both mean "GitHub rejected your token".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.listComments(context.Background(), testRepo, 1)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("403 should map to authentication failed; got %v", err)
	}
}

func TestListComments_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // closed before any request arrives — connection refused

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.listComments(context.Background(), testRepo, 1)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("error = %q, want substring 'request failed'", err)
	}
}

func TestListComments_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.listComments(ctx, testRepo, 1)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// --- postComment / updateComment ---

func TestPostComment_Success(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Comment{ID: 12345, Body: "x", UpdatedAt: time.Now()})
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	id, err := c.postComment(context.Background(), testRepo, 7, "hello body")
	if err != nil {
		t.Fatalf("postComment: %v", err)
	}
	if id != 12345 {
		t.Errorf("id = %d, want 12345", id)
	}
	if !strings.Contains(string(gotBody), `"body":"hello body"`) {
		t.Errorf("server didn't see expected body; got %s", string(gotBody))
	}
}

func TestPostComment_422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"code":"missing"}]}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.postComment(context.Background(), testRepo, 1, "body")
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("422 should map to validation failed; got %v", err)
	}
}

func TestPostComment_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream broken`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.postComment(context.Background(), testRepo, 1, "body")
	if err == nil || !strings.Contains(err.Error(), "server error") {
		t.Errorf("500 should map to server error; got %v", err)
	}
}

func TestPostComment_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	_, err := c.postComment(context.Background(), testRepo, 1, "body")
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Errorf("network error should map to request failed; got %v", err)
	}
}

func TestUpdateComment_Success(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(Comment{ID: 12345, Body: "updated"})
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	if err := c.updateComment(context.Background(), testRepo, 12345, "updated body"); err != nil {
		t.Fatalf("updateComment: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/issues/comments/12345") {
		t.Errorf("path = %s, want suffix '/issues/comments/12345'", gotPath)
	}
	if !strings.Contains(string(gotBody), `"body":"updated body"`) {
		t.Errorf("server didn't see expected body; got %s", string(gotBody))
	}
}

func TestUpdateComment_422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"validation"}`))
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	err := c.updateComment(context.Background(), testRepo, 1, "body")
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("422 should map to validation failed; got %v", err)
	}
}

func TestUpdateComment_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	err := c.updateComment(context.Background(), testRepo, 1, "body")
	if err == nil || !strings.Contains(err.Error(), "server error") {
		t.Errorf("502 should map to server error; got %v", err)
	}
}

// --- PostOrUpdateComment integration ---

const testMarker = "<!-- cloudoracle-pr-v1 -->"

// scriptedServer is a tiny mux that lets each test wire its own
// list / post / update behaviour. Every request increments calls
// counters so tests can assert on which verbs were exercised.
type scriptedServer struct {
	t            *testing.T
	listComments func(page string) ([]Comment, int)
	postBody     string
	postID       int64
	postStatus   int
	updateBody   string
	updateID     int64
	updateStatus int
	calls        struct {
		list, post, update int
	}
}

func (s *scriptedServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/issues/") && strings.HasSuffix(r.URL.Path, "/comments"):
			s.calls.list++
			batch, status := s.listComments(r.URL.Query().Get("page"))
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			_ = json.NewEncoder(w).Encode(batch)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/comments"):
			s.calls.post++
			body, _ := io.ReadAll(r.Body)
			s.postBody = string(body)
			if s.postStatus != 0 {
				w.WriteHeader(s.postStatus)
				_, _ = w.Write([]byte(`{"message":"forced failure"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(Comment{ID: s.postID, Body: "x"})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/issues/comments/"):
			s.calls.update++
			body, _ := io.ReadAll(r.Body)
			s.updateBody = string(body)
			parts := strings.Split(r.URL.Path, "/")
			s.updateID, _ = parseInt64(parts[len(parts)-1])
			if s.updateStatus != 0 {
				w.WriteHeader(s.updateStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(Comment{ID: s.updateID, Body: "updated"})
		default:
			s.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestPostOrUpdateComment_Update(t *testing.T) {
	existing := []Comment{
		makeComment(101, "## 💰 Cloud Cost Impact\nbody A\n"+testMarker, mustParse(t, "2026-05-01T10:00:00Z")),
		makeComment(102, "an unrelated review comment", mustParse(t, "2026-05-02T10:00:00Z")),
	}
	scr := &scriptedServer{
		t: t,
		listComments: func(page string) ([]Comment, int) {
			if page == "1" {
				return existing, 0
			}
			return nil, 0
		},
		updateID: 101,
	}
	srv := httptest.NewServer(scr.handler())
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	id, created, err := c.PostOrUpdateComment(context.Background(), testRepo, 5, "new body "+testMarker, testMarker)
	if err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}
	if created {
		t.Errorf("expected created=false (update path)")
	}
	if id != 101 {
		t.Errorf("id = %d, want 101", id)
	}
	if scr.calls.update != 1 {
		t.Errorf("expected 1 update call, got %d", scr.calls.update)
	}
	if scr.calls.post != 0 {
		t.Errorf("expected 0 post calls on update path, got %d", scr.calls.post)
	}
	if scr.updateID != 101 {
		t.Errorf("PATCHed wrong comment ID: %d", scr.updateID)
	}
	if !strings.Contains(scr.updateBody, "new body") {
		t.Errorf("update body did not contain new content; got %s", scr.updateBody)
	}
}

func TestPostOrUpdateComment_PostNew(t *testing.T) {
	scr := &scriptedServer{
		t:            t,
		listComments: func(_ string) ([]Comment, int) { return []Comment{}, 0 },
		postID:       999,
	}
	srv := httptest.NewServer(scr.handler())
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	id, created, err := c.PostOrUpdateComment(context.Background(), testRepo, 5, "fresh "+testMarker, testMarker)
	if err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}
	if !created {
		t.Errorf("expected created=true (new comment path)")
	}
	if id != 999 {
		t.Errorf("id = %d, want 999", id)
	}
	if scr.calls.post != 1 {
		t.Errorf("expected 1 post call, got %d", scr.calls.post)
	}
	if scr.calls.update != 0 {
		t.Errorf("expected 0 update calls on post path, got %d", scr.calls.update)
	}
}

func TestPostOrUpdateComment_MultipleMatches(t *testing.T) {
	logs := captureLogs(t)
	existing := []Comment{
		makeComment(10, "old "+testMarker, mustParse(t, "2026-04-01T10:00:00Z")),
		makeComment(20, "newest "+testMarker, mustParse(t, "2026-05-10T10:00:00Z")),
		makeComment(30, "middle "+testMarker, mustParse(t, "2026-04-15T10:00:00Z")),
	}
	scr := &scriptedServer{
		t: t,
		listComments: func(page string) ([]Comment, int) {
			if page == "1" {
				return existing, 0
			}
			return nil, 0
		},
		updateID: 20,
	}
	srv := httptest.NewServer(scr.handler())
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	id, created, err := c.PostOrUpdateComment(context.Background(), testRepo, 5, "newer "+testMarker, testMarker)
	if err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}
	if created {
		t.Errorf("expected update path, got created=true")
	}
	if id != 20 {
		t.Errorf("id = %d, want 20 (most recent updated_at)", id)
	}
	if scr.updateID != 20 {
		t.Errorf("PATCHed comment ID = %d, want 20", scr.updateID)
	}
	if !strings.Contains(logs.String(), "multiple comments match marker") {
		t.Errorf("expected multi-match warn; logs:\n%s", logs.String())
	}
}

func TestPostOrUpdateComment_BodyTooLong(t *testing.T) {
	logs := captureLogs(t)
	scr := &scriptedServer{
		t:            t,
		listComments: func(_ string) ([]Comment, int) { return []Comment{}, 0 },
		postID:       1,
	}
	srv := httptest.NewServer(scr.handler())
	defer srv.Close()

	bigBody := strings.Repeat("x", 70000) + testMarker
	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	if _, _, err := c.PostOrUpdateComment(context.Background(), testRepo, 1, bigBody, testMarker); err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}

	if !strings.Contains(logs.String(), "exceeds size cap") {
		t.Errorf("expected truncation warn; logs:\n%s", logs.String())
	}
	// The server should have observed a body shorter than the original.
	if len(scr.postBody) >= 70000 {
		t.Errorf("post body not truncated: len=%d", len(scr.postBody))
	}
	if !strings.Contains(scr.postBody, "[truncated]") {
		t.Errorf("post body missing truncation suffix; got tail: %q",
			scr.postBody[max(0, len(scr.postBody)-200):])
	}
}

func TestPostOrUpdateComment_PostFails(t *testing.T) {
	scr := &scriptedServer{
		t:            t,
		listComments: func(_ string) ([]Comment, int) { return []Comment{}, 0 },
		postStatus:   http.StatusUnprocessableEntity,
	}
	srv := httptest.NewServer(scr.handler())
	defer srv.Close()

	c := NewClientWithConfig("tok", srv.URL, srv.Client(), "")
	id, created, err := c.PostOrUpdateComment(context.Background(), testRepo, 1, "body "+testMarker, testMarker)
	if err == nil {
		t.Fatal("expected post failure to surface")
	}
	if id != 0 {
		t.Errorf("expected id=0 on failure, got %d", id)
	}
	if created {
		t.Errorf("expected created=false on failure")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error = %v, want validation failed", err)
	}
}

// --- helpers ---

func makeBatch(n, startID int) []Comment {
	out := make([]Comment, n)
	for i := range n {
		out[i] = Comment{ID: int64(startID + i), Body: "x"}
	}
	return out
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(s, &n)
	return n, err
}
