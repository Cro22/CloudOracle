package gcp

// RouterNATAttributes captures the cost-impacting fields of a
// google_compute_router_nat (Cloud NAT gateway).
//
// Cloud NAT is the GCP counterpart to an AWS NAT Gateway and, like it, has
// charges that surprise people in cost reviews: a per-VM uptime fee (billed by
// the hour for every instance that uses the gateway, capped at 32 VMs), a
// per-external-IP hourly fee, and per-GiB data processing on all traffic. Only
// the external-IP fee is estimable from a plan — VM count and traffic aren't in
// it — so a PR adding one should still surface with a loud caveat.
type RouterNATAttributes struct {
	// Name is the gateway's logical name. Required.
	Name string

	// AllocateOption is "AUTO_ONLY" (GCP allocates NAT IPs on demand) or
	// "MANUAL_ONLY" (you list them in nat_ips). Empty when the plan omits it.
	AllocateOption string

	// ManualIPCount is len(nat_ips): the number of external IPs the gateway
	// reserves under MANUAL_ONLY. Zero for AUTO_ONLY, where the count scales
	// with load and isn't knowable from the plan.
	ManualIPCount int
}

// ExtractRouterNAT reads cost-impacting attributes from a
// google_compute_router_nat attribute map.
//
// Required: name. Optional: nat_ip_allocate_option, nat_ips.
func ExtractRouterNAT(attrs map[string]interface{}) (*RouterNATAttributes, error) {
	const typ = "google_compute_router_nat"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}
	wrap := func(err error) error { return wrapAttr(typ, err) }

	name, present, err := getString(attrs, "name")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "name")
	}

	allocate, _, err := getString(attrs, "nat_ip_allocate_option")
	if err != nil {
		return nil, wrap(err)
	}

	natIPs, _, err := getStringList(attrs, "nat_ips")
	if err != nil {
		return nil, wrap(err)
	}

	return &RouterNATAttributes{
		Name:           name,
		AllocateOption: allocate,
		ManualIPCount:  len(natIPs),
	}, nil
}
