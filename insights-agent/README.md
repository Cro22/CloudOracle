# insights-agent

LangGraph-based FinOps insights agent for CloudOracle. Ask in natural language
("how much did I spend on AWS in April 2026?") and the agent picks the right
`/api/v1` calls against the CloudOracle Go server, then answers in the same
language with the relevant caveats.

Single-turn agent (no conversational memory), Gemini as the model. Five tools
call the Go `/api/v1` endpoints — two cost endpoints (milestone 8.1) plus
savings recommendations, cost trends, and a resource-inventory endpoint
(milestone 8.2). A sixth tool, `finops_knowledge_search`, does RAG over a
curated FinOps corpus stored in pgvector (milestone 8.3) for conceptual /
policy / how-to questions.

The default orchestration is a **hand-rolled supervisor** (milestone 8.4): a
`StateGraph` where a supervisor routes between three specialist workers and a
synthesizer composes the final answer — replacing `create_react_agent`. See
[Multi-agent supervisor](#multi-agent-supervisor).

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
| `finops_knowledge_search`     | "what is rightsizing?", "should I buy RIs?" (concepts/policy) | pgvector RAG over the FinOps corpus |

`finops_knowledge_search` is only registered when `DATABASE_URL` points at a
pgvector-enabled Postgres (see [Knowledge base (RAG)](#knowledge-base-rag)).
Without it the five HTTP tools still work — the agent just can't answer
conceptual questions from the corpus.

## Multi-agent supervisor

`graph/supervisor.py` is the default orchestration — a hand-rolled
`StateGraph`, not `create_react_agent`:

```
START → supervisor → {worker} → supervisor → … → synthesize → END
```

- **supervisor** routes by *tool call*: it's bound with one routing tool per
  specialist plus `finish`, and the tool it calls names the next hop. (Routing
  via tool calls — rather than `with_structured_output` — keeps the node
  driveable by the scripted fake model the tests use.)
- **workers** are three specialists, each a hand-rolled ReAct loop
  (`_run_react`, the actual `create_react_agent` replacement) over a tool
  subset:
  - `cost_analyst` → cost-summary / cost-by-service / cost-trends / inventory
  - `savings_advisor` → recommendations + knowledge search
  - `concept_expert` → knowledge search
  A worker contributes one summarizing message back; its own tool churn stays
  local so the supervisor and synthesizer see a clean transcript.
- **synthesize** composes the final answer from the specialists' findings,
  in the user's language, with the data-source caveats and source citations.

A hop cap (`MAX_HOPS`) bounds the supervisor loop so a model that never emits
`finish` still terminates. The simpler single-agent graph (`graph/basic.py`,
`create_react_agent`) is retained for tests and comparison.

## Guardrails

Production guardrails (milestone 8.5) wrap every run via
`guardrails/runner.py:run_guarded`:

- **Cost / usage caps** (`graph.supervisor.RunLimits`, from `MAX_*` env vars):
  bound total tool calls, supervisor hops, and per-worker iterations. When a cap
  is hit, the supervisor stops dispatching and synthesizes from what it has — a
  confused or injected loop can't run up unbounded LLM/tool cost.
- **Layered answer validation** (`guardrails/validation.py`):
  1. *Deterministic grounding* — every monetary figure in the answer must match
     (within tolerance) a number in the tool observations. An unmatched figure
     is almost certainly fabricated → hard fail, no LLM needed.
  2. *LLM judge* — when the deterministic layer passes but the answer makes
     numeric claims (so there's something to get subtly wrong), an optional
     judge model gives a second opinion grounded in the observations.
- **Deterministic fallback** (`guardrails/fallback.py`): if the run throws
  (quota, timeout) or validation rejects the answer, the user gets an honest,
  no-LLM response that surfaces the raw tool data (or says nothing was
  retrieved) instead of a fabricated narrative or a raw traceback.

Toggle validation with `ENABLE_ANSWER_VALIDATION` / `ENABLE_LLM_JUDGE`. The
`--json` CLI output includes `fallback_used` and the `validation` verdict.

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
| `DATABASE_URL`           | no       | —                          | pgvector URL; enables `finops_knowledge_search`. Unset = RAG off |
| `EMBEDDINGS_MODEL`       | no       | `models/text-embedding-004`| Gemini embeddings model (free tier) |
| `KNOWLEDGE_COLLECTION`   | no       | `finops_knowledge`         | pgvector collection name |
| `RAG_TOP_K`              | no       | `4`                        | Chunks retrieved per knowledge query (1–20) |
| `MAX_HOPS`               | no       | `6`                        | Supervisor decisions before forced synthesis |
| `MAX_TOOL_CALLS`         | no       | `8`                        | Total tool calls per run (cost cap) |
| `MAX_WORKER_ITERS`       | no       | `6`                        | ReAct iterations within one specialist |
| `ENABLE_ANSWER_VALIDATION` | no     | `true`                     | Run the layered answer validation |
| `ENABLE_LLM_JUDGE`       | no       | `true`                     | Add the LLM-judge layer on numeric answers |

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

## Knowledge base (RAG)

The `finops_knowledge_search` tool retrieves from a curated FinOps corpus
(`src/insights_agent/knowledge/*.md`) embedded into pgvector. It answers
conceptual / policy / how-to questions ("what is rightsizing?", "should I buy
reserved instances?", "how accurate are these numbers?") that the HTTP tools
can't — they fetch numbers, this fetches guidance with source citations.

RAG is **optional**: with `DATABASE_URL` unset the agent runs with just the five
HTTP tools. To enable it:

1. **Use a pgvector-enabled Postgres.** The bundled `docker compose` stack uses
   the `pgvector/pgvector:pg16` image (a drop-in for stock Postgres 16), so
   `docker compose up` already gives you one.

2. **Point the agent at it** in `.env`:

   ```bash
   DATABASE_URL=postgresql+psycopg://oracle:oracle_dev@localhost:5432/cloudoracle
   ```

3. **Ingest the corpus** (creates the `vector` extension + collection on first
   run, embeds each chunk via Gemini, upserts into pgvector):

   ```bash
   uv run insights-agent-ingest            # add / refresh the corpus
   uv run insights-agent-ingest --recreate # drop the collection first
   ```

4. **Ask a conceptual question:**

   ```bash
   uv run insights-agent --verbose "Should I rightsize before buying reserved instances?"
   ```

   With RAG on, `--verbose` shows a `finops_knowledge_search` call and the
   answer cites the corpus. Re-run the ingester whenever the markdown changes;
   editing the corpus does not require re-embedding unchanged files only if you
   `--recreate`, otherwise new chunks are appended.

The architecture deliberately keeps RAG in Python (where LangChain lives): the
Go server stays a clean data API, and the agent owns embeddings + retrieval
against the shared Postgres.

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
uv run mypy src/               # strict type-check
uv run insights-agent-ingest   # (needs DATABASE_URL) embed the FinOps corpus
```

The tests never contact Gemini, a live Go server, or Postgres. A
`ScriptedChatModel` (a `BaseChatModel` subclass) replays hand-written
`AIMessage` sequences and `pytest-httpx` mocks the Go endpoints, so both graphs
run deterministically: `tests/test_graph.py` drives the simple
`create_react_agent` graph, and `tests/test_supervisor.py` drives the supervisor
end-to-end (route → worker tool call → finish → synthesize, plus the hop cap).
The RAG layer is tested offline too: `tests/test_corpus.py` checks chunking, and
`tests/test_knowledge_tool.py` drives the real retrieval + citation path through
an `InMemoryVectorStore` + `DeterministicFakeEmbedding`, so no pgvector or
embeddings API is needed.

### Architecture pointers

| Concern             | Where to look | Why |
| ------------------- | ------------- | --- |
| Vendor-agnostic LLM | `src/insights_agent/llm/base.py` + `gemini.py` | ABC + one implementation. Add `AnthropicProvider` / `OpenAIProvider` later by implementing `LLMProvider`; no graph changes required. |
| Tools               | `src/insights_agent/tools/cloudoracle.py` | `CloudOracleClient` owns the HTTP + auth + request-ID conventions; `build_tools(client)` wraps the five methods as `StructuredTool`s with rich docstrings so the LLM picks the right one. Errors flow as `ToolException` so the model sees them as observations and can recover instead of aborting the run. |
| RAG                 | `src/insights_agent/rag/` + `tools/knowledge.py` | `corpus.py` loads + chunks the packaged markdown (offline-testable); `embeddings.py` mirrors the LLM-provider ABC for Gemini embeddings; `store.py` wraps pgvector; `ingest.py` is the `insights-agent-ingest` CLI; `knowledge.py` exposes `finops_knowledge_search`. Only wired in when `DATABASE_URL` is set. |
| Guardrails          | `src/insights_agent/guardrails/` | `RunLimits` cost caps (in `graph/supervisor.py`); `validation.py` layered grounding + LLM judge; `fallback.py` no-LLM honest answer; `runner.py:run_guarded` ties run → validate → fallback. The single entry point the CLI and HTTP surface share. |
| Graph (default)     | `src/insights_agent/graph/supervisor.py` | Hand-rolled `StateGraph`: tool-call-routing supervisor + three specialist workers (each a `_run_react` loop) + synthesizer, with a hop cap. The production path `main.py` wires. |
| Graph (simple)      | `src/insights_agent/graph/basic.py` | `create_react_agent` single-agent graph. Retained for tests/comparison; `AgentResult` + `_stringify_content` live here and the supervisor reuses them. |
| CLI                 | `src/insights_agent/main.py` | argparse, three flags, four exit codes, single async run. No conversational memory (each call is independent). |
| Settings            | `src/insights_agent/config.py` | `pydantic-settings.BaseSettings` — fail-fast `ValidationError` at startup if any required env var is missing. |
| Logging             | `src/insights_agent/logging.py` | `structlog` matching the Go side's `slog` output (text or JSON to stderr) so a tail of both streams reads coherently. |

### What is **not** here yet

- Cost caps, semantic answer validation, deterministic fallback (8.5)
- HTTP API surface for the agent — CLI only until 8.5
- Other LLM providers (Anthropic, OpenAI)
- Streaming responses
- Conversational memory across queries
- Real billing / Cost Explorer integration (8.7)
