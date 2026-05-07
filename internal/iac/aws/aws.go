// Package aws extracts strongly-typed cost-impacting attributes from
// Terraform plan resource changes for AWS resources.
//
// Each ExtractXxx function takes a map[string]interface{} (the shape of
// terraform-iac.Change.Before or .After) rather than the full ResourceChange
// from the parser package. That separation is deliberate: extractors don't
// need to know about actions, addresses, or before/after distinctions —
// the diff engine in the next milestone decides which state to extract.
//
// Currently supported: aws_instance, aws_db_instance, aws_ebs_volume.
// Other types (Lambda, NAT, RDS Cluster, EKS, ElastiCache, S3) are added
// in subsequent milestones.
package aws

// ResourceAttributes is a discriminated union over the AWS resource types
// this package supports. Exactly one of EC2, RDS, EBS is non-nil; the
// Type field identifies which.
//
// We use this shape rather than a bare interface{} for two reasons:
//
//  1. Type safety at call sites — pricing code can write `if r.EC2 != nil`
//     and get nil-deref protection from the compiler.
//  2. Exhaustive switches feasible — adding a new type forces every
//     consumer to add a case (or explicitly default), instead of silently
//     dispatching a runtime type assertion that no-ops on the new type.
type ResourceAttributes struct {
	Type string
	EC2  *EC2Attributes
	RDS  *RDSAttributes
	EBS  *EBSAttributes
}

// Extract dispatches to the type-specific extractor for resourceType.
//
// Unsupported resource types return (nil, nil) — the caller treats
// "no data" as "no cost impact for this resource". This is intentional:
// a real Terraform plan contains many resources we don't price (IAM roles,
// VPCs, Route53 records, etc.). Forcing every caller to distinguish
// "unsupported" from "extraction failed" would push complexity outward
// instead of containing it here.
//
// Extraction failures on supported types still return (nil, error).
func Extract(resourceType string, attrs map[string]interface{}) (*ResourceAttributes, error) {
	switch resourceType {
	case "aws_instance":
		ec2, err := ExtractEC2(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, EC2: ec2}, nil
	case "aws_db_instance":
		rds, err := ExtractRDS(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, RDS: rds}, nil
	case "aws_ebs_volume":
		ebs, err := ExtractEBS(attrs)
		if err != nil {
			return nil, err
		}
		return &ResourceAttributes{Type: resourceType, EBS: ebs}, nil
	}
	return nil, nil
}

// SupportedTypes returns the AWS resource types this package can extract
// attributes for. The returned slice is freshly allocated on each call so
// callers can mutate it without affecting future returns. Order is stable
// across calls so it can drive menus or docs without sorting.
func SupportedTypes() []string {
	return []string{"aws_instance", "aws_db_instance", "aws_ebs_volume"}
}
