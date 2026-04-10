package generator

import (
	"CloudOracle/internal/shared"
	"fmt"
	"math/rand/v2"
	"time"
)

var ec2Types = []struct {
	Type string
	Cost float64
}{
	{"t3.micro", 7.50},
	{"t3.small", 15.00},
	{"t3.medium", 30.00},
	{"t3.large", 60.00},
	{"m5.large", 70.00},
	{"m5.xlarge", 140.00},
	{"c5.xlarge", 125.00},
}
var rdsTypes = []struct {
	Type string
	Cost float64
}{
	{"db.t3.micro", 15.00},
	{"db.t3.small", 30.00},
	{"db.r5.large", 180.00},
	{"db.r5.xlarge", 360.00},
}

var regions = []string{
	"us-east-1",
	"us-west-2",
	"eu-west-1",
	"sa-east-1",
}

func GenerateResources(n int, accountID string) []shared.Resource {
	resources := make([]shared.Resource, 0, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		roll := rand.Float64()

		var r shared.Resource
		switch {
		case roll < 0.50:
			r = generateEC2(i, accountID, now)
		case roll < 0.70:
			r = generateRDS(i, accountID, now)
		case roll < 0.95:
			r = generateEBS(i, accountID, now)
		default:
			r = generateLambda(i, accountID, now)
		}
		resources = append(resources, r)

	}
	return resources
}

func generateEC2(idx int, accountID string, now time.Time) shared.Resource {
	instance := ec2Types[rand.IntN(len(ec2Types))]
	var cpu float64
	roll := rand.Float64()
	switch {
	case roll < 0.15:
		cpu = rand.Float64() * 5
	case roll < 0.75:
		cpu = 10 + rand.Float64()*30
	case roll < 0.95:
		cpu = 40 + rand.Float64()*30
	default:
		cpu = 70 + rand.Float64()*25
	}
	return shared.Resource{
		ID:           fmt.Sprintf("i-%d", rand.Uint32()),
		AccountID:    accountID,
		Service:      "EC2",
		ResourceType: instance.Type,
		Region:       regions[rand.IntN(len(regions))],
		MonthlyCost:  instance.Cost,
		UsageMetric:  cpu,
		CreatedAt:    now.Add(-time.Duration(rand.IntN(365)) * 24 * time.Hour),
		UpdatedAt:    now,
	}
}

func generateRDS(idx int, accountID string, now time.Time) shared.Resource {
	instance := rdsTypes[rand.IntN(len(rdsTypes))]

	var cpu float64
	if rand.Float64() < 0.20 {
		cpu = rand.Float64() * 10
	} else {
		cpu = 20 + rand.Float64()*50
	}

	return shared.Resource{
		ID:           fmt.Sprintf("db-%08x", rand.Uint32()),
		AccountID:    accountID,
		Service:      "rds",
		ResourceType: instance.Type,
		Region:       regions[rand.IntN(len(regions))],
		MonthlyCost:  instance.Cost,
		UsageMetric:  cpu,
		CreatedAt:    now.Add(-time.Duration(rand.IntN(365)) * 24 * time.Hour),
		UpdatedAt:    now,
	}
}

func generateEBS(idx int, accountID string, now time.Time) shared.Resource {
	sizeGB := []int{50, 100, 250, 500, 1000}[rand.IntN(5)]
	cost := float64(sizeGB) * 0.10

	var usage float64
	if rand.Float64() < 0.15 {
		usage = 0
	} else {
		usage = 20 + rand.Float64()*70
	}

	return shared.Resource{
		ID:           fmt.Sprintf("vol-%08x", rand.Uint32()),
		AccountID:    accountID,
		Service:      "ebs",
		ResourceType: fmt.Sprintf("gp3-%dGB", sizeGB),
		Region:       regions[rand.IntN(len(regions))],
		MonthlyCost:  cost,
		UsageMetric:  usage,
		CreatedAt:    now.Add(-time.Duration(rand.IntN(365)) * 24 * time.Hour),
		UpdatedAt:    now,
	}
}

func generateLambda(idx int, accountID string, now time.Time) shared.Resource {
	memoryMB := []int{128, 256, 512, 1024, 2048}[rand.IntN(5)]
	invocations := rand.Float64() * 1000000
	cost := (float64(memoryMB) / 1024) * (invocations / 1000000) * 0.20

	return shared.Resource{
		ID:           fmt.Sprintf("fn-%08x", rand.Uint32()),
		AccountID:    accountID,
		Service:      "lambda",
		ResourceType: fmt.Sprintf("%dMB", memoryMB),
		Region:       regions[rand.IntN(len(regions))],
		MonthlyCost:  cost,
		UsageMetric:  invocations,
		CreatedAt:    now.Add(-time.Duration(rand.IntN(365)) * 24 * time.Hour),
		UpdatedAt:    now,
	}
}
