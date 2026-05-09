package pricing

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac"
)

// ec2CreateAttrs builds a valid aws_instance after-state map matching
// what a Terraform plan would emit (ints come through as float64).
func ec2CreateAttrs(instanceType, volumeType string, volumeSize int) map[string]interface{} {
	m := map[string]interface{}{
		"instance_type": instanceType,
		"tenancy":       "default",
	}
	if volumeSize > 0 {
		m["root_block_device"] = []interface{}{
			map[string]interface{}{
				"volume_size": float64(volumeSize),
				"volume_type": volumeType,
			},
		}
	}
	return m
}

func rdsAttrs(instanceClass string) map[string]interface{} {
	return map[string]interface{}{
		"engine":            "postgres",
		"instance_class":    instanceClass,
		"allocated_storage": float64(100),
		"storage_type":      "gp2",
	}
}

func ebsAttrs(volType string, size int) map[string]interface{} {
	return map[string]interface{}{
		"type": volType,
		"size": float64(size),
	}
}

func TestEstimateChange_CreateEC2(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {gp3}}}

	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   ec2CreateAttrs("t3.large", "gp3", 50),
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Skipped {
		t.Fatalf("Skipped = true, reason=%q", ce.SkipReason)
	}
	if ce.Action != iac.ActionCreate {
		t.Errorf("Action = %q, want create", ce.Action)
	}
	if ce.BeforeMonthly != 0 {
		t.Errorf("BeforeMonthly = %v, want 0", ce.BeforeMonthly)
	}
	wantAfter := 0.0832*HoursPerMonth + 0.08*50
	if math.Abs(ce.AfterMonthly-wantAfter) > 1e-6 {
		t.Errorf("AfterMonthly = %v, want %v", ce.AfterMonthly, wantAfter)
	}
	if math.Abs(ce.MonthlyDelta-wantAfter) > 1e-6 {
		t.Errorf("MonthlyDelta = %v, want %v", ce.MonthlyDelta, wantAfter)
	}
	if ce.Currency != "USD" {
		t.Errorf("Currency = %q", ce.Currency)
	}
	if ce.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", ce.Confidence)
	}
	if len(ce.Breakdown) != 2 {
		t.Errorf("Breakdown len = %d, want 2", len(ce.Breakdown))
	}
}

func TestEstimateChange_DeleteEBS(t *testing.T) {
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{gp3}}}

	rc := iac.ResourceChange{
		Address: "aws_ebs_volume.disk",
		Mode:    "managed",
		Type:    "aws_ebs_volume",
		Change: iac.Change{
			Actions: []string{"delete"},
			Before:  ebsAttrs("gp3", 100),
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	wantBefore := 0.08 * 100
	if math.Abs(ce.BeforeMonthly-wantBefore) > 1e-6 {
		t.Errorf("BeforeMonthly = %v, want %v", ce.BeforeMonthly, wantBefore)
	}
	if ce.AfterMonthly != 0 {
		t.Errorf("AfterMonthly = %v, want 0", ce.AfterMonthly)
	}
	if math.Abs(ce.MonthlyDelta+wantBefore) > 1e-6 {
		t.Errorf("MonthlyDelta = %v, want %v", ce.MonthlyDelta, -wantBefore)
	}
	if len(ce.Breakdown) != 1 || ce.Breakdown[0].MonthlyUSD >= 0 {
		t.Errorf("Breakdown = %+v, expected single negative line item", ce.Breakdown)
	}
}

func TestEstimateChange_UpdateRDS(t *testing.T) {
	compute := loadFixture(t, "rds_postgres_db_t3_medium_us_east_2.json")
	storage := loadFixture(t, "rds_storage_gp2_us_east_2.json")
	// "before": $0.082/hr compute. "after": $0.164/hr (mutated copy).
	afterCompute := strings.Replace(compute, `"USD": "0.082"`, `"USD": "0.164"`, 1)
	src := &scriptedGetter{responses: [][]string{{compute}, {storage}, {afterCompute}, {storage}}}

	rc := iac.ResourceChange{
		Address: "aws_db_instance.db",
		Mode:    "managed",
		Type:    "aws_db_instance",
		Change: iac.Change{
			Actions: []string{"update"},
			Before:  rdsAttrs("db.t3.medium"),
			After:   rdsAttrs("db.t3.large"),
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	wantBefore := 0.082*HoursPerMonth + 0.115*100
	wantAfter := 0.164*HoursPerMonth + 0.115*100
	wantDelta := wantAfter - wantBefore
	if math.Abs(ce.BeforeMonthly-wantBefore) > 1e-6 {
		t.Errorf("BeforeMonthly = %v, want %v", ce.BeforeMonthly, wantBefore)
	}
	if math.Abs(ce.AfterMonthly-wantAfter) > 1e-6 {
		t.Errorf("AfterMonthly = %v, want %v", ce.AfterMonthly, wantAfter)
	}
	if math.Abs(ce.MonthlyDelta-wantDelta) > 1e-6 {
		t.Errorf("MonthlyDelta = %v, want %v", ce.MonthlyDelta, wantDelta)
	}

	// Delta breakdown: Compute should be positive delta, Storage = 0.
	gotCompute, gotStorage := math.NaN(), math.NaN()
	for _, li := range ce.Breakdown {
		switch li.Component {
		case "Compute":
			gotCompute = li.MonthlyUSD
		case "Storage":
			gotStorage = li.MonthlyUSD
		}
	}
	wantComputeDelta := (0.164 - 0.082) * HoursPerMonth
	if math.Abs(gotCompute-wantComputeDelta) > 1e-6 {
		t.Errorf("Compute delta = %v, want %v", gotCompute, wantComputeDelta)
	}
	if math.Abs(gotStorage) > 1e-6 {
		t.Errorf("Storage delta = %v, want 0", gotStorage)
	}
}

func TestEstimateChange_ReplaceEC2(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	afterCompute := strings.Replace(compute, `"USD": "0.0832"`, `"USD": "0.1664"`, 1)
	src := &scriptedGetter{responses: [][]string{{compute}, {gp3}, {afterCompute}, {gp3}}}

	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change: iac.Change{
			Actions: []string{"delete", "create"}, // replacement
			Before:  ec2CreateAttrs("t3.large", "gp3", 50),
			After:   ec2CreateAttrs("t3.xlarge", "gp3", 50),
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateChange: %v", err)
	}
	if ce.Action != iac.ActionReplace {
		t.Errorf("Action = %q, want replace", ce.Action)
	}
	wantDelta := (0.1664 - 0.0832) * HoursPerMonth
	if math.Abs(ce.MonthlyDelta-wantDelta) > 1e-6 {
		t.Errorf("MonthlyDelta = %v, want %v", ce.MonthlyDelta, wantDelta)
	}
}

func TestEstimateChange_NoOp(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change:  iac.Change{Actions: []string{"no-op"}},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true")
	}
	if ce.MonthlyDelta != 0 || ce.BeforeMonthly != 0 || ce.AfterMonthly != 0 {
		t.Errorf("expected all zero costs: %+v", ce)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls for no-op, got %d", len(src.calls))
	}
}

func TestEstimateChange_Read(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change:  iac.Change{Actions: []string{"read"}},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true")
	}
}

func TestEstimateChange_DataSource(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "data.aws_ami.ubuntu",
		Mode:    "data",
		Type:    "aws_ami",
		Change:  iac.Change{Actions: []string{"create"}},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true (data source)")
	}
	if !strings.Contains(ce.SkipReason, "data source") {
		t.Errorf("SkipReason = %q", ce.SkipReason)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls for data source, got %d", len(src.calls))
	}
}

func TestEstimateChange_UnsupportedTypeWithCreate(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_iam_role.r",
		Mode:    "managed",
		Type:    "aws_iam_role",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   map[string]interface{}{"name": "r"},
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true")
	}
	if !strings.Contains(ce.SkipReason, "unsupported resource type") {
		t.Errorf("SkipReason = %q", ce.SkipReason)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls for unsupported type, got %d", len(src.calls))
	}
}

func TestEstimateChange_UnsupportedTypeWithDelete(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_iam_role.r",
		Mode:    "managed",
		Type:    "aws_iam_role",
		Change: iac.Change{
			Actions: []string{"delete"},
			Before:  map[string]interface{}{"name": "r"},
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true")
	}
}

func TestEstimateChange_UnsupportedTypeWithUpdate(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_iam_role.r",
		Mode:    "managed",
		Type:    "aws_iam_role",
		Change: iac.Change{
			Actions: []string{"update"},
			Before:  map[string]interface{}{"name": "r"},
			After:   map[string]interface{}{"name": "r2"},
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ce.Skipped {
		t.Errorf("Skipped = false, want true")
	}
}

func TestEstimateChange_ConfidenceMerging(t *testing.T) {
	// Update of an EBS volume gp2 (Medium confidence) → io1 (Low).
	// scriptedGetter returns the same minimal product for both calls; only
	// the type differs in the Estimate's classification.
	src := &scriptedGetter{responses: [][]string{
		{minimalGBMoProduct},
		{minimalGBMoProduct},
	}}
	rc := iac.ResourceChange{
		Address: "aws_ebs_volume.disk",
		Mode:    "managed",
		Type:    "aws_ebs_volume",
		Change: iac.Change{
			Actions: []string{"update"},
			Before:  ebsAttrs("gp2", 50),
			After:   ebsAttrs("io1", 50),
		},
	}
	ce, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ce.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low (Medium + Low → Low)", ce.Confidence)
	}
}

func TestEstimateChange_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change:  iac.Change{Actions: []string{"create"}, After: ec2CreateAttrs("t3.large", "", 0)},
	}
	_, err := EstimateChange(context.Background(), src, rc, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateChange_APIErrorPropagated(t *testing.T) {
	innerErr := errors.New("AccessDenied")
	src := &scriptedGetter{errs: []error{innerErr}}
	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change: iac.Change{
			Actions: []string{"create"},
			After:   ec2CreateAttrs("t3.large", "", 0),
		},
	}
	_, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner: %v", err)
	}
	if !strings.Contains(err.Error(), "aws_instance.web") {
		t.Errorf("error missing resource address context: %v", err)
	}
}

func TestEstimateChange_BeforeExtractFailsOnUpdate(t *testing.T) {
	src := &scriptedGetter{}
	rc := iac.ResourceChange{
		Address: "aws_instance.web",
		Mode:    "managed",
		Type:    "aws_instance",
		Change: iac.Change{
			Actions: []string{"update"},
			Before: map[string]interface{}{
				"instance_type": 42, // wrong type — extractor fails
			},
			After: ec2CreateAttrs("t3.large", "", 0),
		},
	}
	_, err := EstimateChange(context.Background(), src, rc, "us-east-2")
	if err == nil {
		t.Fatal("expected error from before extraction failure")
	}
	if !strings.Contains(err.Error(), "before") {
		t.Errorf("error missing 'before' context: %v", err)
	}
}

func TestWeakestConfidence(t *testing.T) {
	cases := []struct {
		a, b, want Confidence
	}{
		{ConfidenceHigh, ConfidenceHigh, ConfidenceHigh},
		{ConfidenceHigh, ConfidenceMedium, ConfidenceMedium},
		{ConfidenceMedium, ConfidenceHigh, ConfidenceMedium},
		{ConfidenceMedium, ConfidenceLow, ConfidenceLow},
		{ConfidenceLow, ConfidenceMedium, ConfidenceLow},
		{ConfidenceLow, ConfidenceLow, ConfidenceLow},
	}
	for _, c := range cases {
		if got := weakestConfidence(c.a, c.b); got != c.want {
			t.Errorf("weakestConfidence(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestMergeDeltaBreakdown_BeforeOnlyComponent(t *testing.T) {
	before := []LineItem{
		{Component: "Compute", MonthlyUSD: 100},
		{Component: "RootEBS", MonthlyUSD: 5},
	}
	after := []LineItem{
		{Component: "Compute", MonthlyUSD: 80},
	}
	out := mergeDeltaBreakdown(before, after)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Component != "Compute" || out[0].MonthlyUSD != -20 {
		t.Errorf("Compute = %+v, want -20", out[0])
	}
	if out[1].Component != "RootEBS" || out[1].MonthlyUSD != -5 {
		t.Errorf("RootEBS = %+v, want -5", out[1])
	}
}
