package pricing

import (
	"errors"
	"fmt"

	"CloudOracle/internal/iac/azure"
)

// defaultAzureOSDiskType is the SKU assumed when an azurerm_linux_virtual_machine
// carries an os_disk size but no storage_account_type (rare — the block requires
// it — but handled defensively).
const defaultAzureOSDiskType = "Standard_LRS"

// errUnpricedAzureVMSize marks a VM size absent from the static price table. The
// dispatcher turns it into a Skipped change (the "unsupported" substring buckets
// it with unsupported types in the plan-wide notes) rather than a hard error.
var errUnpricedAzureVMSize = errors.New("unsupported VM size not in static price table")

// EstimateAzureVirtualMachine calculates the monthly cost of an
// azurerm_linux_virtual_machine from the embedded static price table. Like the
// GCP estimators it makes no API call, so it takes no productGetter.
//
// planRegion is the plan-wide --region used when the VM's own location was
// absent from the plan (attrs.Location empty).
//
// The estimate is capped at ConfidenceMedium (static table, may drift) and
// dropped to ConfidenceLow when the region is not in the multiplier table or
// the VM is Spot (priced at pay-as-you-go as an upper bound).
//
// An unknown VM size returns errUnpricedAzureVMSize so EstimateChange routes it
// to a Skipped result rather than a hard error.
func EstimateAzureVirtualMachine(attrs *azure.VirtualMachineAttributes, planRegion string) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateAzureVirtualMachine: nil attrs")
	}
	if attrs.Size == "" {
		return Estimate{}, fmt.Errorf("EstimateAzureVirtualMachine: empty Size")
	}

	region := attrs.Location
	if region == "" {
		region = planRegion
	}

	hourly, sizeKnown, regionKnown := azureVMHourly(attrs.Size, region)
	if !sizeKnown {
		return Estimate{}, fmt.Errorf("%w: %q", errUnpricedAzureVMSize, attrs.Size)
	}

	compute := hourly * HoursPerMonth
	confidence := ConfidenceMedium
	notes := []string{"Priced from a static Azure price table (may drift from current list price)"}

	if !regionKnown {
		confidence = ConfidenceLow
		notes = append(notes, fmt.Sprintf("Region %q not in price table; used East US base rate", region))
	}
	if attrs.Spot {
		confidence = ConfidenceLow
		notes = append(notes, "Spot VM priced at pay-as-you-go rate (real cost is typically far lower)")
	}

	breakdown := []LineItem{{Component: "Compute", MonthlyUSD: compute}}
	total := compute

	if attrs.OSDiskSizeGB > 0 {
		diskType := attrs.OSDiskType
		if diskType == "" {
			diskType = defaultAzureOSDiskType
		}
		gbMo, ok := azureDiskGBMonth(diskType)
		if !ok {
			return Estimate{}, fmt.Errorf("EstimateAzureVirtualMachine: unknown OS disk type %q", diskType)
		}
		osDisk := gbMo * float64(attrs.OSDiskSizeGB)
		breakdown = append(breakdown, LineItem{Component: "OSDisk", MonthlyUSD: osDisk})
		total += osDisk
	} else {
		notes = append(notes, "OS disk size not in plan, compute-only estimate")
	}

	return Estimate{
		MonthlyUSD: total,
		Currency:   "USD",
		Breakdown:  breakdown,
		Confidence: confidence,
		Notes:      notes,
	}, nil
}
