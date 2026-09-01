package azure

// ManagedDiskAttributes captures the cost-impacting fields of a standalone
// azurerm_managed_disk.
type ManagedDiskAttributes struct {
	// StorageAccountType is the disk SKU ("Standard_LRS", "StandardSSD_LRS",
	// "Premium_LRS", ...). Required.
	StorageAccountType string

	// SizeGB is disk_size_gb. Required for a standalone disk; a missing size
	// routes to a Skipped estimate downstream.
	SizeGB int

	// Location is the Azure region. Empty falls back to the plan-wide --region.
	Location string
}

// ExtractManagedDisk reads cost-impacting attributes from an
// azurerm_managed_disk attribute map. Required: storage_account_type. `size`
// is optional at the extractor level; a missing size routes to a Skipped
// estimate downstream (mirrors the gcp compute_disk extractor).
func ExtractManagedDisk(attrs map[string]interface{}) (*ManagedDiskAttributes, error) {
	const typ = "azurerm_managed_disk"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	accountType, present, err := getString(attrs, "storage_account_type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "storage_account_type")
	}

	size, _, err := getInt(attrs, "disk_size_gb")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	location, _, err := getString(attrs, "location")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	return &ManagedDiskAttributes{
		StorageAccountType: accountType,
		SizeGB:             size,
		Location:           location,
	}, nil
}
