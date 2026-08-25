package gcp

import "testing"

func TestExtract_DispatchesSQLInstance(t *testing.T) {
	r, err := Extract("google_sql_database_instance", map[string]interface{}{
		"database_version": "POSTGRES_15",
		"region":           "us-central1",
		"settings": []interface{}{map[string]interface{}{
			"tier":              "db-custom-2-8192",
			"disk_size":         float64(50),
			"disk_type":         "PD_SSD",
			"availability_type": "REGIONAL",
		}},
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	si := r.SQLInstance
	if si == nil {
		t.Fatal("SQLInstance nil — dispatch failed")
	}
	if si.Tier != "db-custom-2-8192" || si.DiskSizeGB != 50 || si.DiskType != "PD_SSD" {
		t.Errorf("got %+v", si)
	}
	if !si.Regional {
		t.Error("Regional = false, want true for availability_type=REGIONAL")
	}
	if si.DatabaseVersion != "POSTGRES_15" {
		t.Errorf("DatabaseVersion = %q", si.DatabaseVersion)
	}
}

func TestExtractSQLInstance_NoSettingsLeavesTierEmpty(t *testing.T) {
	si, err := ExtractSQLInstance(map[string]interface{}{"database_version": "MYSQL_8_0"})
	if err != nil {
		t.Fatalf("ExtractSQLInstance: %v", err)
	}
	if si.Tier != "" {
		t.Errorf("Tier = %q, want empty when settings absent", si.Tier)
	}
}

func TestExtractSQLInstance_EmptyAttrsErrors(t *testing.T) {
	if _, err := ExtractSQLInstance(map[string]interface{}{}); err == nil {
		t.Fatal("want error for empty attrs")
	}
}
