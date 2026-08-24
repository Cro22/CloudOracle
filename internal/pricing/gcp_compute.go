package pricing

import (
	"errors"
	"fmt"

	"CloudOracle/internal/iac/gcp"
)

// defaultBootDiskType is google_compute_instance's default boot disk type when
// initialize_params.type is omitted.
const defaultBootDiskType = "pd-balanced"

// errUnpricedGCPMachineType marks a machine type absent from the static price
// table. The dispatcher turns it into a Skipped change (the "unsupported"
// substring makes it count under unsupported types in the plan-wide notes)
// rather than a hard estimation error.
var errUnpricedGCPMachineType = errors.New("unsupported machine type not in static price table")

// EstimateGCPComputeInstance calculates the monthly cost of a
// google_compute_instance from the embedded static price table. Unlike the AWS
// estimators it makes no API call, so it takes no productGetter.
//
// planRegion is the plan-wide --region used when the instance's own zone was
// absent from the plan (attrs.Region empty).
//
// The estimate is capped at ConfidenceMedium (static table, may drift) and
// dropped to ConfidenceLow when the region is not in the multiplier table or
// the instance is preemptible/Spot (priced at on-demand as an upper bound).
//
// An unknown machine type returns errUnpricedGCPMachineType so EstimateChange
// routes it to a Skipped result rather than a hard error.
func EstimateGCPComputeInstance(attrs *gcp.ComputeInstanceAttributes, planRegion string) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateGCPComputeInstance: nil attrs")
	}
	if attrs.MachineType == "" {
		return Estimate{}, fmt.Errorf("EstimateGCPComputeInstance: empty MachineType")
	}

	region := attrs.Region
	if region == "" {
		region = planRegion
	}

	hourly, machineKnown, regionKnown := gcpComputeHourly(attrs.MachineType, region)
	if !machineKnown {
		return Estimate{}, fmt.Errorf("%w: %q", errUnpricedGCPMachineType, attrs.MachineType)
	}

	compute := hourly * HoursPerMonth
	confidence := ConfidenceMedium
	notes := []string{"Priced from a static GCP price table (may drift from current list price)"}

	if !regionKnown {
		confidence = ConfidenceLow
		notes = append(notes, fmt.Sprintf("Region %q not in price table; used us-central1 base rate", region))
	}
	if attrs.Preemptible {
		confidence = ConfidenceLow
		notes = append(notes, "Spot/preemptible VM priced at on-demand rate (real cost is 60–91% lower)")
	}

	breakdown := []LineItem{{Component: "Compute", MonthlyUSD: compute}}
	total := compute

	if attrs.BootDiskSizeGB > 0 {
		diskType := attrs.BootDiskType
		if diskType == "" {
			diskType = defaultBootDiskType
		}
		gbMo, ok := gcpPDGBMonth(diskType)
		if !ok {
			return Estimate{}, fmt.Errorf("EstimateGCPComputeInstance: unknown boot disk type %q", diskType)
		}
		boot := gbMo * float64(attrs.BootDiskSizeGB)
		breakdown = append(breakdown, LineItem{Component: "BootDisk", MonthlyUSD: boot})
		total += boot
	} else {
		notes = append(notes, "Boot disk size not in plan, compute-only estimate")
	}

	return Estimate{
		MonthlyUSD: total,
		Currency:   "USD",
		Breakdown:  breakdown,
		Confidence: confidence,
		Notes:      notes,
	}, nil
}
