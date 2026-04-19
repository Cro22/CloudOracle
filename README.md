# CloudOracle

![Tests](https://img.shields.io/badge/tests-91%20passing-brightgreen)
![Go Version](https://img.shields.io/badge/go-1.25-blue)
![License](https://img.shields.io/badge/license-MIT-green)

A CLI tool built in Go that analyzes cloud infrastructure resources and detects cost optimization opportunities. It simulates a real-world FinOps workflow: ingesting cloud resource data, storing it in PostgreSQL, and running deterministic rules to surface waste such as idle EC2 instances, orphaned EBS volumes, oversized RDS databases, and over-provisioned Lambda functions.

## Why this project?

Cloud waste is a real problem. Companies routinely overspend 20-30% on cloud infrastructure because nobody is watching the bill. CloudOracle demonstrates how to build a system that catches these issues automatically, using the same patterns that tools like AWS Trusted Advisor or Datadog Cloud Cost Management use internally.

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

## Architecture

```
cmd/oracle/main.go          # CLI entry point (seed, list, analyze, report)
internal/
  shared/
    resource.go             # Resource domain model
    finding.go              # Finding + Severity types
  cloud/
    provider.go             # CloudProvider interface (Strategy pattern)
    factory.go              # Provider factory: env var -> concrete provider
    synthetic_provider.go   # Synthetic data provider (dev/demo)
    aws_provider.go         # Real AWS provider (EC2, RDS, EBS, Lambda via SDK v2)
    gcp_provider.go         # Real GCP provider (Compute, Cloud SQL, Persistent Disks, Functions)
    azure_provider.go       # Real Azure provider (VMs, SQL, Managed Disks, Function Apps)
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

The cloud provider layer uses the **Strategy pattern**: `CloudProvider` is the interface, and `SyntheticProvider`, `AWSProvider`, `GCPProvider`, and `AzureProvider` are the concrete strategies. `factory.go` selects the strategy at runtime based on `CLOUDORACLE_PROVIDER`. This lets `main.go` work with any provider without knowing which one is active.

## Tech Stack

| Component    | Technology                         |
|-------------|-------------------------------------|
| Language    | Go 1.25                             |
| Database    | PostgreSQL 16 (Alpine)              |
| DB Driver   | pgx v5 (connection pool)            |
| AWS SDK     | aws-sdk-go-v2 (EC2, RDS, Lambda, STS) |
| GCP SDK     | Google Cloud Go (Compute, SQL, Functions) |
| Azure SDK   | Azure SDK for Go (Compute, SQL, App Service) |
| PDF         | go-pdf/fpdf                         |
| LLM         | Gemini / Claude / OpenAI            |
| Testing     | `testing` + `httptest`              |
| Containers  | Docker Compose                      |

## Getting Started

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- (Optional) AWS CLI configured with a `cloudoracle` profile for real AWS integration (see [AWS Setup](#aws-setup) below)

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

## AWS Setup

To use the real AWS provider (`CLOUDORACLE_PROVIDER=aws`), you need an AWS profile named `cloudoracle` in `~/.aws/credentials`:

```ini
[cloudoracle]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
region = us-east-2
```

The IAM user needs read-only access to inventory the cloud resources. For development, we use `ReadOnlyAccess` + `AWSBillingReadOnlyAccess` managed policies. Production would scope permissions to least privilege:

```
ec2:DescribeInstances, ec2:DescribeVolumes
rds:DescribeDBInstances, rds:ListTagsForResource
lambda:ListFunctions, lambda:ListTags
ce:GetCostAndUsage
sts:GetCallerIdentity
```

The provider validates credentials at startup via `sts:GetCallerIdentity` — if the profile is misconfigured or credentials are expired, the error appears immediately instead of failing mid-scan.

## GCP Setup

To use the GCP provider (`CLOUDORACLE_PROVIDER=gcp`), you need:

1. A GCP project with the following APIs enabled: Compute Engine, Cloud SQL Admin, Cloud Functions
2. Application Default Credentials configured via one of:
   - `gcloud auth application-default login` (development)
   - `GOOGLE_APPLICATION_CREDENTIALS` env var pointing to a service account JSON (production)
3. `GOOGLE_CLOUD_PROJECT` env var set to your project ID

Required IAM roles (least privilege):
```
compute.instances.list, compute.disks.list
cloudsql.instances.list
cloudfunctions.functions.list
```

## Azure Setup

To use the Azure provider (`CLOUDORACLE_PROVIDER=azure`), you need:

1. `AZURE_SUBSCRIPTION_ID` env var set to your subscription ID
2. Credentials configured via one of:
   - Azure CLI: `az login` (development)
   - Environment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET` (production)
   - Managed Identity (when running in Azure)

The provider uses `DefaultAzureCredential` which tries all methods automatically.

Required RBAC role: `Reader` on the subscription. Production would scope to:
```
Microsoft.Compute/virtualMachines/read
Microsoft.Compute/disks/read
Microsoft.Sql/servers/read, Microsoft.Sql/servers/databases/read
Microsoft.Web/sites/read
```

## Environment Variables

| Variable      | Default       | Description           |
|--------------|---------------|-----------------------|
| `CLOUDORACLE_PROVIDER` | `synthetic` | Cloud provider: `aws`, `gcp`, `azure`, or `synthetic` |
| `GOOGLE_CLOUD_PROJECT` | _(unset)_ | GCP project ID (required when provider is `gcp`) |
| `AZURE_SUBSCRIPTION_ID` | _(unset)_ | Azure subscription ID (required when provider is `azure`) |
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

## Testing

The project is covered by 91 unit tests across every package — analyzer, generator, LLM providers, PDF report, and database config:

- **Per-rule tests**: each detection rule (`ec2-idle`, `rds-oversized`, `ebs-orphan`, `lambda-over-provisioned`) has happy-path, negative, and boundary tests.
- **Boundary testing**: CPU thresholds, age cutoffs, memory limits, and invocation counts are explicitly tested at their exact values to catch off-by-one errors.
- **Aggregator tests**: `Analyze` is tested for empty input, mixed input, false-positive prevention, and correct savings-descending ordering.
- **LLM provider tests**: all three providers (Gemini, Claude, OpenAI) are tested against mock HTTP servers using `httptest`, covering success responses, API errors, empty payloads, error fields, and context cancellation.
- **Provider factory tests**: auto-detection order (Gemini > Claude > OpenAI), explicit selection, missing keys, and unknown providers.
- **Prompt builder tests**: total calculations, severity breakdowns, service rollups, top-5 limiting, and empty input handling.
- **PDF generation tests**: file creation, AI summary inclusion/exclusion, empty findings, 100-finding page-break stress test, invalid paths, and all severity color codes.
- **Generator tests**: correct count, valid services/regions/types, non-negative costs, timestamp ordering, and service distribution.

```bash
go test ./internal/... -v
```

All rules are pure functions (`Resource -> *Finding`), which makes them trivially testable without mocks, fixtures, or test databases. The code was designed to be testable from the start — not tested after the fact.

## Architecture Decisions

### Why interfaces over inheritance for LLM providers
The `Provider` interface in `internal/llm` is intentionally minimal — just `GenerateSummary` and `Name`. Each provider (Gemini, Claude, OpenAI) is a fully independent implementation. Adding a fourth provider requires zero changes to existing code: write a new file, register it in `provider.go`, done. This is Go's structural typing at its best — no inheritance, no abstract base classes, no framework lock-in.

### Why net/http directly instead of vendor SDKs
All three LLM providers are implemented with the standard library `net/http` package, no vendor SDKs. This keeps the dependency tree small (the entire project has fewer than 10 direct dependencies), makes the code portable, and forces explicit handling of errors, timeouts, and retries — all of which are usually hidden behind SDK abstractions.

### Why deterministic rules first, LLMs second
The analyzer detects 80% of cloud waste using simple pure functions, before any LLM is involved. This is by design: deterministic rules are predictable, testable, free, and instant. LLMs are reserved for what they're actually good at — translating structured data into executive prose. Inverting this order (using LLMs to detect waste) would be slower, more expensive, and less reliable.

### Why graceful degradation when no LLM is configured
If no API key is set, the report generates without the AI summary section instead of failing. This means anyone can clone the repo and run it immediately, and the same binary works in restricted environments where outbound API calls aren't allowed.

### Why synthetic data instead of real AWS integration in v1
Building the rule engine and report generator against a synthetic data generator allowed iteration without paying for AWS resources, without rate limits, and without coupling the early development to credentials. Real AWS integration is the next milestone, but the abstraction was earned by first solving the harder problem: detecting waste from any data source.

## Lessons Learned

Building this project surfaced a subtle but important bug that would have gone unnoticed without testing against real(istic) data:

**The case-sensitivity trap:** The EC2 idle detection rule was comparing `r.Service != "EC2"` (uppercase), but the data generator and database stored services as `"ec2"` (lowercase). The rule silently passed over every EC2 instance without flagging a single one. The RDS, EBS, and Lambda rules all used lowercase correctly, making this inconsistency easy to miss during code review. It was only caught when analyzing output and noticing zero EC2 findings despite seeding idle instances.

**Takeaway:** String comparison bugs are among the most common sources of silent failures in cloud tooling. Production systems use canonical enumerations or case-insensitive matching for exactly this reason. Finding this during development -- not after deployment -- is the difference between a tool that works and one that looks like it works.

**The Strategy pattern for cloud providers:** The `CloudProvider` interface started as a formality — there was only the synthetic provider. But when adding real AWS support, the pattern paid for itself: `AWSProvider` and `SyntheticProvider` both satisfy the same interface, `factory.go` picks the right one from an env var, and `main.go` never knows which is active. The key insight was keeping the mapping logic (SDK types -> domain types) as pure functions separated from the API calls. This made it possible to unit test the field mapping with struct literals instead of mocking the entire AWS SDK — a pattern worth repeating for GCP and Azure providers.

## Roadmap

- [x] LLM-powered analysis: executive summaries generated by Gemini / Claude / OpenAI
- [x] PDF report generation with executive summary and severity-coded tables
- [x] Test suite: 91 unit tests across analyzer, generator, LLM providers, PDF, and config
- [x] Real AWS integration via SDK (EC2, RDS, EBS, Lambda with STS validation and graceful degradation)
- [x] Multi-cloud support (GCP, Azure) with Compute, SQL, Disks, and Functions for each provider
- [ ] Cost trend tracking over time
- [ ] Export findings to JSON/CSV
- [ ] Web dashboard with cost visualizations

## License

MIT
