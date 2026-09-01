package azure

// VirtualMachineAttributes captures the cost-impacting fields of an
// azurerm_linux_virtual_machine. Non-pricing fields (tags, network interfaces,
// admin credentials) are deliberately excluded.
type VirtualMachineAttributes struct {
	// Size is the VM SKU, e.g. "Standard_D2s_v5". Required.
	Size string

	// Location is the Azure region, e.g. "eastus". Empty when the plan omits it,
	// in which case pricing falls back to the plan-wide --region.
	Location string

	// Spot reports whether this is a Spot VM (priority = "Spot"). Spot pricing is
	// well below pay-as-you-go and varies, so the estimator prices it at
	// pay-as-you-go as a labeled upper bound.
	Spot bool

	// OSDiskType is the os_disk.storage_account_type ("Standard_LRS",
	// "StandardSSD_LRS", "Premium_LRS"). Empty when unspecified.
	OSDiskType string

	// OSDiskSizeGB is os_disk.disk_size_gb. Zero when the plan omits it (Azure
	// then uses the image's default disk size).
	OSDiskSizeGB int
}

// ExtractVirtualMachine reads cost-impacting attributes from an
// azurerm_linux_virtual_machine attribute map.
//
// Required: size. Optional: location, priority (→ Spot), os_disk block
// (→ OSDiskType/OSDiskSizeGB). Unknown attributes are ignored so Terraform
// version drift doesn't break extraction.
func ExtractVirtualMachine(attrs map[string]interface{}) (*VirtualMachineAttributes, error) {
	const typ = "azurerm_linux_virtual_machine"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	size, present, err := getString(attrs, "size")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "size")
	}

	location, _, err := getString(attrs, "location")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	priority, _, err := getString(attrs, "priority")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	out := &VirtualMachineAttributes{
		Size:     size,
		Location: location,
		Spot:     priority == "Spot",
	}

	// os_disk is a nested block carrying the managed OS disk's SKU and size.
	osDisk, present, err := getNestedFirst(attrs, "os_disk")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if present {
		diskType, _, err := getString(osDisk, "storage_account_type")
		if err != nil {
			return nil, wrapAttr(typ+".os_disk", err)
		}
		out.OSDiskType = diskType

		size, _, err := getInt(osDisk, "disk_size_gb")
		if err != nil {
			return nil, wrapAttr(typ+".os_disk", err)
		}
		out.OSDiskSizeGB = size
	}

	return out, nil
}
