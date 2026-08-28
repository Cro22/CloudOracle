package pricing

import (
	"testing"

	"CloudOracle/internal/iac/gcp"
)

func TestEstimateGCPCloudFunction_ZeroStandingLowConfidence(t *testing.T) {
	est, err := EstimateGCPCloudFunction(&gcp.CloudFunctionAttributes{Name: "fn", Runtime: "python312"})
	if err != nil {
		t.Fatalf("EstimateGCPCloudFunction: %v", err)
	}
	if est.MonthlyUSD != 0 {
		t.Errorf("MonthlyUSD = %v, want 0 (invocation-based)", est.MonthlyUSD)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low", est.Confidence)
	}
	if len(est.Notes) == 0 {
		t.Error("want a caveat note that invocation charges are unmodeled")
	}
}

func TestEstimateGCPCloudFunction_NilErrors(t *testing.T) {
	if _, err := EstimateGCPCloudFunction(nil); err == nil {
		t.Fatal("want error for nil attrs")
	}
}
