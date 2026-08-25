package gcp

// ComputeInstanceAttributes captures the cost-impacting fields of a
// google_compute_instance. Non-pricing fields (tags, network config, metadata)
// are deliberately excluded.
type ComputeInstanceAttributes struct {
	// MachineType is the short machine-type name, e.g. "e2-standard-4". Required.
	// Terraform may render it as a full self-link URL; the extractor reduces it
	// to the trailing name.
	MachineType string

	// Region is derived from the instance zone ("us-central1-a" → "us-central1").
	// Empty when the plan omits the zone, in which case pricing falls back to the
	// plan-wide --region.
	Region string

	// Preemptible reports whether the instance is a Spot/preemptible VM, from
	// either scheduling.preemptible=true or scheduling.provisioning_model="SPOT".
	// Spot pricing is 60–91% below on-demand and varies, so the estimator prices
	// it at on-demand as a labeled upper bound.
	Preemptible bool

	// BootDiskSizeGB is the boot disk size in GB from
	// boot_disk.initialize_params.size. Zero when the plan doesn't specify it
	// (GCP then uses the source image's default, commonly 10 GB).
	BootDiskSizeGB int

	// BootDiskType is the boot disk type ("pd-standard", "pd-balanced",
	// "pd-ssd"). Empty when unspecified; the estimator defaults to pd-balanced,
	// the google_compute_instance default.
	BootDiskType string
}

// ExtractComputeInstance reads cost-impacting attributes from a
// google_compute_instance attribute map.
//
// Required: machine_type. Optional: zone (→ Region), scheduling block
// (→ Preemptible), boot_disk.initialize_params (→ size/type). Unknown
// attributes are ignored so Terraform version drift doesn't break extraction.
func ExtractComputeInstance(attrs map[string]interface{}) (*ComputeInstanceAttributes, error) {
	const typ = "google_compute_instance"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	machineType, present, err := getString(attrs, "machine_type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "machine_type")
	}

	zone, _, err := getString(attrs, "zone")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	out := &ComputeInstanceAttributes{
		MachineType: lastPathSegment(machineType),
		Region:      regionFromZone(lastPathSegment(zone)),
	}

	// scheduling is a nested block; preemptible VMs set either the legacy
	// `preemptible` bool or the newer `provisioning_model = "SPOT"`.
	sched, present, err := getNestedFirst(attrs, "scheduling")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if present {
		preempt, _, err := getBool(sched, "preemptible")
		if err != nil {
			return nil, wrapAttr(typ+".scheduling", err)
		}
		model, _, err := getString(sched, "provisioning_model")
		if err != nil {
			return nil, wrapAttr(typ+".scheduling", err)
		}
		out.Preemptible = preempt || model == "SPOT"
	}

	// boot_disk → initialize_params holds the size and type.
	bootDisk, present, err := getNestedFirst(attrs, "boot_disk")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if present {
		params, present, err := getNestedFirst(bootDisk, "initialize_params")
		if err != nil {
			return nil, wrapAttr(typ+".boot_disk", err)
		}
		if present {
			size, _, err := getInt(params, "size")
			if err != nil {
				return nil, wrapAttr(typ+".boot_disk.initialize_params", err)
			}
			out.BootDiskSizeGB = size

			diskType, _, err := getString(params, "type")
			if err != nil {
				return nil, wrapAttr(typ+".boot_disk.initialize_params", err)
			}
			out.BootDiskType = diskType
		}
	}

	return out, nil
}
