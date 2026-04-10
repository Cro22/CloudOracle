package main

import (
	"CloudOracle/internal/db"
	"CloudOracle/internal/generator"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
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
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("CloudOracle - FindOps for AWS - GCP - Azure")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  oracle seed <account_id> <num_resources>  - Generate and insert random resources for an account")
	fmt.Println("  oracle list                              - List all resources ordered by monthly cost")
}

func runSeed(ctx context.Context, pool *db.Pool, args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	count := fs.Int("count", 100, "Number of resources to generate")
	account := fs.String("account", "acc-001", "Account ID to associate with generated resources")
	err := fs.Parse(args)
	if err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}
	log.Println("Generating resources...")
	resources := generator.GenerateResources(*count, *account)
	if err := db.InsertResources(ctx, pool, resources); err != nil {
		log.Fatalf("Failed to insert resources: %v", err)
	}
	log.Printf("Generated %d resources for account %s", len(resources), *account)
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
	fmt.Printf("Total: %d recursos | Costo mensual: $%.2f\n", len(resources), total)
}
