package main

import (
	"context"
	"fmt"
	"log"

	"CloudOracle/internal/iac/aws"
	"CloudOracle/internal/pricing"
)

func main() {
	ctx := context.Background()

	client, err := pricing.NewClient(ctx)
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}

	// ---- RDS: postgres db.t3.medium 100GB gp2 single-AZ ----
	rdsAttrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
		MultiAZ:          false,
	}
	estRDS, err := pricing.EstimateRDS(ctx, client, rdsAttrs, "us-east-2")
	if err != nil {
		log.Fatalf("EstimateRDS: %v", err)
	}
	printEstimate("RDS postgres db.t3.medium 100GB gp2 us-east-2", estRDS)

	fmt.Println()

	// ---- EBS standalone: gp3 200GB ----
	ebsAttrs := &aws.EBSAttributes{
		Type: "gp3",
		Size: 200,
	}
	estEBS, err := pricing.EstimateEBS(ctx, client, ebsAttrs, "us-east-2")
	if err != nil {
		log.Fatalf("EstimateEBS: %v", err)
	}
	printEstimate("EBS standalone gp3 200GB us-east-2", estEBS)
}

func printEstimate(label string, est pricing.Estimate) {
	fmt.Printf("=== %s ===\n", label)
	fmt.Printf("Total monthly: $%.2f %s\n", est.MonthlyUSD, est.Currency)
	fmt.Printf("Confidence: %s\n", est.Confidence)
	fmt.Println("Breakdown:")
	for _, item := range est.Breakdown {
		fmt.Printf("  %-12s $%.4f\n", item.Component, item.MonthlyUSD)
	}
	fmt.Println("Notes:")
	for _, note := range est.Notes {
		fmt.Printf("  - %s\n", note)
	}
}
