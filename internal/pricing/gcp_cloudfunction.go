package pricing

import (
	"fmt"

	"CloudOracle/internal/iac/gcp"
)

// EstimateGCPCloudFunction returns the monthly STANDING cost of a Cloud
// Function — always $0, with a caveat.
//
// A function's cost is invocation-driven: per-request plus per-GB-second of
// execution time, neither of which a Terraform plan declares. Gen2 functions
// bill as Cloud Run, where even the min-instance idle rate derives from a
// memory→vCPU mapping that a static table can't track reliably. So, like the
// Lambda estimator's no-provisioned-concurrency path, we report $0 standing at
// Low confidence and make the unmodeled invocation cost explicit, so a PR
// adding a function still surfaces instead of being silently skipped.
func EstimateGCPCloudFunction(attrs *gcp.CloudFunctionAttributes) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateGCPCloudFunction: nil attrs")
	}
	return Estimate{
		MonthlyUSD: 0,
		Currency:   "USD",
		Breakdown:  nil,
		Confidence: ConfidenceLow,
		Notes: []string{
			"Standing cost is $0; per-invocation charges (requests + GB-seconds) and " +
				"any min-instance idle cost are not modeled — they depend on runtime traffic",
		},
	}, nil
}
