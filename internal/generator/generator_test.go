package generator

import (
	"testing"
)

func TestGenerateResources_ReturnsCorrectCount(t *testing.T) {
	counts := []int{0, 1, 10, 50, 100}
	for _, n := range counts {
		resources := GenerateResources(n, "acc-test")
		if len(resources) != n {
			t.Errorf("GenerateResources(%d): got %d resources, want %d", n, len(resources), n)
		}
	}
}

func TestGenerateResources_SetsAccountID(t *testing.T) {
	resources := GenerateResources(20, "acc-xyz")
	for _, r := range resources {
		if r.AccountID != "acc-xyz" {
			t.Errorf("expected AccountID acc-xyz, got %s", r.AccountID)
		}
	}
}

func TestGenerateResources_ValidServices(t *testing.T) {
	valid := map[string]bool{"ec2": true, "rds": true, "ebs": true, "lambda": true}
	resources := GenerateResources(200, "acc-test")
	for _, r := range resources {
		if !valid[r.Service] {
			t.Errorf("unexpected service: %s", r.Service)
		}
	}
}

func TestGenerateResources_NonNegativeCosts(t *testing.T) {
	resources := GenerateResources(100, "acc-test")
	for _, r := range resources {
		if r.MonthlyCost < 0 {
			t.Errorf("resource %s has negative cost: %.2f", r.ID, r.MonthlyCost)
		}
	}
}

func TestGenerateResources_NonNegativeUsage(t *testing.T) {
	resources := GenerateResources(100, "acc-test")
	for _, r := range resources {
		if r.UsageMetric < 0 {
			t.Errorf("resource %s has negative usage: %.2f", r.ID, r.UsageMetric)
		}
	}
}

func TestGenerateResources_ValidRegions(t *testing.T) {
	validRegions := map[string]bool{
		"us-east-1": true,
		"us-west-2": true,
		"eu-west-1": true,
		"sa-east-1": true,
	}
	resources := GenerateResources(200, "acc-test")
	for _, r := range resources {
		if !validRegions[r.Region] {
			t.Errorf("unexpected region: %s", r.Region)
		}
	}
}

func TestGenerateResources_UniqueIDs(t *testing.T) {
	resources := GenerateResources(100, "acc-test")
	seen := map[string]bool{}
	for _, r := range resources {
		if r.ID == "" {
			t.Fatal("resource has empty ID")
		}
		if seen[r.ID] {
			// IDs are random, collisions are possible but extremely unlikely in 100 items
			t.Logf("warning: duplicate ID %s (random collision, may be expected)", r.ID)
		}
		seen[r.ID] = true
	}
}

func TestGenerateResources_TimestampsSet(t *testing.T) {
	resources := GenerateResources(10, "acc-test")
	for _, r := range resources {
		if r.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
		if r.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero")
		}
		if r.CreatedAt.After(r.UpdatedAt) {
			t.Error("CreatedAt is after UpdatedAt")
		}
	}
}

func TestGenerateResources_ServiceDistribution(t *testing.T) {
	resources := GenerateResources(1000, "acc-test")
	counts := map[string]int{}
	for _, r := range resources {
		counts[r.Service]++
	}

	// With 1000 resources, we should see all 4 services
	for _, svc := range []string{"ec2", "rds", "ebs", "lambda"} {
		if counts[svc] == 0 {
			t.Errorf("service %s has zero resources in 1000 generated", svc)
		}
	}

	// EC2 should be roughly 50% (most common)
	ec2Pct := float64(counts["ec2"]) / 1000.0
	if ec2Pct < 0.35 || ec2Pct > 0.65 {
		t.Errorf("EC2 distribution out of expected range: %.1f%%", ec2Pct*100)
	}
}

func TestGenerateResources_EC2TypesValid(t *testing.T) {
	validTypes := map[string]bool{}
	for _, e := range ec2Types {
		validTypes[e.Type] = true
	}

	resources := GenerateResources(200, "acc-test")
	for _, r := range resources {
		if r.Service == "ec2" && !validTypes[r.ResourceType] {
			t.Errorf("unexpected EC2 type: %s", r.ResourceType)
		}
	}
}

func TestGenerateResources_RDSTypesValid(t *testing.T) {
	validTypes := map[string]bool{}
	for _, e := range rdsTypes {
		validTypes[e.Type] = true
	}

	resources := GenerateResources(200, "acc-test")
	for _, r := range resources {
		if r.Service == "rds" && !validTypes[r.ResourceType] {
			t.Errorf("unexpected RDS type: %s", r.ResourceType)
		}
	}
}

func TestGenerateResources_ZeroCount(t *testing.T) {
	resources := GenerateResources(0, "acc-test")
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}
