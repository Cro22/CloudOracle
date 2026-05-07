package aws

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestExtract_DispatchesEC2(t *testing.T) {
	r, err := Extract("aws_instance", map[string]interface{}{
		"instance_type": "t3.large",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "aws_instance" {
		t.Errorf("Type = %q", r.Type)
	}
	if r.EC2 == nil {
		t.Fatal("EC2 is nil — dispatch failed")
	}
	if r.EC2.InstanceType != "t3.large" {
		t.Errorf("InstanceType = %q", r.EC2.InstanceType)
	}
	if r.RDS != nil || r.EBS != nil {
		t.Errorf("non-EC2 fields should be nil: RDS=%v EBS=%v", r.RDS, r.EBS)
	}
}

func TestExtract_DispatchesRDS(t *testing.T) {
	r, err := Extract("aws_db_instance", map[string]interface{}{
		"engine":            "postgres",
		"instance_class":    "db.t3.micro",
		"allocated_storage": float64(20),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.RDS == nil || r.RDS.Engine != "postgres" {
		t.Errorf("RDS not populated: %+v", r)
	}
	if r.EC2 != nil || r.EBS != nil {
		t.Errorf("non-RDS fields should be nil")
	}
}

func TestExtract_DispatchesEBS(t *testing.T) {
	r, err := Extract("aws_ebs_volume", map[string]interface{}{
		"type": "gp3",
		"size": float64(100),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.EBS == nil || r.EBS.Type != "gp3" {
		t.Errorf("EBS not populated: %+v", r)
	}
	if r.EC2 != nil || r.RDS != nil {
		t.Errorf("non-EBS fields should be nil")
	}
}

func TestExtract_DispatchesLambda(t *testing.T) {
	r, err := Extract("aws_lambda_function", map[string]interface{}{
		"function_name": "checkout",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "aws_lambda_function" {
		t.Errorf("Type = %q", r.Type)
	}
	if r.Lambda == nil || r.Lambda.FunctionName != "checkout" {
		t.Errorf("Lambda not populated: %+v", r)
	}
	if r.EC2 != nil || r.RDS != nil || r.EBS != nil ||
		r.NATGateway != nil || r.RDSClusterInstance != nil {
		t.Error("non-Lambda fields should be nil")
	}
}

func TestExtract_DispatchesNATGateway(t *testing.T) {
	r, err := Extract("aws_nat_gateway", map[string]interface{}{
		"subnet_id": "subnet-123",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "aws_nat_gateway" {
		t.Errorf("Type = %q", r.Type)
	}
	if r.NATGateway == nil || r.NATGateway.SubnetID != "subnet-123" {
		t.Errorf("NATGateway not populated: %+v", r)
	}
}

func TestExtract_DispatchesRDSClusterInstance(t *testing.T) {
	r, err := Extract("aws_rds_cluster_instance", map[string]interface{}{
		"cluster_identifier": "c1",
		"instance_class":     "db.t3.medium",
		"engine":             "aurora-postgresql",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if r.Type != "aws_rds_cluster_instance" {
		t.Errorf("Type = %q", r.Type)
	}
	if r.RDSClusterInstance == nil ||
		r.RDSClusterInstance.ClusterIdentifier != "c1" ||
		r.RDSClusterInstance.Engine != "aurora-postgresql" {
		t.Errorf("RDSClusterInstance not populated: %+v", r.RDSClusterInstance)
	}
}

// TestExtract_UnsupportedType verifies the contract documented in the
// dispatcher comment: unknown types are NOT errors, they are silently
// reported as "no data" so the caller can skip them.
func TestExtract_UnsupportedType(t *testing.T) {
	r, err := Extract("aws_iam_role", map[string]interface{}{
		"name": "my-role",
	})
	if err != nil {
		t.Errorf("err = %v, want nil for unsupported type", err)
	}
	if r != nil {
		t.Errorf("r = %+v, want nil for unsupported type", r)
	}
}

// TestExtract_PropagatesUnderlyingErrors verifies that when the type IS
// supported but extraction fails, the error from the inner extractor
// surfaces unchanged through Extract.
func TestExtract_PropagatesUnderlyingErrors(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		attrs map[string]interface{}
		want  string
	}{
		{
			"EC2 nil attrs",
			"aws_instance", nil,
			"aws_instance: empty attributes",
		},
		{
			"RDS missing required",
			"aws_db_instance",
			map[string]interface{}{"instance_class": "db.t3.micro"},
			`aws_db_instance: missing required attribute "engine"`,
		},
		{
			"EBS empty attrs",
			"aws_ebs_volume", map[string]interface{}{},
			"aws_ebs_volume: empty attributes",
		},
		{
			"Lambda missing required",
			"aws_lambda_function", map[string]interface{}{"runtime": "python3.12"},
			`aws_lambda_function: missing required attribute "function_name"`,
		},
		{
			"NAT empty attrs",
			"aws_nat_gateway", nil,
			"aws_nat_gateway: empty attributes",
		},
		{
			"RDS cluster instance missing required",
			"aws_rds_cluster_instance",
			map[string]interface{}{"cluster_identifier": "c1", "instance_class": "db.t3.medium"},
			`aws_rds_cluster_instance: missing required attribute "engine"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Extract(c.typ, c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != c.want {
				t.Errorf("error = %q\nwant %q", err.Error(), c.want)
			}
		})
	}
}

func TestSupportedTypes(t *testing.T) {
	got := SupportedTypes()
	// Sort defensively — the contract is "stable order across calls",
	// but the test asserts membership, not a particular order.
	sortedGot := append([]string(nil), got...)
	sort.Strings(sortedGot)
	want := []string{
		"aws_db_instance",
		"aws_ebs_volume",
		"aws_instance",
		"aws_lambda_function",
		"aws_nat_gateway",
		"aws_rds_cluster_instance",
	}
	if !reflect.DeepEqual(sortedGot, want) {
		t.Errorf("got %v, want %v", sortedGot, want)
	}

	// Mutating the returned slice must not affect future calls — the
	// docstring promises a fresh allocation each call.
	got[0] = "MUTATED"
	again := SupportedTypes()
	if again[0] == "MUTATED" {
		t.Error("SupportedTypes() shares state across calls — should return a fresh slice")
	}
}

func ExampleExtract() {
	attrs := map[string]interface{}{
		"instance_type": "m5.large",
	}
	r, err := Extract("aws_instance", attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s -> %s\n", r.Type, r.EC2.InstanceType)
	// Output: aws_instance -> m5.large
}
