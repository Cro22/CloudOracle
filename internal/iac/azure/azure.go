// Package azure extracts strongly-typed cost-impacting attributes from
// Terraform plan resource changes for Microsoft Azure resources. It is the
// Azure counterpart to internal/iac/aws and internal/iac/gcp and follows the
// same contract: each ExtractXxx reads a map[string]interface{} (the shape of
// Change.Before / Change.After) and returns a typed attribute struct the
// pricing package consumes.
//
// Currently supported: azurerm_linux_virtual_machine and azurerm_managed_disk,
// the two resources that dominate Azure spend in most plans.
package azure

// ResourceAttributes is a discriminated union over the Azure resource types
// this package supports. Exactly one inner pointer is non-nil; Type identifies
// which. Mirrors internal/iac/{aws,gcp}.ResourceAttributes so the pricing
// dispatcher switches on the inner pointer the same way for every provider.
type ResourceAttributes struct {
	Type           string
	VirtualMachine *VirtualMachineAttributes
	ManagedDisk    *ManagedDiskAttributes
}

// Extract dispatches to the type-specific extractor for resourceType.
//
// Unsupported types return (nil, nil): the caller treats "no data" as "no cost
// impact", exactly as the aws/gcp extractors do — a real plan is full of Azure
// types we don't price (resource groups, VNets, NSGs). Extraction failures on
// supported types return (nil, error).
func Extract(resourceType string, attrs map[string]interface{}) (*ResourceAttributes, error) {
	switch resourceType {
	case "azurerm_linux_virtual_machine":
		vm, err := ExtractVirtualMachine(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, VirtualMachine: vm}, nil
	case "azurerm_managed_disk":
		d, err := ExtractManagedDisk(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, ManagedDisk: d}, nil
	default:
		return nil, nil
	}
}

// SupportedTypes returns the Azure resource types this package can extract, for
// docs and the pr-check "unsupported" diagnostics.
func SupportedTypes() []string {
	return []string{
		"azurerm_linux_virtual_machine",
		"azurerm_managed_disk",
	}
}
