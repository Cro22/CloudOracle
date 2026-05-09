package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/pricing"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: checkpoint135 <plan.json>")
	}

	ctx := context.Background()

	plan, err := iac.ParsePlanFile(os.Args[1])
	if err != nil {
		log.Fatalf("parse: %v", err)
	}

	client, err := pricing.NewClient(ctx)
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}

	fmt.Printf("Plan parsed: %d resource changes\n\n", len(plan.ResourceChanges))

	estimates := make([]pricing.ChangeEstimate, 0, len(plan.ResourceChanges))
	var totalDelta float64
	for _, rc := range plan.ResourceChanges {
		est, err := pricing.EstimateChange(ctx, client, rc, "us-east-2")
		if err != nil {
			fmt.Printf("[FAIL] %s (%s): %v\n", rc.Address, rc.Type, err)
			continue
		}
		estimates = append(estimates, est)
		totalDelta += est.MonthlyDelta
	}

	sort.Slice(estimates, func(i, j int) bool {
		return math.Abs(estimates[i].MonthlyDelta) > math.Abs(estimates[j].MonthlyDelta)
	})

	fmt.Printf("%-50s %-10s %-12s %-10s\n", "RESOURCE", "ACTION", "DELTA/MO", "CONFIDENCE")
	fmt.Println(string(make([]byte, 90)))
	for _, est := range estimates {
		marker := ""
		if est.Skipped {
			marker = "  (skipped: " + est.SkipReason + ")"
		}
		fmt.Printf("%-50s %-10s $%-11.2f %-10s%s\n",
			est.ResourceAddress, est.Action, est.MonthlyDelta, est.Confidence, marker)
	}

	fmt.Printf("\n=== Total monthly delta: $%.2f ===\n\n", totalDelta)

	fmt.Println("--- Detailed breakdowns ---")
	for _, est := range estimates {
		if est.Skipped {
			continue
		}
		fmt.Printf("\n%s (%s, action=%s):\n", est.ResourceAddress, est.ResourceType, est.Action)
		fmt.Printf("  Before: $%.4f | After: $%.4f | Delta: $%.4f\n",
			est.BeforeMonthly, est.AfterMonthly, est.MonthlyDelta)
		for _, item := range est.Breakdown {
			fmt.Printf("    %-12s $%.4f\n", item.Component, item.MonthlyUSD)
		}
		for _, note := range est.Notes {
			fmt.Printf("  - %s\n", note)
		}
	}
}
