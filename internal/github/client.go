package github

import (
	"net/http"
	"time"
)

const (
	defaultBaseURL   = "https://api.github.com"
	defaultUserAgent = "CloudOracle/v2"
	defaultTimeout   = 30 * time.Second

	// apiVersion is the GitHub REST API version pinned via the
	// X-GitHub-Api-Version header. GitHub recommends setting this
	// explicitly so behaviour does not change under us when they ship
	// a new API version.
	apiVersion = "2022-11-28"

	// acceptHeader is the "modern" media type GitHub asks REST clients
	// to send. application/vnd.github+json picks the latest stable
	// representation for whatever endpoint we hit.
	acceptHeader = "application/vnd.github+json"
)

// Client is a thin GitHub REST API client scoped to the operations
// CloudOracle needs (PR comment list / post / update). It is not a
// general-purpose SDK and intentionally does not retry, throttle, or
// cache — those belong to the caller (the Hito 16.3 Action wrapper).
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	userAgent  string
}

// NewClient creates a Client with the production defaults: GitHub's
// public API host, a 30s HTTP timeout, and the "CloudOracle/v2"
// User-Agent. token must be a personal access token or a workflow
// GITHUB_TOKEN with the correct PR-write scope; passing an empty
// string is allowed (calls will fail with 401), so the caller can
// surface auth errors uniformly with a real-but-bad token.
func NewClient(token string) *Client {
	return &Client{
		token:      token,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  defaultUserAgent,
	}
}

// NewClientWithConfig is the explicit constructor used by tests
// (httptest.NewServer URL via baseURL) and by callers targeting GitHub
// Enterprise Server. Empty strings on baseURL or userAgent fall back
// to the package defaults; httpClient may be nil to use the default
// 30s-timeout client.
func NewClientWithConfig(token, baseURL string, httpClient *http.Client, userAgent string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		token:      token,
		baseURL:    baseURL,
		httpClient: httpClient,
		userAgent:  userAgent,
	}
}

// setHeaders applies the standard set of GitHub REST headers (auth,
// accept, version, user-agent) to a request built by this package.
// Centralised so a single point owns the auth contract.
func (c *Client) setHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", acceptHeader)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
}
