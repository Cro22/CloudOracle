package pricing

import (
	"context"
	"testing"

	"CloudOracle/internal/iac"
)

// The Azure path prices from the static table and never calls the productGetter,
// so these dispatch tests pass a nil src.

func TestEstimateChange_CreateAzureVM(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "azurerm_linux_virtual_machine.web",
		Mode:    "managed",
		Type:    "azurerm_linux_virtual_machine",
		Change: iac.Change{
			Actions: []string{"create"},
			After: map[string]interface{}{
				"size":     "Standard_D4s_v5",
				"location": "eastus",
				"os_disk": []interface{}{map[string]interface{}{
					"storage_account_type": "Premium_LRS",
					"disk_size_gb":         float64(100),
				}},
			},
		},
	}
	// region flag is AWS-style; the VM's own location overrides it.
	ce, err := EstimateChange(context.Background(), nil, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	if ce.AfterMonthly != ce.MonthlyDelta || ce.MonthlyDelta <= 0 {
		t.Errorf("create: AfterMonthly (%.2f) should equal MonthlyDelta (%.2f) and be > 0", ce.AfterMonthly, ce.MonthlyDelta)
	}
}

func TestEstimateChange_AzureUnknownSizeSkipped(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "azurerm_linux_virtual_machine.exotic",
		Mode:    "managed",
		Type:    "azurerm_linux_virtual_machine",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"size": "Standard_Z999", "location": "eastus"},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "eastus")
	if err != nil {
		t.Fatalf("EstimateChange returned error, want Skipped: %v", err)
	}
	if !ce.Skipped {
		t.Fatal("Skipped = false, want true for unpriced VM size")
	}
}

func TestEstimateChange_DeleteAzureDiskNegativeDelta(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "azurerm_managed_disk.data",
		Mode:    "managed",
		Type:    "azurerm_managed_disk",
		Change: iac.Change{
			Actions: []string{"delete"},
			Before: map[string]interface{}{
				"storage_account_type": "Standard_LRS",
				"disk_size_gb":         float64(512),
			},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "eastus")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	// delete: BeforeMonthly > 0, delta negative.
	if ce.MonthlyDelta >= 0 || ce.BeforeMonthly <= 0 {
		t.Errorf("delete: want negative delta and positive before, got delta=%.2f before=%.2f", ce.MonthlyDelta, ce.BeforeMonthly)
	}
}
