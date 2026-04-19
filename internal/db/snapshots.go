package db

import (
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Snapshot struct {
	TakenAt          time.Time
	AccountID        string
	Service          string
	ResourceCount    int
	TotalMonthlyCost float64
}

func CreateSnapshot(ctx context.Context, pool *pgxpool.Pool, resources []shared.Resource) error {
	if len(resources) == 0 {
		return nil
	}

	type key struct {
		AccountID string
		Service   string
	}
	agg := make(map[key]struct {
		Count int
		Cost  float64
	})

	for _, r := range resources {
		k := key{r.AccountID, r.Service}
		entry := agg[k]
		entry.Count++
		entry.Cost += r.MonthlyCost
		agg[k] = entry
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Printf("failed to rollback snapshot transaction: %v", err)
		}
	}()

	for k, v := range agg {
		_, err := tx.Exec(ctx,
			`INSERT INTO cost_snapshots (account_id, service, resource_count, total_monthly_cost)
			 VALUES ($1, $2, $3, $4)`,
			k.AccountID, k.Service, v.Count, v.Cost,
		)
		if err != nil {
			return fmt.Errorf("inserting snapshot for %s/%s: %w", k.AccountID, k.Service, err)
		}
	}

	return tx.Commit(ctx)
}

func ListSnapshots(ctx context.Context, pool *pgxpool.Pool, days int) ([]Snapshot, error) {
	rows, err := pool.Query(ctx,
		`SELECT taken_at, account_id, service, resource_count, total_monthly_cost
		 FROM cost_snapshots
		 WHERE taken_at >= NOW() - $1 * INTERVAL '1 day'
		 ORDER BY taken_at ASC`,
		days,
	)
	if err != nil {
		return nil, fmt.Errorf("querying snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []Snapshot
	for rows.Next() {
		var s Snapshot
		if err := rows.Scan(&s.TakenAt, &s.AccountID, &s.Service, &s.ResourceCount, &s.TotalMonthlyCost); err != nil {
			return nil, fmt.Errorf("scanning snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, rows.Err()
}
