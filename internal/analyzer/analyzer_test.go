package analyzer

import (
	"CloudOracle/internal/shared"
	"testing"
	"time"
)

// --- EC2 Idle Rule ---

func TestCheckEC2Idle_DetectsIdleInstance(t *testing.T) {
	r := shared.Resource{
		ID:           "i-123",
		Service:      "ec2",
		ResourceType: "t3.micro",
		Region:       "us-east-1",
		MonthlyCost:  7.50,
		UsageMetric:  2.0,                                  // CPU < 5%
		CreatedAt:    time.Now().Add(-30 * 24 * time.Hour), // 30 days old
	}
	f := checkEC2Idle(r)
	if f == nil {
		t.Fatal("expected finding for idle EC2, got nil")
	}
	if f.Rule != "ec2-idle" {
		t.Errorf("expected rule ec2-idle, got %s", f.Rule)
	}
	if f.Severity != shared.SeverityHigh {
		t.Errorf("expected HIGH severity, got %s", f.Severity)
	}
	if f.MonthlySavings != r.MonthlyCost {
		t.Errorf("expected savings %.2f, got %.2f", r.MonthlyCost, f.MonthlySavings)
	}
}

func TestCheckEC2Idle_IgnoresNonEC2(t *testing.T) {
	r := shared.Resource{Service: "rds", UsageMetric: 1.0, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	if f := checkEC2Idle(r); f != nil {
		t.Error("should not flag non-EC2 resource")
	}
}

func TestCheckEC2Idle_IgnoresActiveInstance(t *testing.T) {
	r := shared.Resource{
		Service:     "ec2",
		UsageMetric: 50.0, // CPU > 5%
		CreatedAt:   time.Now().Add(-30 * 24 * time.Hour),
	}
	if f := checkEC2Idle(r); f != nil {
		t.Error("should not flag active EC2 instance")
	}
}

func TestCheckEC2Idle_IgnoresRecentInstance(t *testing.T) {
	r := shared.Resource{
		Service:     "ec2",
		UsageMetric: 1.0,
		CreatedAt:   time.Now().Add(-3 * 24 * time.Hour), // Only 3 days old
	}
	if f := checkEC2Idle(r); f != nil {
		t.Error("should not flag EC2 instance younger than 7 days")
	}
}

func TestCheckEC2Idle_BoundaryCPU(t *testing.T) {
	r := shared.Resource{
		Service:     "ec2",
		UsageMetric: 5.0, // Exactly at threshold
		CreatedAt:   time.Now().Add(-30 * 24 * time.Hour),
	}
	if f := checkEC2Idle(r); f != nil {
		t.Error("should not flag EC2 at exactly 5% CPU")
	}
}

func TestCheckEC2Idle_BoundaryAge(t *testing.T) {
	r := shared.Resource{
		Service:     "ec2",
		UsageMetric: 1.0,
		CreatedAt:   time.Now().Add(-6 * 24 * time.Hour), // 6 days, under 7
	}
	if f := checkEC2Idle(r); f != nil {
		t.Error("should not flag EC2 at 6 days old")
	}
}

// --- RDS Oversized Rule ---

func TestCheckRDSOversized_DetectsOversized(t *testing.T) {
	r := shared.Resource{
		ID:           "db-abc",
		Service:      "rds",
		ResourceType: "db.r5.large",
		Region:       "us-west-2",
		MonthlyCost:  180.00,
		UsageMetric:  3.0, // CPU < 10%
	}
	f := checkRDSOversized(r)
	if f == nil {
		t.Fatal("expected finding for oversized RDS, got nil")
	}
	if f.Rule != "rds-oversized" {
		t.Errorf("expected rule rds-oversized, got %s", f.Rule)
	}
	if f.Severity != shared.SeverityMedium {
		t.Errorf("expected MEDIUM severity, got %s", f.Severity)
	}
	expectedSavings := r.MonthlyCost * 0.50
	if f.MonthlySavings != expectedSavings {
		t.Errorf("expected savings %.2f, got %.2f", expectedSavings, f.MonthlySavings)
	}
}

func TestCheckRDSOversized_IgnoresNonRDS(t *testing.T) {
	r := shared.Resource{Service: "ec2", UsageMetric: 1.0}
	if f := checkRDSOversized(r); f != nil {
		t.Error("should not flag non-RDS resource")
	}
}

func TestCheckRDSOversized_IgnoresWellUsed(t *testing.T) {
	r := shared.Resource{Service: "rds", UsageMetric: 50.0}
	if f := checkRDSOversized(r); f != nil {
		t.Error("should not flag RDS with 50% CPU")
	}
}

func TestCheckRDSOversized_BoundaryCPU(t *testing.T) {
	r := shared.Resource{Service: "rds", UsageMetric: 10.0}
	if f := checkRDSOversized(r); f != nil {
		t.Error("should not flag RDS at exactly 10% CPU")
	}
}

// --- EBS Orphan Rule ---

func TestCheckEBSOrphan_DetectsOrphan(t *testing.T) {
	r := shared.Resource{
		ID:           "vol-abc",
		Service:      "ebs",
		ResourceType: "gp3-500GB",
		Region:       "eu-west-1",
		MonthlyCost:  50.00,
		UsageMetric:  0, // Unattached
		CreatedAt:    time.Now().Add(-60 * 24 * time.Hour),
	}
	f := checkEBSOrphan(r)
	if f == nil {
		t.Fatal("expected finding for orphan EBS, got nil")
	}
	if f.Rule != "ebs-orphan" {
		t.Errorf("expected rule ebs-orphan, got %s", f.Rule)
	}
	if f.Severity != shared.SeverityHigh {
		t.Errorf("expected HIGH severity, got %s", f.Severity)
	}
	if f.MonthlySavings != r.MonthlyCost {
		t.Errorf("expected savings %.2f, got %.2f", r.MonthlyCost, f.MonthlySavings)
	}
}

func TestCheckEBSOrphan_IgnoresNonEBS(t *testing.T) {
	r := shared.Resource{Service: "ec2", UsageMetric: 0}
	if f := checkEBSOrphan(r); f != nil {
		t.Error("should not flag non-EBS resource")
	}
}

func TestCheckEBSOrphan_IgnoresAttached(t *testing.T) {
	r := shared.Resource{Service: "ebs", UsageMetric: 50.0}
	if f := checkEBSOrphan(r); f != nil {
		t.Error("should not flag attached EBS volume")
	}
}

func TestCheckEBSOrphan_BoundaryUsage(t *testing.T) {
	r := shared.Resource{Service: "ebs", UsageMetric: 0.001}
	if f := checkEBSOrphan(r); f != nil {
		t.Error("should not flag EBS with usage > 0")
	}
}

// --- Lambda Over-Provisioned Rule ---

func TestCheckLambdaOverProvisioned_Detects(t *testing.T) {
	r := shared.Resource{
		ID:           "fn-abc",
		Service:      "lambda",
		ResourceType: "2048MB",
		Region:       "us-east-1",
		MonthlyCost:  0.10,
		UsageMetric:  500, // Low invocations
	}
	f := checkLambdaOverProvisioned(r)
	if f == nil {
		t.Fatal("expected finding for over-provisioned lambda, got nil")
	}
	if f.Rule != "lambda-over-provisioned" {
		t.Errorf("expected rule lambda-over-provisioned, got %s", f.Rule)
	}
	if f.Severity != shared.SeverityLow {
		t.Errorf("expected LOW severity, got %s", f.Severity)
	}
	expectedSavings := r.MonthlyCost * 0.30
	if f.MonthlySavings != expectedSavings {
		t.Errorf("expected savings %.4f, got %.4f", expectedSavings, f.MonthlySavings)
	}
}

func TestCheckLambdaOverProvisioned_IgnoresNonLambda(t *testing.T) {
	r := shared.Resource{Service: "ec2", ResourceType: "2048MB", UsageMetric: 100}
	if f := checkLambdaOverProvisioned(r); f != nil {
		t.Error("should not flag non-Lambda resource")
	}
}

func TestCheckLambdaOverProvisioned_IgnoresSmallMemory(t *testing.T) {
	r := shared.Resource{Service: "lambda", ResourceType: "512MB", UsageMetric: 100}
	if f := checkLambdaOverProvisioned(r); f != nil {
		t.Error("should not flag lambda with 512MB")
	}
}

func TestCheckLambdaOverProvisioned_IgnoresExact1024(t *testing.T) {
	r := shared.Resource{Service: "lambda", ResourceType: "1024MB", UsageMetric: 100}
	if f := checkLambdaOverProvisioned(r); f != nil {
		t.Error("should not flag lambda with exactly 1024MB")
	}
}

func TestCheckLambdaOverProvisioned_IgnoresHighInvocations(t *testing.T) {
	r := shared.Resource{Service: "lambda", ResourceType: "2048MB", UsageMetric: 500000}
	if f := checkLambdaOverProvisioned(r); f != nil {
		t.Error("should not flag lambda with high invocations")
	}
}

func TestCheckLambdaOverProvisioned_BoundaryInvocations(t *testing.T) {
	r := shared.Resource{Service: "lambda", ResourceType: "2048MB", UsageMetric: 100001}
	if f := checkLambdaOverProvisioned(r); f != nil {
		t.Error("should not flag lambda with invocations > 100000")
	}
}

// --- Analyze function ---

func TestAnalyze_EmptyResources(t *testing.T) {
	findings := Analyze(nil)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestAnalyze_MixedResources(t *testing.T) {
	resources := []shared.Resource{
		{Service: "ec2", UsageMetric: 1.0, MonthlyCost: 100, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		{Service: "ec2", UsageMetric: 50.0, MonthlyCost: 100, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		{Service: "ebs", UsageMetric: 0, MonthlyCost: 50, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		{Service: "rds", UsageMetric: 5.0, MonthlyCost: 180},
		{Service: "lambda", ResourceType: "2048MB", UsageMetric: 100, MonthlyCost: 0.10},
	}
	findings := Analyze(resources)
	if len(findings) != 4 {
		t.Errorf("expected 4 findings, got %d", len(findings))
	}
}

func TestAnalyze_SortedBySavingsDescending(t *testing.T) {
	resources := []shared.Resource{
		{Service: "ebs", UsageMetric: 0, MonthlyCost: 10, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		{Service: "ebs", UsageMetric: 0, MonthlyCost: 100, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
		{Service: "ebs", UsageMetric: 0, MonthlyCost: 50, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
	}
	findings := Analyze(resources)
	if len(findings) < 2 {
		t.Fatal("expected at least 2 findings")
	}
	for i := 1; i < len(findings); i++ {
		if findings[i].MonthlySavings > findings[i-1].MonthlySavings {
			t.Errorf("findings not sorted by savings: %.2f > %.2f at index %d",
				findings[i].MonthlySavings, findings[i-1].MonthlySavings, i)
		}
	}
}

func TestAnalyze_NoFalsePositives(t *testing.T) {
	resources := []shared.Resource{
		{Service: "ec2", UsageMetric: 80.0, MonthlyCost: 100, CreatedAt: time.Now().Add(-2 * 24 * time.Hour)},
		{Service: "rds", UsageMetric: 60.0, MonthlyCost: 180},
		{Service: "ebs", UsageMetric: 70.0, MonthlyCost: 50},
		{Service: "lambda", ResourceType: "256MB", UsageMetric: 500000, MonthlyCost: 0.05},
	}
	findings := Analyze(resources)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for healthy resources, got %d", len(findings))
	}
}

func TestAnalyze_FindingsHaveCorrectFields(t *testing.T) {
	resources := []shared.Resource{
		{
			ID:           "i-test",
			Service:      "ec2",
			ResourceType: "t3.micro",
			Region:       "us-east-1",
			UsageMetric:  1.0,
			MonthlyCost:  7.50,
			CreatedAt:    time.Now().Add(-30 * 24 * time.Hour),
		},
	}
	findings := Analyze(resources)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.ResourceID != "i-test" {
		t.Errorf("expected ResourceID i-test, got %s", f.ResourceID)
	}
	if f.Service != "ec2" {
		t.Errorf("expected Service ec2, got %s", f.Service)
	}
	if f.Region != "us-east-1" {
		t.Errorf("expected Region us-east-1, got %s", f.Region)
	}
	if f.Description == "" {
		t.Error("expected non-empty Description")
	}
	if f.Recommendation == "" {
		t.Error("expected non-empty Recommendation")
	}
}

// --- sortBySavings ---

func TestSortBySavings_AlreadySorted(t *testing.T) {
	findings := []shared.Finding{
		{MonthlySavings: 100},
		{MonthlySavings: 50},
		{MonthlySavings: 10},
	}
	sortBySavings(findings)
	for i := 1; i < len(findings); i++ {
		if findings[i].MonthlySavings > findings[i-1].MonthlySavings {
			t.Error("not sorted descending")
		}
	}
}

func TestSortBySavings_ReverseSorted(t *testing.T) {
	findings := []shared.Finding{
		{MonthlySavings: 10},
		{MonthlySavings: 50},
		{MonthlySavings: 100},
	}
	sortBySavings(findings)
	if findings[0].MonthlySavings != 100 || findings[2].MonthlySavings != 10 {
		t.Error("not sorted descending after reverse input")
	}
}

func TestSortBySavings_Empty(t *testing.T) {
	var findings []shared.Finding
	sortBySavings(findings) // Should not panic
}

func TestSortBySavings_SingleElement(t *testing.T) {
	findings := []shared.Finding{{MonthlySavings: 42}}
	sortBySavings(findings)
	if findings[0].MonthlySavings != 42 {
		t.Error("single element sorting failed")
	}
}
