package pricing

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
