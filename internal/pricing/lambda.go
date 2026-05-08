package pricing

import (
	"context"
	"fmt"

	"CloudOracle/internal/iac/aws"
)

// SecondsPerMonth is HoursPerMonth * 3600. AWS Lambda Provisioned
// Concurrency is priced per GB-second rather than per GB-hour, so this
// is the conversion factor used to compute monthly cost from the
// per-second rate. Defined alongside HoursPerMonth so callers don't
// rediscover the constant on every per-second mapper.
const SecondsPerMonth = HoursPerMonth * 3600

// EstimateLambda calculates the monthly STANDING cost of a Lambda
// function.
//
// Lambda has two billing components and only the first is estimable
// from a Terraform plan:
//
//  1. Standing cost. $0 unless ProvisionedConcurrency > 0, in which
//     case it is ProvisionedConcurrency * (MemorySize/1024) *
//     SecondsPerMonth * pricePerGBSecond. Provisioned Concurrency
//     keeps execution environments warm and is billed by the second
//     regardless of invocations.
//  2. Invocation cost. Per-request fee plus per-GB-second of execution
//     time. NOT estimable from a plan — depends on runtime traffic.
//
// This function returns the standing cost only. When
// ProvisionedConcurrency is 0 (the Lambda default) MonthlyUSD is 0 and
// the Notes explicitly call out that invocation charges are not
// modelled. No API call is made in that case — we already know the
// answer is 0.
//
// Confidence is always Low because the invocation component is unknown,
// even when the standing cost is precisely $0: the user reading the
// estimate should always be aware that real Lambda spend depends on
// traffic. The Notes carry the same warning in human-readable form.
//
// Filter strategy: AWS exposes the standing cost SKU under
// productFamily="Serverless" with usagetype="<RegionPrefix>-Lambda-
// Provisioned-Concurrency" (x86_64) or "...-Provisioned-Concurrency-ARM"
// (arm64). There is no `architecture` attribute on these products, so
// usagetype IS the only architecture discriminator — that's why
// regionPrefix exists.
//
// Returns an error for nil attrs, empty region, unknown architectures,
// API failures, missing products, ambiguous matches, or any unit other
// than "Lambda-GB-Second".
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

	suffix, err := lambdaArchitectureSuffix(attrs.Architecture)
	if err != nil {
		return Estimate{}, err
	}
	usageType := fmt.Sprintf("%s-Lambda-Provisioned-Concurrency%s", regionPrefix(region), suffix)

	filters := map[string]string{
		"productFamily": "Serverless",
		"regionCode":    region,
		"usagetype":     usageType,
	}
	products, err := src.GetProducts(ctx, "AWSLambda", filters)
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateLambda: provisioned concurrency lookup: %w", err)
	}
	if len(products) == 0 {
		return Estimate{}, fmt.Errorf("EstimateLambda: no provisioned concurrency price found for usagetype=%s", usageType)
	}
	if len(products) > 1 {
		return Estimate{}, fmt.Errorf("EstimateLambda: PC query returned %d products; filter under-constrained for usagetype=%s region=%s",
			len(products), usageType, region)
	}
	gbSecond, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateLambda: parsing PC price: %w", err)
	}
	if unit != "Lambda-GB-Second" {
		return Estimate{}, fmt.Errorf("EstimateLambda: expected PC unit Lambda-GB-Second, got %q", unit)
	}

	memGB := float64(attrs.MemorySize) / 1024.0
	cost := float64(attrs.ProvisionedConcurrency) * memGB * SecondsPerMonth * gbSecond

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

// lambdaArchitectureSuffix returns the suffix appended to the
// Provisioned Concurrency usagetype for a given Terraform architecture.
// Empty / "x86_64" → no suffix; "arm64" → "-ARM". Unknown values
// return an error so a typo in the plan doesn't silently match (and
// mis-price as) the wrong architecture's SKU.
func lambdaArchitectureSuffix(arch string) (string, error) {
	switch arch {
	case "", "x86_64":
		return "", nil
	case "arm64":
		return "-ARM", nil
	default:
		return "", fmt.Errorf("EstimateLambda: unknown architecture %q", arch)
	}
}
