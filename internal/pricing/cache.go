package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// productGetter is the abstraction Cache wraps. *Client satisfies it
// naturally; tests inject a fake. Defining it here (and not exporting)
// keeps the cache focused on a single shape without leaking an interface
// that callers might mistakenly depend on instead of *Client.
type productGetter interface {
	GetProducts(ctx context.Context, serviceCode string, filters map[string]string) ([]string, error)
}

// Cache wraps a productGetter with disk-based caching of GetProducts
// results.
//
// Entries are stored as JSON files under dir, keyed by sha256 of
// (serviceCode + sorted filters). Entries older than ttl are treated as
// misses and refreshed from the underlying source. The on-disk format is
// described on cacheEntry.
//
// All cache operations are best-effort: if a read fails for any reason
// (missing file, IO error, corrupt JSON) the underlying source is
// consulted and its result returned. If a write fails, the products are
// still returned to the caller. Cache failures emit slog.Warn but never
// propagate to the caller — the cache must not break the happy path.
//
// Concurrent access: there is no explicit locking. Two processes
// hitting the same entry at the same time may both call the underlying
// source and both write the file; the OS gives last-writer-wins, which
// is acceptable for a TTL'd best-effort cache. Writes use a temp-file +
// rename so a reader never observes a half-written file.
type Cache struct {
	inner productGetter
	dir   string
	ttl   time.Duration
}

// cacheEntry is the on-disk shape of a single cache record.
//
// service_code and filters are denormalized into the file purely for
// human debugging — the cache integrity is guaranteed by the sha256
// filename, and the loader does NOT cross-check these fields against
// the request. Treat them as "what was this entry for?" annotations
// rather than authoritative data.
type cacheEntry struct {
	ServiceCode string            `json:"service_code"`
	Filters     map[string]string `json:"filters"`
	FetchedAt   time.Time         `json:"fetched_at"`
	Products    []string          `json:"products"`
}

// NewCache creates a Cache backed by inner, storing entries in dir with
// the given TTL. dir is created (with os.MkdirAll) if it doesn't exist.
//
// Returns an error only if dir cannot be created — that's a
// configuration problem (bad path, permission denied) and surfacing it
// at construction time avoids confusing best-effort failures later.
//
// Recommended ttl is 7 days: AWS public prices change rarely, and
// stale-by-a-week is much cheaper than hammering the API on every PR
// update. Recommended dir is DefaultCacheDir().
func NewCache(inner productGetter, dir string, ttl time.Duration) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("pricing: creating cache dir %q: %w", dir, err)
	}
	return &Cache{inner: inner, dir: dir, ttl: ttl}, nil
}

// DefaultCacheDir returns the recommended on-disk cache location:
//   - Windows: %USERPROFILE%\.cloudoracle\pricing-cache
//   - Unix:    $HOME/.cloudoracle/pricing-cache
//
// Returns an error if the user's home directory cannot be determined
// (rare; happens in stripped-down container environments).
func DefaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("pricing: resolving user home dir: %w", err)
	}
	return filepath.Join(home, ".cloudoracle", "pricing-cache"), nil
}

// GetProducts checks the cache before consulting the underlying source.
//
// Hit  (file present, valid JSON, within TTL): returns the cached
// products. inner is not called.
// Miss (file absent, IO error, corrupt JSON, or expired): calls
// inner.GetProducts and writes the result to disk. Corrupt and expired
// files are removed opportunistically. Cache write failures log a
// warning but do not affect the returned data.
//
// If the underlying source returns an error, that error is propagated
// verbatim and nothing is written to disk.
func (c *Cache) GetProducts(ctx context.Context, serviceCode string, filters map[string]string) ([]string, error) {
	key := cacheKey(serviceCode, filters)
	path := filepath.Join(c.dir, key+".json")

	if products, ok := c.readEntry(path); ok {
		slog.Debug("pricing: cache hit",
			"serviceCode", serviceCode,
			"key", key,
		)
		return products, nil
	}

	slog.Debug("pricing: cache miss",
		"serviceCode", serviceCode,
		"key", key,
	)

	products, err := c.inner.GetProducts(ctx, serviceCode, filters)
	if err != nil {
		return nil, err
	}

	c.writeEntry(path, cacheEntry{
		ServiceCode: serviceCode,
		Filters:     filters,
		FetchedAt:   time.Now().UTC(),
		Products:    products,
	})

	return products, nil
}

// readEntry returns (products, true) on a valid, fresh cache hit and
// (nil, false) for any miss reason. Corrupt and expired files are
// deleted before returning so the next miss path doesn't keep tripping
// over the same garbage.
func (c *Cache) readEntry(path string) ([]string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// A missing file is the common case — don't even debug-log it.
		// IO errors on a present file are worth a warn.
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("pricing: cache read failed",
				"path", path,
				"error", err,
			)
		}
		return nil, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		slog.Warn("pricing: cache file corrupt, removing",
			"path", path,
			"error", err,
		)
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			slog.Warn("pricing: failed to remove corrupt cache file",
				"path", path,
				"error", rmErr,
			)
		}
		return nil, false
	}

	if time.Since(entry.FetchedAt) > c.ttl {
		slog.Debug("pricing: cache entry expired",
			"path", path,
			"fetched_at", entry.FetchedAt,
		)
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			slog.Warn("pricing: failed to remove expired cache file",
				"path", path,
				"error", rmErr,
			)
		}
		return nil, false
	}

	return entry.Products, true
}

// writeEntry persists an entry atomically: marshal, write to a temp
// sibling, rename into place. Any failure is logged at warn and
// swallowed — the caller already has the data and shouldn't be
// penalised because we couldn't write to disk.
func (c *Cache) writeEntry(path string, entry cacheEntry) {
	body, err := json.Marshal(entry)
	if err != nil {
		// json.Marshal on a struct of strings/time/[]string shouldn't
		// fail in practice, but log if it ever does instead of panicking.
		slog.Warn("pricing: cache marshal failed",
			"path", path,
			"error", err,
		)
		return
	}

	tmp, err := os.CreateTemp(c.dir, "tmp-*.json")
	if err != nil {
		slog.Warn("pricing: cache temp file create failed",
			"dir", c.dir,
			"error", err,
		)
		return
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(body); err != nil {
		slog.Warn("pricing: cache write failed",
			"path", tmpPath,
			"error", err,
		)
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		slog.Warn("pricing: cache temp file close failed",
			"path", tmpPath,
			"error", err,
		)
		_ = os.Remove(tmpPath)
		return
	}

	if err := os.Rename(tmpPath, path); err != nil {
		slog.Warn("pricing: cache rename failed",
			"from", tmpPath,
			"to", path,
			"error", err,
		)
		_ = os.Remove(tmpPath)
		return
	}
}

// cacheKey computes the deterministic cache key for a lookup.
//
// Layout: sha256( serviceCode + "\n" + sortedFiltersSerialized ).
// sortedFiltersSerialized joins filter entries by alphabetical key as
// "key=value\n" pairs, producing the same string for nil filters and
// empty maps (both serialize to "").
func cacheKey(serviceCode string, filters map[string]string) string {
	var sb strings.Builder
	sb.WriteString(serviceCode)
	sb.WriteByte('\n')
	sb.WriteString(serializeFilters(filters))
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func serializeFilters(filters map[string]string) string {
	if len(filters) == 0 {
		return ""
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(filters[k])
		sb.WriteByte('\n')
	}
	return sb.String()
}
