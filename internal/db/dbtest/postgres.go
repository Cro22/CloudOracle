//go:build integration

// Package dbtest provides a shared Postgres container helper for integration
// tests. It is only compiled when the `integration` build tag is set, so the
// heavy testcontainers dependency stays out of the default `go test ./...`
// build path used by unit tests and CI's fast lane.
package dbtest

import (
	"CloudOracle/internal/migrations"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Strategy: one Postgres container shared by every integration test in the
// process, with each test cleaning the tables it touches via TRUNCATE before
// it runs. A fresh container per test would be cleaner in theory, but the
// startup cost (~3-5s per test) makes the suite unusable as the test count
// grows. TRUNCATE … RESTART IDENTITY CASCADE is fast (sub-millisecond on an
// empty schema) and gives the same isolation guarantee for our purposes,
// because every table here is small and there are no triggers/sequences
// shared across tests.

var (
	sharedOnce      sync.Once
	sharedPool      *pgxpool.Pool
	sharedContainer testcontainers.Container
	sharedErr       error
)

// SharedPool returns a *pgxpool.Pool connected to a Postgres container that
// is started once per process. The container has the migrations applied. On
// the first call only, the helper boots the container and applies migrations;
// every subsequent call returns the same pool instantly.
//
// If Docker isn't available (CI without docker, dev box without Docker
// Desktop running), the test is t.Skip'd with a clear message rather than
// failing — we want unit tests + integration tests to share a binary, so
// running the binary without Docker just skips the integration cases.
func SharedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	sharedOnce.Do(func() {
		sharedPool, sharedContainer, sharedErr = startSharedContainer()
	})
	if sharedErr != nil {
		t.Skipf("docker/postgres unavailable, skipping integration test: %v", sharedErr)
	}

	// Reset state for the test. RESTART IDENTITY resets the cost_snapshots
	// SERIAL so tests can assert exact IDs if they need to. CASCADE handles
	// any future foreign-key relations.
	if _, err := sharedPool.Exec(t.Context(),
		`TRUNCATE TABLE resources, cost_snapshots RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("truncating tables: %v", err)
	}

	return sharedPool
}

func startSharedContainer() (*pgxpool.Pool, testcontainers.Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("cloudoracle_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("starting postgres container: %w", err)
	}

	connString, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, nil, fmt.Errorf("getting connection string: %w", err)
	}

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		_ = c.Terminate(ctx)
		return nil, nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = c.Terminate(ctx)
		return nil, nil, fmt.Errorf("pinging postgres: %w", err)
	}

	if err := migrations.Run(ctx, pool); err != nil {
		pool.Close()
		_ = c.Terminate(ctx)
		return nil, nil, fmt.Errorf("running migrations: %w", err)
	}

	return pool, c, nil
}
