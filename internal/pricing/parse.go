package pricing

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// parseOnDemandPriceUSD extracts the per-unit OnDemand USD price from a
// single AWS Pricing API product JSON document — one element of the
// PriceList slice returned by GetProducts.
//
// The product payload nests pricing under
//
//	terms.OnDemand.<sku>.priceDimensions.<dim>.pricePerUnit.USD
//
// where <sku> and <dim> are AWS-internal opaque strings. EC2 compute,
// EBS storage, and RDS instances each carry exactly one OnDemand SKU
// with a single price dimension, so this helper picks the first entry of
// each map sorted by key. Sorting matters because Go map iteration order
// is randomised: we want byte-identical results across runs.
//
// Tiered services (S3 storage classes, Lambda invocations) expose
// multiple price dimensions in a single SKU and need a different helper
// — they are out of scope here.
//
// The unit string ("Hrs", "GB-Mo", ...) is returned alongside the price
// so callers can confirm the product they pulled matches the cost
// component they expected (compute → "Hrs", storage → "GB-Mo"). A
// mismatch usually means the filters were too loose and selected the
// wrong product family.
//
// An error is returned (with context) when the JSON does not parse,
// terms.OnDemand is missing or empty, the priceDimensions map is missing
// or empty, the USD entry is absent, or the price string fails to parse
// as a float.
func parseOnDemandPriceUSD(productJSON string) (price float64, unit string, err error) {
	var doc struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					Unit         string            `json:"unit"`
					PricePerUnit map[string]string `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(productJSON), &doc); err != nil {
		return 0, "", fmt.Errorf("pricing: parsing product JSON: %w", err)
	}

	if len(doc.Terms.OnDemand) == 0 {
		return 0, "", fmt.Errorf("pricing: no OnDemand pricing in product")
	}
	skuKeys := make([]string, 0, len(doc.Terms.OnDemand))
	for k := range doc.Terms.OnDemand {
		skuKeys = append(skuKeys, k)
	}
	sort.Strings(skuKeys)
	sku := doc.Terms.OnDemand[skuKeys[0]]

	if len(sku.PriceDimensions) == 0 {
		return 0, "", fmt.Errorf("pricing: OnDemand SKU has no priceDimensions")
	}
	dimKeys := make([]string, 0, len(sku.PriceDimensions))
	for k := range sku.PriceDimensions {
		dimKeys = append(dimKeys, k)
	}
	sort.Strings(dimKeys)
	dim := sku.PriceDimensions[dimKeys[0]]

	usdStr, ok := dim.PricePerUnit["USD"]
	if !ok {
		return 0, "", fmt.Errorf("pricing: priceDimension has no USD price")
	}
	usd, err := strconv.ParseFloat(usdStr, 64)
	if err != nil {
		return 0, "", fmt.Errorf("pricing: parsing USD price %q: %w", usdStr, err)
	}
	return usd, dim.Unit, nil
}
