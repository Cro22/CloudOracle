"""Settings loaded from environment with fail-fast validation.

We load every setting once at startup so individual modules don't reach for
`os.environ` directly — same pattern the Go side uses (`internal/config.Load`).
Required values trigger a `pydantic.ValidationError` at instantiation; the CLI
entry point surfaces a readable message and exits non-zero.
"""

from __future__ import annotations

from pydantic import Field, HttpUrl, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from insights_agent.graph.supervisor import RunLimits


class Settings(BaseSettings):
    """Process-wide configuration.

    All required fields are checked at construction time. Defaults match the
    Go server's defaults (`CLOUDORACLE_API_PORT=8080`, `gemini-2.5-flash`) so
    a local dev setup needs to fill only the two API keys.
    """

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    gemini_api_key: str = Field(min_length=1)
    cloudoracle_api_url: HttpUrl
    cloudoracle_api_key: str = Field(min_length=1)

    gemini_model: str = "gemini-2.5-flash"
    log_level: str = "INFO"
    log_format: str = "text"
    http_timeout_seconds: float = Field(default=10.0, gt=0)

    # RAG / knowledge base (milestone 8.3). Optional: when database_url is
    # unset the agent runs without the finops_knowledge_search tool, so the
    # cost/inventory/recommendation tools work with no Postgres dependency.
    # database_url is a SQLAlchemy/psycopg URL, e.g.
    # postgresql+psycopg://oracle:oracle_dev@localhost:5432/cloudoracle
    database_url: str | None = None
    embeddings_model: str = "models/text-embedding-004"
    knowledge_collection: str = "finops_knowledge"
    rag_top_k: int = Field(default=4, ge=1, le=20)

    # Guardrails (milestone 8.5). Cost caps bound the work per query; the
    # validation toggles control the layered answer check.
    max_hops: int = Field(default=6, ge=1, le=50)
    max_tool_calls: int = Field(default=8, ge=1, le=100)
    max_worker_iters: int = Field(default=6, ge=1, le=50)
    enable_answer_validation: bool = True
    enable_llm_judge: bool = True

    # HTTP surface (milestone 8.5). agent_api_key, when set, gates POST /ask
    # behind an X-API-Key header (same convention as the Go server).
    agent_host: str = "127.0.0.1"
    agent_port: int = Field(default=8099, ge=1, le=65535)
    agent_api_key: str | None = None

    @property
    def run_limits(self) -> RunLimits:
        """Cost caps as the graph's RunLimits."""
        return RunLimits(
            max_hops=self.max_hops,
            max_tool_calls=self.max_tool_calls,
            max_worker_iters=self.max_worker_iters,
        )

    @field_validator("log_level")
    @classmethod
    def _normalize_log_level(cls, v: str) -> str:
        allowed = {"DEBUG", "INFO", "WARNING", "WARN", "ERROR", "CRITICAL"}
        upper = v.upper()
        if upper not in allowed:
            raise ValueError(f"log_level={v!r} must be one of {sorted(allowed)}")
        return "WARNING" if upper == "WARN" else upper

    @field_validator("log_format")
    @classmethod
    def _normalize_log_format(cls, v: str) -> str:
        lower = v.lower()
        if lower not in {"text", "json"}:
            raise ValueError(f"log_format={v!r} must be 'text' or 'json'")
        return lower

    @property
    def cloudoracle_base_url(self) -> str:
        """Stringified base URL without trailing slash (httpx prefers no trailing /)."""
        return str(self.cloudoracle_api_url).rstrip("/")
