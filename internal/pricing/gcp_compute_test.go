package pricing

import (
	"errors"
	"math"
	"testing"

	"CloudOracle/internal/iac/gcp"
)

func approxEq(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestEstimateGCPComputeInstance_ComputePlusBootDisk(t *testing.T) {
	// e2-standard-4 @ us-central1 = 0.134012/hr * 730 = 97.83; boot 100GB
	// pd-balanced @ 0.10 = 10.00.
	est, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType:    "e2-standard-4",
		Region:         "us-central1",
		BootDiskSizeGB: 100,
		BootDiskType:   "pd-balanced",
	}, "us-east1")
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !approxEq(est.MonthlyUSD, 97.83+10.00) {
		t.Errorf("MonthlyUSD = %.2f, want ~107.83", est.MonthlyUSD)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
	if len(est.Breakdown) != 2 || est.Breakdown[1].Component != "BootDisk" {
		t.Errorf("breakdown = %+v, want Compute+BootDisk", est.Breakdown)
	}
}

func TestEstimateGCPComputeInstance_RegionMultiplierApplied(t *testing.T) {
	// europe-west2 multiplier is 1.16 → compute should be 16% above base.
	base, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "n1-standard-1", Region: "us-central1",
	}, "us-central1")
	if err != nil {
		t.Fatal(err)
	}
	euro, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "n1-standard-1", Region: "europe-west2",
	}, "us-central1")
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(euro.MonthlyUSD, base.MonthlyUSD*1.16) {
		t.Errorf("europe-west2 = %.2f, want ~%.2f (1.16x)", euro.MonthlyUSD, base.MonthlyUSD*1.16)
	}
}

func TestEstimateGCPComputeInstance_UnknownRegionDropsConfidence(t *testing.T) {
	est, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "e2-medium", Region: "mars-central1",
	}, "mars-central1")
	if err != nil {
		t.Fatal(err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low for unknown region", est.Confidence)
	}
}

func TestEstimateGCPComputeInstance_PreemptibleIsLowConfidence(t *testing.T) {
	est, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "e2-standard-4", Region: "us-central1", Preemptible: true,
	}, "us-central1")
	if err != nil {
		t.Fatal(err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low for preemptible", est.Confidence)
	}
}

func TestEstimateGCPComputeInstance_ZoneRegionBeatsPlanRegion(t *testing.T) {
	// attrs.Region set → planRegion ignored.
	est, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "e2-medium", Region: "europe-west2",
	}, "us-central1")
	if err != nil {
		t.Fatal(err)
	}
	base, _, _ := gcpComputeHourly("e2-medium", "us-central1")
	if approxEq(est.MonthlyUSD, base*HoursPerMonth) {
		t.Error("used plan region us-central1; should have used instance region europe-west2")
	}
}

func TestEstimateGCPComputeInstance_UnknownMachineTypeSentinel(t *testing.T) {
	_, err := EstimateGCPComputeInstance(&gcp.ComputeInstanceAttributes{
		MachineType: "z9-mega-9999", Region: "us-central1",
	}, "us-central1")
	if !errors.Is(err, errUnpricedGCPMachineType) {
		t.Fatalf("err = %v, want errUnpricedGCPMachineType", err)
	}
}
