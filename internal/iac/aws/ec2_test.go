package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractEC2_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"instance_type":     "t3.large",
		"availability_zone": "us-east-1a",
		"tenancy":           "dedicated",
		"ebs_optimized":     true,
		"root_block_device": []interface{}{
			map[string]interface{}{
				"volume_size": float64(100),
				"volume_type": "gp3",
			},
		},
		// Unknown attribute — must be ignored, not error.
		"some_future_field": "future-value",
	}
	got, err := ExtractEC2(attrs)
	if err != nil {
		t.Fatalf("ExtractEC2: %v", err)
	}
	want := EC2Attributes{
		InstanceType:     "t3.large",
		AvailabilityZone: "us-east-1a",
		Tenancy:          "dedicated",
		EBSOptimized:     true,
		RootBlockSize:    100,
		RootBlockType:    "gp3",
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractEC2_OnlyRequiredFields(t *testing.T) {
	attrs := map[string]interface{}{
		"instance_type": "m5.xlarge",
	}
	got, err := ExtractEC2(attrs)
	if err != nil {
		t.Fatalf("ExtractEC2: %v", err)
	}
	if got.InstanceType != "m5.xlarge" {
		t.Errorf("InstanceType = %q", got.InstanceType)
	}
	// Defaults must apply when optional fields are absent.
	if got.Tenancy != "default" {
		t.Errorf("Tenancy = %q, want %q (default)", got.Tenancy, "default")
	}
	if got.EBSOptimized {
		t.Error("EBSOptimized = true, want false (default)")
	}
	if got.AvailabilityZone != "" {
		t.Errorf("AvailabilityZone = %q, want empty", got.AvailabilityZone)
	}
	if got.RootBlockSize != 0 || got.RootBlockType != "" {
		t.Errorf("root block fields should be zero when block is absent: %+v", got)
	}
}

func TestExtractEC2_MissingInstanceType(t *testing.T) {
	_, err := ExtractEC2(map[string]interface{}{
		"availability_zone": "us-east-1a",
	})
	if err == nil {
		t.Fatal("expected error for missing instance_type")
	}
	want := `aws_instance: missing required attribute "instance_type"`
	if err.Error() != want {
		t.Errorf("error = %q\nwant %q", err.Error(), want)
	}
}

func TestExtractEC2_WrongTypes(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string // substring expected in error
	}{
		{"instance_type as int", map[string]interface{}{"instance_type": 42}, "instance_type"},
		{"availability_zone as bool", map[string]interface{}{
			"instance_type":     "t3.micro",
			"availability_zone": true,
		}, "availability_zone"},
		{"ebs_optimized as string", map[string]interface{}{
			"instance_type": "t3.micro",
			"ebs_optimized": "yes",
		}, "ebs_optimized"},
		{"root_block_device as string", map[string]interface{}{
			"instance_type":     "t3.micro",
			"root_block_device": "not-a-list",
		}, "root_block_device"},
		{"root_block_device[0] not a map", map[string]interface{}{
			"instance_type":     "t3.micro",
			"root_block_device": []interface{}{"not-a-map"},
		}, "root_block_device"},
		{"tenancy as int", map[string]interface{}{
			"instance_type": "t3.micro",
			"tenancy":       42,
		}, "tenancy"},
		{"root_block_device.volume_size as string", map[string]interface{}{
			"instance_type": "t3.micro",
			"root_block_device": []interface{}{
				map[string]interface{}{"volume_size": "100"},
			},
		}, "volume_size"},
		{"root_block_device.volume_type as bool", map[string]interface{}{
			"instance_type": "t3.micro",
			"root_block_device": []interface{}{
				map[string]interface{}{"volume_type": true},
			},
		}, "volume_type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractEC2(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error should mention %q: %v", c.want, err)
			}
			if !strings.HasPrefix(err.Error(), "aws_instance") {
				t.Errorf("error should start with 'aws_instance': %v", err)
			}
		})
	}
}

func TestExtractEC2_RootBlockDeviceEmptyList(t *testing.T) {
	// HCL allows declaring `root_block_device {}` zero or one times.
	// When zero times, JSON renders as []. Must not panic, must not error,
	// must leave RootBlock* zero.
	attrs := map[string]interface{}{
		"instance_type":     "t3.small",
		"root_block_device": []interface{}{},
	}
	got, err := ExtractEC2(attrs)
	if err != nil {
		t.Fatalf("empty root_block_device list: %v", err)
	}
	if got.RootBlockSize != 0 || got.RootBlockType != "" {
		t.Errorf("expected zero root block fields, got size=%d type=%q",
			got.RootBlockSize, got.RootBlockType)
	}
}

func TestExtractEC2_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractEC2(attrs)
		if err == nil {
			t.Fatalf("expected error for empty/nil attrs (got nil for %v)", attrs)
		}
		if err.Error() != "aws_instance: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractEC2() {
	attrs := map[string]interface{}{
		"instance_type":     "t3.large",
		"availability_zone": "us-east-1a",
	}
	out, err := ExtractEC2(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s in %s (tenancy=%s)\n", out.InstanceType, out.AvailabilityZone, out.Tenancy)
	// Output: t3.large in us-east-1a (tenancy=default)
}
