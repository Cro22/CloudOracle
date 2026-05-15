# Configuration

Reference for every environment variable CloudOracle reads. All vars are loaded once at startup by `internal/config.Load()` and injected into the cloud, LLM, and DB layers — no component reaches for `os.Getenv` on its own.

| Variable      | Default       | Description           |
|--------------|---------------|-----------------------|
| `CLOUDORACLE_PROVIDER` | `synthetic` | Cloud provider: `aws`, `gcp`, `azure`, or `synthetic` |
| `AWS_PROFILE` | `cloudoracle` | AWS shared-config profile to use |
| `AWS_REGION` | `us-east-2` | AWS region to scan |
| `GOOGLE_CLOUD_PROJECT` | _(unset)_ | GCP project ID (required when provider is `gcp`) |
| `AZURE_SUBSCRIPTION_ID` | _(unset)_ | Azure subscription ID (required when provider is `azure`) |
| `SYNTHETIC_COUNT` | `100` | Default number of synthetic resources to generate |
| `SYNTHETIC_ACCOUNT` | `synthetic-account` | Default account ID for synthetic data |
| `CLOUD_SERVICE_TIMEOUT` | `30s` | Per-service timeout for each cloud API call (Go duration string) |
| `DB_HOST`    | `localhost`   | PostgreSQL host       |
| `DB_PORT`    | `5432`        | PostgreSQL port       |
| `DB_USER`    | `oracle`      | Database user         |
| `DB_PASSWORD`| `oracle_dev`  | Database password     |
| `DB_NAME`    | `cloudoracle` | Database name         |
| `LLM_PROVIDER`     | _(auto)_ | Force a specific LLM provider: `gemini`, `claude`, or `openai`. If unset, auto-detects based on which API key is present. |
| `LLM_TIMEOUT`      | `30s` | HTTP timeout for LLM API calls (Go duration string) |
| `LLM_MAX_RETRIES`  | `3` | Number of retries on transient LLM failures (429, 5xx, network errors). Set to `0` to disable. |
| `LLM_BASE_DELAY`   | `500ms` | Initial backoff between retries; doubles on each attempt with full jitter |
| `LLM_MAX_DELAY`    | `30s` | Cap for the per-retry wait (also caps `Retry-After` headers) |
| `GEMINI_API_KEY`   | _(unset)_ | API key for Google Gemini (`gemini-2.5-flash`)     |
| `ANTHROPIC_API_KEY`| _(unset)_ | API key for Anthropic Claude (`claude-haiku-4-5`)  |
| `OPENAI_API_KEY`   | _(unset)_ | API key for OpenAI (`gpt-4o-mini`)                 |
| `LOG_LEVEL`        | `info`    | Log level: `debug`, `info`, `warn`, or `error`     |
| `LOG_FORMAT`       | `text`    | Log format: `text` (human-readable) or `json` (structured)  |

---

For the design of the LLM provider layer and the analyzer rule engine, see [architecture.md](architecture.md). For per-cloud setup details (profiles, credentials, IAM scopes), see [cloud-providers.md](cloud-providers.md).
