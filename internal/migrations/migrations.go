package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

func Run(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("running %s: %w", name, err)
		}
		slog.Debug("migration applied", "file", name)
	}
	slog.Info("migrations up to date", "count", len(names))
	return nil
}
