package analyzer

import (
	"CloudOracle/internal/shared"
	"slices"
)

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
	slices.SortFunc(findings, func(a, b shared.Finding) int {
		return int(b.MonthlySavings*100) - int(a.MonthlySavings*100)
	})
}
