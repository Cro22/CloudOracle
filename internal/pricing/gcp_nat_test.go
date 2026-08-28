package pricing

import (
	"testing"

	"CloudOracle/internal/iac/gcp"
)

func TestEstimateGCPRouterNAT_ManualIPsPriced(t *testing.T) {
	est, err := EstimateGCPRouterNAT(&gcp.RouterNATAttributes{
		Name: "nat", AllocateOption: "MANUAL_ONLY", ManualIPCount: 2,
	})
	if err != nil {
		t.Fatalf("EstimateGCPRouterNAT: %v", err)
	}
	// 2 IPs * $0.005/hr * 730 hr = $7.30/month.
	want := 2 * 0.005 * HoursPerMonth
	if est.MonthlyUSD != want {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want Low (per-VM/data unmodeled)", est.Confidence)
	}
	if len(est.Breakdown) != 1 || est.Breakdown[0].Component != "ExternalIPs" {
		t.Errorf("Breakdown = %+v, want one ExternalIPs line", est.Breakdown)
	}
}

func TestEstimateGCPRouterNAT_AutoOnlyIsZero(t *testing.T) {
	est, err := EstimateGCPRouterNAT(&gcp.RouterNATAttributes{
		Name: "nat", AllocateOption: "AUTO_ONLY", ManualIPCount: 0,
	})
	if err != nil {
		t.Fatalf("EstimateGCPRouterNAT: %v", err)
	}
	if est.MonthlyUSD != 0 {
		t.Errorf("MonthlyUSD = %v, want 0 for AUTO_ONLY", est.MonthlyUSD)
	}
}
