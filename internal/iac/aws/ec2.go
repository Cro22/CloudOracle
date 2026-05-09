package aws

// EC2Attributes captures the cost-impacting fields of an aws_instance
// resource. We deliberately exclude attributes that don't affect price
// (tags, security_groups, key_name, etc.) so the struct stays focused on
// what the pricing engine cares about.
type EC2Attributes struct {
	// InstanceType is the EC2 instance class, e.g. "t3.large", "m5.xlarge".
	// Required.
	InstanceType string

	// AvailabilityZone is the AZ the instance is launched into, e.g.
	// "us-east-1a". Optional — when missing, region-level pricing applies.
	AvailabilityZone string

	// Tenancy controls whether the instance shares hardware. Defaults to
	// "default"; other valid values are "dedicated" and "host" — both have
	// significant pricing implications.
	Tenancy string

	// EBSOptimized indicates whether the instance has dedicated EBS
	// throughput. Defaults to false. Some instance families have it
	// enabled implicitly, but we track only the explicit attribute here.
	EBSOptimized bool

	// RootBlockSize is the size of the root volume in GB, drawn from the
	// first root_block_device entry. Zero when the block isn't specified
	// (in which case AWS applies the AMI default).
	RootBlockSize int

	// RootBlockType is the volume type of the root device (e.g. "gp3").
	// Empty when the block isn't specified.
	RootBlockType string
}

// ExtractEC2 reads cost-impacting attributes from an aws_instance attribute
// map (typically resource_changes[].change.after for a create, or
// resource_changes[].change.before for a delete).
//
// Required: instance_type. Defaults: tenancy="default", ebs_optimized=false.
//
// Unknown attributes are ignored. Terraform adds and renames fields between
// versions, so a strict allow-list would force a parser update on every
// minor Terraform release.
func ExtractEC2(attrs map[string]interface{}) (*EC2Attributes, error) {
	const typ = "aws_instance"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	instanceType, present, err := getString(attrs, "instance_type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "instance_type")
	}

	az, _, err := getString(attrs, "availability_zone")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	tenancy, _, err := getString(attrs, "tenancy")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if tenancy == "" {
		tenancy = "default"
	}

	ebsOpt, _, err := getBool(attrs, "ebs_optimized")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	out := &EC2Attributes{
		InstanceType:     instanceType,
		AvailabilityZone: az,
		Tenancy:          tenancy,
		EBSOptimized:     ebsOpt,
	}

	// root_block_device is an HCL block but the JSON plan renders it as a
	// list, even though only one entry is allowed. Pull the first entry
	// when present and read its size and type.
	rbd, present, err := getNestedFirst(attrs, "root_block_device")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if present {
		size, _, err := getInt(rbd, "volume_size")
		if err != nil {
			return nil, wrapAttr(typ+".root_block_device", err)
		}
		out.RootBlockSize = size

		volType, _, err := getString(rbd, "volume_type")
		if err != nil {
			return nil, wrapAttr(typ+".root_block_device", err)
		}
		out.RootBlockType = volType
	}

	return out, nil
}
