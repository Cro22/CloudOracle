package pricing

import (
	"context"
	"errors"
	"testing"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/iac/gcp"
)

func TestEstimateGCPComputeDisk_SizeTimesRate(t *testing.T) {
	// 500GB pd-ssd @ 0.17 = 85.00.
	est, err := EstimateGCPComputeDisk(&gcp.ComputeDiskAttributes{Type: "pd-ssd", SizeGB: 500})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !approxEq(est.MonthlyUSD, 85.00) {
		t.Errorf("MonthlyUSD = %.2f, want 85.00", est.MonthlyUSD)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
}

func TestEstimateGCPComputeDisk_DefaultsToPDStandard(t *testing.T) {
	// no type → pd-standard @ 0.04; 100GB = 4.00.
	est, err := EstimateGCPComputeDisk(&gcp.ComputeDiskAttributes{SizeGB: 100})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !approxEq(est.MonthlyUSD, 4.00) {
		t.Errorf("MonthlyUSD = %.2f, want 4.00 (pd-standard default)", est.MonthlyUSD)
	}
}

func TestEstimateGCPComputeDisk_MissingSizeSentinel(t *testing.T) {
	_, err := EstimateGCPComputeDisk(&gcp.ComputeDiskAttributes{Type: "pd-ssd"})
	if !errors.Is(err, errUnpricedGCPDisk) {
		t.Fatalf("err = %v, want errUnpricedGCPDisk", err)
	}
}

func TestEstimateChange_CreateGCPComputeDisk(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_compute_disk.data",
		Mode:    "managed",
		Type:    "google_compute_disk",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"type": "pd-balanced", "size": float64(200)},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	if !approxEq(ce.MonthlyDelta, 20.00) { // 200 * 0.10
		t.Errorf("MonthlyDelta = %.2f, want 20.00", ce.MonthlyDelta)
	}
}

func TestEstimateChange_GCPDiskMissingSizeSkipped(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_compute_disk.fromimage",
		Mode:    "managed",
		Type:    "google_compute_disk",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"type": "pd-ssd"},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange returned error, want Skipped: %v", err)
	}
	if !ce.Skipped {
		t.Fatal("Skipped = false, want true for disk with no size")
	}
}
