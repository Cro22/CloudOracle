package analyzer

import "CloudOracle/internal/shared"

type Rule func(r shared.Resource) *shared.Finding

func Analyze(resources []shared.Resource) []shared.Finding {
	rules := []Rule{
		checkEC2Idle,
		checkRDSOversized,
		checkEBSOrphan,
		checkLambdaOverProvisioned,
	}

	var findings []shared.Finding
	for _, resource := range resources {
		for _, rule := range rules {
			if finding := rule(resource); finding != nil {
				findings = append(findings, *finding)
			}
		}
	}
	sortBySavings(findings)

	return findings
}

func sortBySavings(findings []shared.Finding) {
	for i := 0; i < len(findings); i++ {
		for j := i + 1; j < len(findings); j++ {
			if findings[j].MonthlySavings > findings[i].MonthlySavings {
				findings[i], findings[j] = findings[j], findings[i]
			}
		}
	}
}
