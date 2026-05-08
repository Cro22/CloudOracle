package pricing

import (
	"context"
	"fmt"
	"log/slog"

	"CloudOracle/internal/iac/aws"
)

// EstimateLambda calculates the monthly STANDING cost of a Lambda function.
//
// Lambda has two billing components and only the first is estimable from a
// Terraform plan:
//
//  1. Standing cost. $0 unless ProvisionedConcurrency > 0, in which case
//     it is ProvisionedConcurrency * (MemorySize/1024) * 730 hours *
//     pricePerGBHour. Provisioned Concurrency keeps execution environments
//     warm and is billed by the hour regardless of invocations.
//  2. Invocation cost. Per-request fee plus per-GB-second of execution
//     time. NOT estimable from a plan — depends on runtime traffic.
//
// This function returns the standing cost only. When ProvisionedConcurrency
// is 0 (the Lambda default) MonthlyUSD is 0 and the Notes explicitly call
// out that invocation charges are not modelled. No API call is made in
// that case — we already know the answer is 0.
//
// Confidence is always Low because the invocation component is unknown,
// even when the standing cost is precisely $0: the user reading the
// estimate should always be aware that real Lambda spend depends on
// traffic. The Notes carry the same warning in human-readable form.
//
// Returns an error for nil attrs, empty region, unknown architectures,
// API failures, missing products, or any unit other than "GB-Hour".
func EstimateLambda(ctx context.Context, src productGetter, attrs *aws.LambdaAttributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateLambda: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateLambda: nil attrs")
	}

	if attrs.ProvisionedConcurrency == 0 {
		return Estimate{
			MonthlyUSD: 0,
			Currency:   "USD",
			Breakdown:  nil,
			Confidence: ConfidenceLow,
			Notes: []string{
				"Standing cost is $0; per-invocation charges (requests + GB-seconds) not modeled",
			},
		}, nil
	}

	arch, err := mapLambdaArchitecture(attrs.Architecture)
	if err != nil {
		return Estimate{}, err
	}

	filters := map[string]string{
		"productFamily": "Provisioned Concurrency",
		"regionCode":    region,
		"architecture":  arch,
	}
	products, err := src.GetProducts(ctx, "AWSLambda", filters)
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateLambda: provisioned concurrency lookup: %w", err)
	}
	if len(products) == 0 {
		return Estimate{}, fmt.Errorf("EstimateLambda: no provisioned concurrency price found for %s in %s", attrs.Architecture, region)
	}
	if len(products) > 1 {
		slog.Warn("pricing: Lambda PC query returned multiple products; using first",
			"architecture", attrs.Architecture,
			"region", region,
			"count", len(products),
		)
	}
	gbHour, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateLambda: parsing PC price: %w", err)
	}
	if unit != "GB-Hour" {
		return Estimate{}, fmt.Errorf("EstimateLambda: expected PC unit GB-Hour, got %q", unit)
	}

	memGB := float64(attrs.MemorySize) / 1024.0
	cost := float64(attrs.ProvisionedConcurrency) * memGB * HoursPerMonth * gbHour

	return Estimate{
		MonthlyUSD: cost,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "ProvisionedConcurrency", MonthlyUSD: cost}},
		Confidence: ConfidenceLow,
		Notes: []string{
			"Provisioned Concurrency standing cost only; per-invocation charges not modeled",
		},
	}, nil
}

// mapLambdaArchitecture converts a Terraform Lambda architecture value
// to the Pricing API's "architecture" filter value. Terraform uses
// "x86_64"/"arm64"; the Pricing API uses "x86"/"ARM" (note the case
// difference). Unknown values return an error so a typo doesn't silently
// match the wrong product.
func mapLambdaArchitecture(arch string) (string, error) {
	switch arch {
	case "", "x86_64":
		return "x86", nil
	case "arm64":
		return "ARM", nil
	default:
		return "", fmt.Errorf("EstimateLambda: unknown architecture %q", arch)
	}
}
