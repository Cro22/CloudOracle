package pricing

import "CloudOracle/internal/iac"

// Estimate is the result of a cost estimation for a single resource.
//
// MonthlyUSD is the sum of every Breakdown line item, expressed in USD
// per AWS-standard 730-hour month (see HoursPerMonth). Currency is always
// "USD" today; the AWS Pricing API supports CNY for AWS China but we do
// not — adding it would multiply the surface area of every mapper.
//
// Confidence reports how many defaults the mapper had to assume. Low
// estimates should reach the user with a "may differ from real bill"
// caveat; the assumptions themselves are listed in Notes for transparency
// rather than buried in the Confidence value.
type Estimate struct {
	MonthlyUSD float64
	Currency   string
	Breakdown  []LineItem
	Confidence Confidence
	Notes      []string
}

// LineItem is one component of an Estimate's total cost. The components
// of an EC2 Estimate are "Compute" and (optionally) "RootEBS"; future
// mappers will introduce their own component names.
type LineItem struct {
	Component  string
	MonthlyUSD float64
}

// Confidence indicates how reliable an Estimate is.
//
//   - ConfidenceHigh:   no defaults applied, all relevant attributes
//     were present on the resource.
//   - ConfidenceMedium: minor defaults applied (e.g., AZ unknown so
//     region-level pricing was used).
//   - ConfidenceLow:    one or more strong assumptions were made (e.g.,
//     OS=Linux assumed because Terraform plans don't carry OS).
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// ChangeEstimate is the cost impact of a single resource change in a
// Terraform plan, produced by EstimateChange. One ChangeEstimate maps
// one-to-one with one iac.ResourceChange.
//
// Sign convention for the cost fields:
//
//   - create:  BeforeMonthly = 0, AfterMonthly  >= 0, MonthlyDelta =  AfterMonthly
//   - delete:  AfterMonthly  = 0, BeforeMonthly >= 0, MonthlyDelta = -BeforeMonthly
//   - update:  both populated, MonthlyDelta = AfterMonthly - BeforeMonthly
//   - replace: same as update; the resource is destroyed and re-created so
//     the delta is computed against the priced "after" shape, not zero
//   - no-op / read / data sources: all three are 0, Skipped is true
//
// Unsupported resource types (aws_iam_role, aws_route53_zone, anything
// outside aws.SupportedTypes()) also produce a ChangeEstimate with all
// costs at 0 and Skipped=true. This is INTENTIONAL: callers iterate over
// the entire plan and want a per-resource result for every change, even
// ones we can't price — silently dropping unsupported types would force
// callers to maintain their own "why didn't this show up?" lookup.
//
// Confidence reflects how reliable the cost numbers are. For Skipped
// estimates the cost is deterministically 0, so Confidence is High; for
// priced estimates it is the weakest of the before/after confidences
// (low > medium > high in "weakness" order).
type ChangeEstimate struct {
	ResourceAddress string
	ResourceType    string
	Action          iac.Action
	BeforeMonthly   float64
	AfterMonthly    float64
	MonthlyDelta    float64
	Currency        string
	Confidence      Confidence
	Notes           []string
	Breakdown       []LineItem
	Skipped         bool
	SkipReason      string
}
