package gcp

// ComputeDiskAttributes captures the cost-impacting fields of a
// google_compute_disk (a standalone zonal persistent disk).
type ComputeDiskAttributes struct {
	// Type is the PD type ("pd-standard", "pd-balanced", "pd-ssd", "pd-extreme").
	// Empty when unspecified; the estimator defaults to pd-standard, the
	// google_compute_disk default.
	Type string

	// SizeGB is the disk size in GB. Zero when the plan doesn't carry it (the
	// size is computed from an image/snapshot); the estimator then skips it.
	SizeGB int
}

// ExtractComputeDisk reads cost-impacting attributes from a google_compute_disk
// attribute map. Only `type` and `size` affect price. Both are optional at the
// extractor level; a missing size routes to a Skipped estimate downstream.
func ExtractComputeDisk(attrs map[string]interface{}) (*ComputeDiskAttributes, error) {
	const typ = "google_compute_disk"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	diskType, _, err := getString(attrs, "type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	size, _, err := getInt(attrs, "size")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	return &ComputeDiskAttributes{Type: diskType, SizeGB: size}, nil
}
