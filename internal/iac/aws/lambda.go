package aws

// LambdaAttributes captures the cost-impacting fields of an
// aws_lambda_function resource.
//
// Note: Lambda's main cost is invocation-based — per-request charges and
// per-GB-second compute time. The plan only declares the *standing* shape
// (memory, runtime, provisioned concurrency); we don't try to predict
// invocation volume here. The pricing engine in a later milestone will
// combine these attributes with usage assumptions or historical data.
type LambdaAttributes struct {
	// FunctionName is the function's logical name. Required.
	FunctionName string

	// Runtime is e.g. "python3.12" or "nodejs20.x". Optional and not used
	// for pricing — we record it as context for diff messages and logs.
	Runtime string

	// MemorySize is the configured memory in MB. AWS scales CPU
	// proportionally, so this is the dominant per-invocation cost lever.
	// Defaults to 128 MB (Lambda's API default) when absent.
	MemorySize int

	// Timeout is the maximum execution time in seconds. Doesn't affect
	// per-second pricing, but it does cap the per-invocation cost ceiling.
	// Defaults to 3 seconds (Lambda's API default).
	Timeout int

	// Architecture is "x86_64" or "arm64". arm64 is ~20% cheaper. Defaults
	// to "x86_64" when absent. Validation of the value is intentionally
	// deferred to the pricing engine.
	Architecture string

	// ProvisionedConcurrency is the count of always-warm executions. Each
	// one carries a flat per-hour fee on top of any invocation charges,
	// so a non-zero value is what makes Lambda cost predictable from the
	// plan alone. Zero means on-demand only.
	ProvisionedConcurrency int
}

// ExtractLambda reads cost-impacting attributes from an aws_lambda_function
// attribute map.
//
// Required: function_name. Defaults: memory_size=128, timeout=3,
// architecture="x86_64", provisioned_concurrency=0.
func ExtractLambda(attrs map[string]interface{}) (*LambdaAttributes, error) {
	const typ = "aws_lambda_function"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}
	wrap := func(err error) error { return wrapAttr(typ, err) }

	functionName, present, err := getString(attrs, "function_name")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "function_name")
	}

	runtime, _, err := getString(attrs, "runtime")
	if err != nil {
		return nil, wrap(err)
	}

	memSize, present, err := getInt(attrs, "memory_size")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		memSize = 128
	}

	timeout, present, err := getInt(attrs, "timeout")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		timeout = 3
	}

	// `architectures` is a single-element list in the plan JSON even though
	// the API only accepts one architecture per function. The extractor
	// reports the first element. Empty/missing lists fall back to x86_64,
	// which matches Lambda's default. Value validation (must be x86_64
	// or arm64) is a pricing concern, not a parse concern.
	archList, _, err := getStringList(attrs, "architectures")
	if err != nil {
		return nil, wrap(err)
	}
	architecture := "x86_64"
	if len(archList) > 0 {
		architecture = archList[0]
	}

	// In newer aws provider versions, provisioned concurrency moved into
	// a sub-block (provisioned_concurrency_config { … }). This extractor
	// reads only the top-level provisioned_concurrent_executions field;
	// extending to the sub-block path is intentionally a future task —
	// it would couple this milestone to provider-version detection logic
	// we don't need yet.
	provisioned, _, err := getInt(attrs, "provisioned_concurrent_executions")
	if err != nil {
		return nil, wrap(err)
	}

	return &LambdaAttributes{
		FunctionName:           functionName,
		Runtime:                runtime,
		MemorySize:             memSize,
		Timeout:                timeout,
		Architecture:           architecture,
		ProvisionedConcurrency: provisioned,
	}, nil
}
