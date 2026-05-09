package pricing

import (
	"context"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

func TestEstimateNATGateway_HappyPath(t *testing.T) {
	body := loadFixture(t, "nat_gateway_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{body}}}

	attrs := &aws.NATGatewayAttributes{
		SubnetID:         "subnet-abc",
		ConnectivityType: "public",
	}
	est, err := EstimateNATGateway(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateNATGateway: %v", err)
	}
	want := 0.045 * HoursPerMonth // 32.85
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
	if est.Currency != "USD" {
		t.Errorf("Currency = %q", est.Currency)
	}
	if len(est.Breakdown) != 1 || est.Breakdown[0].Component != "Gateway" {
		t.Errorf("Breakdown = %+v", est.Breakdown)
	}
	foundNote := false
	for _, n := range est.Notes {
		if strings.Contains(n, "data processing") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("Notes missing data-processing caveat: %v", est.Notes)
	}

	// Filters
	c := src.calls[0]
	if c.service != "AmazonEC2" {
		t.Errorf("service = %q, want AmazonEC2", c.service)
	}
	for k, want := range map[string]string{
		"productFamily":    "NAT Gateway",
		"regionCode":       "us-east-2",
		"groupDescription": "Hourly charge for NAT Gateways",
	} {
		if c.filters[k] != want {
			t.Errorf("filter %s = %q, want %q", k, c.filters[k], want)
		}
	}
}

func TestEstimateNATGateway_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateNATGateway(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateNATGateway_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.NATGatewayAttributes{SubnetID: "subnet-abc"}
	_, err := EstimateNATGateway(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateNATGateway_NoProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	attrs := &aws.NATGatewayAttributes{SubnetID: "subnet-abc"}
	_, err := EstimateNATGateway(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no NAT Gateway price found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateNATGateway_BadUnit(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "nat_gateway_us_east_2.json"),
		`"unit": "Hrs"`,
		`"unit": "GB-Mo"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.NATGatewayAttributes{SubnetID: "subnet-abc"}
	_, err := EstimateNATGateway(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected unit Hrs") {
		t.Fatalf("err = %v", err)
	}
}
