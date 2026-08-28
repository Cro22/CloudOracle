package gcp

import "testing"

func TestExtract_DispatchesRouterNAT(t *testing.T) {
	r, err := Extract("google_compute_router_nat", map[string]interface{}{
		"name":                   "nat-gw",
		"nat_ip_allocate_option": "MANUAL_ONLY",
		"nat_ips":                []interface{}{"ip-a", "ip-b"},
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "google_compute_router_nat" || r.RouterNAT == nil {
		t.Fatalf("dispatch failed: %+v", r)
	}
	if r.RouterNAT.ManualIPCount != 2 || r.RouterNAT.AllocateOption != "MANUAL_ONLY" {
		t.Errorf("got %+v, want 2 manual IPs / MANUAL_ONLY", r.RouterNAT)
	}
}

func TestExtractRouterNAT_AutoOnlyHasNoIPs(t *testing.T) {
	nat, err := ExtractRouterNAT(map[string]interface{}{
		"name":                   "nat-gw",
		"nat_ip_allocate_option": "AUTO_ONLY",
	})
	if err != nil {
		t.Fatalf("ExtractRouterNAT: %v", err)
	}
	if nat.ManualIPCount != 0 {
		t.Errorf("ManualIPCount = %d, want 0", nat.ManualIPCount)
	}
}

func TestExtractRouterNAT_MissingNameErrors(t *testing.T) {
	if _, err := ExtractRouterNAT(map[string]interface{}{"nat_ip_allocate_option": "AUTO_ONLY"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
