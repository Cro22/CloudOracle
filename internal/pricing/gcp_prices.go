package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// GCP has no attribute-queryable pricing API comparable to AWS's Pricing API —
// the Cloud Billing Catalog exposes SKUs whose machine-type mapping lives in
// free-text descriptions, which is brittle to match and needs a live API call
// in CI. Since v2 is an approximate pre-merge estimate with explicit confidence
// levels, we price GCP from a curated static table embedded at build time. It
// drifts from the real list price over time; estimators surface that as a
// "static price table" note and cap confidence at Medium.
//
//go:embed gcp_prices.json
var gcpPricesJSON []byte

type gcpPriceTable struct {
	ComputeHourlyUSD map[string]float64 `json:"compute_hourly_usd"`
	RegionMultiplier map[string]float64 `json:"region_multiplier"`
	PDGBMonthUSD     map[string]float64 `json:"pd_gb_month_usd"`
	CloudSQL         gcpCloudSQLPrices  `json:"cloudsql"`
}

type gcpCloudSQLPrices struct {
	VCPUHourlyUSD       float64            `json:"vcpu_hourly_usd"`
	RAMGBHourlyUSD      float64            `json:"ram_gb_hourly_usd"`
	SharedCoreHourlyUSD map[string]float64 `json:"shared_core_hourly_usd"`
	StorageGBMonthUSD   map[string]float64 `json:"storage_gb_month_usd"`
}

// gcpPrices is parsed once at package init. A malformed embedded table is a
// build/programming error, so we panic rather than thread an error through
// every estimator constructor.
var gcpPrices = mustLoadGCPPrices()

func mustLoadGCPPrices() gcpPriceTable {
	var t gcpPriceTable
	if err := json.Unmarshal(gcpPricesJSON, &t); err != nil {
		panic(fmt.Sprintf("pricing: parsing embedded gcp_prices.json: %v", err))
	}
	return t
}

// gcpComputeHourly returns the on-demand hourly price for machineType in region
// and whether it was found. region defaults to a 1.0 multiplier when it isn't in
// the table; regionKnown reports whether the multiplier was an exact match so
// the caller can lower confidence for a guessed region.
func gcpComputeHourly(machineType, region string) (price float64, machineKnown bool, regionKnown bool) {
	base, ok := gcpPrices.ComputeHourlyUSD[machineType]
	if !ok {
		return 0, false, false
	}
	mult, regionKnown := gcpPrices.RegionMultiplier[region]
	if !regionKnown {
		mult = 1.0
	}
	return base * mult, true, regionKnown
}

// gcpPDGBMonth returns the per-GB-month price for a persistent-disk type and
// whether it was found.
func gcpPDGBMonth(diskType string) (float64, bool) {
	p, ok := gcpPrices.PDGBMonthUSD[diskType]
	return p, ok
}
