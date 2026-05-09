package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("token-abc")
	if c.token != "token-abc" {
		t.Errorf("token not stored: got %q", c.token)
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.userAgent != defaultUserAgent {
		t.Errorf("userAgent = %q, want %q", c.userAgent, defaultUserAgent)
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v", c.httpClient.Timeout, defaultTimeout)
	}
}

func TestNewClientWithConfig_OverridesAndFallbacks(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	c := NewClientWithConfig("tok", "https://ghe.example.com", custom, "Test/1.0")
	if c.baseURL != "https://ghe.example.com" {
		t.Errorf("baseURL not overridden")
	}
	if c.userAgent != "Test/1.0" {
		t.Errorf("userAgent not overridden")
	}
	if c.httpClient != custom {
		t.Errorf("httpClient not overridden")
	}

	// Empty / nil arguments fall back to defaults.
	def := NewClientWithConfig("tok", "", nil, "")
	if def.baseURL != defaultBaseURL {
		t.Errorf("empty baseURL did not fall back to default")
	}
	if def.userAgent != defaultUserAgent {
		t.Errorf("empty userAgent did not fall back to default")
	}
	if def.httpClient == nil {
		t.Errorf("nil httpClient did not fall back to default")
	}
}

// TestClient_AuthHeaderSet confirms every outbound request from this
// package includes the GitHub-required header bundle. We exercise the
// listComments path since it's the simplest GET; the other verbs share
// the same setHeaders helper, so a regression there would surface
// here too.
func TestClient_AuthHeaderSet(t *testing.T) {
	var (
		gotAuth    string
		gotAccept  string
		gotVersion string
		gotUA      string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode([]Comment{})
	}))
	defer srv.Close()

	c := NewClientWithConfig("the-token", srv.URL, srv.Client(), "")
	if _, err := c.listComments(context.Background(), Repo{Owner: "o", Name: "r"}, 1); err != nil {
		t.Fatalf("listComments: %v", err)
	}

	if gotAuth != "Bearer the-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer the-token")
	}
	if gotAccept != acceptHeader {
		t.Errorf("Accept = %q, want %q", gotAccept, acceptHeader)
	}
	if gotVersion != apiVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", gotVersion, apiVersion)
	}
	if gotUA != defaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, defaultUserAgent)
	}
}

// TestClient_AuthHeaderOmittedWhenTokenEmpty: an empty token must NOT
// produce a "Bearer " header (auth header absent), so a misconfigured
// caller fails with a clean GitHub 401 rather than an oddly-formed
// header that some servers reject earlier.
func TestClient_AuthHeaderOmittedWhenTokenEmpty(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClientWithConfig("", srv.URL, srv.Client(), "")
	_, _ = c.listComments(context.Background(), Repo{Owner: "o", Name: "r"}, 1)
	if hadAuth {
		t.Error("Authorization header set despite empty token")
	}
}
