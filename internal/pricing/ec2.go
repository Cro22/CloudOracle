package pricing

import (
	"context"
	"fmt"
	"log/slog"

	"CloudOracle/internal/iac/aws"
)

// HoursPerMonth is the AWS-standard month length used to convert hourly
// On-Demand prices to a monthly figure. AWS uses 730 across every public
// pricing page (see https://aws.amazon.com/ec2/pricing/on-demand/). Cited
// inline because future readers always ask why 730 rather than 720, 744,
// or 8760/12.
const HoursPerMonth = 730

// EstimateEC2 calculates the monthly cost of a single EC2 instance using
// the AWS Pricing API. The total includes the compute hours and, when a
// root_block_device is present in the plan, the root EBS volume's
// GB-month rate.
//
// region is the AWS region of the resource (e.g. "us-east-2"). The
// Pricing API endpoint region — pinned to us-east-1 inside Client — is
// independent of this value: the resource's region is supplied as a
// regionCode filter.
//
// src can be a *Client (live calls) or a *Cache (disk-cached); the caller
// chooses. Tests inject a fake productGetter.
//
// The mapper applies four assumptions, each of which contributes to the
// returned Confidence being ConfidenceLow:
//
//  1. operatingSystem = "Linux". Terraform plans don't carry the OS — it
//     comes from the AMI, and resolving the AMI requires an extra
//     ec2:DescribeImages call (out of scope here).
//  2. preInstalledSw = "NA". No SQL Server, SAP, or other licensed
//     software sold by the hour through AWS.
//  3. capacitystatus = "Used". On-Demand only — Reserved Instances,
//     Savings Plans, and Capacity Reservations are not modelled.
//  4. tenancy mapping. Terraform's lowercase "default"/"dedicated"/"host"
//     maps to the Pricing API's "Shared"/"Dedicated"/"Host" via
//     mapTenancy.
//
// Returns an error for nil attrs, empty region, empty InstanceType, API
// failures, products that come back empty, parse failures, or any unit
// mismatch (compute price not in "Hrs", storage price not in "GB-Mo").
// A unit mismatch usually means the filters were too loose and matched
// the wrong product family — failing fast surfaces it to the developer.
func EstimateEC2(ctx context.Context, src productGetter, attrs *aws.EC2Attributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateEC2: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateEC2: nil attrs")
	}
	if attrs.InstanceType == "" {
		return Estimate{}, fmt.Errorf("EstimateEC2: empty InstanceType")
	}

	compute, err := lookupComputePrice(ctx, src, attrs, region)
	if err != nil {
		return Estimate{}, err
	}

	notes := []string{
		"Operating system assumed Linux (plan does not specify)",
		"Pricing assumes On-Demand (no Reserved Instances or Savings Plans)",
	}
	breakdown := []LineItem{{Component: "Compute", MonthlyUSD: compute}}
	total := compute

	if attrs.RootBlockSize > 0 {
		gbMo, err := lookupEBSStoragePrice(ctx, src, attrs.RootBlockType, region)
		if err != nil {
			return Estimate{}, fmt.Errorf("EstimateEC2: root EBS: %w", err)
		}
		rootEBS := gbMo * float64(attrs.RootBlockSize)
		breakdown = append(breakdown, LineItem{Component: "RootEBS", MonthlyUSD: rootEBS})
		total += rootEBS
	} else {
		notes = append(notes, "Root block device unknown, compute-only estimate")
	}

	return Estimate{
		MonthlyUSD: total,
		Currency:   "USD",
		Breakdown:  breakdown,
		Confidence: ConfidenceLow,
		Notes:      notes,
	}, nil
}

// lookupComputePrice runs the Pricing API query for the EC2 compute
// component and returns the monthly USD cost (hourly price * 730).
func lookupComputePrice(ctx context.Context, src productGetter, attrs *aws.EC2Attributes, region string) (float64, error) {
	filters := map[string]string{
		"productFamily":   "Compute Instance",
		"instanceType":    attrs.InstanceType,
		"regionCode":      region,
		"tenancy":         mapTenancy(attrs.Tenancy),
		"operatingSystem": "Linux",
		"preInstalledSw":  "NA",
		"capacitystatus":  "Used",
	}
	products, err := src.GetProducts(ctx, "AmazonEC2", filters)
	if err != nil {
		return 0, fmt.Errorf("EstimateEC2: compute lookup: %w", err)
	}
	if len(products) == 0 {
		return 0, fmt.Errorf("EstimateEC2: no compute price found for %s in %s", attrs.InstanceType, region)
	}
	if len(products) > 1 {
		return 0, fmt.Errorf("EstimateEC2: compute query returned %d products; filter under-constrained for instanceType=%s region=%s",
			len(products), attrs.InstanceType, region)
	}
	hourly, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return 0, fmt.Errorf("EstimateEC2: parsing compute price: %w", err)
	}
	if unit != "Hrs" {
		return 0, fmt.Errorf("EstimateEC2: expected compute unit Hrs, got %q", unit)
	}
	return hourly * HoursPerMonth, nil
}

// mapTenancy translates Terraform's tenancy attribute to the AWS Pricing
// API's tenancy filter value. Terraform uses lowercase ("default",
// "dedicated", "host"); the Pricing API uses TitleCase ("Shared",
// "Dedicated", "Host"). The empty string maps to "Shared" because
// aws_instance defaults tenancy to "default" when omitted.
//
// Unknown values fall back to "Shared" with a warn log so an unexpected
// future tenancy doesn't silently mis-price.
func mapTenancy(tenancy string) string {
	switch tenancy {
	case "", "default":
		return "Shared"
	case "dedicated":
		return "Dedicated"
	case "host":
		return "Host"
	default:
		slog.Warn("pricing: unknown EC2 tenancy, defaulting to Shared",
			"tenancy", tenancy,
		)
		return "Shared"
	}
}
