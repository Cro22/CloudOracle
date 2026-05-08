package pricing

import (
	"context"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

func TestEstimateRDSClusterInstance_AuroraPostgres_HappyPath(t *testing.T) {
	body := loadFixture(t, "rds_aurora_db_r5_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{body}}}

	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "test-cluster",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-postgresql",
	}
	est, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateRDSClusterInstance: %v", err)
	}
	want := 0.29 * HoursPerMonth // 211.7
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	foundClusterNote, foundMAZNote := false, false
	for _, n := range est.Notes {
		if strings.Contains(n, "Cluster-level storage") {
			foundClusterNote = true
		}
		if strings.Contains(n, "Aurora Multi-AZ is via reader replicas") {
			foundMAZNote = true
		}
	}
	if !foundClusterNote {
		t.Errorf("Notes missing cluster-storage caveat: %v", est.Notes)
	}
	if !foundMAZNote {
		t.Errorf("Notes missing Aurora-Multi-AZ caveat: %v", est.Notes)
	}

	// Filters
	c := src.calls[0]
	if c.service != "AmazonRDS" {
		t.Errorf("service = %q, want AmazonRDS", c.service)
	}
	for k, want := range map[string]string{
		"productFamily":    "Database Instance",
		"databaseEngine":   "Aurora PostgreSQL",
		"instanceType":     "db.r5.large",
		"deploymentOption": "Single-AZ",
		"licenseModel":     "No license required",
		"regionCode":       "us-east-2",
	} {
		if c.filters[k] != want {
			t.Errorf("filter %s = %q, want %q", k, c.filters[k], want)
		}
	}
}

func TestEstimateRDSClusterInstance_AuroraMySQL_EngineMapping(t *testing.T) {
	body := loadFixture(t, "rds_aurora_db_r5_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-mysql",
	}
	if _, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2"); err != nil {
		t.Fatalf("EstimateRDSClusterInstance: %v", err)
	}
	if got := src.calls[0].filters["databaseEngine"]; got != "Aurora MySQL" {
		t.Errorf("databaseEngine = %q, want Aurora MySQL", got)
	}
}

func TestEstimateRDSClusterInstance_AuroraLegacy_MapsToMySQL(t *testing.T) {
	body := loadFixture(t, "rds_aurora_db_r5_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora",
	}
	if _, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2"); err != nil {
		t.Fatalf("EstimateRDSClusterInstance: %v", err)
	}
	if got := src.calls[0].filters["databaseEngine"]; got != "Aurora MySQL" {
		t.Errorf("databaseEngine = %q, want Aurora MySQL (legacy aurora)", got)
	}
}

func TestEstimateRDSClusterInstance_NonAuroraPointsToEstimateRDS(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.t3.medium",
		Engine:            "postgres",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "use EstimateRDS") {
		t.Fatalf("err = %v, want pointer to EstimateRDS", err)
	}
}

func TestEstimateRDSClusterInstance_UnsupportedEngine(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "weird",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "unsupported engine") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateRDSClusterInstance(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-postgresql",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_EmptyEngine(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty Engine") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_EmptyInstanceClass(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		Engine:            "aurora-postgresql",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty InstanceClass") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_NoProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-postgresql",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no compute price found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDSClusterInstance_BadUnit(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "rds_aurora_db_r5_large_us_east_2.json"),
		`"unit": "Hrs"`,
		`"unit": "GB-Mo"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "c",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-postgresql",
	}
	_, err := EstimateRDSClusterInstance(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected unit Hrs") {
		t.Fatalf("err = %v", err)
	}
}
