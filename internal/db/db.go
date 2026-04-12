package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}
type Pool = pgxpool.Pool

func LoadConfigFromEnv() Config {
	return Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "oracle"),
		Password: getEnv("DB_PASSWORD", "oracle_dev"),
		Database: getEnv("DB_NAME", "cloudoracle"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exist := os.LookupEnv(key); exist {
		return value
	}
	return defaultValue
}

func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dns := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)

	pool, err := pgxpool.New(ctx, dns)
	if err != nil {
		return nil, err
	}

	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return pool, nil
}
