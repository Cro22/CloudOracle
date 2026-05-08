package pricing

import (
	"context"
	"fmt"

	"CloudOracle/internal/iac/aws"
)

// EstimateNATGateway calculates the monthly cost of a NAT Gateway.
//
// NAT Gateway has two billing components:
//
//  1. Hourly gateway charge. Region-dependent (~$0.045/hr in us-east-2,
//     ~$32.85/month), billed regardless of traffic. Fully estimable from
//     the plan.
//  2. Per-GB data processing. ~$0.045/GB on every byte that flows
//     through. NOT estimable from a Terraform plan — depends on traffic.
//
// This function returns the hourly gateway cost only (price * 730). The
// Notes always include a warning about the unmodelled data-processing
// charges, because a NAT in a chatty subnet can cost an order of
// magnitude more than the standing cost. Confidence is Medium: the
// standing component is exact, but the unmodelled component can dominate
// the bill in real workloads.
//
// Returns an error for nil attrs, empty region, API failures, missing
// products, or any unit other than "Hrs".
func EstimateNATGateway(ctx context.Context, src productGetter, attrs *aws.NATGatewayAttributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: nil attrs")
	}

	filters := map[string]string{
		"productFamily":    "NAT Gateway",
		"regionCode":       region,
		"groupDescription": "Hourly charge for NAT Gateways",
	}
	products, err := src.GetProducts(ctx, "AmazonEC2", filters)
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: lookup: %w", err)
	}
	if len(products) == 0 {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: no NAT Gateway price found in %s", region)
	}
	if len(products) > 1 {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: query returned %d products; filter under-constrained for region=%s",
			len(products), region)
	}
	hourly, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: parsing price: %w", err)
	}
	if unit != "Hrs" {
		return Estimate{}, fmt.Errorf("EstimateNATGateway: expected unit Hrs, got %q", unit)
	}
	cost := hourly * HoursPerMonth

	return Estimate{
		MonthlyUSD: cost,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "Gateway", MonthlyUSD: cost}},
		Confidence: ConfidenceMedium,
		Notes: []string{
			"Hourly gateway charge only; per-GB data processing charges (~$0.045/GB) not modeled",
		},
	}, nil
}
