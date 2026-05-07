package aws

// NATGatewayAttributes captures the cost-impacting fields of an
// aws_nat_gateway resource.
//
// NAT Gateways are notorious in cost-review meetings because they have
// two charges that surprise people: a flat per-hour fee (~$32/month each,
// 24x7) plus per-GB data processing on every byte that flows through.
// A PR that adds even one of these to a private subnet should produce a
// loud signal in the diff comment.
type NATGatewayAttributes struct {
	// SubnetID is the subnet the gateway lives in. Required because
	// downstream pricing infers the AZ (and hence the region) from the
	// subnet — NAT pricing is regional. We don't fetch the subnet's AZ
	// here; that's the diff/pricing engine's job.
	SubnetID string

	// ConnectivityType is "public" or "private". Public is the default
	// and is what most teams use; "private" is for cross-VPC private
	// access without internet egress and has a different price structure.
	// Defaults to "public" when absent — matching the AWS provider default.
	ConnectivityType string
}

// ExtractNATGateway reads cost-impacting attributes from an aws_nat_gateway
// attribute map.
//
// Required: subnet_id. Defaults: connectivity_type="public".
func ExtractNATGateway(attrs map[string]interface{}) (*NATGatewayAttributes, error) {
	const typ = "aws_nat_gateway"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}
	wrap := func(err error) error { return wrapAttr(typ, err) }

	subnetID, present, err := getString(attrs, "subnet_id")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "subnet_id")
	}

	connType, _, err := getString(attrs, "connectivity_type")
	if err != nil {
		return nil, wrap(err)
	}
	if connType == "" {
		connType = "public"
	}

	return &NATGatewayAttributes{
		SubnetID:         subnetID,
		ConnectivityType: connType,
	}, nil
}
