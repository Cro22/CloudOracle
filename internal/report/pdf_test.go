package report

import (
	"CloudOracle/internal/shared"
	"os"
	"path/filepath"
	"testing"
)

func sampleFindings() []shared.Finding {
	return []shared.Finding{
		{
			ResourceID:     "i-001",
			Service:        "ec2",
			ResourceType:   "c5.xlarge",
			Region:         "us-east-1",
			Rule:           "ec2-idle",
			Severity:       shared.SeverityHigh,
			MonthlyCost:    125.00,
			MonthlySavings: 125.00,
			Description:    "EC2 i-001 has low CPU usage",
			Recommendation: "Terminate the instance",
		},
		{
			ResourceID:     "vol-002",
			Service:        "ebs",
			ResourceType:   "gp3-1000GB",
			Region:         "us-west-2",
			Rule:           "ebs-orphan",
			Severity:       shared.SeverityHigh,
			MonthlyCost:    100.00,
			MonthlySavings: 100.00,
			Description:    "EBS vol-002 is orphaned",
			Recommendation: "Delete the volume",
		},
		{
			ResourceID:     "db-003",
			Service:        "rds",
			ResourceType:   "db.r5.large",
			Region:         "eu-west-1",
			Rule:           "rds-oversized",
			Severity:       shared.SeverityMedium,
			MonthlyCost:    180.00,
			MonthlySavings: 90.00,
			Description:    "RDS db-003 is oversized",
			Recommendation: "Downgrade instance",
		},
	}
}

func TestGeneratePDF_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-report.pdf")

	err := GeneratePDF(sampleFindings(), "", path)
	if err != nil {
		t.Fatalf("GeneratePDF failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("PDF file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("PDF file is empty")
	}
}

func TestGeneratePDF_WithAISummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-report-ai.pdf")

	aiSummary := "This is an AI-generated executive summary about cloud cost optimization."
	err := GeneratePDF(sampleFindings(), aiSummary, path)
	if err != nil {
		t.Fatalf("GeneratePDF with AI summary failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("PDF file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("PDF file is empty")
	}
}

func TestGeneratePDF_WithoutAISummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-report-no-ai.pdf")

	err := GeneratePDF(sampleFindings(), "", path)
	if err != nil {
		t.Fatalf("GeneratePDF without AI summary failed: %v", err)
	}

	_, err = os.Stat(path)
	if err != nil {
		t.Fatalf("PDF file not created: %v", err)
	}
}

func TestGeneratePDF_EmptyFindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-empty.pdf")

	err := GeneratePDF(nil, "", path)
	if err != nil {
		t.Fatalf("GeneratePDF with empty findings failed: %v", err)
	}

	_, err = os.Stat(path)
	if err != nil {
		t.Fatalf("PDF file not created: %v", err)
	}
}

func TestGeneratePDF_ManyFindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-many.pdf")

	findings := make([]shared.Finding, 100)
	for i := range findings {
		findings[i] = shared.Finding{
			ResourceID:     "res-" + string(rune('A'+i%26)),
			Service:        "ec2",
			ResourceType:   "t3.micro",
			Region:         "us-east-1",
			Severity:       shared.SeverityHigh,
			MonthlyCost:    7.50,
			MonthlySavings: 7.50,
			Description:    "Test finding for page break handling",
			Recommendation: "Test recommendation",
		}
	}

	err := GeneratePDF(findings, "AI summary for many findings", path)
	if err != nil {
		t.Fatalf("GeneratePDF with many findings failed: %v", err)
	}
}

func TestGeneratePDF_InvalidPath(t *testing.T) {
	err := GeneratePDF(sampleFindings(), "", "/nonexistent/dir/report.pdf")
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestGeneratePDF_AllSeverities(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-severities.pdf")

	findings := []shared.Finding{
		{Severity: shared.SeverityHigh, ResourceID: "r1", Service: "ec2", Description: "high", Recommendation: "fix"},
		{Severity: shared.SeverityMedium, ResourceID: "r2", Service: "rds", Description: "medium", Recommendation: "fix"},
		{Severity: shared.SeverityLow, ResourceID: "r3", Service: "lambda", Description: "low", Recommendation: "fix"},
	}

	err := GeneratePDF(findings, "", path)
	if err != nil {
		t.Fatalf("GeneratePDF with all severities failed: %v", err)
	}
}

// --- truncate ---

func TestTruncate_ShortString(t *testing.T) {
	result := truncate("hello", 10)
	if result != "hello" {
		t.Errorf("expected hello, got %s", result)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	result := truncate("hello", 5)
	if result != "hello" {
		t.Errorf("expected hello, got %s", result)
	}
}

func TestTruncate_LongString(t *testing.T) {
	result := truncate("hello world", 8)
	if result != "hello..." {
		t.Errorf("expected hello..., got %s", result)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	result := truncate("", 10)
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}
