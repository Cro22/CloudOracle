package llm

import (
	"CloudOracle/internal/shared"
	"fmt"
	"strings"
)

func BuildPrompt(findings []shared.Finding) string {
	var totalSavings, totalCost float64
	severityCounts := map[shared.Severity]int{}
	serviceCounts := map[string]int{}
	serviceSavings := map[string]float64{}

	for _, f := range findings {
		totalSavings += f.MonthlySavings
		totalCost += f.MonthlyCost
		severityCounts[f.Severity]++
		serviceCounts[f.Service]++
		serviceSavings[f.Service] += f.MonthlySavings
	}

	var sb strings.Builder

	sb.WriteString("You are a senior cloud cost optimization consultant. ")
	sb.WriteString("Write an executive summary for a CTO/CFO based on the following AWS cost analysis findings. ")
	sb.WriteString("The summary must be 3-4 short paragraphs, professional but accessible (avoid jargon). ")
	sb.WriteString("Focus on: (1) the financial impact, (2) which problems matter most, (3) recommended next steps in priority order. ")
	sb.WriteString("Do not use bullet points or markdown. Write in flowing prose. Do not greet or sign off.\n\n")

	sb.WriteString("=== ANALYSIS DATA ===\n\n")
	sb.WriteString(fmt.Sprintf("Total findings: %d\n", len(findings)))
	sb.WriteString(fmt.Sprintf("Current monthly cost analyzed: $%.2f\n", totalCost))
	sb.WriteString(fmt.Sprintf("Potential monthly savings: $%.2f\n", totalSavings))
	sb.WriteString(fmt.Sprintf("Potential annual savings: $%.2f\n\n", totalSavings*12))

	sb.WriteString("Severity breakdown:\n")
	sb.WriteString(fmt.Sprintf("- HIGH: %d findings\n", severityCounts[shared.SeverityHigh]))
	sb.WriteString(fmt.Sprintf("- MEDIUM: %d findings\n", severityCounts[shared.SeverityMedium]))
	sb.WriteString(fmt.Sprintf("- LOW: %d findings\n\n", severityCounts[shared.SeverityLow]))

	sb.WriteString("Savings by service:\n")
	for service, savings := range serviceSavings {
		sb.WriteString(fmt.Sprintf("- %s: $%.2f/month (%d findings)\n", service, savings, serviceCounts[service]))
	}
	sb.WriteString("\n")

	sb.WriteString("Top 5 individual findings by savings:\n")
	limit := 5
	if len(findings) < limit {
		limit = len(findings)
	}
	for i := 0; i < limit; i++ {
		f := findings[i]
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (%s) - $%.2f/month - %s\n",
			i+1, f.Severity, f.ResourceID, f.Service, f.MonthlySavings, f.Description))
	}

	return sb.String()

}
