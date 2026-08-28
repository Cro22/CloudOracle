package gcp

// CloudFunctionAttributes captures the cost-impacting fields of a Cloud
// Function — both google_cloudfunctions_function (gen1) and
// google_cloudfunctions2_function (gen2, billed as Cloud Run).
//
// Like AWS Lambda, a function's cost is invocation-driven: per-request plus
// per-GB-second of execution time, neither of which a Terraform plan declares.
// The plan only carries the standing shape (runtime, memory), so there is no
// reliable standing cost to compute — the estimator reports $0 with a caveat,
// mirroring the Lambda estimator's no-provisioned-concurrency path. Runtime is
// kept purely as context for the diff note.
type CloudFunctionAttributes struct {
	// Name is the function's logical name. Required.
	Name string

	// Runtime is e.g. "python312" or "nodejs20". Optional; not used for
	// pricing, recorded as context for diff messages.
	Runtime string
}

// ExtractCloudFunction reads cost-impacting attributes from a Cloud Function
// attribute map (gen1 or gen2). Required: name. Runtime is read from the
// top-level `runtime` (gen1) and falls back to the nested build_config block
// (gen2); its absence is not an error.
func ExtractCloudFunction(attrs map[string]interface{}) (*CloudFunctionAttributes, error) {
	const typ = "google_cloudfunctions_function"
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

	runtime, present, err := getString(attrs, "runtime")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		// gen2 nests runtime under build_config { runtime = ... }.
		if bc, ok, berr := getNestedFirst(attrs, "build_config"); berr == nil && ok {
			runtime, _, _ = getString(bc, "runtime")
		}
	}

	return &CloudFunctionAttributes{Name: name, Runtime: runtime}, nil
}
