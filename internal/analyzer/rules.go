package analyzer

import (
	"CloudOracle/internal/shared"
	"fmt"
	"time"
)

func checkEC2Idle(r shared.Resource) *shared.Finding {
	if r.Service != "ec2" {
		return nil
	}
	if r.UsageMetric >= 5.0 {
		return nil
	}

	daysSinceCreated := time.Since(r.CreatedAt).Hours() / 24

	if daysSinceCreated < 7 {
		return nil
	}

	return &shared.Finding{
		ResourceID:     r.ID,
		Service:        r.Service,
		ResourceType:   r.ResourceType,
		Region:         r.Region,
		Rule:           "ec2-idle",
		Severity:       shared.SeverityHigh,
		MonthlyCost:    r.MonthlyCost,
		MonthlySavings: r.MonthlyCost,
		Description: fmt.Sprintf(
			"EC2 %s (%s) has an average CPU usage of %.1f%%. It has been active for %.0f days.",
			r.ID, r.ResourceType, r.UsageMetric, daysSinceCreated,
		),
		Recommendation: "Consider shutting down or terminating this instance. If necessary, consider a smaller instance type.",
	}
}

func checkRDSOversized(r shared.Resource) *shared.Finding {
	if r.Service != "rds" {
		return nil
	}
	if r.UsageMetric >= 10.0 {
		return nil
	}
	return &shared.Finding{
		ResourceID:     r.ID,
		Service:        r.Service,
		ResourceType:   r.ResourceType,
		Region:         r.Region,
		Rule:           "rds-oversized",
		Severity:       shared.SeverityMedium,
		MonthlyCost:    r.MonthlyCost,
		MonthlySavings: r.MonthlyCost * 0.50,
		Description: fmt.Sprintf(
			"RDS %s (%s) has an average CPU usage of %.1f%%. It is likely oversized.",
			r.ID, r.ResourceType, r.UsageMetric,
		),
		Recommendation: "Consider downgrading to the next smaller RDS instance tier.",
	}
}

func checkEBSOrphan(r shared.Resource) *shared.Finding {
	if r.Service != "ebs" {
		return nil
	}
	if r.UsageMetric > 0 {
		return nil
	}
	daysSinceCreated := time.Since(r.CreatedAt).Hours() / 24
	return &shared.Finding{
		ResourceID:     r.ID,
		Service:        r.Service,
		ResourceType:   r.ResourceType,
		Region:         r.Region,
		Rule:           "ebs-orphan",
		Severity:       shared.SeverityHigh,
		MonthlyCost:    r.MonthlyCost,
		MonthlySavings: r.MonthlyCost,
		Description: fmt.Sprintf(
			"EBS %s (%s) is not attached to any instance. It has been orphaned for %.0f days.",
			r.ID, r.ResourceType, daysSinceCreated,
		),
		Recommendation: "Create a backup snapshot and delete the volume. If it does not contain critical data, delete it directly.",
	}
}

func checkLambdaOverProvisioned(r shared.Resource) *shared.Finding {
	if r.Service != "lambda" {
		return nil
	}
	var memoryMB int

	fmt.Sscanf(r.ResourceType, "%dMB", &memoryMB)

	if memoryMB <= 1024 {
		return nil
	}
	if r.UsageMetric > 100000 {
		return nil
	}
	return &shared.Finding{
		ResourceID:     r.ID,
		Service:        r.Service,
		ResourceType:   r.ResourceType,
		Region:         r.Region,
		Rule:           "lambda-over-provisioned",
		Severity:       shared.SeverityLow,
		MonthlyCost:    r.MonthlyCost,
		MonthlySavings: r.MonthlyCost * 0.30,
		Description: fmt.Sprintf(
			"Lambda %s uses %dMB of memory with only %.0f invocations per month.",
			r.ID, memoryMB, r.UsageMetric,
		),
		Recommendation: "Use AWS Lambda Power Tuning to find the optimal memory setting. 512MB is likely sufficient.",
	}
}
