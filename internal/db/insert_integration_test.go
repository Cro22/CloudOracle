//go:build integration

package db

import (
	"CloudOracle/internal/db/dbtest"
	"CloudOracle/internal/shared"
	"testing"
	"time"
)

func TestInsertResources_HappyPath(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	resources := []shared.Resource{
		{
			ID: "i-aaa", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: 10.50, UsageMetric: 5.2,
			CreatedAt: time.Now().Add(-24 * time.Hour), UpdatedAt: time.Now(),
		},
		{
			ID: "vol-bbb", AccountID: "acc-1", Service: "ebs", ResourceType: "gp3",
			Region: "us-east-2", MonthlyCost: 100.00, UsageMetric: 0,
			CreatedAt: time.Now().Add(-30 * 24 * time.Hour), UpdatedAt: time.Now(),
		},
	}

	if err := InsertResources(ctx, pool, resources); err != nil {
		t.Fatalf("InsertResources: %v", err)
	}

	got, err := ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// ListResources orders by monthly_cost DESC — vol-bbb ($100) before i-aaa ($10.50).
	if got[0].ID != "vol-bbb" || got[1].ID != "i-aaa" {
		t.Errorf("order = [%s, %s], want [vol-bbb, i-aaa]", got[0].ID, got[1].ID)
	}
	if got[0].MonthlyCost != 100.00 {
		t.Errorf("MonthlyCost = %v, want 100.00", got[0].MonthlyCost)
	}
}

// TestInsertResources_UpsertOnConflict verifies the ON CONFLICT DO UPDATE
// behavior: re-inserting an existing ID updates monthly_cost / usage_metric /
// updated_at but does NOT modify the original created_at. This is the core
// invariant of the seed flow — running seed twice doesn't duplicate rows.
func TestInsertResources_UpsertOnConflict(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	originalCreatedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	first := []shared.Resource{{
		ID: "i-upsert", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
		Region: "us-east-2", MonthlyCost: 5.00, UsageMetric: 10,
		CreatedAt: originalCreatedAt, UpdatedAt: time.Now(),
	}}
	if err := InsertResources(ctx, pool, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Re-insert same ID with different cost / usage / updated_at, but a
	// fake "earlier" created_at to prove upsert doesn't overwrite it.
	newUpdatedAt := time.Now().Add(time.Hour)
	updated := []shared.Resource{{
		ID: "i-upsert", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
		Region: "us-east-2", MonthlyCost: 99.99, UsageMetric: 80,
		CreatedAt: time.Now(), // intentionally different from originalCreatedAt
		UpdatedAt: newUpdatedAt,
	}}
	if err := InsertResources(ctx, pool, updated); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (upsert should NOT duplicate)", len(got))
	}
	r := got[0]
	if r.MonthlyCost != 99.99 {
		t.Errorf("MonthlyCost not updated: got %v, want 99.99", r.MonthlyCost)
	}
	if r.UsageMetric != 80 {
		t.Errorf("UsageMetric not updated: got %v, want 80", r.UsageMetric)
	}
	if !r.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt was overwritten: got %v, want %v (original)",
			r.CreatedAt, originalCreatedAt)
	}
	// Postgres TIMESTAMPTZ has microsecond resolution while Go time.Time has
	// nanoseconds, so the round trip can lose up to 1µs of precision.
	if d := r.UpdatedAt.Sub(newUpdatedAt); d < -time.Microsecond || d > time.Microsecond {
		t.Errorf("UpdatedAt: got %v, want %v (diff %v)", r.UpdatedAt, newUpdatedAt, d)
	}
}

// TestInsertResources_TransactionRollback verifies that if InsertResources
// fails mid-batch, no partial rows are committed. We trigger the failure by
// passing a resource with an ID identical to an earlier one in the same
// batch — but ON CONFLICT handles that, so we need a different failure mode.
// Use a NOT-NULL violation: empty service violates nothing schema-wise (TEXT
// allows ”), so the cleanest trigger is an oversized monthly_cost that
// overflows NUMERIC(10,2). 99999999.99 fits, 999999999.99 doesn't.
func TestInsertResources_TransactionRollback(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	// Pre-insert one row that should remain stable across the failed batch.
	preExisting := []shared.Resource{{
		ID: "i-existing", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
		Region: "us-east-2", MonthlyCost: 1.00, UsageMetric: 0,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}
	if err := InsertResources(ctx, pool, preExisting); err != nil {
		t.Fatalf("pre-insert: %v", err)
	}

	// Attempt a batch where the second row will overflow NUMERIC(10,2).
	// NUMERIC(10,2) holds up to 99,999,999.99. 1e10 (10 billion) overflows.
	bad := []shared.Resource{
		{
			ID: "i-good", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: 5.00, UsageMetric: 10,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			ID: "i-bad", AccountID: "acc-1", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: 1e10, UsageMetric: 0,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}
	if err := InsertResources(ctx, pool, bad); err == nil {
		t.Fatal("expected error on numeric overflow, got nil")
	}

	got, err := ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	// Only the pre-existing row should remain. i-good must NOT be present —
	// it was rolled back along with the failing i-bad.
	if len(got) != 1 || got[0].ID != "i-existing" {
		t.Errorf("rollback failed: got %d rows, want 1 (i-existing). Rows: %+v",
			len(got), got)
	}
}

func TestListResources_EmptyTable(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	got, err := ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources on empty table: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want 0", len(got))
	}
}

// TestListResources_OrderingByCostDesc covers the contract that the CLI
// "list" command and the dashboard rely on: the highest-cost resources
// surface first.
func TestListResources_OrderingByCostDesc(t *testing.T) {
	pool := dbtest.SharedPool(t)
	ctx := t.Context()

	costs := []float64{10, 50, 5, 200, 100}
	var resources []shared.Resource
	for i, c := range costs {
		resources = append(resources, shared.Resource{
			ID: ids("r", i), AccountID: "acc", Service: "ec2", ResourceType: "t3.micro",
			Region: "us-east-2", MonthlyCost: c, UsageMetric: 0,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	}
	if err := InsertResources(ctx, pool, resources); err != nil {
		t.Fatalf("InsertResources: %v", err)
	}

	got, err := ListResources(ctx, pool)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	wantOrder := []float64{200, 100, 50, 10, 5}
	for i, want := range wantOrder {
		if got[i].MonthlyCost != want {
			t.Errorf("got[%d].MonthlyCost = %v, want %v", i, got[i].MonthlyCost, want)
		}
	}
}

// ids generates "r-0", "r-1", … so we don't fight type assertions in tests.
func ids(prefix string, i int) string {
	return prefix + "-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
