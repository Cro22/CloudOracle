// Package gcp extracts strongly-typed cost-impacting attributes from
// Terraform plan resource changes for Google Cloud resources. It is the GCP
// counterpart to internal/iac/aws and follows the same contract: each
// ExtractXxx reads a map[string]interface{} (the shape of Change.Before /
// Change.After) and returns a typed attribute struct the pricing package
// consumes.
//
// Currently supported: google_compute_instance. Persistent disks and Cloud SQL
// are added in subsequent milestones.
package gcp

// ResourceAttributes is a discriminated union over the GCP resource types this
// package supports. Exactly one inner pointer is non-nil; Type identifies which.
// Mirrors internal/iac/aws.ResourceAttributes so the pricing dispatcher can
// switch on the inner pointer the same way for both providers.
type ResourceAttributes struct {
	Type            string
	ComputeInstance *ComputeInstanceAttributes
	ComputeDisk     *ComputeDiskAttributes
}

// Extract dispatches to the type-specific extractor for resourceType.
//
// Unsupported types return (nil, nil): the caller treats "no data" as "no cost
// impact", exactly as the aws extractor does — a real plan is full of GCP types
// we don't price (IAM, VPCs, DNS records). Extraction failures on supported
// types return (nil, error).
func Extract(resourceType string, attrs map[string]interface{}) (*ResourceAttributes, error) {
	switch resourceType {
	case "google_compute_instance":
		ci, err := ExtractComputeInstance(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, ComputeInstance: ci}, nil
	case "google_compute_disk":
		cd, err := ExtractComputeDisk(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, ComputeDisk: cd}, nil
	default:
		return nil, nil
	}
}

// SupportedTypes returns the GCP resource types this package can extract, for
// docs and the pr-check "unsupported" diagnostics.
func SupportedTypes() []string {
	return []string{"google_compute_instance", "google_compute_disk"}
}
