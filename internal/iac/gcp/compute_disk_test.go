package gcp

import "testing"

func TestExtract_DispatchesComputeDisk(t *testing.T) {
	r, err := Extract("google_compute_disk", map[string]interface{}{
		"type": "pd-ssd",
		"size": float64(500),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "google_compute_disk" || r.ComputeDisk == nil {
		t.Fatalf("dispatch failed: %+v", r)
	}
	if r.ComputeDisk.Type != "pd-ssd" || r.ComputeDisk.SizeGB != 500 {
		t.Errorf("got %+v, want pd-ssd/500", r.ComputeDisk)
	}
}

func TestExtractComputeDisk_MissingSizeIsZero(t *testing.T) {
	cd, err := ExtractComputeDisk(map[string]interface{}{"type": "pd-balanced"})
	if err != nil {
		t.Fatalf("ExtractComputeDisk: %v", err)
	}
	if cd.SizeGB != 0 {
		t.Errorf("SizeGB = %d, want 0 (absent)", cd.SizeGB)
	}
}

func TestExtractComputeDisk_EmptyAttrsErrors(t *testing.T) {
	if _, err := ExtractComputeDisk(map[string]interface{}{}); err == nil {
		t.Fatal("want error for empty attrs")
	}
}
