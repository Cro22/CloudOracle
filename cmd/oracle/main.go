package main

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/api"
	"CloudOracle/internal/cloud"
	"CloudOracle/internal/config"
	"CloudOracle/internal/db"
	"CloudOracle/internal/llm"
	"CloudOracle/internal/logging"
	"CloudOracle/internal/migrations"
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
