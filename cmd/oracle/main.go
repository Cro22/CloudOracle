package main

import (
	"CloudOracle/internal/db"
	"context"
	"log"
)

func main() {
	ctx := context.Background()
	cfg := db.LoadConfigFromEnv()
	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()
	log.Println("✓ Connected to database!")
}
