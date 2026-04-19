package main

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/cloud"
	"CloudOracle/internal/db"
	"CloudOracle/internal/llm"
	"CloudOracle/internal/report"
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	ctx := context.Background()
	cfg := db.LoadConfigFromEnv()
	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()
	log.Println("✓ Connected to database!")
	switch os.Args[1] {
	case "seed":
		runSeed(ctx, pool, os.Args[2:])
	case "list":
		runList(ctx, pool)
	case "analyze":
		runAnalyze(ctx, pool)
	case "report":
		runReport(ctx, pool, os.Args[2:])
	case "trend":
		runTrend(ctx, pool, os.Args[2:])
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
}

func runSeed(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	count := fs.Int("count", 100, "Number of resources to generate")
	account := fs.String("account", "acc-001", "Account ID to associate with generated resources")
	err := fs.Parse(args)
	if err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	provider, err := cloud.NewProvider(ctx)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}

	if provider.Name() == "synthetic" {
		provider = cloud.NewSyntheticProvider(*count, *account)
	} else {
		log.Printf("Using %s provider — flags --count and --account are ignored (data comes from the real account)", provider.Name())
	}

	log.Printf("Fetching resources from provider: %s", provider.Name())
	resources, err := provider.FetchResources(ctx)
	if err != nil {
		log.Fatalf("Failed to fetch resources from %s provider: %v", provider.Name(), err)
	}
	if err := db.InsertResources(ctx, pool, resources); err != nil {
		log.Fatalf("Failed to insert resources: %v", err)
	}
	log.Printf("Inserted %d resources from %s provider", len(resources), provider.Name())

	if err := db.CreateSnapshot(ctx, pool, resources); err != nil {
		log.Printf("⚠ Failed to create cost snapshot: %v (continuing)", err)
	} else {
		log.Println("✓ Cost snapshot recorded")
	}
}

func runList(ctx context.Context, pool *db.Pool) {
	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		log.Fatalf("Failed to list resources: %v", err)
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
		log.Fatalf("Failed to list resources: %v", err)
	}
	if len(resources) == 0 {
		log.Println("No resources to analyze")
		return
	}
	findings := analyzer.Analyze(resources)

	if len(findings) == 0 {
		log.Println("✓ No findings to report. All looks good")
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

func runReport(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	output := fs.String("output", "cloudoracle-report.pdf", "output PDF file path")
	fs.Parse(args)

	resources, err := db.ListResources(ctx, pool)
	if err != nil {
		log.Fatalf("error loading resources: %v", err)
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
	provider, err := llm.NewProvider()
	if err != nil {
		if errors.Is(err, llm.ErrNoProvider) {
			log.Println("ℹ No LLM provider configured, skipping AI summary")
		} else {
			log.Printf("⚠ LLM provider error: %v (continuing without AI summary)", err)
		}
	} else {
		log.Printf("Generating AI summary using %s...", provider.Name())
		aiSummary, err = provider.GenerateSummary(ctx, findings)
		if err != nil {
			log.Printf("⚠ Failed to generate AI summary: %v", err)
			aiSummary = ""
		}
	}

	log.Printf("Generating PDF with %d findings...", len(findings))

	if err := report.GeneratePDF(findings, aiSummary, *output); err != nil {
		log.Fatalf("error generating PDF: %v", err)
	}

	fmt.Printf("✓ Report generated: %s\n", *output)
}

func runTrend(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("trend", flag.ExitOnError)
	days := fs.Int("days", 90, "Number of days to look back")
	fs.Parse(args)

	snapshots, err := db.ListSnapshots(ctx, pool, *days)
	if err != nil {
		log.Fatalf("Failed to list snapshots: %v", err)
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
