package api

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/config"
	"CloudOracle/internal/db"
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Server struct {
	data    apiData
	apiKey  string
	handler http.Handler
}

// NewServer wires the production handler: legacy `/api/*` dashboard
// endpoints stay open (they're consumed by the embedded React UI), and
// the new `/api/v1/*` endpoints sit behind authMiddleware so only the
// insights-agent — or any client that holds the configured API key —
// can reach them.
func NewServer(pool *db.Pool, apiCfg config.APIConfig) *Server {
	return newServerWithData(&pgxAdapter{pool: pool}, apiCfg.Key)
}

// newTestServer builds a Server with a caller-supplied apiData so unit tests
// can exercise the handlers without a live database. Production must go
// through NewServer.
func newTestServer(data apiData, apiKey string) *Server {
	return newServerWithData(data, apiKey)
}

func newServerWithData(data apiData, apiKey string) *Server {
	s := &Server{data: data, apiKey: apiKey}
	s.handler = s.buildHandler()
	return s
}

func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// v0 dashboard endpoints — left unauthenticated to match the existing
	// React UI which is served from the same binary and assumes local trust.
	mux.HandleFunc("GET /api/resources", s.handleResources)
	mux.HandleFunc("GET /api/findings", s.handleFindings)
	mux.HandleFunc("GET /api/trends", s.handleTrends)
	mux.HandleFunc("GET /api/summary", s.handleSummary)

	// v1 endpoints for the insights-agent — gated by X-API-Key so the
	// surface area an agent can reach is explicitly auth'd. Sit on
	// /api/v1/* so the dashboard endpoints can evolve independently.
	authed := authMiddleware(s.apiKey)
	mux.Handle("GET /api/v1/cost-summary",
		authed(http.HandlerFunc(s.handleCostSummary)))
	mux.Handle("GET /api/v1/cost-by-service",
		authed(http.HandlerFunc(s.handleCostByService)))
	mux.Handle("GET /api/v1/recommendations",
		authed(http.HandlerFunc(s.handleRecommendations)))
	mux.Handle("GET /api/v1/cost-trends",
		authed(http.HandlerFunc(s.handleCostTrends)))

	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "endpoint not found: "+r.Method+" "+r.URL.Path)
	})
	mux.Handle("GET /", staticHandler())

	return corsMiddleware(requestIDMiddleware(loggingMiddleware(mux)))
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run starts the HTTP server on addr and blocks until ctx is cancelled.
// On cancellation it triggers http.Server.Shutdown with the configured
// timeout. Returns nil on a clean shutdown, the listener error otherwise.
//
// We pattern-match the canonical Go shutdown idiom (goroutine + select on
// errCh / ctx.Done) so SIGINT/SIGTERM forwarded by the parent context land
// in Shutdown rather than abruptly killing in-flight requests — the agent
// flow in insights-agent/ can take a few seconds on a slow LLM response.
func (s *Server) Run(ctx context.Context, addr string, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting API server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down API server", "timeout", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

type resourcesResponse struct {
	TotalCount       int               `json:"total_count"`
	TotalMonthlyCost float64           `json:"total_monthly_cost"`
	Resources        []shared.Resource `json:"resources"`
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	resources, err := s.data.ListResources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list resources: "+err.Error())
		return
	}

	var total float64
	for _, res := range resources {
		total += res.MonthlyCost
	}

	writeJSON(w, http.StatusOK, resourcesResponse{
		TotalCount:       len(resources),
		TotalMonthlyCost: total,
		Resources:        resources,
	})
}

type findingsResponse struct {
	TotalCount            int              `json:"total_count"`
	TotalPotentialSavings float64          `json:"total_potential_savings"`
	Page                  int              `json:"page"`
	PageSize              int              `json:"page_size"`
	TotalPages            int              `json:"total_pages"`
	Sort                  string           `json:"sort"`
	Order                 string           `json:"order"`
	Findings              []shared.Finding `json:"findings"`
}

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	resources, err := s.data.ListResources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list resources: "+err.Error())
		return
	}

	findings := analyzer.Analyze(resources)

	var savings float64
	for _, f := range findings {
		savings += f.MonthlySavings
	}

	q := r.URL.Query()
	sortCol := normalizeSortColumn(q.Get("sort"))
	order := normalizeOrder(q.Get("order"))
	if sortCol != "" {
		sortFindings(findings, sortCol, order)
	}

	page := clampInt(parseIntOr(q.Get("page"), 1), 1, 1<<30)
	pageSize := clampInt(parseIntOr(q.Get("page_size"), defaultPageSize), 1, maxPageSize)

	total := len(findings)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	page_findings := findings[start:end]
	if page_findings == nil {
		page_findings = []shared.Finding{}
	}

	writeJSON(w, http.StatusOK, findingsResponse{
		TotalCount:            total,
		TotalPotentialSavings: savings,
		Page:                  page,
		PageSize:              pageSize,
		TotalPages:            totalPages,
		Sort:                  sortCol,
		Order:                 order,
		Findings:              page_findings,
	})
}

var severityRank = map[shared.Severity]int{
	shared.SeverityLow:    1,
	shared.SeverityMedium: 2,
	shared.SeverityHigh:   3,
}

func normalizeSortColumn(s string) string {
	switch strings.ToLower(s) {
	case "severity", "service", "cost", "savings":
		return strings.ToLower(s)
	default:
		return ""
	}
}

func normalizeOrder(s string) string {
	if strings.EqualFold(s, "asc") {
		return "asc"
	}
	return "desc"
}

func sortFindings(findings []shared.Finding, col, order string) {
	desc := order != "asc"
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		var cmp int
		switch col {
		case "severity":
			cmp = severityRank[a.Severity] - severityRank[b.Severity]
		case "service":
			cmp = strings.Compare(a.Service, b.Service)
		case "cost":
			cmp = cmpFloat(a.MonthlyCost, b.MonthlyCost)
		case "savings":
			cmp = cmpFloat(a.MonthlySavings, b.MonthlySavings)
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func parseIntOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := parsePositiveInt(s)
	if err != nil {
		return def
	}
	return n
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (s *Server) handleTrends(w http.ResponseWriter, r *http.Request) {
	days := 90
	if q := r.URL.Query().Get("days"); q != "" {
		if parsed, err := parsePositiveInt(q); err == nil {
			days = parsed
		}
	}

	trends, err := s.data.ListTrends(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load trends: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, trends)
}

type serviceAgg struct {
	Count   int     `json:"count"`
	Cost    float64 `json:"cost"`
	Savings float64 `json:"savings"`
}

type providerAgg struct {
	Count int     `json:"count"`
	Cost  float64 `json:"cost"`
}

type summaryResponse struct {
	TotalResources        int                    `json:"total_resources"`
	TotalMonthlyCost      float64                `json:"total_monthly_cost"`
	TotalPotentialSavings float64                `json:"total_potential_savings"`
	FindingsCount         int                    `json:"findings_count"`
	ByService             map[string]serviceAgg  `json:"by_service"`
	BySeverity            map[string]int         `json:"by_severity"`
	ByProvider            map[string]providerAgg `json:"by_provider"`
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	resources, err := s.data.ListResources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list resources: "+err.Error())
		return
	}

	findings := analyzer.Analyze(resources)

	summary := summaryResponse{
		TotalResources: len(resources),
		FindingsCount:  len(findings),
		ByService:      make(map[string]serviceAgg),
		BySeverity:     make(map[string]int),
		ByProvider:     make(map[string]providerAgg),
	}

	for _, res := range resources {
		summary.TotalMonthlyCost += res.MonthlyCost

		svc := summary.ByService[res.Service]
		svc.Count++
		svc.Cost += res.MonthlyCost
		summary.ByService[res.Service] = svc

		provider := providerFromResource(res)
		pAgg := summary.ByProvider[provider]
		pAgg.Count++
		pAgg.Cost += res.MonthlyCost
		summary.ByProvider[provider] = pAgg
	}

	for _, f := range findings {
		summary.TotalPotentialSavings += f.MonthlySavings

		svc := summary.ByService[f.Service]
		svc.Savings += f.MonthlySavings
		summary.ByService[f.Service] = svc

		summary.BySeverity[string(f.Severity)]++
	}

	writeJSON(w, http.StatusOK, summary)
}

var providerByService = map[string]string{
	"ec2":             "aws",
	"rds":             "aws",
	"ebs":             "aws",
	"lambda":          "aws",
	"compute":         "gcp",
	"cloudsql":        "gcp",
	"persistent-disk": "gcp",
	"vm":              "azure",
	"sql":             "azure",
	"managed-disk":    "azure",
}

func providerFromResource(r shared.Resource) string {
	return providerForServiceAccount(r.Service, r.AccountID)
}

// providerForServiceAccount is the shared service-to-provider mapping used
// by both the v0 summary handler and the v1 cost endpoints (which only
// have AccountID + Service available on snapshots). The "functions" tie
// is broken by checking the AccountID shape — Azure subscription IDs are
// 36-char UUIDs, GCP project IDs aren't — same heuristic the summary
// handler has used since the dashboard shipped.
func providerForServiceAccount(service, accountID string) string {
	if p, ok := providerByService[service]; ok {
		return p
	}
	if service == "functions" {
		if len(accountID) == 36 && accountID[8] == '-' && accountID[13] == '-' {
			return "azure"
		}
		return "gcp"
	}
	return "other"
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, errInvalidInt
	}
	return n, nil
}
