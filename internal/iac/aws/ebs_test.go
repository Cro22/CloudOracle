package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractEBS_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"type":              "gp3",
		"size":              float64(500),
		"iops":              float64(3000),
		"throughput":        float64(125),
		"availability_zone": "us-east-1a",
		"encrypted":         true,
		"some_extra_field":  "ignored",
	}
	got, err := ExtractEBS(attrs)
	if err != nil {
		t.Fatalf("ExtractEBS: %v", err)
	}
	want := EBSAttributes{
		Type:             "gp3",
		Size:             500,
		Iops:             3000,
		Throughput:       125,
		AvailabilityZone: "us-east-1a",
		Encrypted:        true,
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractEBS_OnlyRequiredFields(t *testing.T) {
	attrs := map[string]interface{}{
		"type": "standard",
		"size": float64(100),
	}
	got, err := ExtractEBS(attrs)
	if err != nil {
		t.Fatalf("ExtractEBS: %v", err)
	}
	if got.Type != "standard" || got.Size != 100 {
		t.Errorf("required fields wrong: %+v", got)
	}
	// Optional defaults.
	if got.Iops != 0 || got.Throughput != 0 {
		t.Errorf("Iops/Throughput should default to 0: %+v", got)
	}
	if got.Encrypted {
		t.Error("Encrypted should default to false")
	}
}

// TestExtractEBS_NoCrossAttrValidation confirms the explicit non-feature:
// we do NOT reject io1 without iops here. That validation lives in the
// pricing engine.
func TestExtractEBS_NoCrossAttrValidation(t *testing.T) {
	attrs := map[string]interface{}{
		"type": "io1",
		"size": float64(100),
		// iops absent — pricing might fail later, but the extractor must not.
	}
	got, err := ExtractEBS(attrs)
	if err != nil {
		t.Fatalf("io1 without iops should NOT error here: %v", err)
	}
	if got.Iops != 0 {
		t.Errorf("Iops = %d, want 0", got.Iops)
	}
}

func TestExtractEBS_MissingRequired(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{
			"no type",
			map[string]interface{}{"size": float64(100)},
			`aws_ebs_volume: missing required attribute "type"`,
		},
		{
			"no size",
			map[string]interface{}{"type": "gp3"},
			`aws_ebs_volume: missing required attribute "size"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractEBS(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != c.want {
				t.Errorf("error = %q\nwant %q", err.Error(), c.want)
			}
		})
	}
}

func TestExtractEBS_WrongTypes(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{"type as int", map[string]interface{}{"type": 42, "size": float64(10)}, "type"},
		{"size as string", map[string]interface{}{"type": "gp3", "size": "100"}, "size"},
		{"throughput fractional", map[string]interface{}{
			"type":       "gp3",
			"size":       float64(10),
			"throughput": 1.5,
		}, "throughput"},
		{"encrypted as int", map[string]interface{}{
			"type":      "gp3",
			"size":      float64(10),
			"encrypted": 1,
		}, "encrypted"},
		{"iops fractional", map[string]interface{}{
			"type": "gp3",
			"size": float64(10),
			"iops": 1.5,
		}, "iops"},
		{"availability_zone as int", map[string]interface{}{
			"type":              "gp3",
			"size":              float64(10),
			"availability_zone": 1,
		}, "availability_zone"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractEBS(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error should mention %q: %v", c.want, err)
			}
			if !strings.HasPrefix(err.Error(), "aws_ebs_volume:") {
				t.Errorf("error should start with 'aws_ebs_volume:': %v", err)
			}
		})
	}
}

func TestExtractEBS_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractEBS(attrs)
		if err == nil {
			t.Fatal("expected error for empty/nil attrs")
		}
		if err.Error() != "aws_ebs_volume: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractEBS() {
	attrs := map[string]interface{}{
		"type":       "gp3",
		"size":       float64(500),
		"throughput": float64(250),
	}
	out, err := ExtractEBS(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %dGB throughput=%dMB/s\n", out.Type, out.Size, out.Throughput)
	// Output: gp3 500GB throughput=250MB/s
}
