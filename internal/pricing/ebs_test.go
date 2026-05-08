package pricing

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

// minimalGBMoProduct is a synthetic but well-formed Pricing API product
// JSON for tests that don't care about the real values, only that the
// parser succeeds and the unit is "GB-Mo".
const minimalGBMoProduct = `{"terms":{"OnDemand":{"S.T":{"priceDimensions":{"S.T.D":{"unit":"GB-Mo","pricePerUnit":{"USD":"0.10"}}}}}}}`

func TestLookupEBSStoragePrice_AllSupportedTypes(t *testing.T) {
	types := []string{"gp2", "gp3", "io1", "io2", "st1", "sc1", "standard"}
	for _, vt := range types {
		t.Run(vt, func(t *testing.T) {
			src := &scriptedGetter{responses: [][]string{{minimalGBMoProduct}}}
			price, err := lookupEBSStoragePrice(context.Background(), src, vt, "us-east-2")
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if math.Abs(price-0.10) > 1e-9 {
				t.Errorf("price = %v, want 0.10", price)
			}
			if len(src.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(src.calls))
			}
			if got := src.calls[0].service; got != "AmazonEC2" {
				t.Errorf("service = %q, want AmazonEC2", got)
			}
			if got := src.calls[0].filters["volumeApiName"]; got != vt {
				t.Errorf("volumeApiName = %q, want %q", got, vt)
			}
			if got := src.calls[0].filters["productFamily"]; got != "Storage" {
				t.Errorf("productFamily = %q, want Storage", got)
			}
			if got := src.calls[0].filters["regionCode"]; got != "us-east-2" {
				t.Errorf("regionCode = %q", got)
			}
		})
	}
}

func TestLookupEBSStoragePrice_UnknownType(t *testing.T) {
	src := &scriptedGetter{}
	_, err := lookupEBSStoragePrice(context.Background(), src, "weird", "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "unknown volume type") {
		t.Fatalf("err = %v, want unknown-type error", err)
	}
	if len(src.calls) != 0 {
		t.Errorf("expected no API calls on unknown type, got %d", len(src.calls))
	}
}

func TestLookupEBSStoragePrice_EmptyType(t *testing.T) {
	src := &scriptedGetter{}
	_, err := lookupEBSStoragePrice(context.Background(), src, "", "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty volume type") {
		t.Fatalf("err = %v, want empty-type error", err)
	}
}

func TestLookupEBSStoragePrice_NoProducts(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{nil}}
	_, err := lookupEBSStoragePrice(context.Background(), src, "gp3", "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "no EBS price found") {
		t.Fatalf("err = %v, want 'no EBS price found' error", err)
	}
}

func TestLookupEBSStoragePrice_MultipleProductsErrors(t *testing.T) {
	// After 13.6 tightening, ambiguity is a hard error rather than a warn.
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	second := strings.Replace(gp3, `"USD": "0.08"`, `"USD": "9.99"`, 1)
	src := &scriptedGetter{responses: [][]string{{gp3, second}}}

	_, err := lookupEBSStoragePrice(context.Background(), src, "gp3", "us-east-2")
	if err == nil {
		t.Fatal("expected error on ambiguous EBS query")
	}
	if !strings.Contains(err.Error(), "filter under-constrained") {
		t.Errorf("err = %v, want 'filter under-constrained' message", err)
	}
	if !strings.Contains(err.Error(), "volumeType=gp3") {
		t.Errorf("err missing volumeType context: %v", err)
	}
}

func TestLookupEBSStoragePrice_BadUnit(t *testing.T) {
	body := strings.Replace(minimalGBMoProduct, `"unit":"GB-Mo"`, `"unit":"Hrs"`, 1)
	src := &scriptedGetter{responses: [][]string{{body}}}
	_, err := lookupEBSStoragePrice(context.Background(), src, "gp3", "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "expected EBS unit GB-Mo") {
		t.Fatalf("err = %v, want unit-mismatch error", err)
	}
}

func TestLookupEBSStoragePrice_PropagatesAPIError(t *testing.T) {
	innerErr := errors.New("RateExceeded")
	src := &scriptedGetter{errs: []error{innerErr}}
	_, err := lookupEBSStoragePrice(context.Background(), src, "gp3", "us-east-2")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner: %v", err)
	}
}

func TestEstimateEBS_GP3DefaultIOPS_ConfidenceMedium(t *testing.T) {
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{gp3}}}

	attrs := &aws.EBSAttributes{
		Type: "gp3",
		Size: 100,
	}
	est, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	want := 0.08 * 100
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
	if len(est.Breakdown) != 1 || est.Breakdown[0].Component != "Storage" {
		t.Errorf("Breakdown = %+v", est.Breakdown)
	}
	if est.Currency != "USD" {
		t.Errorf("Currency = %q", est.Currency)
	}
}

func TestEstimateEBS_GP3HighIOPS_ConfidenceLow(t *testing.T) {
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{gp3}}}
	attrs := &aws.EBSAttributes{Type: "gp3", Size: 100, Iops: 5000}
	est, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low (Iops > 3000)", est.Confidence)
	}
}

func TestEstimateEBS_GP3HighThroughput_ConfidenceLow(t *testing.T) {
	gp3 := loadFixture(t, "ec2_gp3_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{gp3}}}
	attrs := &aws.EBSAttributes{Type: "gp3", Size: 100, Throughput: 250}
	est, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low (Throughput > 125)", est.Confidence)
	}
}

func TestEstimateEBS_IO1_ConfidenceLowWithNote(t *testing.T) {
	io1 := loadFixture(t, "ebs_io1_us_east_2.json")
	src := &scriptedGetter{responses: [][]string{{io1}}}

	attrs := &aws.EBSAttributes{Type: "io1", Size: 200, Iops: 4000}
	est, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	want := 0.125 * 200
	if math.Abs(est.MonthlyUSD-want) > 1e-6 {
		t.Errorf("MonthlyUSD = %v, want %v", est.MonthlyUSD, want)
	}
	if est.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want low", est.Confidence)
	}
	foundIOPS := false
	for _, n := range est.Notes {
		if strings.Contains(n, "io1/io2 IOPS billing") {
			foundIOPS = true
			break
		}
	}
	if !foundIOPS {
		t.Errorf("Notes missing io1/io2 IOPS caveat: %v", est.Notes)
	}
	// volumeApiName filter should be "io1"
	if got := src.calls[0].filters["volumeApiName"]; got != "io1" {
		t.Errorf("volumeApiName = %q, want io1", got)
	}
}

func TestEstimateEBS_GP2_ConfidenceMedium(t *testing.T) {
	src := &scriptedGetter{responses: [][]string{{minimalGBMoProduct}}}
	attrs := &aws.EBSAttributes{Type: "gp2", Size: 50}
	est, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	if est.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want medium", est.Confidence)
	}
}

func TestEstimateEBS_NilAttrs(t *testing.T) {
	src := &scriptedGetter{}
	_, err := EstimateEBS(context.Background(), src, nil, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "nil attrs") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateEBS_EmptyRegion(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EBSAttributes{Type: "gp3", Size: 50}
	_, err := EstimateEBS(context.Background(), src, attrs, "")
	if err == nil || !strings.Contains(err.Error(), "empty region") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateEBS_EmptyType(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EBSAttributes{Size: 50}
	_, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "empty Type") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateEBS_SizeZero(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EBSAttributes{Type: "gp3"}
	_, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "Size must be > 0") {
		t.Fatalf("err = %v", err)
	}
}

func TestEstimateEBS_UnknownType(t *testing.T) {
	src := &scriptedGetter{}
	attrs := &aws.EBSAttributes{Type: "weird", Size: 50}
	_, err := EstimateEBS(context.Background(), src, attrs, "us-east-2")
	if err == nil || !strings.Contains(err.Error(), "unknown volume type") {
		t.Fatalf("err = %v", err)
	}
}
