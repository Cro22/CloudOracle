package db

import (
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func InsertResources(ctx context.Context, pool *pgxpool.Pool, resources []shared.Resource) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Printf("failed to rollback transaction: %v", err)
		}
	}()

	query := `INSERT INTO resources (
			id, account_id, service, resource_type, region,
			monthly_cost, usage_metric, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			monthly_cost = EXCLUDED.monthly_cost,
			usage_metric = EXCLUDED.usage_metric,
			updated_at = EXCLUDED.updated_at`
	for _, r := range resources {
		_, err = tx.Exec(ctx, query,
			r.ID, r.AccountID, r.Service, r.ResourceType, r.Region,
			r.MonthlyCost, r.UsageMetric, r.CreatedAt, r.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert resource: %s: %w", r.ID, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func ListResources(ctx context.Context, pool *pgxpool.Pool) ([]shared.Resource, error) {
	query := `
		SELECT id, account_id, service, resource_type, region,
		       monthly_cost, usage_metric, created_at, updated_at
		FROM resources
		ORDER BY monthly_cost DESC
	`
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query resources: %w", err)
	}
	defer rows.Close()

	var resources []shared.Resource
	for rows.Next() {
		var r shared.Resource
		err = rows.Scan(
			&r.ID, &r.AccountID, &r.Service, &r.ResourceType, &r.Region,
			&r.MonthlyCost, &r.UsageMetric, &r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		resources = append(resources, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate over resources: %w", err)
	}
	return resources, nil
}
