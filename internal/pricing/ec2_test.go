package pricing

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

// scriptedGetter is a productGetter that replays pre-programmed responses
// in call order. It records every call's serviceCode and a copy of the
// filters map so tests can assert exactly what was sent. Different from
// fakeProductGetter (cache_test.go) which always returns the same data —
// EC2 tests need different replies for the compute and EBS queries.
type scriptedGetter struct {
	responses [][]string
	errs      []error
	calls     []scriptedCall
}

type scriptedCall struct {
	service string
	filters map[string]string
}

func (s *scriptedGetter) GetProducts(_ context.Context, service string, filters map[string]string) ([]string, error) {
	idx := len(s.calls)
	cp := make(map[string]string, len(filters))
	for k, v := range filters {
		cp[k] = v
	}
	s.calls = append(s.calls, scriptedCall{service: service, filters: cp})

	if idx < len(s.errs) && s.errs[idx] != nil {
		return nil, s.errs[idx]
	}
	if idx >= len(s.responses) {
		return nil, errors.New("scriptedGetter: no response programmed for this call")
	}
	return s.responses[idx], nil
}

func TestEstimateEC2_WithRootBlock(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {gp3}}}

	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		Tenancy:       "default",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	est, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}

	wantCompute := 0.0832 * HoursPerMonth // 60.736
	wantEBS := 0.08 * 50                  // 4.0
	wantTotal := wantCompute + wantEBS

	if math.Abs(est.MonthlyUSD-wantTotal) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, wantTotal)
	}
	if est.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", est.Currency)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	if len(est.Breakdown) != 2 {
		t.Fatalf("Breakdown len = %d, want 2", len(est.Breakdown))
	}
	if est.Breakdown[0].Component != "Compute" || math.Abs(est.Breakdown[0].MonthlyUSD-wantCompute) > 1e-6 {
		t.Errorf("Breakdown[0] = %+v, want Compute=%v", est.Breakdown[0], wantCompute)
	}
	if est.Breakdown[1].Component != "RootEBS" || math.Abs(est.Breakdown[1].MonthlyUSD-wantEBS) > 1e-6 {
		t.Errorf("Breakdown[1] = %+v, want RootEBS=%v", est.Breakdown[1], wantEBS)
	}

	// Compute query filters
	if len(src.calls) != 2 {
		t.Fatalf("got %d API calls, want 2", len(src.calls))
	}
	c := src.calls[0]
	if c.service != "AmazonEC2" {
		t.Errorf("compute call service = %q", c.service)
	}
	for k, want := range map[string]string{
		"productFamily":   "Compute Instance",
		"instanceType":    "t3.large",
		"regionCode":      "us-east-2",
		"tenancy":         "Shared",
		"operatingSystem": "Linux",
		"preInstalledSw":  "NA",
		"capacitystatus":  "Used",
	} {
		if c.filters[k] != want {
			t.Errorf("compute filter %s = %q, want %q", k, c.filters[k], want)
		}
	}

	// EBS query filters
	e := src.calls[1]
	if e.service != "AmazonEC2" {
		t.Errorf("ebs call service = %q", e.service)
	}
	for k, want := range map[string]string{
		"productFamily": "Storage",
		"volumeApiName": "gp3",
		"regionCode":    "us-east-2",
	} {
		if e.filters[k] != want {
			t.Errorf("ebs filter %s = %q, want %q", k, e.filters[k], want)
		}
	}
}

func TestEstimateEC2_NoRootBlock(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}}}

	attrs := &aws.EC2Attributes{
		InstanceType: "t3.large",
		Tenancy:      "default",
	}
	est, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}
	if len(src.calls) != 1 {
		t.Errorf("got %d API calls, want 1 (no EBS lookup when RootBlockSize=0)", len(src.calls))
	}
	if len(est.Breakdown) != 1 {
		t.Fatalf("Breakdown len = %d, want 1", len(est.Breakdown))
	}
	if est.Breakdown[0].Component != "Compute" {
		t.Errorf("Breakdown[0].Component = %q, want Compute", est.Breakdown[0].Component)
	}
	wantCompute := 0.0832 * HoursPerMonth
	if math.Abs(est.MonthlyUSD-wantCompute) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, wantCompute)
	}
	foundNote := false
	for _, n := range est.Notes {
		if strings.Contains(n, "Root block device unknown") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("Notes missing 'Root block device unknown' caveat: %v", est.Notes)
	}
}

func TestEstimateEC2_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateEC2(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v, want 'nil attrs' error", err)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls on nil attrs, got %d", len(src.calls))
	}
}

func TestEstimateEC2_EmptyInstanceType(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EC2Attributes{}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty InstanceType") {
		t.Fatalf("err = %v, want 'empty InstanceType' error", err)
	}
}

func TestEstimateEC2_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v, want 'empty region' error", err)
	}
}

func TestEstimateEC2_NoComputeProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil {
		t.Fatal("expected error when compute query returns 0 products")
	}
	if !strings.Contains(err.Error(), "no compute price found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEstimateEC2_MultipleComputeProductsErrors(t *testing.T) {
	// Multiple products signals an under-constrained filter set, not a
	// "pick one and warn" scenario. Tightened in milestone 13.6 — see
	// the godoc on lookupComputePrice.
	first := loadFixture(t, "ec2_t3_large_us_east_2.json")
	second := strings.Replace(first, `"USD": "0.0832"`, `"USD": "9.99"`, 1)

	src := &scriptedGetter{responses: [][]string{{first, second}}}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil {
		t.Fatal("expected error on ambiguous compute query")
	}
	if !strings.Contains(err.Error(), "filter under-constrained") {
		t.Errorf("err = %v, want 'filter under-constrained' message", err)
	}
	if !strings.Contains(err.Error(), "instanceType=t3.large") {
		t.Errorf("err missing instanceType context: %v", err)
	}
}

func TestEstimateEC2_BadComputeUnit(t *testing.T) {
	body := strings.Replace(
		loadFixture(t, "ec2_t3_large_us_east_2.json"),
		`"unit": "Hrs"`,
		`"unit": "GB-Mo"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{body}}}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected compute unit Hrs") {
		t.Fatalf("err = %v, want unit-mismatch error", err)
	}
}

func TestEstimateEC2_BadEBSUnit(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	bad := strings.Replace(
		loadFixture(t, "ec2_gp3_us_east_2.json"),
		`"unit": "GB-Mo"`,
		`"unit": "Hrs"`,
		1,
	)
	src := &scriptedGetter{responses: [][]string{{compute}, {bad}}}
	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected EBS unit GB-Mo") {
		t.Fatalf("err = %v, want unit-mismatch error", err)
	}
}

func TestEstimateEC2_TenancyDedicated(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}}}

	attrs := &aws.EC2Attributes{
		InstanceType: "t3.large",
		Tenancy:      "dedicated",
	}
	if _, err := EstimateEC2(context.Background(), src, attrs, "us-east-2"); err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}
	if got := src.calls[0].filters["tenancy"]; got != "Dedicated" {
		t.Errorf("tenancy filter = %q, want Dedicated", got)
	}
}

func TestEstimateEC2_TenancyHost(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}}}

	attrs := &aws.EC2Attributes{
		InstanceType: "t3.large",
		Tenancy:      "host",
	}
	if _, err := EstimateEC2(context.Background(), src, attrs, "us-east-2"); err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}
	if got := src.calls[0].filters["tenancy"]; got != "Host" {
		t.Errorf("tenancy filter = %q, want Host", got)
	}
}

func TestEstimateEC2_ConfidenceAlwaysLow(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {gp3}}}

	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		Tenancy:       "default",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	est, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low (OS is always assumed)", est.Confidence)
	}
	foundOS := false
	for _, n := range est.Notes {
		if strings.Contains(n, "Linux") {
			foundOS = true
			break
		}
	}
	if !foundOS {
		t.Errorf("Notes missing OS=Linux assumption: %v", est.Notes)
	}
}

func TestEstimateEC2_ComputeError(t *testing.T) {
	innerErr := errors.New("AccessDenied")
	src := &scriptedGetter{errs: []error{innerErr}}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner: %v", err)
	}
}

func TestEstimateEC2_EBSError(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	innerErr := errors.New("RateExceeded")
	src := &scriptedGetter{
		responses: [][]string{{compute}, nil},
		errs:      []error{nil, innerErr},
	}
	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner: %v", err)
	}
}

func TestEstimateEC2_NoEBSProducts(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, nil}}
	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no EBS price found") {
		t.Fatalf("err = %v, want 'no EBS price found' error", err)
	}
}

func TestEstimateEC2_BadComputeJSON(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{{"not-json{{{"}}}
	attrs := &aws.EC2Attributes{InstanceType: "t3.large"}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "parsing compute price") {
		t.Fatalf("err = %v, want 'parsing compute price' error", err)
	}
}

func TestEstimateEC2_BadEBSJSON(t *testing.T) {
	compute := loadFixture(t, "ec2_t3_large_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{compute}, {"not-json{{{"}}}
	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	_, err := EstimateEC2(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "parsing EBS price") {
		t.Fatalf("err = %v, want 'parsing EBS price' error", err)
	}
}

func TestMapTenancy(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"default", "Shared"},
		{"", "Shared"},
		{"dedicated", "Dedicated"},
		{"host", "Host"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := mapTenancy(c.in); got != c.want {
				t.Errorf("mapTenancy(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestMapTenancy_UnknownDefaultsToShared(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	got := mapTenancy("on-prem-bring-your-own")
	if got != "Shared" {
		t.Errorf("mapTenancy = %q, want Shared", got)
	}
	if !strings.Contains(logs.String(), "unknown EC2 tenancy") {
		t.Errorf("expected warn log on unknown tenancy, got: %s", logs.String())
	}
}
