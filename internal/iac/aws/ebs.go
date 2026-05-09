package aws

// EBSAttributes captures the cost-impacting fields of an aws_ebs_volume
// resource (standalone volumes — root volumes attached to EC2 instances
// are tracked under EC2Attributes.RootBlock*).
type EBSAttributes struct {
	// Type is the volume type: gp2, gp3, io1, io2, st1, sc1, or "standard"
	// for the legacy magnetic volumes. Required. Cross-attribute validation
	// (e.g. "io1 must have iops") is intentionally deferred — that's a
	// pricing-engine concern; here we just surface what Terraform declared.
	Type string

	// Size is the volume size in GB. Required.
	Size int

	// Iops is the provisioned IOPS. Optional — required by io1/io2 and
	// allowed for gp3, but we don't enforce that here.
	Iops int

	// Throughput is the provisioned throughput in MB/s. Optional, only
	// meaningful for gp3.
	Throughput int

	// AvailabilityZone is the AZ the volume lives in. Optional — pricing
	// for EBS is regional, not AZ-specific, but the attribute is included
	// so callers can match the volume to its instance's AZ if needed.
	AvailabilityZone string

	// Encrypted indicates whether the volume is encrypted at rest.
	// Defaults to false. Encryption itself is free; the attribute is
	// captured because some pricing analyses do compliance scoring.
	Encrypted bool
}

// ExtractEBS reads cost-impacting attributes from an aws_ebs_volume
// attribute map.
//
// Required: type, size. Defaults: iops=0, throughput=0, encrypted=false.
//
// We don't validate cross-attribute consistency (gp3 with/without
// throughput, io1 with/without iops) — that's the pricing engine's job.
// Here we report exactly what Terraform declared.
func ExtractEBS(attrs map[string]interface{}) (*EBSAttributes, error) {
	const typ = "aws_ebs_volume"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	volType, present, err := getString(attrs, "type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "type")
	}

	size, present, err := getInt(attrs, "size")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "size")
	}

	iops, _, err := getInt(attrs, "iops")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	throughput, _, err := getInt(attrs, "throughput")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	az, _, err := getString(attrs, "availability_zone")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	encrypted, _, err := getBool(attrs, "encrypted")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	return &EBSAttributes{
		Type:             volType,
		Size:             size,
		Iops:             iops,
		Throughput:       throughput,
		AvailabilityZone: az,
		Encrypted:        encrypted,
	}, nil
}
