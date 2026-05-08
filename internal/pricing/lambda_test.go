package pricing

import (
	"context"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

func TestEstimateLambda_ProvisionedConcurrencyZero_NoAPICall(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.LambdaAttributes{
		FunctionName: "f",
		MemorySize:   128,
		Architecture: "x86_64",
	}
	est, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateLambda: %v", err)
	}
	if est.MonthlyUSD != 0 {
		t.Errorf("MonthlyUSD = %v, want 0", est.MonthlyUSD)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls when PC=0, got %d", len(src.calls))
	}
	foundNote := false
	for _, n := range est.Notes {
		if strings.Contains(n, "Standing cost is $0") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("Notes missing 'Standing cost is $0' note: %v", est.Notes)
	}
}

func TestEstimateLambda_ProvisionedConcurrency_X86_64(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "lambda_arm64_us_east_2.json"),
		`"USD": "0.012"`,
		`"USD": "0.015"`,
		1,
	)
	body = strings.Replace(body, `"architecture": "ARM"`, `"architecture": "x86"`, 1)
	src := &scriptedGetter{responses: [][]string{{body}}}

	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             1024,
		Architecture:           "x86_64",
		ProvisionedConcurrency: 5,
	}
	est, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateLambda: %v", err)
	}
	want := 5 * 1.0 * HoursPerMonth * 0.015 // 54.75
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	if got := src.calls[0].filters["architecture"]; got != "x86" {
		t.Errorf("architecture filter = %q, want x86", got)
	}
	if got := src.calls[0].filters["productFamily"]; got != "Provisioned Concurrency" {
		t.Errorf("productFamily = %q", got)
	}
	if got := src.calls[0].service; got != "AWSLambda" {
		t.Errorf("service = %q, want AWSLambda", got)
	}
}

func TestEstimateLambda_ProvisionedConcurrency_ARM64(t *testing.T) {
	body := loadFixture(t, "lambda_arm64_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{body}}}

	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             512,
		Architecture:           "arm64",
		ProvisionedConcurrency: 10,
	}
	est, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateLambda: %v", err)
	}
	want := 10 * 0.5 * HoursPerMonth * 0.012 // 43.8
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if got := src.calls[0].filters["architecture"]; got != "ARM" {
		t.Errorf("architecture filter = %q, want ARM", got)
	}
}

func TestEstimateLambda_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateLambda(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLambda_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.LambdaAttributes{FunctionName: "f"}
	_, err := EstimateLambda(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLambda_UnknownArchitecture(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             512,
		Architecture:           "weird",
		ProvisionedConcurrency: 1,
	}
	_, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "unknown architecture") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLambda_NoProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             512,
		Architecture:           "x86_64",
		ProvisionedConcurrency: 1,
	}
	_, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no provisioned concurrency price found") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLambda_BadUnit(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "lambda_arm64_us_east_2.json"),
		`"unit": "GB-Hour"`,
		`"unit": "Hrs"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             512,
		Architecture:           "arm64",
		ProvisionedConcurrency: 1,
	}
	_, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected PC unit GB-Hour") {
		t.Fatalf("err = %v", err)
	}
}

func TestMapLambdaArchitecture(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"x86_64", "x86", false},
		{"", "x86", false},
		{"arm64", "ARM", false},
		{"weird", "", true},
	}
	for _, c := range cases {
		got, err := mapLambdaArchitecture(c.in)
		if c.err {
			if err == nil {
				t.Errorf("input %q: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("input %q: unexpected err: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("input %q: got %q, want %q", c.in, got, c.want)
		}
	}
}
