"""Settings loaded from environment with fail-fast validation.

We load every setting once at startup so individual modules don't reach for
`os.environ` directly — same pattern the Go side uses (`internal/config.Load`).
Required values trigger a `pydantic.ValidationError` at instantiation; the CLI
entry point surfaces a readable message and exits non-zero.
"""

from __future__ import annotations

from pydantic import Field, HttpUrl, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


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
