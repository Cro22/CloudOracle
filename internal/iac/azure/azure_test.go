package azure

import "testing"

func TestExtract_DispatchesAndSkipsUnknown(t *testing.T) {
	vm, err := Extract("azurerm_linux_virtual_machine", map[string]interface{}{"size": "Standard_D2s_v5"})
	if err != nil {
		t.Fatalf("Extract VM: %v", err)
	}
	if vm == nil || vm.VirtualMachine == nil {
		t.Fatalf("Extract VM = %+v, want VirtualMachine set", vm)
	}

	disk, err := Extract("azurerm_managed_disk", map[string]interface{}{"storage_account_type": "Premium_LRS"})
	if err != nil {
		t.Fatalf("Extract disk: %v", err)
	}
	if disk == nil || disk.ManagedDisk == nil {
		t.Fatalf("Extract disk = %+v, want ManagedDisk set", disk)
	}

	// Unsupported type → (nil, nil): "no data" means "no cost impact".
	other, err := Extract("azurerm_resource_group", map[string]interface{}{"name": "rg"})
	if err != nil || other != nil {
		t.Errorf("Extract unsupported = (%+v, %v), want (nil, nil)", other, err)
	}
}

func TestExtractVirtualMachine_Fields(t *testing.T) {
	vm, err := ExtractVirtualMachine(map[string]interface{}{
		"size":     "Standard_D4s_v5",
		"location": "westeurope",
		"priority": "Spot",
		"os_disk": []interface{}{map[string]interface{}{
			"storage_account_type": "Premium_LRS",
			"disk_size_gb":         float64(128),
		}},
	})
	if err != nil {
		t.Fatalf("ExtractVirtualMachine: %v", err)
	}
	if vm.Size != "Standard_D4s_v5" || vm.Location != "westeurope" || !vm.Spot {
		t.Errorf("core fields = %+v", vm)
	}
	if vm.OSDiskType != "Premium_LRS" || vm.OSDiskSizeGB != 128 {
		t.Errorf("os_disk fields = type %q size %d", vm.OSDiskType, vm.OSDiskSizeGB)
	}
}

func TestExtractVirtualMachine_SizeRequired(t *testing.T) {
	if _, err := ExtractVirtualMachine(map[string]interface{}{"location": "eastus"}); err == nil {
		t.Error("want error when size is missing")
	}
}

func TestExtractManagedDisk_Fields(t *testing.T) {
	d, err := ExtractManagedDisk(map[string]interface{}{
		"storage_account_type": "StandardSSD_LRS",
		"disk_size_gb":         float64(256),
		"location":             "eastus",
	})
	if err != nil {
		t.Fatalf("ExtractManagedDisk: %v", err)
	}
	if d.StorageAccountType != "StandardSSD_LRS" || d.SizeGB != 256 || d.Location != "eastus" {
		t.Errorf("fields = %+v", d)
	}
}

func TestExtractManagedDisk_AccountTypeRequired(t *testing.T) {
	if _, err := ExtractManagedDisk(map[string]interface{}{"disk_size_gb": float64(100)}); err == nil {
		t.Error("want error when storage_account_type is missing")
	}
}

func TestSupportedTypes(t *testing.T) {
	got := SupportedTypes()
	if len(got) != 2 {
		t.Fatalf("SupportedTypes() = %v, want 2 entries", got)
	}
}
