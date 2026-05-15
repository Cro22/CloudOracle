# v1 — Cloud cost audit

Detailed guide for v1 audit mode: ingest live (or synthetic) cloud inventory into Postgres, run deterministic rules over it, and produce an executive PDF + dashboard with an LLM-narrated summary.

## Why this project?

Cloud waste is a real problem. Companies routinely overspend 20-30% on cloud infrastructure because nobody is watching the bill. CloudOracle demonstrates how to build a system that catches these issues automatically, using the same patterns that tools like AWS Trusted Advisor or Datadog Cloud Cost Management use internally.

Unlike policy engines like **Cloud Custodian** that focus on automated enforcement, CloudOracle is an *analysis-first* tool built for FinOps visibility — combining deterministic rules with LLM-generated insights to produce executive-ready reports and dashboards.

## Features

- **Multi-cloud support** - Switch between AWS, GCP, Azure, and synthetic data via a single env var (`CLOUDORACLE_PROVIDER`)
- **Real AWS integration** - Fetches live EC2 instances, RDS databases, EBS volumes, and Lambda functions using AWS SDK v2 with STS credential validation
- **Real GCP integration** - Fetches Compute Engine VMs, Cloud SQL instances, Persistent Disks, and Cloud Functions using Google Cloud Go client libraries
- **Real Azure integration** - Fetches Virtual Machines, Azure SQL databases, Managed Disks, and Function Apps using Azure SDK for Go
- **Synthetic data generation** - Realistic resource simulation across EC2, RDS, EBS, and Lambda with configurable account IDs and resource counts
- **PostgreSQL persistence** - Transactional bulk inserts with upsert support (`ON CONFLICT DO UPDATE`)
- **Rule-based analysis engine** - Pluggable rules architecture where each rule is a pure function `Resource -> Finding`
- **4 detection rules**:
  - `ec2-idle` - Flags instances with <5% CPU usage running for more than 7 days (HIGH severity)
  - `rds-oversized` - Identifies RDS instances with <10% CPU utilization (MEDIUM severity)
  - `ebs-orphan` - Detects unattached EBS volumes with zero usage (HIGH severity)
  - `lambda-over-provisioned` - Finds Lambda functions with >1GB memory and low invocation counts (LOW severity)
- **Savings-ranked output** - Findings are sorted by potential monthly savings (highest first)
- **Service summary** - Aggregated view of findings and potential savings per AWS service
- **PDF report generation** - Professional executive-style PDF reports with severity-coded tables, recommended actions, and annual savings projections
- **LLM-powered executive summaries** - Pluggable provider layer (Gemini, Claude, OpenAI) that turns raw findings into a CTO/CFO-ready narrative embedded directly into the PDF report
- **Resilient LLM calls** - Shared `http.RoundTripper` retries 429s, 5xx, and network errors with exponential-backoff-with-full-jitter; honors the `Retry-After` header from Anthropic/OpenAI; cancellable via the request context
- **Cost trend tracking** - Automatic cost snapshots on every seed, with a `trend` command that shows per-service cost changes over time with directional arrows and percentage deltas
- **Parallel resource fetching** - Each provider fans out service calls (Compute / SQL / Disks / Functions) concurrently with `errgroup`, cutting scan time on accounts with many services
- **Per-service timeouts** - Every API call to a cloud service is wrapped in `context.WithTimeout` so a single slow region can't stall the entire scan
- **Structured logging (`log/slog`)** - Every log line carries typed attributes (`provider`, `service`, `error`, ...), with pluggable text or JSON output for ingestion into log aggregators
- **Centralized configuration** - A single `config.Load()` reads every env var up front and is injected into the cloud, LLM, and DB layers — no component reaches for `os.Getenv` on its own
- **Export findings to JSON or CSV** - Pipe analyzer output into downstream tooling (dashboards, spreadsheets, ticket systems) via `oracle export --format=json|csv`, writing to stdout or a file
- **Single-binary web dashboard** - React + Recharts UI embedded into the Go binary via `go:embed`; `oracle serve` boots API and dashboard on one port with no external assets required

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- (Optional) AWS CLI configured with a `cloudoracle` profile for real AWS integration (see [cloud-providers.md](cloud-providers.md))

### 1. Start the stack

Single command for the full demo (Postgres + API + embedded React dashboard):

```bash
docker compose up --build
# → open http://localhost:8080
```

Compose brings up two services:
- **postgres** — PostgreSQL 16 with a healthcheck; the app only starts once it responds to `pg_isready`.
- **app** — multi-stage build of the Go binary with the React bundle embedded via `go:embed`, exposed on `:8080`.

The app auto-applies the SQL migrations in `internal/migrations/*.sql` on every startup (they're idempotent — `CREATE TABLE/INDEX IF NOT EXISTS`), so there's no separate migration step. To populate demo data:

```bash
docker compose exec app /app/cloudoracle seed --count 120
```

For local development without Docker you still need Postgres running somewhere; the easiest is `docker compose up -d postgres` and then run the Go binary on the host. Migrations run automatically whichever way you boot the app.

### 2. Seed sample data

```bash
go run cmd/oracle/main.go seed --account acc-001 --count 100
```

### 3. List all resources

```bash
go run cmd/oracle/main.go list
```

### 4. Run the cost analyzer

```bash
go run cmd/oracle/main.go analyze
```

### 5. Generate a PDF report

```bash
go run cmd/oracle/main.go report --output cloudoracle-report.pdf
```

This generates a professional PDF with:
- Executive summary (total findings, monthly/annual savings projections)
- Severity breakdown (HIGH / MEDIUM / LOW)
- Color-coded findings table with cost and savings per resource
- Recommended actions for each finding
- **AI-generated narrative** (when an LLM provider is configured) — 3-4 paragraph executive summary written for a CTO/CFO audience, focused on financial impact, highest-priority problems, and recommended next steps

![CloudOracle PDF report example](../examplepdf.png)

### 6. View cost trends

Each `seed` automatically creates a cost snapshot. After running `seed` multiple times (on different days or with different data), view how costs change:

```bash
go run cmd/oracle/main.go trend --days 30
```

```
Cost Trends (last 30 days, 3 snapshots)

Service      Oldest       Latest         Change
────────────────────────────────────────────────────────
ebs          $   100.00 $    90.00    -10.00 (-10.0%) ↓
ec2          $   460.00 $   510.00    +50.00 (+10.9%) ↑
lambda       $     2.50 $     3.10     +0.60 (+24.0%) ↑
rds          $   180.00 $   195.00    +15.00 (+8.3%)  ↑
────────────────────────────────────────────────────────
Total        $   742.50 $   798.10    +55.60 (+7.5%)  ↑
```

### 7. Export findings to JSON or CSV

Run the analyzer and pipe its findings into another tool — a dashboard, a spreadsheet, a ticketing system. By default, the exporter writes to stdout so it composes naturally with shell pipelines; pass `--output` to write to a file.

```bash
# Pretty-printed JSON to stdout
go run cmd/oracle/main.go export --format=json

# CSV to a file (header row + one finding per row)
go run cmd/oracle/main.go export --format=csv --output findings.csv

# Pipe straight into jq
go run cmd/oracle/main.go export --format=json | jq '.[] | select(.Severity == "High")'
```

The JSON output is an array of `Finding` objects. The CSV output has a fixed header: `resource_id, service, resource_type, region, rule, severity, monthly_cost, monthly_savings, description, recommendation`. Numeric fields are formatted with two decimals. Commas, quotes, and newlines in descriptions are escaped per RFC 4180 — the output is safe to open in Excel or parse with any standard CSV library.

### 8. Web dashboard

CloudOracle ships a React + Recharts dashboard that reads the same database as the CLI. There are two workflows:

**Production / demo — one binary, one command.** The Go binary embeds the compiled frontend via `go:embed`, so after a single `npm run build` the whole stack (API + UI) is served on one port.

```bash
# Build the React bundle into internal/api/dist (go:embed target)
cd web
npm install   # first time only
npm run build
cd ..

# Build the self-contained binary and run it
go build -o cloudoracle ./cmd/oracle
./cloudoracle serve --port 8080
# → open http://localhost:8080
```

The binary is fully self-contained. Copy the single file (`cloudoracle` / `cloudoracle.exe`) to any machine, point it at a reachable Postgres via `DB_*` env vars, and the dashboard loads. No `web/` directory needed at runtime.

**Development — hot reload.** During iteration, run the API and the Vite dev server separately so you get HMR on React changes without rebuilding Go:

```bash
# Terminal 1 — API on :8080
go run ./cmd/oracle serve --port 8080

# Terminal 2 — Vite on :5173 with /api/* proxied to :8080
cd web
npm run dev
# → open http://localhost:5173
```

> **Note:** `go:embed` requires `internal/api/dist/` to exist at compile time. The repo commits a `.gitkeep` so `go build` always works — if you haven't run `npm run build`, visiting the root route shows a "Dashboard bundle not found" page with instructions. The JSON API at `/api/*` works either way.

### 9. (Optional) Enable the LLM-powered executive summary

The `report` command will automatically call an LLM provider if any supported API key is present in the environment. No flags required — just export a key and run `report` again. If no key is configured, the PDF is still generated without the narrative section.

| Provider | Env variable        | Default model        |
|----------|---------------------|----------------------|
| Gemini   | `GEMINI_API_KEY`    | `gemini-2.5-flash`   |
| Claude   | `ANTHROPIC_API_KEY` | `claude-haiku-4-5`   |
| OpenAI   | `OPENAI_API_KEY`    | `gpt-4o-mini`        |

```bash
# Pick one
export GEMINI_API_KEY=...
export ANTHROPIC_API_KEY=...
export OPENAI_API_KEY=...

# Force a specific provider when multiple keys are present
export LLM_PROVIDER=claude   # gemini | claude | openai

go run cmd/oracle/main.go report --output cloudoracle-report.pdf
```

Auto-detection order when `LLM_PROVIDER` is unset: **Gemini → Claude → OpenAI**. The first key found wins. LLM failures (missing key, network error, API error) are logged but never block PDF generation — the report falls back to the deterministic summary.

## Sample Output

![CloudOracle analyze output](../example.png)

```
CloudOracle found 10 problems with potential monthly savings of $680.00

  1. [HIGH] EC2 i-3592027508 (c5.xlarge) has average CPU usage of 2.8%. Active for 325 days.
     Consider shutting down or terminating this instance.
     Monthly Cost: $125.00 | Potential Monthly Savings: $125.00

  2. [HIGH] EBS vol-fcebf509 (gp3-1000GB) is not attached to any instance. Orphaned for 60 days.
     Create a backup snapshot and delete the volume.
     Monthly Cost: $100.00 | Potential Monthly Savings: $100.00

  3. [MEDIUM] RDS db-f7fdfc2b (db.t3.micro) has average CPU usage of 7.1%. Likely oversized.
     Consider downgrading to the next smaller RDS instance tier.
     Monthly Cost: $15.00 | Potential Monthly Savings: $7.50
  ...

Summary per service
  ec2  -> 5 problems, save: $460.00/month
  ebs  -> 3 problems, save: $205.00/month
  rds  -> 2 problems, save: $15.00/month
```

---

For cloud provider setup (AWS, GCP, Azure), see [cloud-providers.md](cloud-providers.md). For env var reference, see [configuration.md](configuration.md).
