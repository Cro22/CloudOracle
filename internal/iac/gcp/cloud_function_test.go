package gcp

import "testing"

func TestExtract_DispatchesCloudFunctionGen1(t *testing.T) {
	r, err := Extract("google_cloudfunctions_function", map[string]interface{}{
		"name":    "fn",
		"runtime": "python312",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.CloudFunction == nil || r.CloudFunction.Runtime != "python312" {
		t.Fatalf("dispatch/runtime failed: %+v", r)
	}
}

func TestExtractCloudFunction_Gen2RuntimeFromBuildConfig(t *testing.T) {
	cf, err := ExtractCloudFunction(map[string]interface{}{
		"name": "fn2",
		"build_config": []interface{}{
			map[string]interface{}{"runtime": "nodejs20"},
		},
	})
	if err != nil {
		t.Fatalf("ExtractCloudFunction: %v", err)
	}
	if cf.Runtime != "nodejs20" {
		t.Errorf("Runtime = %q, want nodejs20 (from build_config)", cf.Runtime)
	}
}

func TestExtractCloudFunction_MissingNameErrors(t *testing.T) {
	if _, err := ExtractCloudFunction(map[string]interface{}{"runtime": "go121"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
