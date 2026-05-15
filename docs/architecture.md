# Architecture

This document covers v1 and v2 internal layout, the design patterns behind the analyzer and LLM provider abstraction, and the architecture decisions made along the way.

## v2 architecture (Terraform PR cost analysis)

```
internal/iac/                  # Terraform plan parser
  terraform.go                 # ParsePlan / ParsePlanFile + the canonical Plan model
  aws/                         # AWS-specific resource shape decoders (after_unknown handling, attr extraction)
internal/pricing/              # AWS Pricing API client + per-service estimators
  aws.go                       # *pricing.Client wrapping the AWS SDK
  cache.go                     # 7-day disk cache (best-effort) keyed by service+filters
  ec2.go / ebs.go / rds.go / lambda.go / nat.go   # one estimator per supported resource type
  estimator.go                 # EstimateChange entry point — dispatches to the right estimator
internal/diff/                 # CostDiff aggregation + Markdown rendering
  engine.go                    # Analyze: per-resource estimates -> CostDiff (Created/Deleted/Updated/Replaced/Skipped)
  markdown.go                  # template-based PR comment renderer (header / table / breakdown / caveats / footer)
  narrative.go                 # LLM narrative + grouped caveats + silent fallback to templated text
internal/github/               # Thin GitHub REST client (issue comments only)
  client.go / comments.go      # listComments (paginated, capped) + postComment + updateComment + PostOrUpdateComment
cmd/oracle/                    # pr-check subcommand wires it all together
  main.go                      # runPRCheck: ParsePlan -> Analyze -> Render -> [Post]
Dockerfile.action              # Multi-stage golang:1.25-alpine -> alpine:3.19, ENTRYPOINT entrypoint.sh
entrypoint.sh                  # POSIX shim: INPUT_* env vars -> oracle pr-check flags
action.yml                     # GitHub Action manifest (runs: docker, image: Dockerfile.action)
```

The v1 dashboard `Dockerfile` at the repo root is **untouched** — `Dockerfile.action` is a separate, leaner image just for the Action. They share a single `.dockerignore`.

## v1 architecture (Cloud cost audit)

```
cmd/oracle/main.go          # CLI entry point (seed, list, analyze, report, trend, pr-check)
internal/
  config/
    config.go               # Central Config + Load(): reads every env var up front
  logging/
    logging.go              # slog setup (text or JSON, configurable level)
  shared/
    resource.go             # Resource domain model
    finding.go              # Finding + Severity types
  cloud/
    provider.go             # CloudProvider interface (Strategy pattern)
    factory.go              # Provider factory: Config -> concrete provider
    synthetic_provider.go   # Synthetic data provider (dev/demo)
    aws_provider.go         # Real AWS provider — parallel fetchers with per-service timeouts
    aws_clients.go          # Narrow ec2/rds/lambda interfaces — *aws.Client satisfies them, fakes drive tests
    gcp_provider.go         # Real GCP provider — parallel fetchers with per-service timeouts
    gcp_clients.go          # Lister interfaces + SDK adapters that flatten pagination
    azure_provider.go       # Real Azure provider — parallel fetchers with per-service timeouts
    azure_clients.go        # Lister interfaces + SDK adapters that flatten pagers
  generator/
    generator.go            # Synthetic data generation for EC2, RDS, EBS, Lambda
  analyzer/
    analyzer.go             # Rule engine: runs all rules, sorts by savings
    rules.go                # Detection rules (pure functions)
  report/
    pdf.go                  # PDF report generator (executive summary + findings table)
    export.go               # JSON and CSV exporters for findings
  llm/
    provider.go             # Provider interface + Config-driven factory (Gemini / Claude / OpenAI)
    prompt.go               # Shared prompt builder (findings -> structured analysis)
    http.go                 # newHTTPClient: builds the *http.Client every provider uses
    retry.go                # http.RoundTripper that retries 429/5xx/net errors with full-jitter backoff
    gemini.go               # Google Gemini client (gemini-2.5-flash)
    claude.go               # Anthropic Claude client (claude-haiku-4-5)
    openai.go               # OpenAI client (gpt-4o-mini)
  db/
    db.go                   # PostgreSQL connection pool (pgx)
    insert.go               # Transactional insert + query logic
    snapshots.go            # Cost snapshot creation + trend queries
    trends.go               # Aggregated trends for the /api/trends endpoint
    dbtest/postgres.go      # testcontainers-go helper (gated by `integration` build tag)
    *_integration_test.go   # //go:build integration — real Postgres tests
  e2e/
    seed_analyze_test.go    # //go:build integration — full seed -> analyze flow
  migrations/
    migrations.go           # go:embed runner executed at app startup
    001_create_resources.sql
    002_create_cost_snapshots.sql
Dockerfile                  # Multi-stage: npm build → go build → alpine runtime
docker-compose.yml          # Postgres (with healthcheck) + app service
```

The cloud provider layer uses the **Strategy pattern**: `CloudProvider` is the interface, and `SyntheticProvider`, `AWSProvider`, `GCPProvider`, and `AzureProvider` are the concrete strategies. `factory.go` selects the strategy at runtime based on the `Config` loaded from `internal/config`. This lets `main.go` work with any provider without knowing which one is active.

Configuration is loaded once in `main()` via `config.Load()` and injected downward. No component in `cloud/`, `llm/`, or `db/` calls `os.Getenv` directly — every dependency arrives as a typed struct field. This keeps the surface area predictable, makes the code easy to test with struct literals, and means adding a new env var is a single-file change in `internal/config/config.go`.

Each real provider's `FetchResources` fans out its service calls (for example: EC2, RDS, EBS, and Lambda on AWS) onto separate goroutines via `golang.org/x/sync/errgroup`. Each goroutine wraps its API call in `context.WithTimeout(cfg.ServiceTimeout)`, so one slow service can't block the others and a regional outage surfaces as a structured warning rather than a hung process. Per-service failures are logged with `slog` and the successful services still return their resources — the scan degrades gracefully instead of failing hard.

The SDK call surface for every real provider is hidden behind narrow interfaces (`ec2APIClient`, `gcpInstancesLister`, `azureVMLister`, …) defined in `aws_clients.go` / `gcp_clients.go` / `azure_clients.go`. Concrete `*ec2.Client`, `*compute.InstancesClient`, and `*armcompute.VirtualMachinesClient` values satisfy those interfaces transparently, so production code is unchanged — but unit tests can plug in fakes that return canned slices and simulate API errors without ever touching the network or needing credentials. The mapping logic (`SDK type -> shared.Resource`) stays inline with the fetcher, which means tests can exercise pagination, error handling, graceful degradation, and edge-case field handling end-to-end.

## How the Analyzer Works

The analyzer follows a simple but extensible pattern:

```go
type Rule func(r shared.Resource) *shared.Finding
```

Each rule is a **pure function** that receives a resource and returns either a finding (if a problem was detected) or `nil`. This makes rules easy to test, compose, and add. The engine iterates over all resources, applies every rule, collects non-nil findings, and sorts them by potential savings descending.

Adding a new rule is a three-step process:
1. Write the function in `internal/analyzer/rules.go`
2. Register it in the `rules` slice in `analyzer.go`
3. That's it. No interfaces, no config files.

## The LLM Provider Layer

The AI summary feature is built around a single interface that every provider satisfies:

```go
type Provider interface {
    GenerateSummary(ctx context.Context, findings []shared.Finding) (string, error)
    Name() string
}
```

Three providers are shipped out of the box — Gemini, Claude, and OpenAI — each owning its own HTTP client, request/response types, and authentication headers. A shared `BuildPrompt` function in `internal/llm/prompt.go` computes totals, severity breakdowns, and per-service rollups, then wraps them in a consistent CTO/CFO-oriented prompt that every provider receives. This guarantees the narrative style stays identical no matter which model generated it.

Provider selection is resolved at runtime by `NewProvider()`:
1. If `LLM_PROVIDER` is set, that provider is used explicitly.
2. Otherwise, the first available API key wins, in the order **Gemini → Claude → OpenAI**.
3. If no key is found, `ErrNoProvider` is returned and the report command gracefully skips the AI section.

Adding a fourth provider is a matter of creating one new file: implement the two methods on a struct, add a `newFooFromEnv()` constructor, and wire it into the switch in `provider.go`. The rest of the system — prompt, PDF rendering, CLI flags — stays untouched.

## Architecture Decisions

### Why not Cloud Custodian?
Cloud Custodian (Python, ~6k stars) is a mature policy engine: you write YAML rules like *"if an EC2 has no `Owner` tag, stop it"* and it **enforces** them across AWS/GCP/Azure. CloudOracle targets a different stage of the FinOps loop:

- **Custodian**: governance and remediation — takes actions (stop, delete, tag, notify). Designed for platform teams running hundreds of policies in CI.
- **CloudOracle**: analysis and reporting — read-only, LLM-assisted narrative, PDF + dashboard. Designed for the conversation between engineering and finance, not for automated enforcement.

The tools are complementary: Custodian is *what to enforce*, CloudOracle is *why it matters this month*. Read-only is intentional — it's safer to adopt in a new org and removes the "did this tool just delete my database?" objection at procurement time.

### Why interfaces over inheritance for LLM providers
The `Provider` interface in `internal/llm` is intentionally minimal — just `GenerateSummary` and `Name`. Each provider (Gemini, Claude, OpenAI) is a fully independent implementation. Adding a fourth provider requires zero changes to existing code: write a new file, register it in `provider.go`, done. This is Go's structural typing at its best — no inheritance, no abstract base classes, no framework lock-in.

### Why a shared Postgres container with TRUNCATE rather than a container per test
The integration helper at `internal/db/dbtest/postgres.go` boots one Postgres 16 container per test process and resets the schema with `TRUNCATE … RESTART IDENTITY CASCADE` between tests. The alternative — a fresh container per test — gives stronger isolation but pays ~3-5s of container-startup cost per case, which adds up fast as the suite grows. TRUNCATE on small tables runs in sub-millisecond, and all our tables are independent (no triggers, no shared sequences spanning tests), so the isolation guarantee is the same in practice. The whole integration suite (12 tests) runs in ~5 seconds total instead of ~60.

If we ever add tests that need different schemas or different Postgres versions, we'd opt back into a per-test container for those specific cases — but as a default, sharing wins on speed.

### Why retries live in a `RoundTripper` rather than around each `client.Do`
Every LLM provider eventually hits a 429 or a 5xx — Anthropic and OpenAI both rate-limit aggressively and both send `Retry-After` headers. Putting the retry loop inside the transport (`internal/llm/retry.go`) means **every** code path that issues an HTTP request gets retries automatically: the three providers today, and whatever future request paths we add (token-counting endpoints, streaming, file uploads). The alternative — wrapping each `client.Do` call — is more obvious but every new call site has to remember to wrap, and tests have to mock the wrapper.

The transport buffers the request body once on entry and replays it via `req.Body` + `req.GetBody` on every attempt. It's safe because LLM POST bodies are tiny (a JSON prompt). It honors `Retry-After` (delta-seconds and HTTP-date forms) before falling back to exponential backoff with full jitter — full jitter (random in `[0, baseDelay * 2^attempt]`) is the AWS-recommended algorithm for distributed clients hitting the same endpoint, because it spreads retries evenly instead of producing thundering herds. Backoff waits respect the request context, so cancellation propagates cleanly mid-retry.

### Why net/http directly instead of vendor SDKs
All three LLM providers are implemented with the standard library `net/http` package, no vendor SDKs. This keeps the dependency tree small (the entire project has fewer than 10 direct dependencies), makes the code portable, and forces explicit handling of errors, timeouts, and retries — all of which are usually hidden behind SDK abstractions.

### Why deterministic rules first, LLMs second
The analyzer detects 80% of cloud waste using simple pure functions, before any LLM is involved. This is by design: deterministic rules are predictable, testable, free, and instant. LLMs are reserved for what they're actually good at — translating structured data into executive prose. Inverting this order (using LLMs to detect waste) would be slower, more expensive, and less reliable.

### Why graceful degradation when no LLM is configured
If no API key is set, the report generates without the AI summary section instead of failing. This means anyone can clone the repo and run it immediately, and the same binary works in restricted environments where outbound API calls aren't allowed.

### Why synthetic data instead of real AWS integration in v1
Building the rule engine and report generator against a synthetic data generator allowed iteration without paying for AWS resources, without rate limits, and without coupling the early development to credentials. Real AWS integration is the next milestone, but the abstraction was earned by first solving the harder problem: detecting waste from any data source.

### Why `errgroup` instead of raw goroutines for provider fan-out
Each real provider issues 4 independent API calls per scan (for example: EC2, RDS, EBS, Lambda on AWS). Running them sequentially meant the total scan time was the sum of the slowest region's latency for every service. Switching to `errgroup.WithContext` + a fixed-size `[][]shared.Resource` result slice (each goroutine owns its own index → no mutex) cut end-to-end scan time roughly in proportion to the number of services per provider. Returning `nil` from each goroutine after logging — instead of propagating errors — preserves the "log one failing service, keep the rest" contract the sequential version had, while giving the rest of the services a genuine chance to finish in parallel.

### Why per-service `context.WithTimeout` rather than a single global deadline
A scan is only as fast as its slowest cloud API. Giving every service its own deadline (`CLOUD_SERVICE_TIMEOUT`, default 30s) means a misbehaving region bounds only itself — the other services still complete normally. A single global timeout would have cancelled every in-flight service the moment one hung, wasting the progress already made.

### Why `log/slog` over `log.Printf`
Every warning now carries typed attributes (`provider=aws`, `service=EC2`, `error=...`) instead of being jammed into a free-form sprintf string. That makes logs grep-able, filterable by level, and — with `LOG_FORMAT=json` — ingestion-ready for Loki, ELK, or Cloud Logging without a log parser. `slog` is the standard library's answer to this, landed in Go 1.21, and needs zero external dependencies.

### Why a central `config.Load()` over per-component `os.Getenv`
Previously every constructor reached into the environment on its own: `NewAWSProvider` for region/profile, `NewGCPProvider` for the project ID, each LLM constructor for its API key, `db.LoadConfigFromEnv` for credentials. That made the contract of each component implicit and the cost of testing high — you had to manipulate real env vars to rearrange behavior. Now `main()` calls `config.Load()` once, and every component receives its typed slice of the config as a parameter. Tests pass struct literals directly.

### Why migrations run from the app at startup (not from `psql` scripts or a separate tool)
SQL files live in `internal/migrations/*.sql` and are baked into the binary with `go:embed`. On every boot — CLI command or `serve` — `main()` reads them in order and executes each against the pool. Because the statements use `CREATE TABLE/INDEX IF NOT EXISTS`, re-running is a no-op. Trade-offs vs. the alternatives:

- **Postgres `docker-entrypoint-initdb.d` mount**: only runs the very first time a volume is created. If the DB already exists (prod restore, bind mount, CI cache), schema changes never land. Silent and dangerous.
- **A separate `migrate` CLI step**: adds a second binary and a deploy-ordering problem (app must not start before `migrate` succeeds). `depends_on` helps but doesn't eliminate it.
- **App-driven startup**: self-contained, idempotent, and works identically whether you boot the binary directly, with Docker Compose, in a test, or in production. The one binary knows how to set up its own schema.

The one thing app-driven migrations don't give you out of the box is a version ledger (`schema_migrations` table) for tracking what's been applied. For a 2-file schema it's overkill; if the project grows a destructive migration (e.g. a column rename) we'd add one. Until then, `IF NOT EXISTS` is enough.

## Lessons Learned

Building this project surfaced a subtle but important bug that would have gone unnoticed without testing against real(istic) data:

**The case-sensitivity trap:** The EC2 idle detection rule was comparing `r.Service != "EC2"` (uppercase), but the data generator and database stored services as `"ec2"` (lowercase). The rule silently passed over every EC2 instance without flagging a single one. The RDS, EBS, and Lambda rules all used lowercase correctly, making this inconsistency easy to miss during code review. It was only caught when analyzing output and noticing zero EC2 findings despite seeding idle instances.

**Takeaway:** String comparison bugs are among the most common sources of silent failures in cloud tooling. Production systems use canonical enumerations or case-insensitive matching for exactly this reason. Finding this during development -- not after deployment -- is the difference between a tool that works and one that looks like it works.

**The Strategy pattern for cloud providers:** The `CloudProvider` interface started as a formality — there was only the synthetic provider. But when adding real AWS support, the pattern paid for itself: `AWSProvider` and `SyntheticProvider` both satisfy the same interface, `factory.go` picks the right one from an env var, and `main.go` never knows which is active. The key insight was keeping the mapping logic (SDK types -> domain types) as pure functions separated from the API calls. This made it possible to unit test the field mapping with struct literals instead of mocking the entire AWS SDK — a pattern worth repeating for GCP and Azure providers.
