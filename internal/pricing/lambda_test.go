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
	// x86_64 SKU: usagetype has no -ARM suffix, price is $0.0000041667/GB-Second.
	body := loadFixture(t, "lambda_arm64_us_east_2.json")
	body = strings.Replace(body, `"USD": "0.0000033334"`, `"USD": "0.0000041667"`, 1)
	body = strings.Replace(body, `USE2-Lambda-Provisioned-Concurrency-ARM`, `USE2-Lambda-Provisioned-Concurrency`, -1)
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
	want := 5 * 1.0 * SecondsPerMonth * 0.0000041667 // ~54.75
	if math.Abs(est.MonthlyUSD-want) > 1e-3 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	if got := src.calls[0].filters["usagetype"]; got != "USE2-Lambda-Provisioned-Concurrency" {
		t.Errorf("usagetype = %q, want USE2-Lambda-Provisioned-Concurrency", got)
	}
	if got := src.calls[0].filters["productFamily"]; got != "Serverless" {
		t.Errorf("productFamily = %q, want Serverless", got)
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
	want := 10 * 0.5 * SecondsPerMonth * 0.0000033334 // ~43.80
	if math.Abs(est.MonthlyUSD-want) > 1e-3 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if got := src.calls[0].filters["usagetype"]; got != "USE2-Lambda-Provisioned-Concurrency-ARM" {
		t.Errorf("usagetype = %q, want -ARM suffix", got)
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
		`"unit": "Lambda-GB-Second"`,
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
	if err == nil || !strings.Contains(err.Error(), "expected PC unit Lambda-GB-Second") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateLambda_AmbiguousQueryErrors(t *testing.T) {
	body := loadFixture(t, "lambda_arm64_us_east_2.json")
	second := strings.Replace(body, `"USD": "0.0000033334"`, `"USD": "0.99"`, 1)
	src := &scriptedGetter{responses: [][]string{{body, second}}}
	attrs := &aws.LambdaAttributes{
		FunctionName:           "f",
		MemorySize:             512,
		Architecture:           "arm64",
		ProvisionedConcurrency: 1,
	}
	_, err := EstimateLambda(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "filter under-constrained") {
		t.Fatalf("err = %v, want under-constrained error", err)
	}
}

func TestLambdaArchitectureSuffix(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"x86_64", "", false},
		{"", "", false},
		{"arm64", "-ARM", false},
		{"weird", "", true},
	}
	for _, c := range cases {
		got, err := lambdaArchitectureSuffix(c.in)
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

func TestRegionPrefix_Known(t *testing.T) {
	cases := map[string]string{
		"us-east-1":      "USE1",
		"us-east-2":      "USE2",
		"us-west-1":      "USW1",
		"us-west-2":      "USW2",
		"eu-west-1":      "EUW1",
		"eu-central-1":   "EUC1",
		"ap-southeast-1": "APS1",
		"ap-northeast-1": "APN1",
	}
	for region, want := range cases {
		if got := regionPrefix(region); got != want {
			t.Errorf("regionPrefix(%q) = %q, want %q", region, got, want)
		}
	}
}

func TestRegionPrefix_UnknownFallsBackToInput(t *testing.T) {
	got := regionPrefix("af-south-1")
	if got != "af-south-1" {
		t.Errorf("regionPrefix(af-south-1) = %q, want literal fallback", got)
	}
}
