//go:build integration

// Integration tests in this file hit the real AWS Pricing API. Run with:
//
//	go test -tags=integration ./internal/pricing/...
//
// They require AWS credentials configured via the standard chain
// (env vars, shared credentials file, EC2/ECS/EKS instance metadata,
// SSO). When credentials are missing, NewClient itself does not error
// — it constructs a client that will fail at the first API call. To
// keep the suite friendly to credential-less machines we still attempt
// a NewClient and skip the test if construction fails; the per-test
// API call surfaces auth issues clearly through their own errors.
//
// What these tests check:
//
//  1. Each EstimateXxx mapper returns a price in a sane range against
//     live AWS data (the upper bound has slack so AWS price changes
//     don't flake the suite).
//  2. None of the queries return >1 product after milestone 13.6's
//     filter tightening — TestIntegration_NoAmbiguity verifies all six
//     mappers in one pass and asserts no "filter under-constrained"
//     error escapes.

package pricing

import (
	"context"
	"strings"
	"testing"

	"CloudOracle/internal/iac/aws"
)

const integrationRegion = "us-east-2"

// integrationClient builds a live pricing.Client or skips the test if
// AWS credential resolution fails. NewClient currently only fails when
// LoadDefaultConfig itself errors — typically because of a malformed
// AWS_PROFILE or a broken shared config — but skipping cleanly there
// keeps the suite usable on any machine.
func integrationClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(context.Background())
	if err != nil {
		t.Skipf("integration test requires AWS credentials; got error: %v", err)
	}
	return c
}

func inBand(t *testing.T, label string, got, lo, hi float64) {
	t.Helper()
	if got < lo || got > hi {
		t.Errorf("%s = %.4f, want in band [%.4f, %.4f]", label, got, lo, hi)
	}
}

func TestIntegration_EC2_T3Large_USEast2(t *testing.T) {
	c := integrationClient(t)
	attrs := &aws.EC2Attributes{
		InstanceType:  "t3.large",
		Tenancy:       "default",
		RootBlockSize: 50,
		RootBlockType: "gp3",
	}
	est, err := EstimateEC2(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateEC2: %v", err)
	}
	// As of late 2024 the price is ~$64.74. The band is wide enough to
	// absorb routine AWS price tweaks without flaking the suite.
	inBand(t, "EC2 t3.large+50GB gp3", est.MonthlyUSD, 50, 80)
}

func TestIntegration_RDS_PostgresT3Medium_USEast2(t *testing.T) {
	c := integrationClient(t)
	attrs := &aws.RDSAttributes{
		Engine:           "postgres",
		InstanceClass:    "db.t3.medium",
		AllocatedStorage: 100,
		StorageType:      "gp2",
	}
	est, err := EstimateRDS(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateRDS: %v", err)
	}
	// ~$71.36 baseline.
	inBand(t, "RDS db.t3.medium+100GB gp2", est.MonthlyUSD, 50, 90)
}

func TestIntegration_EBS_GP3_USEast2(t *testing.T) {
	c := integrationClient(t)
	attrs := &aws.EBSAttributes{Type: "gp3", Size: 200}
	est, err := EstimateEBS(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateEBS: %v", err)
	}
	// $0.08/GB-month * 200 = $16.00.
	inBand(t, "EBS gp3 200GB", est.MonthlyUSD, 14, 20)
}

func TestIntegration_Lambda_PCZero(t *testing.T) {
	// PC=0 must short-circuit and return $0 with no API call. We cannot
	// observe "no API call" through a *Client, but we can still assert
	// the contract: cost is exactly 0.
	c := integrationClient(t)
	attrs := &aws.LambdaAttributes{
		FunctionName: "test",
		MemorySize:   512,
		Architecture: "arm64",
	}
	est, err := EstimateLambda(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateLambda: %v", err)
	}
	if est.MonthlyUSD != 0 {
		t.Errorf("MonthlyUSD = %v, want exactly 0 for PC=0", est.MonthlyUSD)
	}
}

func TestIntegration_NATGateway_USEast2(t *testing.T) {
	c := integrationClient(t)
	attrs := &aws.NATGatewayAttributes{
		SubnetID:         "subnet-irrelevant",
		ConnectivityType: "public",
	}
	est, err := EstimateNATGateway(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateNATGateway: %v", err)
	}
	// $0.045/hr * 730 = $32.85.
	inBand(t, "NAT Gateway", est.MonthlyUSD, 30, 40)
}

func TestIntegration_RDSClusterInstance_AuroraPGR5Large_USEast2(t *testing.T) {
	c := integrationClient(t)
	attrs := &aws.RDSClusterInstanceAttributes{
		ClusterIdentifier: "test-cluster",
		InstanceClass:     "db.r5.large",
		Engine:            "aurora-postgresql",
	}
	est, err := EstimateRDSClusterInstance(context.Background(), c, attrs, integrationRegion)
	if err != nil {
		t.Fatalf("EstimateRDSClusterInstance: %v", err)
	}
	// $0.29/hr * 730 = $211.7.
	inBand(t, "Aurora PG db.r5.large", est.MonthlyUSD, 190, 230)
}

// TestIntegration_NoAmbiguity exercises every mapper end-to-end in one
// pass and asserts that none of them surface the "filter under-
// constrained" error introduced in milestone 13.6. This is the
// regression test that catches a future AWS catalogue change adding a
// new SKU variant we hadn't anticipated.
func TestIntegration_NoAmbiguity(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	type call func() error
	calls := map[string]call{
		"EC2": func() error {
			_, err := EstimateEC2(ctx, c, &aws.EC2Attributes{
				InstanceType:  "t3.large",
				Tenancy:       "default",
				RootBlockSize: 50,
				RootBlockType: "gp3",
			}, integrationRegion)
			return err
		},
		"RDS": func() error {
			_, err := EstimateRDS(ctx, c, &aws.RDSAttributes{
				Engine:           "postgres",
				InstanceClass:    "db.t3.medium",
				AllocatedStorage: 100,
				StorageType:      "gp2",
			}, integrationRegion)
			return err
		},
		"EBS": func() error {
			_, err := EstimateEBS(ctx, c, &aws.EBSAttributes{Type: "gp3", Size: 200}, integrationRegion)
			return err
		},
		"Lambda": func() error {
			// Use PC>0 so the API is actually hit (PC=0 short-circuits).
			_, err := EstimateLambda(ctx, c, &aws.LambdaAttributes{
				FunctionName:           "test",
				MemorySize:             512,
				Architecture:           "arm64",
				ProvisionedConcurrency: 1,
			}, integrationRegion)
			return err
		},
		"NATGateway": func() error {
			_, err := EstimateNATGateway(ctx, c, &aws.NATGatewayAttributes{
				SubnetID:         "subnet-irrelevant",
				ConnectivityType: "public",
			}, integrationRegion)
			return err
		},
		"AuroraClusterInstance": func() error {
			_, err := EstimateRDSClusterInstance(ctx, c, &aws.RDSClusterInstanceAttributes{
				ClusterIdentifier: "test-cluster",
				InstanceClass:     "db.r5.large",
				Engine:            "aurora-postgresql",
			}, integrationRegion)
			return err
		},
	}

	for name, fn := range calls {
		t.Run(name, func(t *testing.T) {
			err := fn()
			if err == nil {
				return
			}
			if strings.Contains(err.Error(), "filter under-constrained") {
				t.Fatalf("%s: ambiguity error after 13.6 tightening: %v", name, err)
			}
			// Any other error is unexpected too — surface it loudly so
			// it's debugged rather than silently passed over.
			t.Fatalf("%s: %v", name, err)
		})
	}
}
