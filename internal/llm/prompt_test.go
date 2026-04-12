package llm

import (
	"CloudOracle/internal/shared"
	"strings"
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
			Description:    "EC2 i-001 idle",
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
			Description:    "EBS vol-002 orphan",
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
			Description:    "RDS db-003 oversized",
		},
	}
}

func TestBuildPrompt_ContainsSystemInstruction(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	if !strings.Contains(prompt, "senior cloud cost optimization consultant") {
		t.Error("prompt missing system instruction")
	}
}

func TestBuildPrompt_ContainsTotalFindings(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	if !strings.Contains(prompt, "Total findings: 3") {
		t.Error("prompt missing total findings count")
	}
}

func TestBuildPrompt_ContainsMonthlySavings(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	// 125 + 100 + 90 = 315
	if !strings.Contains(prompt, "$315.00") {
		t.Error("prompt missing correct monthly savings total")
	}
}

func TestBuildPrompt_ContainsAnnualSavings(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	// 315 * 12 = 3780
	if !strings.Contains(prompt, "$3780.00") {
		t.Error("prompt missing correct annual savings")
	}
}

func TestBuildPrompt_ContainsSeverityBreakdown(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	if !strings.Contains(prompt, "HIGH: 2 findings") {
		t.Error("prompt missing HIGH severity count")
	}
	if !strings.Contains(prompt, "MEDIUM: 1 findings") {
		t.Error("prompt missing MEDIUM severity count")
	}
	if !strings.Contains(prompt, "LOW: 0 findings") {
		t.Error("prompt missing LOW severity count")
	}
}

func TestBuildPrompt_ContainsServiceBreakdown(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	if !strings.Contains(prompt, "ec2") {
		t.Error("prompt missing ec2 service")
	}
	if !strings.Contains(prompt, "ebs") {
		t.Error("prompt missing ebs service")
	}
	if !strings.Contains(prompt, "rds") {
		t.Error("prompt missing rds service")
	}
}

func TestBuildPrompt_ContainsTopFindings(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	if !strings.Contains(prompt, "i-001") {
		t.Error("prompt missing top finding i-001")
	}
	if !strings.Contains(prompt, "vol-002") {
		t.Error("prompt missing top finding vol-002")
	}
}

func TestBuildPrompt_EmptyFindings(t *testing.T) {
	prompt := BuildPrompt(nil)
	if !strings.Contains(prompt, "Total findings: 0") {
		t.Error("prompt should handle empty findings")
	}
	if !strings.Contains(prompt, "$0.00") {
		t.Error("prompt should show zero savings for empty findings")
	}
}

func TestBuildPrompt_LimitsTopFindingsToFive(t *testing.T) {
	findings := make([]shared.Finding, 10)
	for i := range findings {
		findings[i] = shared.Finding{
			ResourceID:     "res-" + string(rune('A'+i)),
			Service:        "ec2",
			Severity:       shared.SeverityHigh,
			MonthlySavings: float64(100 - i*10),
			Description:    "test finding",
		}
	}
	prompt := BuildPrompt(findings)
	// Should contain findings 1-5 but not 6+
	if !strings.Contains(prompt, "1.") {
		t.Error("missing finding 1")
	}
	if !strings.Contains(prompt, "5.") {
		t.Error("missing finding 5")
	}
	if strings.Contains(prompt, "6.") {
		t.Error("should not contain finding 6")
	}
}

func TestBuildPrompt_ContainsMonthlyCost(t *testing.T) {
	prompt := BuildPrompt(sampleFindings())
	// 125 + 100 + 180 = 405
	if !strings.Contains(prompt, "$405.00") {
		t.Error("prompt missing correct monthly cost total")
	}
}
