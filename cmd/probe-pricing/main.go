// Command probe-pricing inspects raw AWS Pricing API responses for a given
// service code and filter set. It exists for two reasons:
//
//  1. Diagnosing "multiple products" warnings: when an EstimateXxx mapper
//     warns that a query returned >1 product, run the same filters through
//     the probe to see exactly which attributes differ between the products
//     and pick a tighter filter to add.
//  2. Ad-hoc exploration of new resource types before writing a mapper —
//     dumping a known-good filter set is the fastest way to learn the
//     attribute vocabulary AWS uses for that productFamily.
//
// Usage:
//
//	go run ./cmd/probe-pricing <serviceCode> '<filters-json>'
//
// Example:
//
//	go run ./cmd/probe-pricing AmazonRDS \
//	    '{"productFamily":"Database Storage","volumeType":"General Purpose","deploymentOption":"Single-AZ","regionCode":"us-east-2"}'
//
// Requires AWS credentials in the standard chain (env vars, shared config,
// instance metadata). The Pricing API endpoint is forced to us-east-1 by
// pricing.NewClient regardless of the resource's region — the resource
// region is a filter value, not the endpoint region.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"

	"CloudOracle/internal/pricing"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatal("usage: probe-pricing <serviceCode> <filters-json>")
	}

	var filters map[string]string
	if err := json.Unmarshal([]byte(os.Args[2]), &filters); err != nil {
		log.Fatalf("invalid filters JSON: %v", err)
	}

	ctx := context.Background()
	client, err := pricing.NewClient(ctx)
	if err != nil {
		log.Fatalf("NewClient: %v", err)
	}

	products, err := client.GetProducts(ctx, os.Args[1], filters)
	if err != nil {
		log.Fatalf("GetProducts: %v", err)
	}

	fmt.Printf("Got %d products for %s with filters %v\n\n", len(products), os.Args[1], filters)
	for i, p := range products {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(p), &parsed); err != nil {
			fmt.Printf("[%d] parse error: %v\n", i, err)
			continue
		}
		product, _ := parsed["product"].(map[string]interface{})
		attrs, _ := product["attributes"].(map[string]interface{})

		fmt.Printf("[%d] sku=%v\n", i, product["sku"])
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    %-30s = %v\n", k, attrs[k])
		}
		fmt.Println()
	}
}
