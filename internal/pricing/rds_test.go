package pricing

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

func TestEstimateRDS_PostgresSingleAZGP2_HappyPath(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	storage := loadFixture(t, "rds_storage_gp2_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {storage}}}

	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	est, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateRDS: %v", err)
	}

	wantCompute := 0.082 * HoursPerMonth // 59.86
	wantStorage := 0.115 * 100           // 11.5
	wantTotal := wantCompute + wantStorage

	if math.Abs(est.MonthlyUSD-wantTotal) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, wantTotal)
	}
	if est.Currency != "USD" {
		t.Errorf("Currency = %q", est.Currency)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	if len(est.Breakdown) != 2 {
		t.Fatalf("Breakdown len = %d, want 2", len(est.Breakdown))
	}
	if est.Breakdown[0].Component != "Compute" || math.Abs(est.Breakdown[0].MonthlyUSD-wantCompute) > 1e-6 {
		t.Errorf("Breakdown[0] = %+v, want Compute=%v", est.Breakdown[0], wantCompute)
	}
	if est.Breakdown[1].Component != "Storage" || math.Abs(est.Breakdown[1].MonthlyUSD-wantStorage) > 1e-6 {
		t.Errorf("Breakdown[1] = %+v, want Storage=%v", est.Breakdown[1], wantStorage)
	}

	// Compute query
	if len(src.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(src.calls))
	}
	c := src.calls[0]
	if c.service != "AmazonRDS" {
		t.Errorf("compute service = %q", c.service)
	}
	for k, want := range map[string]string{
		"productFamily":    "Database Instance",
		"instanceType":     "db.t3.medium",
		"databaseEngine":   "PostgreSQL",
		"deploymentOption": "Single-AZ",
		"regionCode":       "us-east-2",
		"licenseModel":     "No license required",
	} {
		if c.filters[k] != want {
			t.Errorf("compute filter %s = %q, want %q", k, c.filters[k], want)
		}
	}

	// Storage query
	s := src.calls[1]
	if s.service != "AmazonRDS" {
		t.Errorf("storage service = %q", s.service)
	}
	for k, want := range map[string]string{
		"productFamily":    "Database Storage",
		"volumeType":       "General Purpose",
		"deploymentOption": "Single-AZ",
		"regionCode":       "us-east-2",
	} {
		if s.filters[k] != want {
			t.Errorf("storage filter %s = %q, want %q", k, s.filters[k], want)
		}
	}
}

func TestEstimateRDS_MySQLMultiAZGP3_FilterCheck(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	storage := loadFixture(t, "rds_storage_gp2_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {storage}}}

	attrs := &aws.RDSAttributes{
		Engine:           "mysql",
		InstanceClass:    "db.m5.large",
		AllocatedStorage: 50,
		StorageType:      "gp3",
		MultiAZ:          true,
	}
	est, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateRDS: %v", err)
	}

	if got := src.calls[0].filters["databaseEngine"]; got != "MySQL" {
		t.Errorf("databaseEngine = %q, want MySQL", got)
	}
	if got := src.calls[0].filters["deploymentOption"]; got != "Multi-AZ" {
		t.Errorf("deploymentOption = %q, want Multi-AZ", got)
	}
	if got := src.calls[1].filters["volumeType"]; got != "General Purpose-GP3" {
		t.Errorf("storage volumeType = %q, want General Purpose-GP3", got)
	}
	if got := src.calls[1].filters["deploymentOption"]; got != "Multi-AZ" {
		t.Errorf("storage deploymentOption = %q, want Multi-AZ", got)
	}

	foundMAZ := false
	for _, n := range est.Notes {
		if strings.Contains(n, "Multi-AZ") {
			foundMAZ = true
			break
		}
	}
	if !foundMAZ {
		t.Errorf("Notes missing Multi-AZ caveat: %v", est.Notes)
	}
}

func TestEstimateRDS_MariaDB_EngineMapping(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	storage := loadFixture(t, "rds_storage_gp2_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {storage}}}

	attrs := &aws.RDSAttributes{
		Engine:           "mariadb",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 20,
		StorageType:      "gp2",
	}
	if _, err := EstimateRDS(context.Background(), src, attrs, "us-east-2"); err != nil {
		t.Fatalf("EstimateRDS: %v", err)
	}
	if got := src.calls[0].filters["databaseEngine"]; got != "MariaDB" {
		t.Errorf("databaseEngine = %q, want MariaDB", got)
	}
}

func TestEstimateRDS_MultiAZWithIO1_BothNotes(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	storage := loadFixture(t, "rds_storage_gp2_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {storage}}}

	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "io1",
		MultiAZ:          true,
		Iops:             1000,
	}
	est, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateRDS: %v", err)
	}
	foundMAZ, foundIOPS := false, false
	for _, n := range est.Notes {
		if strings.Contains(n, "Multi-AZ") {
			foundMAZ = true
		}
		if strings.Contains(n, "io1/io2 IOPS-month") {
			foundIOPS = true
		}
	}
	if !foundMAZ {
		t.Errorf("Notes missing Multi-AZ caveat: %v", est.Notes)
	}
	if !foundIOPS {
		t.Errorf("Notes missing io1/io2 IOPS caveat: %v", est.Notes)
	}
	if got := src.calls[1].filters["volumeType"]; got != "Provisioned IOPS" {
		t.Errorf("storage volumeType = %q, want Provisioned IOPS", got)
	}
}

func TestEstimateRDS_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateRDS(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{Engine: "postgres", InstanceClass: "db.t3.medium", AllocatedStorage: 100}
	_, err := EstimateRDS(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_AuroraEngineRejectedImmediately(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{
		Engine:           "aurora-postgresql",
		InstanceClass:    "db.r5.large",
		AllocatedStorage: 100,
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "Aurora") {
		t.Fatalf("err = %v, want Aurora rejection", err)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls for Aurora, got %d", len(src.calls))
	}
}

func TestEstimateRDS_OracleEngineNotSupported(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{
		Engine:           "oracle-ee",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "not supported in this version") {
		t.Fatalf("err = %v, want not-supported error", err)
	}
}

func TestEstimateRDS_EmptyEngine(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{InstanceClass: "db.t3.medium", AllocatedStorage: 100}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty Engine") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_EmptyInstanceClass(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{Engine: "postgres", AllocatedStorage: 100}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty InstanceClass") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_AllocatedStorageZero(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{
		Engine:        "postgres",
		InstanceClass: "db.t3.medium",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "AllocatedStorage must be > 0") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_NoComputeProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no compute price found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_NoStorageProducts(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, nil}}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no storage price found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_BadComputeUnit(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json"),
		`"unit": "Hrs"`,
		`"unit": "GB-Mo"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected compute unit Hrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_BadStorageUnit(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	bad := strings.Replace(
		loadFixture(t, "rds_storage_gp2_us_east_2.json"),
		`"unit": "GB-Mo"`,
		`"unit": "Hrs"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{compute}, {bad}}}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected storage unit GB-Mo") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateRDS_PropagatesAPIError(t *testing.T) {
	innerErr := errors.New("AccessDenied")
	src := &scriptedGetter{errs: []error{innerErr}}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner: %v", err)
	}
}

func TestEstimateRDS_UnknownStorageType(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "weird",
	}
	_, err := EstimateRDS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "unknown storage type") {
		t.Fatalf("err = %v", err)
	}
}

func TestMapRDSStorageType(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"", "General Purpose", false},
		{"gp2", "General Purpose", false},
		{"gp3", "General Purpose-GP3", false},
		{"io1", "Provisioned IOPS", false},
		{"io2", "Provisioned IOPS-IO2", false},
		{"standard", "Magnetic", false},
		{"weird", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := mapRDSStorageType(c.in)
			if c.err {
				if err == nil {
					t.Errorf("expected error for input %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestMapDeploymentOption(t *testing.T) {
	if got := mapDeploymentOption(false); got != "Single-AZ" {
		t.Errorf("false -> %q, want Single-AZ", got)
	}
	if got := mapDeploymentOption(true); got != "Multi-AZ" {
		t.Errorf("true -> %q, want Multi-AZ", got)
	}
}

func TestMapEngine(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"postgres", "PostgreSQL", false},
		{"mysql", "MySQL", false},
		{"mariadb", "MariaDB", false},
		{"oracle-ee", "", true},
		{"sqlserver-ee", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := mapEngine(c.in)
			if c.err {
				if err == nil {
					t.Errorf("expected error for %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
