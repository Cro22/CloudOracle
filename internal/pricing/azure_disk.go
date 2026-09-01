package pricing

import (
	"errors"
	"fmt"

	"CloudOracle/internal/iac/azure"
)

// errUnpricedAzureDisk marks a managed disk we can't price (unknown SKU or a
// size the plan doesn't carry). The dispatcher turns it into a Skipped change;
// the "unsupported" prefix buckets it with unsupported types in the plan notes.
var errUnpricedAzureDisk = errors.New("unsupported managed disk not priced")

// EstimateAzureManagedDisk calculates the monthly cost of a standalone managed
// disk from the embedded static price table. Priced at the base $/GB-month rate
// (region variation is not modeled for storage, like the GCP arm); confidence
// is Medium with the static-table caveat.
//
// ponytail: Premium (P-tier) and Ultra disks actually bill by provisioned tier,
// not linearly by GB, so their per-GB rate here is an approximation. Standard
// HDD/SSD bill ~linearly and are accurate. Swap in the Retail Prices API's
// per-tier SKUs if Premium/Ultra accuracy ever matters.
func EstimateAzureManagedDisk(attrs *azure.ManagedDiskAttributes) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateAzureManagedDisk: nil attrs")
	}
	if attrs.StorageAccountType == "" {
		return Estimate{}, fmt.Errorf("EstimateAzureManagedDisk: empty StorageAccountType")
	}
	if attrs.SizeGB <= 0 {
		return Estimate{}, fmt.Errorf("%w: %s size not in plan", errUnpricedAzureDisk, attrs.StorageAccountType)
	}
	gbMo, ok := azureDiskGBMonth(attrs.StorageAccountType)
	if !ok {
		return Estimate{}, fmt.Errorf("%w: unknown SKU %q", errUnpricedAzureDisk, attrs.StorageAccountType)
	}

	monthly := gbMo * float64(attrs.SizeGB)
	return Estimate{
		MonthlyUSD: monthly,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "Disk", MonthlyUSD: monthly}},
		Confidence: ConfidenceMedium,
		Notes:      []string{"Priced from a static Azure price table (may drift from current list price)"},
	}, nil
}
