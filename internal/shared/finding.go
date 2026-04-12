package shared

type Severity string

const (
	SeverityHigh   Severity = "High"
	SeverityMedium Severity = "Medium"
	SeverityLow    Severity = "Low"
)

type Finding struct {
	ResourceID     string
	Service        string
	ResourceType   string
	Region         string
	Rule           string
	Severity       Severity
	MonthlyCost    float64
	MonthlySavings float64
	Description    string
	Recommendation string
}
