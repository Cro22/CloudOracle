package pricing

import (
	"context"
	"errors"
	"testing"

	"CloudOracle/internal/iac"
	"CloudOracle/internal/iac/gcp"
)

func TestParseSQLTier(t *testing.T) {
	cases := []struct {
		tier  string
		vcpu  int
		memGB float64
		ok    bool
	}{
		{"db-custom-2-8192", 2, 8.0, true}, // 8192 MB = 8 GB
		{"db-custom-4-16384", 4, 16.0, true},
		{"db-n1-standard-2", 2, 7.5, true}, // 3.75 * 2
		{"db-n1-highmem-4", 4, 26.0, true}, // 6.5 * 4
		{"db-n1-highcpu-8", 8, 7.2, true},  // 0.9 * 8
		{"db-f1-micro", 0, 0, false},       // shared-core, not a custom tier
		{"garbage", 0, 0, false},
		{"db-custom-0-1024", 0, 0, false}, // zero vcpu rejected
	}
	for _, c := range cases {
		v, m, ok := parseSQLTier(c.tier)
		if ok != c.ok || (ok && (v != c.vcpu || !approxEq(m, c.memGB))) {
			t.Errorf("parseSQLTier(%q) = (%d, %.2f, %v), want (%d, %.2f, %v)",
				c.tier, v, m, ok, c.vcpu, c.memGB, c.ok)
		}
	}
}

func TestEstimateGCPSQLInstance_CustomTierComputePlusStorage(t *testing.T) {
	// db-custom-2-8192: 2 vCPU * 0.0413 + 8 GB * 0.0070 = 0.0826 + 0.056 = 0.1386/hr
	// * 730 = 101.18. Storage 50GB PD_SSD * 0.17 = 8.50. Total 109.68.
	est, err := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{
		DatabaseVersion: "POSTGRES_15",
		Tier:            "db-custom-2-8192",
		DiskSizeGB:      50,
		DiskType:        "PD_SSD",
	})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !approxEq(est.MonthlyUSD, 101.18+8.50) {
		t.Errorf("MonthlyUSD = %.2f, want ~109.68", est.MonthlyUSD)
	}
	if len(est.Breakdown) != 2 {
		t.Errorf("breakdown = %+v, want Compute+Storage", est.Breakdown)
	}
}

func TestEstimateGCPSQLInstance_RegionalDoublesCost(t *testing.T) {
	zonal, _ := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{
		Tier: "db-custom-2-8192", DiskSizeGB: 50, DiskType: "PD_SSD",
	})
	regional, _ := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{
		Tier: "db-custom-2-8192", DiskSizeGB: 50, DiskType: "PD_SSD", Regional: true,
	})
	if !approxEq(regional.MonthlyUSD, zonal.MonthlyUSD*2) {
		t.Errorf("regional = %.2f, want 2x zonal %.2f", regional.MonthlyUSD, zonal.MonthlyUSD)
	}
}

func TestEstimateGCPSQLInstance_SharedCoreFlatRate(t *testing.T) {
	// db-f1-micro flat 0.0150/hr * 730 = 10.95; storage defaults to 10GB PD_SSD = 1.70.
	est, err := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{
		Tier: "db-f1-micro",
	})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !approxEq(est.MonthlyUSD, 10.95+1.70) {
		t.Errorf("MonthlyUSD = %.2f, want ~12.65", est.MonthlyUSD)
	}
}

func TestEstimateGCPSQLInstance_SQLServerSkipped(t *testing.T) {
	_, err := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{
		DatabaseVersion: "SQLSERVER_2019_STANDARD", Tier: "db-custom-2-8192",
	})
	if !errors.Is(err, errSQLServerNotModeled) {
		t.Fatalf("err = %v, want errSQLServerNotModeled", err)
	}
}

func TestEstimateGCPSQLInstance_UnknownTierSentinel(t *testing.T) {
	_, err := EstimateGCPSQLInstance(&gcp.SQLInstanceAttributes{Tier: "db-mystery-9"})
	if !errors.Is(err, errUnpricedGCPSQLTier) {
		t.Fatalf("err = %v, want errUnpricedGCPSQLTier", err)
	}
}

func TestEstimateChange_CreateGCPSQLInstance(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_sql_database_instance.main",
		Mode:    "managed",
		Type:    "google_sql_database_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After: map[string]interface{}{
				"database_version": "POSTGRES_15",
				"settings": []interface{}{map[string]interface{}{
					"tier":      "db-custom-2-8192",
					"disk_size": float64(50),
				}},
			},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	if ce.MonthlyDelta <= 0 {
		t.Errorf("MonthlyDelta = %.2f, want > 0", ce.MonthlyDelta)
	}
}

func TestEstimateChange_GCPSQLServerSkipped(t *testing.T) {
	rc := iac.ResourceChange{
		Address: "google_sql_database_instance.mssql",
		Mode:    "managed",
		Type:    "google_sql_database_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After: map[string]interface{}{
				"database_version": "SQLSERVER_2019_STANDARD",
				"settings":         []interface{}{map[string]interface{}{"tier": "db-custom-4-16384"}},
			},
		},
	}
	ce, err := EstimateChange(context.Background(), nil, rc, "us-central1")
	if err != nil {
		t.Fatalf("EstimateChange returned error, want Skipped: %v", err)
	}
	if !ce.Skipped {
		t.Fatal("Skipped = false, want true for SQL Server")
	}
}
