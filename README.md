# CloudOracle

A CLI tool built in Go that analyzes cloud infrastructure resources and detects cost optimization opportunities. It simulates a real-world FinOps workflow: ingesting cloud resource data, storing it in PostgreSQL, and running deterministic rules to surface waste such as idle EC2 instances, orphaned EBS volumes, oversized RDS databases, and over-provisioned Lambda functions.

## Why this project?

Cloud waste is a real problem. Companies routinely overspend 20-30% on cloud infrastructure because nobody is watching the bill. CloudOracle demonstrates how to build a system that catches these issues automatically, using the same patterns that tools like AWS Trusted Advisor or Datadog Cloud Cost Management use internally.

## Features

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

## Architecture

```
cmd/oracle/main.go          # CLI entry point (seed, list, analyze, report)
internal/
  shared/
    resource.go             # Resource domain model
    finding.go              # Finding + Severity types
  generator/
    generator.go            # Synthetic data generation for EC2, RDS, EBS, Lambda
  analyzer/
    analyzer.go             # Rule engine: runs all rules, sorts by savings
    rules.go                # Detection rules (pure functions)
  report/
    pdf.go                  # PDF report generator (executive summary + findings table)
  llm/
    provider.go             # Provider interface + env-based factory (Gemini / Claude / OpenAI)
    prompt.go               # Shared prompt builder (findings -> structured analysis)
    gemini.go               # Google Gemini client (gemini-2.5-flash)
    claude.go               # Anthropic Claude client (claude-haiku-4-5)
    openai.go               # OpenAI client (gpt-4o-mini)
  db/
    db.go                   # PostgreSQL connection pool (pgx)
    insert.go               # Transactional insert + query logic
migrations/
  001_create_resources.sql  # Schema with indexes on service and account_id
docker-compose.yml          # PostgreSQL 16 setup
```

## Tech Stack

| Component    | Technology                |
|-------------|---------------------------|
| Language    | Go 1.25                   |
| Database    | PostgreSQL 16 (Alpine)    |
| DB Driver   | pgx v5 (connection pool)  |
| PDF         | go-pdf/fpdf               |
| LLM         | Gemini / Claude / OpenAI  |
| Containers  | Docker Compose            |

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose

### 1. Start the database

```bash
docker compose up -d
```

### 2. Run the migration

```bash
docker compose exec -T postgres psql -U oracle -d cloudoracle -f /migrations/001_create_resources.sql
```

### 3. Seed sample data

```bash
go run cmd/oracle/main.go seed --account acc-001 --count 100
```

### 4. List all resources

```bash
go run cmd/oracle/main.go list
```

### 5. Run the cost analyzer

```bash
go run cmd/oracle/main.go analyze
```

### 6. Generate a PDF report

```bash
go run cmd/oracle/main.go report --output cloudoracle-report.pdf
```

This generates a professional PDF with:
- Executive summary (total findings, monthly/annual savings projections)
- Severity breakdown (HIGH / MEDIUM / LOW)
- Color-coded findings table with cost and savings per resource
- Recommended actions for each finding
- **AI-generated narrative** (when an LLM provider is configured) — 3-4 paragraph executive summary written for a CTO/CFO audience, focused on financial impact, highest-priority problems, and recommended next steps

![CloudOracle PDF report example](examplepdf.png)

### 7. (Optional) Enable the LLM-powered executive summary

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

### Sample Output

![CloudOracle analyze output](example.png)

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

## Environment Variables

| Variable      | Default       | Description           |
|--------------|---------------|-----------------------|
| `DB_HOST`    | `localhost`   | PostgreSQL host       |
| `DB_PORT`    | `5432`        | PostgreSQL port       |
| `DB_USER`    | `oracle`      | Database user         |
| `DB_PASSWORD`| `oracle_dev`  | Database password     |
| `DB_NAME`    | `cloudoracle` | Database name         |
| `LLM_PROVIDER`     | _(auto)_ | Force a specific LLM provider: `gemini`, `claude`, or `openai`. If unset, auto-detects based on which API key is present. |
| `GEMINI_API_KEY`   | _(unset)_ | API key for Google Gemini (`gemini-2.5-flash`)     |
| `ANTHROPIC_API_KEY`| _(unset)_ | API key for Anthropic Claude (`claude-haiku-4-5`)  |
| `OPENAI_API_KEY`   | _(unset)_ | API key for OpenAI (`gpt-4o-mini`)                 |

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

## Lessons Learned

Building this project surfaced a subtle but important bug that would have gone unnoticed without testing against real(istic) data:

**The case-sensitivity trap:** The EC2 idle detection rule was comparing `r.Service != "EC2"` (uppercase), but the data generator and database stored services as `"ec2"` (lowercase). The rule silently passed over every EC2 instance without flagging a single one. The RDS, EBS, and Lambda rules all used lowercase correctly, making this inconsistency easy to miss during code review. It was only caught when analyzing output and noticing zero EC2 findings despite seeding idle instances.

**Takeaway:** String comparison bugs are among the most common sources of silent failures in cloud tooling. Production systems use canonical enumerations or case-insensitive matching for exactly this reason. Finding this during development -- not after deployment -- is the difference between a tool that works and one that looks like it works.

## Roadmap

- [x] LLM-powered analysis: executive summaries generated by Gemini / Claude / OpenAI
- [x] PDF report generation with executive summary and severity-coded tables
- [ ] Real AWS integration via SDK (replace synthetic data with live resource inventory)
- [ ] Multi-cloud support (GCP, Azure)
- [ ] Cost trend tracking over time
- [ ] Export findings to JSON/CSV
- [ ] Slack/email alerting for high-severity findings
- [ ] Web dashboard with cost visualizations

## License

MIT
