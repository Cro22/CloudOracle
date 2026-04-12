package shared

import "time"

type Resource struct {
	ID           string
	AccountID    string
	Service      string
	ResourceType string
	Region       string
	MonthlyCost  float64
	UsageMetric  float64
	Tags         map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
