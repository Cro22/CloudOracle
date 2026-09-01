package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Azure has a queryable Retail Prices API, but hitting it needs a live HTTP
// call in CI and per-SKU filter strings that drift. Since v2 is an approximate
// pre-merge estimate with explicit confidence levels, we price Azure from a
// curated static table embedded at build time — the same choice the GCP arm
// makes (see gcp_prices.go). It drifts from the real list price over time;
// estimators surface that as a "static price table" note and cap confidence at
// Medium. Upgrade path: swap these lookups for the Retail Prices API.
//
//go:embed azure_prices.json
var azurePricesJSON []byte

type azurePriceTable struct {
	VMHourlyUSD      map[string]float64 `json:"vm_hourly_usd"`
	RegionMultiplier map[string]float64 `json:"region_multiplier"`
	DiskGBMonthUSD   map[string]float64 `json:"disk_gb_month_usd"`
}

// azurePrices is parsed once at package init. A malformed embedded table is a
// build/programming error, so we panic rather than thread an error through
// every estimator constructor (same contract as gcpPrices).
var azurePrices = mustLoadAzurePrices()

func mustLoadAzurePrices() azurePriceTable {
	var t azurePriceTable
	if err := json.Unmarshal(azurePricesJSON, &t); err != nil {
		panic(fmt.Sprintf("pricing: parsing embedded azure_prices.json: %v", err))
	}
	return t
}

// azureVMHourly returns the pay-as-you-go hourly price for size in region and
// whether it was found. region defaults to a 1.0 multiplier when it isn't in
// the table; regionKnown reports whether the multiplier was an exact match so
// the caller can lower confidence for a guessed region.
func azureVMHourly(size, region string) (price float64, sizeKnown bool, regionKnown bool) {
	base, ok := azurePrices.VMHourlyUSD[size]
	if !ok {
		return 0, false, false
	}
	mult, regionKnown := azurePrices.RegionMultiplier[region]
	if !regionKnown {
		mult = 1.0
	}
	return base * mult, true, regionKnown
}

// azureDiskGBMonth returns the per-GB-month price for a managed-disk SKU and
// whether it was found. Disk storage is priced at the base rate; regional
// variation is not modeled for storage (same simplification as the GCP arm).
func azureDiskGBMonth(accountType string) (float64, bool) {
	p, ok := azurePrices.DiskGBMonthUSD[accountType]
	return p, ok
}
