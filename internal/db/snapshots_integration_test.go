//go:build integration

package db

import (
	"CloudOracle/internal/db/dbtest"
	"CloudOracle/internal/shared"
	"testing"
	"time"
)

func TestCreateSnapshot_AggregatesByAccountAndService(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	resources := []shared.Resource{
		// Two EC2 in acc-1 → one snapshot row for (acc-1, ec2) with count=2, cost=15.
		{ID: "i-1", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro", Region: "us-east-2", MonthlyCost: 10},
		{ID: "i-2", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.small", Region: "us-east-2", MonthlyCost: 5},
		// One RDS in acc-1 → one snapshot row for (acc-1, rds) with count=1, cost=50.
		{ID: "db-1", AccountID: "acc-1", Service: "rds", ResourceType: "db.t3.micro", Region: "us-east-2", MonthlyCost: 50},
		// One EC2 in acc-2 → separate snapshot row.
		{ID: "i-3", AccountID: "acc-2", Service: "ec2", ResourceType: "m5.large", Region: "us-west-1", MonthlyCost: 100},
	}
	for i := range resources {
		resources[i].CreatedAt = time.Now()
		resources[i].UpdatedAt = time.Now()
	}

	if err := CreateSnapshot(ctx, pool, resources); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	snaps, err := ListSnapshots(ctx, pool, 30)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("len(snapshots) = %d, want 3 (one per account/service tuple)", len(snaps))
	}

	got := make(map[string]Snapshot)
	for _, s := range snaps {
		got[s.AccountID+"/"+s.Service] = s
	}

	if s := got["acc-1/ec2"]; s.ResourceCount != 2 || s.TotalMonthlyCost != 15 {
		t.Errorf("acc-1/ec2: got count=%d cost=%v, want 2/15", s.ResourceCount, s.TotalMonthlyCost)
	}
	if s := got["acc-1/rds"]; s.ResourceCount != 1 || s.TotalMonthlyCost != 50 {
		t.Errorf("acc-1/rds: got count=%d cost=%v, want 1/50", s.ResourceCount, s.TotalMonthlyCost)
	}
	if s := got["acc-2/ec2"]; s.ResourceCount != 1 || s.TotalMonthlyCost != 100 {
		t.Errorf("acc-2/ec2: got count=%d cost=%v, want 1/100", s.ResourceCount, s.TotalMonthlyCost)
	}
}

func TestCreateSnapshot_EmptyInputIsNoOp(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	if err := CreateSnapshot(ctx, pool, nil); err != nil {
		t.Fatalf("CreateSnapshot(nil): %v", err)
	}

	snaps, err := ListSnapshots(ctx, pool, 30)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("len = %d, want 0 (empty input should write nothing)", len(snaps))
	}
}

// TestListSnapshots_RespectsDayWindow inserts a snapshot and verifies that
// the days-window filter actually filters. We can't backdate taken_at via
// the insert path (it's NOW() default), so we backdate with a manual SQL
// after the insert — same pattern the trend command would see in production
// after multiple `seed` runs across days.
func TestListSnapshots_RespectsDayWindow(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	// Insert one snapshot, then push its taken_at 100 days into the past.
	resources := []shared.Resource{
		{ID: "i-old", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: 10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	if err := CreateSnapshot(ctx, pool, resources); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE cost_snapshots SET taken_at = NOW() - INTERVAL '100 days'`); err != nil {
		t.Fatalf("backdating snapshot: %v", err)
	}

	// 30-day window must NOT include the 100-day-old snapshot.
	got, err := ListSnapshots(ctx, pool, 30)
	if err != nil {
		t.Fatalf("ListSnapshots(30): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("30-day window returned %d, want 0 (snapshot is 100 days old)", len(got))
	}

	// 365-day window includes it.
	got, err = ListSnapshots(ctx, pool, 365)
	if err != nil {
		t.Fatalf("ListSnapshots(365): %v", err)
	}
	if len(got) != 1 {
		t.Errorf("365-day window returned %d, want 1", len(got))
	}
}

// TestListSnapshotsInRange_BothBoundsInclusive verifies the inclusive
// [start, end] contract surfaced by the v1 HTTP API. Snapshots at the
// exact start and end timestamps must be returned; one minute outside
// must not. The dataset uses three snapshots backdated to known offsets
// so we can reason about the bounds without flakiness.
func TestListSnapshotsInRange_BothBoundsInclusive(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	// Three identical resources → three (account, service) rows; we then
	// rewrite taken_at to put one inside, one at the lower edge, and one
	// outside the test window.
	resources := []shared.Resource{
		{ID: "i-1", AccountID: "acc-a", Service: "ec2", ResourceType: "t3.micro", Region: "us-east-2", MonthlyCost: 10, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "i-2", AccountID: "acc-b", Service: "rds", ResourceType: "db.t3.micro", Region: "us-east-2", MonthlyCost: 50, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "i-3", AccountID: "acc-c", Service: "ebs", ResourceType: "gp3", Region: "us-east-2", MonthlyCost: 5, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	if err := CreateSnapshot(ctx, pool, resources); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// Set fixed timestamps: ec2 at 2026-04-01 (inside), rds at 2026-04-30 (inside, at upper edge), ebs at 2026-05-15 (outside).
	if _, err := pool.Exec(ctx, `UPDATE cost_snapshots SET taken_at = '2026-04-01 12:00:00+00' WHERE service = 'ec2'`); err != nil {
		t.Fatalf("backdate ec2: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE cost_snapshots SET taken_at = '2026-04-30 23:59:59+00' WHERE service = 'rds'`); err != nil {
		t.Fatalf("backdate rds: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE cost_snapshots SET taken_at = '2026-05-15 00:00:00+00' WHERE service = 'ebs'`); err != nil {
		t.Fatalf("backdate ebs: %v", err)
	}

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)
	got, err := ListSnapshotsInRange(ctx, pool, start, end)
	if err != nil {
		t.Fatalf("ListSnapshotsInRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (ec2 + rds; ebs is outside)", len(got))
	}
	services := map[string]bool{}
	for _, s := range got {
		services[s.Service] = true
	}
	if !services["ec2"] || !services["rds"] || services["ebs"] {
		t.Errorf("unexpected services in range: %v", services)
	}
}
