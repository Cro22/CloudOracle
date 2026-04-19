package api

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/db"
	"CloudOracle/internal/shared"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Server struct {
	pool    *db.Pool
	handler http.Handler
}

func NewServer(pool *db.Pool) *Server {
	s := &Server{pool: pool}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/resources", s.handleResources)
	mux.HandleFunc("GET /api/findings", s.handleFindings)
	mux.HandleFunc("GET /api/trends", s.handleTrends)
	mux.HandleFunc("GET /api/summary", s.handleSummary)
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "endpoint not found: "+r.Method+" "+r.URL.Path)
	})
	mux.Handle("GET /", staticHandler())

	s.handler = corsMiddleware(loggingMiddleware(mux))
	return s
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) Start(addr string) error {
	slog.Info("starting API server", "addr", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

type resourcesResponse struct {
	TotalCount       int               `json:"total_count"`
	TotalMonthlyCost float64           `json:"total_monthly_cost"`
	Resources        []shared.Resource `json:"resources"`
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	resources, err := db.ListResources(r.Context(), s.pool)
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
	resources, err := db.ListResources(r.Context(), s.pool)
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

	trends, err := db.ListTrends(r.Context(), s.pool, days)
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
	resources, err := db.ListResources(r.Context(), s.pool)
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
	if p, ok := providerByService[r.Service]; ok {
		return p
	}
	if r.Service == "functions" {
		if len(r.AccountID) == 36 && r.AccountID[8] == '-' && r.AccountID[13] == '-' {
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
