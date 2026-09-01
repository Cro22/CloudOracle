package pricing

import (
	"errors"
	"testing"

	"CloudOracle/internal/iac/azure"
)

func TestEstimateAzureVM_ComputePlusOSDisk(t *testing.T) {
	est, err := EstimateAzureVirtualMachine(&azure.VirtualMachineAttributes{
		Size:         "Standard_D4s_v5",
		Location:     "eastus",
		OSDiskType:   "Premium_LRS",
		OSDiskSizeGB: 100,
	}, "eastus")
	if err != nil {
		t.Fatalf("EstimateAzureVirtualMachine: %v", err)
	}
	// 0.192/hr * 730 = 140.16 compute; 0.135 * 100 = 13.5 disk.
	wantCompute := 0.192 * HoursPerMonth
	wantDisk := 0.135 * 100
	if !approxEq(est.MonthlyUSD, wantCompute+wantDisk) {
		t.Errorf("MonthlyUSD = %.4f, want %.4f", est.MonthlyUSD, wantCompute+wantDisk)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium (known size + region)", est.Confidence)
	}
	if len(est.Breakdown) != 2 || est.Breakdown[0].Component != "Compute" || est.Breakdown[1].Component != "OSDisk" {
		t.Errorf("Breakdown = %+v, want [Compute OSDisk]", est.Breakdown)
	}
}

func TestEstimateAzureVM_UnknownRegionLowersConfidence(t *testing.T) {
	base := azurePrices.VMHourlyUSD["Standard_D2s_v5"]
	est, err := EstimateAzureVirtualMachine(&azure.VirtualMachineAttributes{
		Size:     "Standard_D2s_v5",
		Location: "mars-central1",
	}, "mars-central1")
	if err != nil {
		t.Fatalf("EstimateAzureVirtualMachine: %v", err)
	}
	// Unknown region → 1.0 multiplier → base rate, and Low confidence.
	if !approxEq(est.MonthlyUSD, base*HoursPerMonth) {
		t.Errorf("MonthlyUSD = %.4f, want base rate %.4f", est.MonthlyUSD, base*HoursPerMonth)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low for unknown region", est.Confidence)
	}
}

func TestEstimateAzureVM_RegionMultiplierApplied(t *testing.T) {
	base := azurePrices.VMHourlyUSD["Standard_D2s_v5"]
	mult := azurePrices.RegionMultiplier["westeurope"] // 1.10
	est, err := EstimateAzureVirtualMachine(&azure.VirtualMachineAttributes{
		Size:     "Standard_D2s_v5",
		Location: "westeurope",
	}, "eastus")
	if err != nil {
		t.Fatalf("EstimateAzureVirtualMachine: %v", err)
	}
	if !approxEq(est.MonthlyUSD, base*mult*HoursPerMonth) {
		t.Errorf("MonthlyUSD = %.4f, want %.4f (base*%.2f)", est.MonthlyUSD, base*mult*HoursPerMonth, mult)
	}
}

func TestEstimateAzureVM_SpotIsLowConfidence(t *testing.T) {
	est, err := EstimateAzureVirtualMachine(&azure.VirtualMachineAttributes{
		Size:     "Standard_D2s_v5",
		Location: "eastus",
		Spot:     true,
	}, "eastus")
	if err != nil {
		t.Fatalf("EstimateAzureVirtualMachine: %v", err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low for Spot VM", est.Confidence)
	}
}

func TestEstimateAzureVM_UnknownSizeErrsUnpriced(t *testing.T) {
	_, err := EstimateAzureVirtualMachine(&azure.VirtualMachineAttributes{
		Size:     "Standard_Z999",
		Location: "eastus",
	}, "eastus")
	if !errors.Is(err, errUnpricedAzureVMSize) {
		t.Errorf("err = %v, want errUnpricedAzureVMSize", err)
	}
}
