//go:build integration

// Package e2e holds end-to-end tests that exercise the full flow: synthetic
// data generation -> database insert -> analyzer -> findings. These tests
// run against a real Postgres container and verify that the production code
// paths (not mocks) produce the expected output.
package e2e

import (
	"CloudOracle/internal/analyzer"
	"CloudOracle/internal/cloud"
	"CloudOracle/internal/db"
	"CloudOracle/internal/db/dbtest"
	"CloudOracle/internal/shared"
	"context"
	"testing"
	"time"
)

// TestE2E_SeedThenAnalyze is the integration test that mirrors the flow a
// real operator runs: insert resources -> read back -> analyze. We use a
// deterministic resource set engineered to fire each detection rule exactly
// once, plus a few "healthy" resources that should NOT produce findings.
// This way the assertions are exact instead of probabilistic — random
// synthetic data made the test flaky on small N.
func TestE2E_SeedThenAnalyze(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-365 * 24 * time.Hour) // > 7 days, so age-based rules can fire

	resources := []shared.Resource{
		// Should fire ec2-idle: <5% CPU, > 7 days old.
		{ID: "i-idle", AccountID: "e2e", Service: "ec2", ResourceType: "c5.xlarge",
			Region: "us-east-1", MonthlyCost: 125, UsageMetric: 2.0,
			CreatedAt: old, UpdatedAt: now},
		// Should fire rds-oversized: <10% CPU.
		{ID: "db-oversized", AccountID: "e2e", Service: "rds", ResourceType: "db.r5.large",
			Region: "us-east-1", MonthlyCost: 180, UsageMetric: 3.0,
			CreatedAt: old, UpdatedAt: now},
		// Should fire ebs-orphan: usage = 0.
		{ID: "vol-orphan", AccountID: "e2e", Service: "ebs", ResourceType: "gp3-1000GB",
			Region: "us-east-1", MonthlyCost: 100, UsageMetric: 0,
			CreatedAt: old, UpdatedAt: now},
		// Should fire lambda-over-provisioned: high memory + low invocations.
		{ID: "fn-bloated", AccountID: "e2e", Service: "lambda", ResourceType: "2048MB",
			Region: "us-east-1", MonthlyCost: 5, UsageMetric: 100,
			CreatedAt: old, UpdatedAt: now},
		// Healthy controls: should NOT produce findings.
		{ID: "i-busy", AccountID: "e2e", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-1", MonthlyCost: 7.5, UsageMetric: 65,
			CreatedAt: old, UpdatedAt: now},
		{ID: "db-busy", AccountID: "e2e", Service: "rds", ResourceType: "db.t3.micro",
			Region: "us-east-1", MonthlyCost: 15, UsageMetric: 55,
			CreatedAt: old, UpdatedAt: now},
	}

	if err := db.InsertResources(ctx, pool, resources); err != nil {
		t.Fatalf("InsertResources: %v", err)
	}

	stored, err := db.ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(stored) != 6 {
		t.Fatalf("len(stored) = %d, want 6 (round-trip lost rows)", len(stored))
	}

	findings := analyzer.Analyze(stored)

	rules := map[string]int{}
	for _, f := range findings {
		rules[f.Rule]++
	}
	for _, want := range []string{"ec2-idle", "rds-oversized", "ebs-orphan", "lambda-over-provisioned"} {
		if rules[want] != 1 {
			t.Errorf("rule %q fired %d times, want 1. Distribution: %+v",
				want, rules[want], rules)
		}
	}
	if len(findings) != 4 {
		t.Errorf("total findings = %d, want 4 (one per rule). Got: %+v", len(findings), rules)
	}

	// Findings must be sorted by potential savings descending — the contract
	// the CLI banner and the PDF report depend on.
	for i := 1; i < len(findings); i++ {
		if findings[i-1].MonthlySavings < findings[i].MonthlySavings {
			t.Errorf("findings not sorted by savings DESC at index %d: %v < %v",
				i, findings[i-1].MonthlySavings, findings[i].MonthlySavings)
			break
		}
	}

	// Every finding must reference a real resource ID that came back from the DB.
	storedIDs := make(map[string]bool, len(stored))
	for _, r := range stored {
		storedIDs[r.ID] = true
	}
	for _, f := range findings {
		if !storedIDs[f.ResourceID] {
			t.Errorf("finding references unknown resource %q", f.ResourceID)
		}
	}
}

// TestE2E_SyntheticProviderAgainstRealDB exercises the same flow as the CLI's
// `seed` command: SyntheticProvider -> FetchResources -> InsertResources ->
// ListResources -> Analyze. We don't assert any specific rule mix because
// 50 random resources won't hit every rule deterministically; we assert
// only that the round trip works and the analyzer produces *some* signal.
func TestE2E_SyntheticProviderAgainstRealDB(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := context.Background()

	provider := cloud.NewSyntheticProvider(50, "synth-account")
	resources, err := provider.FetchResources(ctx)
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}
	if len(resources) != 50 {
		t.Fatalf("len(resources) = %d, want 50", len(resources))
	}
	if err := db.InsertResources(ctx, pool, resources); err != nil {
		t.Fatalf("InsertResources: %v", err)
	}

	stored, err := db.ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(stored) != 50 {
		t.Fatalf("len(stored) = %d, want 50", len(stored))
	}

	// 50 random resources should produce *some* findings — the synthetic
	// generator deliberately skews toward waste patterns. We don't pin the
	// exact rule distribution because that's probabilistic.
	findings := analyzer.Analyze(stored)
	if len(findings) == 0 {
		t.Error("analyzer produced 0 findings on 50 random resources — generator skew broken?")
	}
}

// TestE2E_SnapshotAfterSeed verifies that the seed -> snapshot side effect
// (which the CLI runs automatically) records the right per-service totals.
func TestE2E_SnapshotAfterSeed(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := context.Background()

	provider := cloud.NewSyntheticProvider(30, "snap-account")
	resources, err := provider.FetchResources(ctx)
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}

	if err := db.InsertResources(ctx, pool, resources); err != nil {
		t.Fatalf("InsertResources: %v", err)
	}
	if err := db.CreateSnapshot(ctx, pool, resources); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	snaps, err := db.ListSnapshots(ctx, pool, 30)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) == 0 {
		t.Fatal("no snapshots recorded after seed")
	}

	// Sum of snapshot per-service costs must equal sum of per-resource costs.
	var snapshotTotal, resourceTotal float64
	for _, s := range snaps {
		snapshotTotal += s.TotalMonthlyCost
	}
	for _, r := range resources {
		resourceTotal += r.MonthlyCost
	}
	// Allow tiny rounding tolerance because Postgres NUMERIC(12,2) rounds
	// fractional cents on the way in.
	diff := snapshotTotal - resourceTotal
	if diff < -0.01 || diff > 0.01 {
		t.Errorf("snapshot total %v != resource total %v (diff %v)",
			snapshotTotal, resourceTotal, diff)
	}

	// The snapshot per-service breakdown should also match the per-resource
	// breakdown service by service.
	resourceByService := make(map[string]float64)
	for _, r := range resources {
		resourceByService[r.Service] += r.MonthlyCost
	}
	for _, s := range snaps {
		got := s.TotalMonthlyCost
		want := resourceByService[s.Service]
		if d := got - want; d < -0.01 || d > 0.01 {
			t.Errorf("snapshot[%s].cost = %v, want %v", s.Service, got, want)
		}
	}
}

// TestE2E_ReseedIsIdempotent verifies the contract the CLI relies on:
// running `seed` twice doesn't duplicate rows; instead, ON CONFLICT updates
// in place. This is the property that makes the seed safe to re-run on
// schedule (cron, CI, etc.).
func TestE2E_ReseedIsIdempotent(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := context.Background()

	// Synthetic generator uses crypto/rand-shaped IDs, so two runs would
	// produce different IDs and "idempotent re-seed" would be hard to test
	// directly. Instead, build a fixed set of resources twice with the same
	// IDs to exercise the upsert path the way real seed does on stable data.
	fixed := []shared.Resource{
		{ID: "fixed-1", AccountID: "acc", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: 10},
		{ID: "fixed-2", AccountID: "acc", Service: "rds", ResourceType: "db.t3.micro",
			Region: "us-east-2", MonthlyCost: 50},
	}
	now := time.Now()
	for i := range fixed {
		fixed[i].CreatedAt = now
		fixed[i].UpdatedAt = now
	}

	for i := 0; i < 3; i++ {
		if err := db.InsertResources(ctx, pool, fixed); err != nil {
			t.Fatalf("seed iteration %d: %v", i, err)
		}
	}

	got, err := db.ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("after 3 seeds with fixed IDs: len = %d, want 2 (idempotency broken)",
			len(got))
	}
}
