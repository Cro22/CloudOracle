package pricing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProductGetter is a productGetter that records calls and replies
// from a script. Designed for cache tests where we need to assert
// "inner was/was not called" with precision.
type fakeProductGetter struct {
	products []string
	err      error
	calls    int
	// lastService and lastFilters record the most recent input so tests
	// can verify forwarding behaviour without dragging in extra fields.
	lastService string
	lastFilters map[string]string
}

func (f *fakeProductGetter) GetProducts(_ context.Context, serviceCode string, filters map[string]string) ([]string, error) {
	f.calls++
	f.lastService = serviceCode
	f.lastFilters = filters
	if f.err != nil {
		return nil, f.err
	}
	return f.products, nil
}

// captureLogs swaps slog.Default for a text handler writing into the
// returned buffer for the duration of the test. Restored via t.Cleanup.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// readCachedEntry decodes the cache file at path. Used by tests that
// need to assert what we wrote.
func readCachedEntry(t *testing.T, path string) cacheEntry {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cache file %q: %v", path, err)
	}
	var e cacheEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("decoding cache file %q: %v", path, err)
	}
	return e
}

// listCacheFiles returns every .json file in dir, ignoring temp files.
func listCacheFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %q: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, "tmp-") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

func TestCache_MissCallsInnerAndStores(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{`{"a":1}`, `{"b":2}`}}
	c, err := NewCache(fake, dir, time.Hour)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	got, err := c.GetProducts(context.Background(), "AmazonEC2", map[string]string{
		"instanceType": "t3.large",
	})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1", fake.calls)
	}
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != `{"b":2}` {
		t.Errorf("returned products = %v, want fake's response", got)
	}

	files := listCacheFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("got %d cache files, want 1", len(files))
	}
	entry := readCachedEntry(t, files[0])
	if entry.ServiceCode != "AmazonEC2" {
		t.Errorf("ServiceCode = %q", entry.ServiceCode)
	}
	if entry.Filters["instanceType"] != "t3.large" {
		t.Errorf("Filters not persisted: %+v", entry.Filters)
	}
	if len(entry.Products) != 2 {
		t.Errorf("persisted Products len = %d, want 2", len(entry.Products))
	}
	if entry.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
}

func TestCache_HitDoesNotCallInner(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"should-not-be-returned"}}
	c, _ := NewCache(fake, dir, time.Hour)

	// Pre-populate by writing the entry file directly.
	key := cacheKey("AmazonEC2", nil)
	path := filepath.Join(dir, key+".json")
	cached := cacheEntry{
		ServiceCode: "AmazonEC2",
		FetchedAt:   time.Now().UTC(),
		Products:    []string{"cached-1", "cached-2"},
	}
	body, _ := json.Marshal(cached)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if fake.calls != 0 {
		t.Errorf("inner was called %d times on a hit", fake.calls)
	}
	if len(got) != 2 || got[0] != "cached-1" || got[1] != "cached-2" {
		t.Errorf("returned products = %v, want cached entries", got)
	}
}

func TestCache_ExpiredEntryRefreshes(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"fresh-1"}}
	c, _ := NewCache(fake, dir, time.Hour)

	key := cacheKey("AmazonEC2", nil)
	path := filepath.Join(dir, key+".json")
	stale := cacheEntry{
		ServiceCode: "AmazonEC2",
		FetchedAt:   time.Now().UTC().Add(-2 * time.Hour),
		Products:    []string{"stale-1"},
	}
	body, _ := json.Marshal(stale)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (expired entry should refresh)", fake.calls)
	}
	if len(got) != 1 || got[0] != "fresh-1" {
		t.Errorf("returned products = %v, want fresh response", got)
	}

	// File should now be the refreshed entry, not the stale one.
	entry := readCachedEntry(t, path)
	if len(entry.Products) != 1 || entry.Products[0] != "fresh-1" {
		t.Errorf("file not refreshed: %+v", entry)
	}
	if time.Since(entry.FetchedAt) > time.Minute {
		t.Errorf("FetchedAt not updated: %v", entry.FetchedAt)
	}
}

func TestCache_CorruptFileTreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"recovered"}}
	c, _ := NewCache(fake, dir, time.Hour)

	key := cacheKey("AmazonEC2", nil)
	path := filepath.Join(dir, key+".json")
	if err := os.WriteFile(path, []byte("not-json{{{"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	logs := captureLogs(t, slog.LevelWarn)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (corrupt file should miss)", fake.calls)
	}
	if len(got) != 1 || got[0] != "recovered" {
		t.Errorf("returned products = %v, want fresh response", got)
	}

	if !strings.Contains(logs.String(), "cache file corrupt") {
		t.Errorf("expected warn log about corrupt file, got: %s", logs.String())
	}

	// File should be replaced with a valid one.
	entry := readCachedEntry(t, path)
	if len(entry.Products) != 1 || entry.Products[0] != "recovered" {
		t.Errorf("file not replaced with valid entry: %+v", entry)
	}
}

func TestCache_NilAndEmptyFiltersSameKey(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"x"}}
	c, _ := NewCache(fake, dir, time.Hour)

	if _, err := c.GetProducts(context.Background(), "AmazonEC2", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.GetProducts(context.Background(), "AmazonEC2", map[string]string{}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (nil and {} must hash equal)", fake.calls)
	}
}

func TestCache_DifferentFiltersOrderSameKey(t *testing.T) {
	// Go maps don't actually carry insertion order so the test is really
	// asserting "same {key:value} set hashes to the same key regardless
	// of how the caller built the map".
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"x"}}
	c, _ := NewCache(fake, dir, time.Hour)

	first := map[string]string{"a": "1", "b": "2"}
	second := map[string]string{"b": "2", "a": "1"}

	if _, err := c.GetProducts(context.Background(), "AmazonEC2", first); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.GetProducts(context.Background(), "AmazonEC2", second); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (filter order must not affect key)", fake.calls)
	}
	if files := listCacheFiles(t, dir); len(files) != 1 {
		t.Errorf("got %d cache files, want 1", len(files))
	}
}

func TestCache_DifferentServicesDifferentKeys(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"x"}}
	c, _ := NewCache(fake, dir, time.Hour)

	filters := map[string]string{"region": "us-east-1"}
	if _, err := c.GetProducts(context.Background(), "AmazonEC2", filters); err != nil {
		t.Fatalf("EC2 call: %v", err)
	}
	if _, err := c.GetProducts(context.Background(), "AmazonRDS", filters); err != nil {
		t.Fatalf("RDS call: %v", err)
	}

	if fake.calls != 2 {
		t.Errorf("inner calls = %d, want 2 (different services miss separately)", fake.calls)
	}
	if files := listCacheFiles(t, dir); len(files) != 2 {
		t.Errorf("got %d cache files, want 2", len(files))
	}
}

func TestCache_WriteFailureLogsButReturns(t *testing.T) {
	// Cross-platform recipe: build the Cache, then remove the dir
	// underneath it. The next write attempt fails at CreateTemp, the
	// cache logs a warn, and the data is still returned to the caller.
	// This avoids Windows' very different ACL semantics.
	parent := t.TempDir()
	dir := filepath.Join(parent, "pricing-cache")
	fake := &fakeProductGetter{products: []string{"abc"}}
	c, err := NewCache(fake, dir, time.Hour)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing cache dir: %v", err)
	}

	logs := captureLogs(t, slog.LevelWarn)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts returned error on write failure: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1", fake.calls)
	}
	if len(got) != 1 || got[0] != "abc" {
		t.Errorf("returned products = %v, want fake's response", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("expected warn log on write failure, got: %s", logs.String())
	}
}

func TestCache_ReadFailureTreatedAsMiss(t *testing.T) {
	// When os.ReadFile on the cache path returns an error other than
	// fs.ErrNotExist, we want a warn log and a fall-through to the inner
	// source. Simulate that by occupying the cache path with a directory:
	// reading a directory errors with EISDIR (Unix) / access-denied
	// (Windows), neither of which is fs.ErrNotExist.
	//
	// This also exercises the writeEntry rename-failure branch, because
	// the post-miss write tries to Rename a tmp file over the directory
	// we created — Rename fails, the warn is logged, and the products
	// still come back to the caller.
	dir := t.TempDir()
	fake := &fakeProductGetter{products: []string{"recovered"}}
	c, _ := NewCache(fake, dir, time.Hour)

	key := cacheKey("AmazonEC2", nil)
	blockingPath := filepath.Join(dir, key+".json")
	if err := os.Mkdir(blockingPath, 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	logs := captureLogs(t, slog.LevelWarn)

	got, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err != nil {
		t.Fatalf("GetProducts returned error on read failure: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("inner calls = %d, want 1", fake.calls)
	}
	if len(got) != 1 || got[0] != "recovered" {
		t.Errorf("returned products = %v, want fresh response", got)
	}
	if !strings.Contains(logs.String(), "cache read failed") {
		t.Errorf("expected warn log about read failure, got: %s", logs.String())
	}
}

func TestNewCache_BadDir(t *testing.T) {
	// Create a regular file, then try to use a path under it as the
	// cache dir. MkdirAll cannot create a directory below a file on any
	// supported platform, so this exercises the error branch in NewCache
	// without relying on platform-specific permissions.
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}
	bad := filepath.Join(blocker, "child")

	_, err := NewCache(&fakeProductGetter{}, bad, time.Hour)
	if err == nil {
		t.Fatal("expected error from NewCache with un-creatable dir")
	}
	if !strings.Contains(err.Error(), "pricing: creating cache dir") {
		t.Errorf("error missing wrap prefix: %q", err.Error())
	}
}

func TestCache_PropagatesInnerError(t *testing.T) {
	dir := t.TempDir()
	innerErr := errors.New("AccessDenied: not authorized")
	fake := &fakeProductGetter{err: innerErr}
	c, _ := NewCache(fake, dir, time.Hour)

	_, err := c.GetProducts(context.Background(), "AmazonEC2", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("error does not wrap inner error: %v", err)
	}
	if files := listCacheFiles(t, dir); len(files) != 0 {
		t.Errorf("got %d cache files, want 0 (errors must not be cached)", len(files))
	}
}

func TestNewCache_CreatesDirIfMissing(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "nested", "cache")

	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("precondition: target should not exist yet, stat: %v", err)
	}

	fake := &fakeProductGetter{}
	if _, err := NewCache(fake, target, time.Hour); err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after NewCache: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", target)
	}
}

func TestDefaultCacheDir(t *testing.T) {
	got, err := DefaultCacheDir()
	if err != nil {
		t.Fatalf("DefaultCacheDir: %v", err)
	}
	if got == "" {
		t.Fatal("DefaultCacheDir returned empty path")
	}
	if filepath.Base(got) != "pricing-cache" {
		t.Errorf("DefaultCacheDir = %q, want a path ending in pricing-cache", got)
	}
}
