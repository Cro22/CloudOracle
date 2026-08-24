package gcp

import "testing"

func TestExtract_DispatchesComputeInstance(t *testing.T) {
	r, err := Extract("google_compute_instance", map[string]interface{}{
		"machine_type": "e2-standard-4",
		"zone":         "us-central1-a",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "google_compute_instance" || r.ComputeInstance == nil {
		t.Fatalf("dispatch failed: %+v", r)
	}
	if r.ComputeInstance.MachineType != "e2-standard-4" {
		t.Errorf("MachineType = %q", r.ComputeInstance.MachineType)
	}
	if r.ComputeInstance.Region != "us-central1" {
		t.Errorf("Region = %q, want us-central1", r.ComputeInstance.Region)
	}
}

func TestExtract_UnsupportedTypeReturnsNil(t *testing.T) {
	r, err := Extract("google_storage_bucket", map[string]interface{}{"name": "x"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r != nil {
		t.Errorf("want nil for unsupported type, got %+v", r)
	}
}

func TestExtractComputeInstance_FullSelfLinksAndBootDisk(t *testing.T) {
	attrs := map[string]interface{}{
		"machine_type": "projects/p/zones/europe-west2-b/machineTypes/n2-standard-8",
		"zone":         "projects/p/zones/europe-west2-b",
		"boot_disk": []interface{}{map[string]interface{}{
			"initialize_params": []interface{}{map[string]interface{}{
				"size": float64(200),
				"type": "pd-ssd",
			}},
		}},
	}
	ci, err := ExtractComputeInstance(attrs)
	if err != nil {
		t.Fatalf("ExtractComputeInstance: %v", err)
	}
	if ci.MachineType != "n2-standard-8" {
		t.Errorf("MachineType = %q, want n2-standard-8 (last path segment)", ci.MachineType)
	}
	if ci.Region != "europe-west2" {
		t.Errorf("Region = %q, want europe-west2", ci.Region)
	}
	if ci.BootDiskSizeGB != 200 || ci.BootDiskType != "pd-ssd" {
		t.Errorf("boot disk = %d/%q, want 200/pd-ssd", ci.BootDiskSizeGB, ci.BootDiskType)
	}
}

func TestExtractComputeInstance_PreemptibleFromEitherField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sched map[string]interface{}
	}{
		{"legacy preemptible bool", map[string]interface{}{"preemptible": true}},
		{"provisioning_model SPOT", map[string]interface{}{"provisioning_model": "SPOT"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ci, err := ExtractComputeInstance(map[string]interface{}{
				"machine_type": "e2-medium",
				"scheduling":   []interface{}{tc.sched},
			})
			if err != nil {
				t.Fatalf("ExtractComputeInstance: %v", err)
			}
			if !ci.Preemptible {
				t.Error("Preemptible = false, want true")
			}
		})
	}
}

func TestExtractComputeInstance_MissingMachineTypeErrors(t *testing.T) {
	_, err := ExtractComputeInstance(map[string]interface{}{"zone": "us-central1-a"})
	if err == nil {
		t.Fatal("want error for missing machine_type")
	}
}

func TestExtractComputeInstance_EmptyAttrsErrors(t *testing.T) {
	if _, err := ExtractComputeInstance(map[string]interface{}{}); err == nil {
		t.Fatal("want error for empty attrs")
	}
}
