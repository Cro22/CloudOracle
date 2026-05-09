package pricing

import (
	"context"
	"fmt"

	"CloudOracle/internal/iac/aws"
)

// lookupEBSStoragePrice queries the Pricing API for the per-GB-month USD
// rate of an EBS volume type in a given region. Used by both EstimateEBS
// (standalone volumes) and EstimateEC2 (root block devices), so it is
// kept generic — the caller multiplies by size and adds context-specific
// notes.
//
// volumeType is the Terraform value: "gp2", "gp3", "io1", "io2", "st1",
// "sc1", or "standard". The Pricing API's volumeApiName field currently
// uses the same strings, but mapping through mapEBSVolumeAPIName means a
// future divergence (or a typo guard) lives in exactly one place.
//
// Returns an error for empty/unknown volume types, API failures, products
// that come back empty, parse failures, or any unit other than "GB-Mo"
// — that last case usually indicates the filters were too loose and the
// query matched a non-storage product family.
func lookupEBSStoragePrice(ctx context.Context, src productGetter, volumeType, region string) (float64, error) {
	apiName, err := mapEBSVolumeAPIName(volumeType)
	if err != nil {
		return 0, err
	}
	filters := map[string]string{
		"productFamily": "Storage",
		"volumeApiName": apiName,
		"regionCode":    region,
	}
	products, err := src.GetProducts(ctx, "AmazonEC2", filters)
	if err != nil {
		return 0, fmt.Errorf("lookupEBSStoragePrice: %w", err)
	}
	if len(products) == 0 {
		return 0, fmt.Errorf("lookupEBSStoragePrice: no EBS price found for %s in %s", volumeType, region)
	}
	if len(products) > 1 {
		return 0, fmt.Errorf("lookupEBSStoragePrice: query returned %d products; filter under-constrained for volumeType=%s region=%s",
			len(products), volumeType, region)
	}
	gbMo, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return 0, fmt.Errorf("lookupEBSStoragePrice: parsing EBS price: %w", err)
	}
	if unit != "GB-Mo" {
		return 0, fmt.Errorf("lookupEBSStoragePrice: expected EBS unit GB-Mo, got %q", unit)
	}
	return gbMo, nil
}

// mapEBSVolumeAPIName validates a Terraform EBS volume type and returns
// the Pricing API's volumeApiName filter value. Today the strings line
// up one-to-one, but isolating the mapping rejects typos up front (with
// a clear error) instead of via "no products found" pages later.
func mapEBSVolumeAPIName(tfType string) (string, error) {
	switch tfType {
	case "gp2", "gp3", "io1", "io2", "st1", "sc1", "standard":
		return tfType, nil
	case "":
		return "", fmt.Errorf("lookupEBSStoragePrice: empty volume type")
	default:
		return "", fmt.Errorf("lookupEBSStoragePrice: unknown volume type %q", tfType)
	}
}

// EstimateEBS calculates the monthly cost of a standalone aws_ebs_volume.
// It charges the GB-month rate for the volume's type and size.
//
// Charges NOT included in the estimate:
//
//   - IOPS-month billing for gp3 above the 3000 IOPS that ship by default
//   - Throughput-month billing for gp3 above the 125 MB/s default
//   - IOPS-month billing for io1/io2 (a separate Pricing API
//     productFamily that's out of scope for this milestone)
//   - Snapshot storage and data-transfer charges
//
// Confidence rules:
//
//   - gp2, st1, sc1, standard: ConfidenceMedium — flat GB-month price,
//     no IOPS/throughput add-ons exist for these types.
//   - gp3 at default IOPS/throughput: ConfidenceMedium — the included
//     defaults cover most workloads.
//   - gp3 with Iops > 3000 or Throughput > 125: ConfidenceLow — the
//     missing add-on charges are material.
//   - io1, io2: ConfidenceLow — IOPS billing is the dominant cost for
//     these types and we are not modelling it.
//
// Returns an error for nil attrs, empty region, empty Type, Size <= 0,
// unknown volume types, API failures, missing products, or unit
// mismatches.
func EstimateEBS(ctx context.Context, src productGetter, attrs *aws.EBSAttributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateEBS: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateEBS: nil attrs")
	}
	if attrs.Type == "" {
		return Estimate{}, fmt.Errorf("EstimateEBS: empty Type")
	}
	if attrs.Size <= 0 {
		return Estimate{}, fmt.Errorf("EstimateEBS: Size must be > 0, got %d", attrs.Size)
	}

	gbMo, err := lookupEBSStoragePrice(ctx, src, attrs.Type, region)
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateEBS: %w", err)
	}
	storageCost := gbMo * float64(attrs.Size)

	notes := []string{
		"IOPS-month and throughput-month charges not included for gp3 above defaults (3000 IOPS, 125 MB/s)",
	}
	confidence := ConfidenceMedium
	switch attrs.Type {
	case "io1", "io2":
		notes = append(notes, "io1/io2 IOPS billing is separate and not included in this estimate")
		confidence = ConfidenceLow
	case "gp3":
		if attrs.Iops > 3000 || attrs.Throughput > 125 {
			confidence = ConfidenceLow
		}
	}

	return Estimate{
		MonthlyUSD: storageCost,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "Storage", MonthlyUSD: storageCost}},
		Confidence: confidence,
		Notes:      notes,
	}, nil
}
