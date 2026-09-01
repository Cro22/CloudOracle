package pricing

import (
	"errors"
	"testing"

	"CloudOracle/internal/iac/azure"
)

func TestEstimateAzureDisk_PricesByGB(t *testing.T) {
	est, err := EstimateAzureManagedDisk(&azure.ManagedDiskAttributes{
		StorageAccountType: "StandardSSD_LRS",
		SizeGB:             256,
	})
	if err != nil {
		t.Fatalf("EstimateAzureManagedDisk: %v", err)
	}
	want := 0.075 * 256
	if !approxEq(est.MonthlyUSD, want) {
		t.Errorf("MonthlyUSD = %.4f, want %.4f", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
}

func TestEstimateAzureDisk_MissingSizeUnpriced(t *testing.T) {
	_, err := EstimateAzureManagedDisk(&azure.ManagedDiskAttributes{
		StorageAccountType: "Premium_LRS",
	})
	if !errors.Is(err, errUnpricedAzureDisk) {
		t.Errorf("err = %v, want errUnpricedAzureDisk for missing size", err)
	}
}

func TestEstimateAzureDisk_UnknownSKUUnpriced(t *testing.T) {
	_, err := EstimateAzureManagedDisk(&azure.ManagedDiskAttributes{
		StorageAccountType: "Exotic_LRS",
		SizeGB:             100,
	})
	if !errors.Is(err, errUnpricedAzureDisk) {
		t.Errorf("err = %v, want errUnpricedAzureDisk for unknown SKU", err)
	}
}
