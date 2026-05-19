package cloud

import (
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestMapEC2ToResource verifies that mapEC2ToResource maps every field of
// an ec2types.Instance to a shared.Resource correctly.
// We use an SDK struct literal as a "mock" — no real client or network
// calls are needed because mapEC2ToResource is a pure function.
func TestMapEC2ToResource(t *testing.T) {
	launchTime := time.Date(2026, 4, 12, 19, 12, 46, 0, time.UTC)
	instanceID := "i-0d76ebf46c06e285d"
	tagKey := "Name"
	tagValue := "cloudoracle-test"

	// Build an EC2 instance with the same fields the API would return.
	// String pointers are required because the SDK uses *string everywhere.
	instance := ec2types.Instance{
		InstanceId:   &instanceID,
		InstanceType: ec2types.InstanceTypeT3Micro,
		LaunchTime:   &launchTime,
		Tags: []ec2types.Tag{
			{Key: &tagKey, Value: &tagValue},
		},
	}

	r := mapEC2ToResource(instance, "505610409129", "us-east-2")

	// Verify each field individually instead of using reflect.DeepEqual so
	// the error message points at exactly which field failed.
	if r.ID != "i-0d76ebf46c06e285d" {
		t.Errorf("ID = %q, want %q", r.ID, "i-0d76ebf46c06e285d")
	}
	if r.AccountID != "505610409129" {
		t.Errorf("AccountID = %q, want %q", r.AccountID, "505610409129")
	}
	if r.Service != "ec2" {
		t.Errorf("Service = %q, want %q", r.Service, "ec2")
	}
	if r.ResourceType != "t3.micro" {
		t.Errorf("ResourceType = %q, want %q", r.ResourceType, "t3.micro")
	}
	if r.Region != "us-east-2" {
		t.Errorf("Region = %q, want %q", r.Region, "us-east-2")
	}
	if r.MonthlyCost != 0.0 {
		t.Errorf("MonthlyCost = %f, want 0.0", r.MonthlyCost)
	}
	if r.UsageMetric != 0.0 {
		t.Errorf("UsageMetric = %f, want 0.0", r.UsageMetric)
	}
	if !r.CreatedAt.Equal(launchTime) {
		t.Errorf("CreatedAt = %v, want %v", r.CreatedAt, launchTime)
	}
	if r.Tags["Name"] != "cloudoracle-test" {
		t.Errorf("Tags[Name] = %q, want %q", r.Tags["Name"], "cloudoracle-test")
	}
}

// TestMapEC2ToResource_NoTags verifies that an instance with no tags
// produces a Resource with Tags == nil (not an empty map).
func TestMapEC2ToResource_NoTags(t *testing.T) {
	launchTime := time.Now()
	instanceID := "i-notags"

	instance := ec2types.Instance{
		InstanceId:   &instanceID,
		InstanceType: ec2types.InstanceTypeM5Large,
		LaunchTime:   &launchTime,
		Tags:         nil, // no tags
	}

	r := mapEC2ToResource(instance, "123456789", "eu-west-1")

	if r.Tags != nil {
		t.Errorf("Tags = %v, want nil for instance without tags", r.Tags)
	}
	if r.Service != "ec2" {
		t.Errorf("Service = %q, want %q", r.Service, "ec2")
	}
}

// TestMapEC2ToResource_MultipleTags verifies that multiple tags are
// correctly converted into the map.
func TestMapEC2ToResource_MultipleTags(t *testing.T) {
	launchTime := time.Now()
	instanceID := "i-multitags"
	k1, v1 := "Environment", "production"
	k2, v2 := "Team", "platform"
	k3, v3 := "CostCenter", "eng-42"

	instance := ec2types.Instance{
		InstanceId:   &instanceID,
		InstanceType: ec2types.InstanceTypeT3Small,
		LaunchTime:   &launchTime,
		Tags: []ec2types.Tag{
			{Key: &k1, Value: &v1},
			{Key: &k2, Value: &v2},
			{Key: &k3, Value: &v3},
		},
	}

	r := mapEC2ToResource(instance, "111222333", "us-west-2")

	if len(r.Tags) != 3 {
		t.Fatalf("len(Tags) = %d, want 3", len(r.Tags))
	}
	if r.Tags["Environment"] != "production" {
		t.Errorf("Tags[Environment] = %q, want %q", r.Tags["Environment"], "production")
	}
	if r.Tags["Team"] != "platform" {
		t.Errorf("Tags[Team] = %q, want %q", r.Tags["Team"], "platform")
	}
	if r.Tags["CostCenter"] != "eng-42" {
		t.Errorf("Tags[CostCenter] = %q, want %q", r.Tags["CostCenter"], "eng-42")
	}
}

// TestConvertEC2Tags_NilValue verifies that a tag with Value == nil is
// converted to an empty string instead of panicking.
func TestConvertEC2Tags_NilValue(t *testing.T) {
	key := "AutoScalingGroup"
	tags := []ec2types.Tag{
		{Key: &key, Value: nil}, // AWS sometimes returns tags with Value nil
	}

	result := convertEC2Tags(tags)

	if result["AutoScalingGroup"] != "" {
		t.Errorf("tag with nil Value = %q, want empty string", result["AutoScalingGroup"])
	}
}

// TestParseLambdaTimestamp verifies the different formats Lambda may return.
func TestParseLambdaTimestamp(t *testing.T) {
	// Lambda's primary format: "2024-01-15T10:30:00.000+0000"
	ts1 := "2024-01-15T10:30:00.000+0000"
	result := parseLambdaTimestamp(&ts1)
	if result.Year() != 2024 || result.Month() != 1 || result.Day() != 15 {
		t.Errorf("Lambda format: got %v, want 2024-01-15", result)
	}

	// Standard RFC3339 format
	ts2 := "2024-06-20T14:00:00Z"
	result = parseLambdaTimestamp(&ts2)
	if result.Year() != 2024 || result.Month() != 6 || result.Day() != 20 {
		t.Errorf("RFC3339 format: got %v, want 2024-06-20", result)
	}

	// nil returns time.Now() (does not panic)
	result = parseLambdaTimestamp(nil)
	if time.Since(result) > time.Second {
		t.Errorf("nil input should return ~now, got %v", result)
	}
}
