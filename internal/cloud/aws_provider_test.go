package cloud

import (
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestMapEC2ToResource verifica que mapEC2ToResource mapea correctamente
// cada campo de una ec2types.Instance a un shared.Resource.
// Usamos un struct literal del SDK como "mock" — no necesitamos un cliente
// real ni llamadas de red porque mapEC2ToResource es una funcion pura.
func TestMapEC2ToResource(t *testing.T) {
	launchTime := time.Date(2026, 4, 12, 19, 12, 46, 0, time.UTC)
	instanceID := "i-0d76ebf46c06e285d"
	tagKey := "Name"
	tagValue := "cloudoracle-test"

	// Armamos una instancia EC2 con los mismos campos que devolveria la API.
	// Los punteros a string son necesarios porque el SDK usa *string en todos lados.
	instance := ec2types.Instance{
		InstanceId:   &instanceID,
		InstanceType: ec2types.InstanceTypeT3Micro,
		LaunchTime:   &launchTime,
		Tags: []ec2types.Tag{
			{Key: &tagKey, Value: &tagValue},
		},
	}

	r := mapEC2ToResource(instance, "505610409129", "us-east-2")

	// Verificamos cada campo individualmente en vez de usar reflect.DeepEqual
	// porque asi el mensaje de error dice exactamente que campo fallo.
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

// TestMapEC2ToResource_NoTags verifica que una instancia sin tags
// produce un Resource con Tags == nil (no un map vacio).
func TestMapEC2ToResource_NoTags(t *testing.T) {
	launchTime := time.Now()
	instanceID := "i-notags"

	instance := ec2types.Instance{
		InstanceId:   &instanceID,
		InstanceType: ec2types.InstanceTypeM5Large,
		LaunchTime:   &launchTime,
		Tags:         nil, // sin tags
	}

	r := mapEC2ToResource(instance, "123456789", "eu-west-1")

	if r.Tags != nil {
		t.Errorf("Tags = %v, want nil for instance without tags", r.Tags)
	}
	if r.Service != "ec2" {
		t.Errorf("Service = %q, want %q", r.Service, "ec2")
	}
}

// TestMapEC2ToResource_MultipleTags verifica que multiples tags
// se convierten correctamente al map.
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

// TestConvertEC2Tags_NilValue verifica que un tag con Value == nil
// se convierte a string vacio en vez de paniquear.
func TestConvertEC2Tags_NilValue(t *testing.T) {
	key := "AutoScalingGroup"
	tags := []ec2types.Tag{
		{Key: &key, Value: nil}, // AWS a veces devuelve tags con Value nil
	}

	result := convertEC2Tags(tags)

	if result["AutoScalingGroup"] != "" {
		t.Errorf("tag with nil Value = %q, want empty string", result["AutoScalingGroup"])
	}
}

// TestParseLambdaTimestamp verifica los distintos formatos que Lambda puede devolver.
func TestParseLambdaTimestamp(t *testing.T) {
	// Formato principal de Lambda: "2024-01-15T10:30:00.000+0000"
	ts1 := "2024-01-15T10:30:00.000+0000"
	result := parseLambdaTimestamp(&ts1)
	if result.Year() != 2024 || result.Month() != 1 || result.Day() != 15 {
		t.Errorf("Lambda format: got %v, want 2024-01-15", result)
	}

	// Formato RFC3339 estándar
	ts2 := "2024-06-20T14:00:00Z"
	result = parseLambdaTimestamp(&ts2)
	if result.Year() != 2024 || result.Month() != 6 || result.Day() != 20 {
		t.Errorf("RFC3339 format: got %v, want 2024-06-20", result)
	}

	// nil devuelve time.Now() (no paniquea)
	result = parseLambdaTimestamp(nil)
	if time.Since(result) > time.Second {
		t.Errorf("nil input should return ~now, got %v", result)
	}
}
