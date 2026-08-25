package pricing

import (
	"errors"
	"fmt"

	"CloudOracle/internal/iac/gcp"
)

// defaultDiskType is google_compute_disk's default type when `type` is omitted.
const defaultDiskType = "pd-standard"

// errUnpricedGCPDisk marks a disk we can't price (unknown type or a size the
// plan doesn't carry). The dispatcher turns it into a Skipped change; the
// "unsupported" prefix buckets it with unsupported types in the plan-wide notes.
var errUnpricedGCPDisk = errors.New("unsupported persistent disk not priced")

// EstimateGCPComputeDisk calculates the monthly cost of a standalone
// persistent disk from the embedded static PD price table. Priced at the base
// $/GB-month rate (region variation is not modeled for storage, unlike
// compute); confidence is Medium with the static-table caveat.
func EstimateGCPComputeDisk(attrs *gcp.ComputeDiskAttributes) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateGCPComputeDisk: nil attrs")
	}
	diskType := attrs.Type
	if diskType == "" {
		diskType = defaultDiskType
	}
	if attrs.SizeGB <= 0 {
		return Estimate{}, fmt.Errorf("%w: %s size not in plan", errUnpricedGCPDisk, diskType)
	}
	gbMo, ok := gcpPDGBMonth(diskType)
	if !ok {
		return Estimate{}, fmt.Errorf("%w: unknown disk type %q", errUnpricedGCPDisk, diskType)
	}

	monthly := gbMo * float64(attrs.SizeGB)
	return Estimate{
		MonthlyUSD: monthly,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "Disk", MonthlyUSD: monthly}},
		Confidence: ConfidenceMedium,
		Notes:      []string{"Priced from a static GCP price table (may drift from current list price)"},
	}, nil
}
