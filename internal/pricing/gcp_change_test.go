package pricing

import (
	"context"
	"testing"

	"CloudOracle/internal/iac"
)

// The GCP path prices from the static table and never calls the productGetter,
// so these dispatch tests pass a nil src.

func TestEstimateChange_CreateGCPComputeInstance(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_compute_instance.web",
		Mode:    "managed",
		Type:    "google_compute_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After: map[string]interface{}{
				"machine_type": "e2-standard-4",
				"zone":         "us-central1-a",
				"boot_disk": []interface{}{map[string]interface{}{
					"initialize_params": []interface{}{map[string]interface{}{
						"size": float64(50),
						"type": "pd-balanced",
					}},
				}},
			},
		},
	}
	// region flag is us-east-2 (AWS-style default); the instance zone overrides it.
	ce, err := EstimateChange(context.Background(), nil, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	if ce.MonthlyDelta <= 0 {
		t.Errorf("MonthlyDelta = %.2f, want > 0", ce.MonthlyDelta)
	}
	if ce.AfterMonthly != ce.MonthlyDelta {
		t.Errorf("create: AfterMonthly (%.2f) should equal MonthlyDelta (%.2f)", ce.AfterMonthly, ce.MonthlyDelta)
	}
}

func TestEstimateChange_GCPUnknownMachineTypeSkipped(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_compute_instance.exotic",
		Mode:    "managed",
		Type:    "google_compute_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"machine_type": "z9-mega-9999", "zone": "us-central1-a"},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange returned error, want Skipped: %v", err)
	}
	if !ce.Skipped {
		t.Fatal("Skipped = false, want true for unpriced machine type")
	}
}

func TestEstimateChange_GCPUnsupportedTypeSkipped(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_storage_bucket.assets",
		Mode:    "managed",
		Type:    "google_storage_bucket",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"name": "assets", "location": "US"},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if !ce.Skipped {
		t.Fatal("Skipped = false, want true for unsupported GCP type")
	}
}
