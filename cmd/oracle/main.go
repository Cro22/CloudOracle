package main

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/api"
	"CloudOracle/internal/cloud"
	"CloudOracle/internal/config"
	"CloudOracle/internal/db"
	"CloudOracle/internal/diff"
	"CloudOracle/internal/github"
	"CloudOracle/internal/iac"
	"CloudOracle/internal/llm"
	"CloudOracle/internal/logging"
	"CloudOracle/internal/migrations"
	"CloudOracle/internal/pricing"
	"CloudOracle/internal/report"
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Config first, before logging or anything else: if env vars are wrong
	// we want to surface every problem at once and exit cleanly. slog isn't
	// set up yet, so we go to stderr directly.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	logging.Setup(cfg.LogLevel, cfg.LogFormat)

	ctx := context.Background()

	// pr-check is a stateless plan→markdown transform — no database
	// involved. The GitHub Action that wraps this binary in Hito 16.3
	// runs in environments where Postgres is not available, so we
	// dispatch this subcommand before db.Connect to avoid a spurious
	// connection failure.
	if os.Args[1] == "pr-check" {
		os.Exit(runPRCheck(ctx, cfg, os.Args[2:], os.Stdout, os.Stderr))
	}

	pool, err := db.Connect(ctx, cfg.DB)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("connected to database",
		"host", cfg.DB.Host,
		"database", cfg.DB.Database,
	)

	if err := migrations.Run(ctx, pool); err != nil {
		slog.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "seed":
		runSeed(ctx, pool, cfg, os.Args[2:])
	case "list":
		runList(ctx, pool)
	case "analyze":
		runAnalyze(ctx, pool)
	case "report":
		runReport(ctx, pool, cfg, os.Args[2:])
	case "trend":
		runTrend(ctx, pool, os.Args[2:])
	case "export":
		runExport(ctx, pool, os.Args[2:])
	case "serve":
		runServe(pool, os.Args[2:])
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("CloudOracle - FinOps for AWS - GCP - Azure")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  oracle seed [--count N] [--account ID]   - Fetch resources and insert into database")
	fmt.Println("  oracle list                              - List all resources ordered by monthly cost")
	fmt.Println("  oracle analyze                           - Run cost optimization rules")
	fmt.Println("  oracle report [--output file.pdf]        - Generate PDF report")
	fmt.Println("  oracle trend [--days N]                  - Show cost trends over time")
	fmt.Println("  oracle export --format=json|csv [--output file] - Export findings to JSON or CSV (stdout by default)")
	fmt.Println("  oracle serve [--port 8080]               - Start the HTTP API for the dashboard")
	fmt.Println("  oracle pr-check --plan-file=plan.json [--region=us-east-2] [--output=comment.md] [--no-llm]")
	fmt.Println("                  [--post --repo=owner/name --pr=N [--token=TOK] [--marker=cloudoracle-pr-v1]]")
	fmt.Println("                                           - Render a Terraform plan as a PR-comment Markdown,")
	fmt.Println("                                             optionally posting/updating it on GitHub")
}

func runSeed(ctx context.Context, pool *db.Pool, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	count := fs.Int("count", cfg.Cloud.SyntheticCount, "Number of resources to generate")
	account := fs.String("account", cfg.Cloud.SyntheticAcct, "Account ID to associate with generated resources")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	provider, err := cloud.NewProvider(ctx, cfg)
	if err != nil {
		slog.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	if provider.Name() == "synthetic" {
		provider = cloud.NewSyntheticProvider(*count, *account)
	} else {
		slog.Info("using real provider — synthetic flags are ignored",
			"provider", provider.Name(),
		)
	}

	slog.Info("fetching resources", "provider", provider.Name())
	resources, err := provider.FetchResources(ctx)
	if err != nil {
		slog.Error("failed to fetch resources",
			"provider", provider.Name(),
			"error", err,
		)
		os.Exit(1)
	}
	if err := db.InsertResources(ctx, pool, resources); err != nil {
		slog.Error("failed to insert resources", "error", err)
		os.Exit(1)
	}
	slog.Info("inserted resources",
		"count", len(resources),
		"provider", provider.Name(),
	)

	if err := db.CreateSnapshot(ctx, pool, resources); err != nil {
		slog.Warn("failed to create cost snapshot, continuing", "error", err)
	} else {
		slog.Info("cost snapshot recorded")
	}
}

func runList(ctx context.Context, pool *db.Pool) {
	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		slog.Error("failed to list resources", "error", err)
		os.Exit(1)
	}
	fmt.Printf("%-20s %-8s %-20s %-12s %12s %10s\n",
		"ID", "SERVICE", "TYPE", "REGION", "COST/MONTH", "USAGE")
	fmt.Println("-------------------------------------------------------------------------------------")
	var total float64
	for _, r := range resources {
		fmt.Printf("%-20s %-8s %-20s %-12s $%11.2f %9.2f\n",
			r.ID, r.Service, r.ResourceType, r.Region, r.MonthlyCost, r.UsageMetric)
		total += r.MonthlyCost
	}

	fmt.Println("-------------------------------------------------------------------------------------")
	fmt.Printf("Total: %d resources | Monthly cost: $%.2f\n", len(resources), total)
}

func runAnalyze(ctx context.Context, pool *db.Pool) {
	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		slog.Error("failed to list resources", "error", err)
		os.Exit(1)
	}
	if len(resources) == 0 {
		slog.Info("no resources to analyze")
		return
	}
	findings := analyzer.Analyze(resources)

	if len(findings) == 0 {
		slog.Info("no findings to report, all looks good")
		return
	}
	var totalWaste float64
	for _, f := range findings {
		totalWaste += f.MonthlySavings
	}

	fmt.Printf("🔍 CloudOracle found %d problems with potential monthly savings of $%.2f\n", len(findings), totalWaste)

	for i, f := range findings {
		severity := colorSeverity(f.Severity)
		fmt.Printf("  %d. [%s] %s\n", i+1, severity, f.Description)
		fmt.Printf("     💡 %s\n", f.Recommendation)
		fmt.Printf("     💰 Monthly Cost: $%.2f | Potential Monthly Savings: $%.2f\n", f.MonthlyCost, f.MonthlySavings)
	}
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("Summary per service")
	fmt.Println()
	printSummaryByService(findings)
}
func colorSeverity(s shared.Severity) string {
	switch s {
	case shared.SeverityHigh:
		return "🔴 HIGH"
	case shared.SeverityMedium:
		return "🟡 MEDIUM"
	case shared.SeverityLow:
		return "🟢 LOW"
	default:
		return string(s)
	}
}
func printSummaryByService(findings []shared.Finding) {
	summary := make(map[string]struct {
		count   int
		savings float64
	})
	for _, f := range findings {
		s := summary[f.Service]
		s.count++
		s.savings += f.MonthlySavings
		summary[f.Service] = s
	}

	for service, s := range summary {
		fmt.Printf("  %-8s → %d problems, save: $%.2f/month\n", service, s.count, s.savings)
	}
	fmt.Println()
}

func runReport(ctx context.Context, pool *db.Pool, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	output := fs.String("output", "cloudoracle-report.pdf", "output PDF file path")
	fs.Parse(args)

	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		slog.Error("failed to load resources", "error", err)
		os.Exit(1)
	}

	if len(resources) == 0 {
		fmt.Println("No resources in the database. Run 'oracle seed' first.")
		return
	}

	findings := analyzer.Analyze(resources)

	if len(findings) == 0 {
		fmt.Println("✓ No waste detected. Nothing to report.")
		return
	}

	var aiSummary string
	provider, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		if errors.Is(err, llm.ErrNoProvider) {
			slog.Info("no LLM provider configured, skipping AI summary")
		} else {
			slog.Warn("LLM provider error, continuing without AI summary", "error", err)
		}
	} else {
		slog.Info("generating AI summary", "provider", provider.Name())
		aiSummary, err = provider.GenerateSummary(ctx, findings)
		if err != nil {
			slog.Warn("failed to generate AI summary", "error", err)
			aiSummary = ""
		}
	}

	slog.Info("generating PDF", "findings", len(findings))

	if err := report.GeneratePDF(findings, aiSummary, *output); err != nil {
		slog.Error("failed to generate PDF", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Report generated: %s\n", *output)
}

func runTrend(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("trend", flag.ExitOnError)
	days := fs.Int("days", 90, "Number of days to look back")
	fs.Parse(args)

	snapshots, err := db.ListSnapshots(ctx, pool, *days)
	if err != nil {
		slog.Error("failed to list snapshots", "error", err)
		os.Exit(1)
	}

	if len(snapshots) == 0 {
		fmt.Println("No snapshots found. Run 'oracle seed' to create cost snapshots.")
		return
	}

	type trend struct {
		OldestCost float64
		LatestCost float64
		OldestDate string
		LatestDate string
	}

	trends := make(map[string]*trend)

	for _, s := range snapshots {
		date := s.TakenAt.Format("2006-01-02")
		t, exists := trends[s.Service]
		if !exists {
			t = &trend{
				OldestCost: s.TotalMonthlyCost,
				LatestCost: s.TotalMonthlyCost,
				OldestDate: date,
				LatestDate: date,
			}
			trends[s.Service] = t
			continue
		}
		if date < t.OldestDate {
			t.OldestCost = s.TotalMonthlyCost
			t.OldestDate = date
		}
		if date > t.LatestDate {
			t.LatestCost = s.TotalMonthlyCost
			t.LatestDate = date
		}
	}

	services := make([]string, 0, len(trends))
	for svc := range trends {
		services = append(services, svc)
	}
	sort.Strings(services)

	snapshotCount := countUniqueSnapshots(snapshots)
	fmt.Printf("Cost Trends (last %d days, %d snapshots)\n\n", *days, snapshotCount)
	fmt.Printf("%-12s %12s %12s %14s\n", "Service", "Oldest", "Latest", "Change")
	fmt.Println(strings.Repeat("─", 56))

	var totalOldest, totalLatest float64
	for _, svc := range services {
		t := trends[svc]
		printTrendLine(svc, t.OldestCost, t.LatestCost)
		totalOldest += t.OldestCost
		totalLatest += t.LatestCost
	}

	fmt.Println(strings.Repeat("─", 56))
	printTrendLine("Total", totalOldest, totalLatest)
}

func printTrendLine(label string, oldest, latest float64) {
	change := latest - oldest
	arrow := "→"
	if change > 0.005 {
		arrow = "↑"
	}
	if change < -0.005 {
		arrow = "↓"
	}

	pct := 0.0
	if oldest > 0 {
		pct = (change / oldest) * 100
	}

	fmt.Printf("%-12s $%10.2f $%10.2f  %+7.2f (%+.1f%%) %s\n",
		label, oldest, latest, change, pct, arrow)
}

func countUniqueSnapshots(snapshots []db.Snapshot) int {
	seen := make(map[string]bool)
	for _, s := range snapshots {
		seen[s.TakenAt.Format("2006-01-02 15:04")] = true
	}
	return len(seen)
}

func runExport(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	format := fs.String("format", "json", "Output format: json or csv")
	output := fs.String("output", "", "Output file path (default: stdout)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		slog.Error("failed to load resources", "error", err)
		os.Exit(1)
	}

	findings := analyzer.Analyze(resources)

	writer := io.Writer(os.Stdout)
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			slog.Error("failed to create output file", "path", *output, "error", err)
			os.Exit(1)
		}
		defer f.Close()
		writer = f
	}

	switch strings.ToLower(*format) {
	case "json":
		err = report.ExportJSON(writer, findings)
	case "csv":
		err = report.ExportCSV(writer, findings)
	default:
		slog.Error("unknown format (use 'json' or 'csv')", "format", *format)
		os.Exit(1)
	}

	if err != nil {
		slog.Error("failed to export findings", "format", *format, "error", err)
		os.Exit(1)
	}

	if *output != "" {
		slog.Info("exported findings",
			"count", len(findings),
			"format", *format,
			"path", *output,
		)
	}
}

// pr-check exit codes. Differentiated so the GitHub Action wrapper in
// Hito 16.3 can distinguish "the developer's plan is broken" (1) from
// "our pricing dependency failed" (2) from "we can't write the output"
// (3) — different remediations for each. The other oracle subcommands
// use exit 1 uniformly; pr-check is the first to be CI-targeted.
const (
	exitPRCheckOK         = 0
	exitPRCheckInputErr   = 1
	exitPRCheckPricingErr = 2
	exitPRCheckOutputErr  = 3
	exitPRCheckGitHubErr  = 4
)

// defaultMarker matches the HTML marker that diff.RenderMarkdown emits at
// the bottom of every CloudOracle PR comment. The --marker flag exists so
// users can override (e.g. for a future v2 marker without breaking
// existing v1 comments) but the default is the canonical one.
const defaultMarker = "cloudoracle-pr-v1"

// githubPoster is the subset of *github.Client behaviour runPRCheck
// actually exercises. Defining it here (rather than exporting one from
// internal/github) keeps the test fake's surface tiny: a single method
// instead of the whole Client.
type githubPoster interface {
	PostOrUpdateComment(ctx context.Context, repo github.Repo, prNumber int, body, marker string) (int64, bool, error)
}

// newPRCheckGithubClient is the factory used by runPRCheck to obtain a
// githubPoster. Wrapped as a package-level var so tests can swap it for
// a recording fake. Production wraps github.NewClient — *github.Client
// already satisfies githubPoster by structural typing.
var newPRCheckGithubClient = func(token string) githubPoster {
	return github.NewClient(token)
}

// newPRCheckSource builds the pricing.Source used by `pr-check`.
// Wrapped as a package-level var so tests can swap it for a fake
// without spinning up the AWS SDK or hitting the network. Production
// builds get a 7-day disk cache wrapping a real AWS Pricing client;
// the cache is best-effort — a directory-creation failure logs WARN
// and falls back to the uncached client rather than aborting.
var newPRCheckSource = func(ctx context.Context) (diff.Source, error) {
	client, err := pricing.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	dir, dirErr := pricing.DefaultCacheDir()
	if dirErr != nil || dir == "" {
		slog.Warn("pricing cache disabled, using direct client", "error", dirErr)
		return client, nil
	}
	cache, err := pricing.NewCache(client, dir, 7*24*time.Hour)
	if err != nil {
		slog.Warn("pricing cache disabled, using direct client", "error", err)
		return client, nil
	}
	return cache, nil
}

// runPRCheck is the orchestrator for the `pr-check` subcommand. It
// returns the process exit code instead of calling os.Exit directly so
// it is testable in-process; main() does the os.Exit at the dispatch
// site. stdout receives the rendered Markdown when --output is empty
// or "-"; stderr receives flag-parse and error messages.
func runPRCheck(ctx context.Context, cfg config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planFile := fs.String("plan-file", "", "path to `terraform show -json` output (required)")
	region := fs.String("region", "us-east-2", "AWS region for pricing lookups")
	output := fs.String("output", "", "file to write the Markdown to; empty or \"-\" means stdout")
	noLLM := fs.Bool("no-llm", false, "force the templated narrative even if an LLM provider is configured")
	post := fs.Bool("post", false, "post the rendered Markdown as a PR comment (upserts via marker)")
	repoFlag := fs.String("repo", "", "target repo in `owner/name` form (required when --post is set)")
	prNumber := fs.Int("pr", 0, "target PR number (required when --post is set)")
	tokenFlag := fs.String("token", "", "GitHub token; falls back to the GITHUB_TOKEN env var when empty")
	marker := fs.String("marker", defaultMarker, "HTML marker substring used to find the existing CloudOracle comment for upsert")

	if err := fs.Parse(args); err != nil {
		// flag.Parse already wrote a usage message to stderr. -h / --help
		// is a deliberate user action, not a malformed invocation, so it
		// exits 0; everything else is a flag-parse failure (exit 1).
		if errors.Is(err, flag.ErrHelp) {
			return exitPRCheckOK
		}
		return exitPRCheckInputErr
	}

	if *planFile == "" {
		fmt.Fprintln(stderr, "oracle pr-check: --plan-file is required")
		return exitPRCheckInputErr
	}

	plan, err := iac.ParsePlanFile(*planFile)
	if err != nil {
		fmt.Fprintf(stderr, "oracle pr-check: --plan-file %q: %v\n", *planFile, err)
		return exitPRCheckInputErr
	}

	source, err := newPRCheckSource(ctx)
	if err != nil {
		slog.Error("pr-check: pricing source unavailable", "error", err)
		return exitPRCheckPricingErr
	}

	costDiff, err := diff.Analyze(ctx, source, plan, *region)
	if err != nil {
		slog.Error("pr-check: diff analysis failed", "region", *region, "error", err)
		return exitPRCheckPricingErr
	}

	md := renderPRCheckMarkdown(ctx, cfg, costDiff, *noLLM)

	// Output first (stdout when unset or "-"; file otherwise). --output
	// and --post are independent: a CI run that sets both gets the file
	// for artefact upload AND the comment posted.
	if *output == "" || *output == "-" {
		if _, err := io.WriteString(stdout, md); err != nil {
			slog.Error("pr-check: writing to stdout failed", "error", err)
			return exitPRCheckOutputErr
		}
	} else {
		if err := os.WriteFile(*output, []byte(md), 0o644); err != nil {
			fmt.Fprintf(stderr, "oracle pr-check: --output %q: %v\n", *output, err)
			return exitPRCheckOutputErr
		}
		slog.Info("pr-check: wrote markdown", "path", *output, "bytes", len(md))
	}

	if !*post {
		return exitPRCheckOK
	}
	return runPRCheckPost(ctx, *repoFlag, *prNumber, *tokenFlag, md, *marker, stderr)
}

// runPRCheckPost validates the post-related flags and dispatches to the
// configured githubPoster. Split from runPRCheck so the post path's
// flag-validation, token resolution, and error mapping have one home —
// the orchestrator stays linear and the unit tests can target each
// failure mode without rebuilding the analyze/render plumbing.
func runPRCheckPost(ctx context.Context, repoFlag string, prNumber int, tokenFlag, body, marker string, stderr io.Writer) int {
	if prNumber <= 0 {
		fmt.Fprintln(stderr, "oracle pr-check: --pr must be a positive integer when --post is set")
		return exitPRCheckInputErr
	}
	repo, err := parseRepo(repoFlag)
	if err != nil {
		fmt.Fprintf(stderr, "oracle pr-check: %v\n", err)
		return exitPRCheckInputErr
	}
	token := tokenFlag
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		fmt.Fprintln(stderr, "oracle pr-check: --token or GITHUB_TOKEN env required when --post is set")
		return exitPRCheckInputErr
	}

	client := newPRCheckGithubClient(token)
	id, created, err := client.PostOrUpdateComment(ctx, repo, prNumber, body, marker)
	if err != nil {
		return classifyGithubError(err, stderr)
	}

	action := "updated"
	if created {
		action = "created"
	}
	slog.Info("pr-check: github comment "+action,
		"comment_id", id,
		"repo", repoFlag,
		"pr", prNumber,
		"marker", marker)
	return exitPRCheckOK
}

// parseRepo accepts the "owner/name" form used by GitHub URLs and
// returns a github.Repo. Both halves must be non-empty; we use SplitN
// with n=2 so a name containing a slash (which GitHub forbids anyway)
// would be caught by the empty-half check rather than silently keeping
// the trailing portion.
func parseRepo(s string) (github.Repo, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return github.Repo{}, fmt.Errorf("--repo must be in 'owner/name' format, got %q", s)
	}
	return github.Repo{Owner: parts[0], Name: parts[1]}, nil
}

// classifyGithubError maps an error string from internal/github to a
// user-facing stderr line and the exit-4 code. Matching is by stable
// substring rather than wrapped sentinel errors because internal/github
// uses fmt.Errorf with prefix conventions (documented at the
// PostOrUpdateComment godoc); converting to typed errors would couple
// the two packages tighter than necessary.
func classifyGithubError(err error, stderr io.Writer) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "authentication failed"):
		fmt.Fprintln(stderr, "oracle pr-check: github post failed: authentication; check token permissions")
	case strings.Contains(msg, "not found"):
		fmt.Fprintln(stderr, "oracle pr-check: github post failed: repo or PR not found")
	case strings.Contains(msg, "validation failed"):
		fmt.Fprintf(stderr, "oracle pr-check: github post failed: validation: %s\n",
			strings.TrimPrefix(msg, "github: validation failed: "))
	default:
		fmt.Fprintf(stderr, "oracle pr-check: github post failed: %s\n", msg)
	}
	return exitPRCheckGitHubErr
}

// renderPRCheckMarkdown picks between LLM-narrated and templated render.
// Splitting it from runPRCheck keeps the orchestrator's branching
// cyclomatic-low and makes the LLM-fallback path easy to reason about
// in isolation: LLM disabled (--no-llm), no provider keys configured,
// or provider construction error all converge on the same templated
// path.
func renderPRCheckMarkdown(ctx context.Context, cfg config.Config, d diff.CostDiff, noLLM bool) string {
	if noLLM {
		return diff.RenderMarkdown(d)
	}
	provider, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		if errors.Is(err, llm.ErrNoProvider) {
			slog.Info("pr-check: no LLM provider configured, using templated narrative")
		} else {
			slog.Warn("pr-check: LLM provider error, using templated narrative", "error", err)
		}
		return diff.RenderMarkdown(d)
	}
	slog.Info("pr-check: rendering with LLM narrative", "provider", provider.Name())
	return diff.RenderMarkdownWithLLM(ctx, d, provider)
}

func runServe(pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.String("port", "8080", "Port to listen on")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	server := api.NewServer(pool)
	slog.Info("Dashboard available", "url", fmt.Sprintf("http://localhost:%s", *port))
	if err := server.Start(":" + *port); err != nil {
		slog.Error("API server failed", "error", err)
		os.Exit(1)
	}
}
