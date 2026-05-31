# Testing

The project has two tiers of tests:

- **Unit tests** (171, no external dependencies): pure-function tests for the analyzer, generator, LLM providers, LLM retries, PDF report, exporters, cloud mapping, real-provider fetchers, and central config validation. Run with `go test ./internal/...`.
- **Integration tests** (12, require Docker): exercise the real Postgres path via [testcontainers-go](https://golang.testcontainers.org/) — insert/upsert behavior, transaction rollback, snapshot aggregation, and a full end-to-end seed → analyze flow against a containerized Postgres 16. Run with `go test -tags=integration ./internal/db/ ./internal/e2e/`.

Integration tests share a single Postgres container per process and `TRUNCATE … RESTART IDENTITY CASCADE` between cases — fast (sub-millisecond reset on small tables) and hermetic enough for our schema. The helper lives at `internal/db/dbtest/postgres.go` and is gated by the `integration` build tag, so the testcontainers dependency stays out of the unit-test compile path. If Docker isn't running, the helper calls `t.Skip` with a clear message rather than failing — running the binary without Docker just skips the integration cases.

The CI workflow at `.github/workflows/test.yml` runs both tiers on every push and PR. GitHub-hosted Ubuntu runners have Docker preinstalled, so the integration job needs no extra service container.

## Unit test coverage

- **Per-rule tests**: each detection rule (`ec2-idle`, `rds-oversized`, `ebs-orphan`, `lambda-over-provisioned`) has happy-path, negative, and boundary tests.
- **Boundary testing**: CPU thresholds, age cutoffs, memory limits, and invocation counts are explicitly tested at their exact values to catch off-by-one errors.
- **Aggregator tests**: `Analyze` is tested for empty input, mixed input, false-positive prevention, and correct savings-descending ordering.
- **LLM provider tests**: all three providers (Gemini, Claude, OpenAI) are tested against mock HTTP servers using `httptest`, covering success responses, API errors, empty payloads, error fields, and context cancellation.
- **Provider factory tests**: auto-detection order (Gemini > Claude > OpenAI), explicit selection, missing keys, and unknown providers.
- **Prompt builder tests**: total calculations, severity breakdowns, service rollups, top-5 limiting, and empty input handling.
- **PDF generation tests**: file creation, AI summary inclusion/exclusion, empty findings, 100-finding page-break stress test, invalid paths, and all severity color codes.
- **Export tests**: JSON round-trip, CSV header + row layout, numeric formatting, RFC 4180 escaping of commas/quotes/newlines, and empty-findings handling for both formats.
- **Generator tests**: correct count, valid services/regions/types, non-negative costs, timestamp ordering, and service distribution.
- **Config tests**: default values, custom values, timeout parsing (valid and invalid durations), empty-env fallback, and DSN assembly.
- **Cloud mapping tests**: AWS SDK type → `shared.Resource` conversion with struct literals (no AWS calls, no credentials needed).
- **Real-provider fetcher tests**: every cloud provider (AWS, GCP, Azure) is exercised end-to-end against fake SDK clients — pagination exhaustion, per-service API errors, graceful degradation when one service fails, and edge cases (nil hardware profile on Azure VMs, nil settings on Cloud SQL, web apps mixed with function apps in the Azure `/sites` collection).
- **LLM retry tests**: the shared retry transport is verified against `httptest` servers — retries until success, respects `MaxRetries` cap, honors `Retry-After` headers, replays the request body on every attempt, retries transport-level errors (not just non-2xx), bails out on context cancellation, and returns immediately on non-retryable statuses (401, 4xx other than 408/429).
- **Config validation tests**: every invalid input shape (non-numeric port, out-of-range port, unknown enum value, negative integer, malformed Go duration, zero/negative duration), every cross-field rule (provider=gcp without project, provider=azure without subscription, LLM_PROVIDER set without matching API key), and the multi-error accumulator that lists all problems at once instead of failing on the first.

## Integration test coverage

- **Insert + upsert**: round-trip through a real Postgres, asserting that `ON CONFLICT DO UPDATE` updates the right columns (`monthly_cost`, `usage_metric`, `updated_at`) without overwriting `created_at`.
- **Transaction rollback**: a failing batch (one row that overflows `NUMERIC(10,2)`) rolls back the whole batch, leaving pre-existing rows untouched.
- **Snapshot aggregation**: a mixed set of resources across multiple `(account, service)` tuples produces exactly the expected snapshot rows, with correct counts and per-tuple cost totals.
- **Snapshot windowing**: the `--days` filter on the `trend` command actually filters via SQL — old snapshots are excluded from short windows and included in long ones.
- **End-to-end seed → analyze**: a deterministic resource set engineered to fire each rule once, inserted via `InsertResources`, read back via `ListResources`, and analyzed — asserts every rule fires exactly once and findings are sorted by potential savings descending.
- **End-to-end with synthetic data**: 50 random resources generated by `SyntheticProvider`, full round-trip through the DB, analyzer must produce *some* findings (the generator skews toward waste patterns).
- **Re-seed idempotency**: running insert three times on the same fixed-ID set ends with the same row count — proves the seed flow is safe to re-run on a schedule.

## Running the suite

```bash
# Unit tests (no Docker required)
go test ./internal/...

# Integration tests (Docker must be running)
go test -tags=integration ./internal/db/ ./internal/e2e/

# Both, verbose
go test -tags=integration -v ./internal/...
```

All rules are pure functions (`Resource -> *Finding`), which makes them trivially testable without mocks, fixtures, or test databases. The code was designed to be testable from the start — not tested after the fact.
