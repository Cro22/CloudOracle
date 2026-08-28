package pricing

import (
	"fmt"

	"CloudOracle/internal/iac/gcp"
)

// EstimateGCPRouterNAT calculates the monthly STANDING cost of a Cloud NAT
// gateway from the embedded static rate.
//
// Cloud NAT has three billing components and only the last is estimable from a
// Terraform plan:
//
//  1. Per-VM uptime. $0.0014 per VM-hour, capped at $0.044/hr once 32 VMs use
//     the gateway. NOT estimable — the plan doesn't say how many VMs will.
//  2. Data processing. $0.045/GiB on all traffic. NOT estimable — depends on
//     runtime traffic.
//  3. External IP uptime. $0.005/hr per reserved NAT IP. Estimable ONLY under
//     MANUAL_ONLY allocation, where nat_ips lists the reserved addresses.
//
// This returns the external-IP standing cost (0 for AUTO_ONLY, where the IP
// count scales with load and isn't in the plan). Confidence is always Low
// because the dominant per-VM and data-processing components are unmodeled; the
// Notes carry that warning so a PR adding a NAT still surfaces loudly.
func EstimateGCPRouterNAT(attrs *gcp.RouterNATAttributes) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateGCPRouterNAT: nil attrs")
	}

	cost := float64(attrs.ManualIPCount) * gcpPrices.CloudNAT.IPHourlyUSD * HoursPerMonth

	notes := []string{
		"Per-VM uptime ($0.0014/VM-hr, capped at $0.044/hr) and data processing " +
			"($0.045/GiB) not modeled — they depend on VM count and traffic, which a plan doesn't carry",
		"Priced from a static GCP price table (may drift from current list price)",
	}
	var breakdown []LineItem
	if attrs.ManualIPCount > 0 {
		breakdown = []LineItem{{Component: "ExternalIPs", MonthlyUSD: cost}}
	} else {
		notes = append(notes, "AUTO_ONLY allocation: external-IP count scales with load and isn't in the plan, so standing cost is $0")
	}

	return Estimate{
		MonthlyUSD: cost,
		Currency:   "USD",
		Breakdown:  breakdown,
		Confidence: ConfidenceLow,
		Notes:      notes,
	}, nil
}
