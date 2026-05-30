# insights-agent

LangGraph-based FinOps insights agent for CloudOracle. Ask in natural language
("how much did I spend on AWS in April 2026?") and the agent picks the right
`/api/v1` calls against the CloudOracle Go server, then answers in the same
language with the relevant caveats.

Built on `create_react_agent` from `langgraph.prebuilt`: single-turn agent (no
conversational memory), Gemini as the model, five tools wired against the Go
`/api/v1` endpoints — two cost endpoints (milestone 8.1) plus savings
recommendations, cost trends, and a resource-inventory endpoint (milestone 8.2).
Future milestones replace the ReAct loop with a custom supervisor (8.4) and add
RAG over FinOps docs (8.3).

## What it talks to

```
┌────────┐  "How much did I spend on AWS?"  ┌─────────────┐
│  User  │ ───────────────────────────────▶ │ insights-   │
└────────┘                                  │ agent (CLI) │
                                            └──────┬──────┘
                                                   │ LangGraph (Gemini)
                                                   │ tool call →
                                                   ▼
                                            ┌─────────────┐  X-API-Key
                                            │  Go server  │ ──────────▶ Postgres
                                            │ /api/v1/... │            (cost_snapshots)
                                            └─────────────┘
```

Every tool returns a `data_source` field so the agent surfaces the right
caveat:

- The **cost** and **trends** tools return `"snapshots_approximation"` —
  figures come from periodic CloudOracle snapshots, **not** a real billing API
  (the real billing integration lands in milestone 8.7).
- The **recommendations** tool returns `"heuristic_rules"` — savings are
  estimated upper bounds from a rule-based analyzer over the current resource
  inventory, to be validated against real usage before acting.
- The **inventory** tool returns `"live_inventory"` — counts and cost come
  from the latest resource scan; `monthly_cost_usd` is the sum of per-resource
  projected monthly rates, not billed spend.

The agent surfaces these caveats when accuracy materially affects the answer.

### Tools

| Tool | Answers | Backing endpoint |
| ---- | ------- | ---------------- |
| `cloudoracle_cost_summary`    | "how much did I spend?" (totals per provider) | `GET /api/v1/cost-summary` |
| `cloudoracle_cost_by_service` | "what drove AWS spend?" (per-service breakdown) | `GET /api/v1/cost-by-service` |
| `cloudoracle_recommendations` | "where can I save money?" (savings opportunities) | `GET /api/v1/recommendations` |
| `cloudoracle_cost_trends`     | "is my spend growing?" (per-day series + change) | `GET /api/v1/cost-trends` |
| `cloudoracle_inventory`       | "what do I have?" (counts + cost by provider/service) | `GET /api/v1/inventory` |

## Setup in under 10 minutes

### 1 — Prerequisites

- Python 3.12 (the project pins `>=3.12,<3.13`)
- [`uv`](https://docs.astral.sh/uv/) installed and on `PATH`
- A running CloudOracle Go server with `CLOUDORACLE_API_KEY` configured
- A Gemini API key (the [free tier](https://aistudio.google.com/app/apikey) is enough)

### 2 — Install dependencies

```bash
cd insights-agent
uv sync --extra dev
```

`uv sync` reads `pyproject.toml` + `uv.lock` and creates `.venv/` with both
runtime and dev dependencies (pytest, ruff, mypy). Re-running it is the
canonical way to pick up upstream changes.

### 3 — Configure environment

```bash
cp .env.example .env
# then edit .env and fill GEMINI_API_KEY and CLOUDORACLE_API_KEY
```

Required env vars (loaded by `pydantic-settings`, fail-fast at startup):

| Variable                 | Required | Default                    | Notes |
| ------------------------ | -------- | -------------------------- | ----- |
| `GEMINI_API_KEY`         | yes      | —                          | https://aistudio.google.com/app/apikey |
| `CLOUDORACLE_API_URL`    | yes      | `http://localhost:8080`    | Base URL of the Go server |
| `CLOUDORACLE_API_KEY`    | yes      | —                          | Must match the Go server's `CLOUDORACLE_API_KEY` |
| `GEMINI_MODEL`           | no       | `gemini-2.5-flash`         | Free tier covers this model |
| `LOG_LEVEL`              | no       | `INFO`                     | `DEBUG`, `INFO`, `WARNING`, `ERROR`, `CRITICAL` |
| `LOG_FORMAT`             | no       | `text`                     | `text` or `json` — same shapes as the Go side |
| `HTTP_TIMEOUT_SECONDS`   | no       | `10`                       | Per-request timeout against the Go server |

### 4 — Run the CLI

```bash
uv run python -m insights_agent.main "How much did I spend on AWS in April 2026?"
```

Or via the console script entry point:

```bash
uv run insights-agent "Break down GCP spend for May 2026"
```

Useful flags:

| Flag         | Effect |
| ------------ | ------ |
| `--verbose`  | Streams the tool calls the model made (name + args) to stderr |
| `--json`     | Prints `{"answer": "...", "tool_calls": [...]}` on stdout instead of plain text |

Exit codes:

| Code | Meaning |
| ---- | ------- |
| 0    | Success |
| 1    | Unexpected runtime failure |
| 2    | Configuration problem (missing env var, malformed URL, etc.) |
| 130  | User cancelled with Ctrl-C |

## Smoke test (end-to-end with real Gemini + Go server)

This exercises the full chain: Python CLI → LangGraph (Gemini) → HTTP tool
call → Go `/api/v1` → Postgres. Run it once after setup to confirm
everything is wired correctly. Skip it during day-to-day development —
the unit tests already cover the pipeline with a mocked model.

1. **Start CloudOracle Go with v1 auth.** From the repo root:

   ```bash
   export CLOUDORACLE_API_KEY="local-dev-secret"     # any non-empty string
   docker compose up --build                          # or: oracle serve
   ```

   Confirm the server is up:

   ```bash
   curl -sS -H "X-API-Key: $CLOUDORACLE_API_KEY" \
        "http://localhost:8080/api/v1/cost-summary?start=2026-04-01&end=2026-04-30"
   ```

   You should get a JSON envelope with `data_source: "snapshots_approximation"`.
   If you get `unauthorized`, the key in `.env` doesn't match the server's env.
   If you get an empty `providers` map, seed the DB first:
   `docker compose exec app /app/cloudoracle seed --count 120`.

2. **Run the agent** from `insights-agent/`:

   ```bash
   uv run insights-agent --verbose "How much did I spend on AWS in April 2026?"
   ```

3. **Expected output** (shape, not exact wording — Gemini paraphrases):

   - On stdout, a paragraph in Spanish with a dollar figure that matches the
     `grand_total_usd` for AWS in that period.
   - The answer mentions that the numbers are an approximation from snapshots
     (because the tool surfaced `data_source: "snapshots_approximation"`).
   - On stderr, a `Tool calls made:` block listing at least
     `cloudoracle_cost_summary` with the inferred start/end dates.

4. **If it fails:**

   - `Configuration error: ...` (exit 2) → missing or malformed `.env` value.
   - `CloudOracle API 401 ...` (exit 1) → `CLOUDORACLE_API_KEY` mismatch.
   - `CloudOracle transport error: ...` (exit 1) → Go server not reachable
     at `CLOUDORACLE_API_URL`.
   - Gemini quota errors → wait a minute or use a different `GEMINI_API_KEY`;
     the free tier has minute- and day-level limits.

## Development

```bash
uv run pytest                  # unit tests + coverage (>80% threshold)
uv run ruff check .            # lint
uv run mypy src/               # strict type-check (passes on 11 files)
```

The tests never contact Gemini or a live Go server. `tests/test_graph.py`
ships a `ScriptedChatModel` (a `BaseChatModel` subclass) that replays
hand-written `AIMessage` sequences, and `pytest-httpx` mocks the Go
endpoints. The two together let `create_react_agent` run its full
ReAct loop deterministically — including the tool-error branch.

### Architecture pointers

| Concern             | Where to look | Why |
| ------------------- | ------------- | --- |
| Vendor-agnostic LLM | `src/insights_agent/llm/base.py` + `gemini.py` | ABC + one implementation. Add `AnthropicProvider` / `OpenAIProvider` later by implementing `LLMProvider`; no graph changes required. |
| Tools               | `src/insights_agent/tools/cloudoracle.py` | `CloudOracleClient` owns the HTTP + auth + request-ID conventions; `build_tools(client)` wraps the five methods as `StructuredTool`s with rich docstrings so the LLM picks the right one. Errors flow as `ToolException` so the model sees them as observations and can recover instead of aborting the run. |
| Graph               | `src/insights_agent/graph/basic.py` | `create_react_agent` from `langgraph.prebuilt` with a short system prompt. Milestone 8.4 replaces this with a hand-rolled supervisor. |
| CLI                 | `src/insights_agent/main.py` | argparse, three flags, four exit codes, single async run. No conversational memory (each call is independent). |
| Settings            | `src/insights_agent/config.py` | `pydantic-settings.BaseSettings` — fail-fast `ValidationError` at startup if any required env var is missing. |
| Logging             | `src/insights_agent/logging.py` | `structlog` matching the Go side's `slog` output (text or JSON to stderr) so a tail of both streams reads coherently. |

### What is **not** here yet

- pgvector / RAG over FinOps docs (8.3)
- Custom supervisor / multi-agent (8.4)
- Cost caps, semantic answer validation, deterministic fallback (8.5)
- HTTP API surface for the agent — CLI only until 8.5
- Other LLM providers (Anthropic, OpenAI)
- Streaming responses
- Conversational memory across queries
- Real billing / Cost Explorer integration (8.7)
