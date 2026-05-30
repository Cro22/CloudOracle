from __future__ import annotations

import pytest
from pydantic import ValidationError

from insights_agent.config import Settings


def test_settings_loads_from_env(valid_env: None) -> None:
    s = Settings()
    assert s.gemini_api_key == "test-gemini-key"
    assert s.cloudoracle_api_key == "test-cloudoracle-key"
    assert s.cloudoracle_base_url == "http://localhost:8080"
    assert s.gemini_model == "gemini-2.5-flash"
    assert s.log_level == "INFO"
    assert s.log_format == "text"
    assert s.http_timeout_seconds == 10.0


def test_missing_required_fails_fast() -> None:
    with pytest.raises(ValidationError) as exc_info:
        Settings()
    errors = exc_info.value.errors()
    missing = {e["loc"][0] for e in errors}
    assert "gemini_api_key" in missing
    assert "cloudoracle_api_url" in missing
    assert "cloudoracle_api_key" in missing


def test_invalid_log_level_rejected(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("LOG_LEVEL", "loud")
    with pytest.raises(ValidationError):
        Settings()


def test_invalid_log_format_rejected(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("LOG_FORMAT", "yaml")
    with pytest.raises(ValidationError):
        Settings()


def test_log_level_warn_normalized_to_warning(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("LOG_LEVEL", "warn")
    s = Settings()
    assert s.log_level == "WARNING"


def test_cloudoracle_base_url_strips_trailing_slash(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("CLOUDORACLE_API_URL", "http://example.com:9090/")
    s = Settings()
    assert s.cloudoracle_base_url == "http://example.com:9090"


def test_timeout_must_be_positive(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("HTTP_TIMEOUT_SECONDS", "0")
    with pytest.raises(ValidationError):
        Settings()


def test_rag_settings_default_to_disabled(valid_env: None) -> None:
    s = Settings()
    # No DATABASE_URL → RAG is off; the rest carry sensible defaults.
    assert s.database_url is None
    assert s.embeddings_model == "models/text-embedding-004"
    assert s.knowledge_collection == "finops_knowledge"
    assert s.rag_top_k == 4


def test_rag_settings_from_env(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv(
        "DATABASE_URL", "postgresql+psycopg://oracle:oracle_dev@localhost:5432/cloudoracle"
    )
    monkeypatch.setenv("KNOWLEDGE_COLLECTION", "kb")
    monkeypatch.setenv("RAG_TOP_K", "8")
    s = Settings()
    assert s.database_url is not None
    assert s.knowledge_collection == "kb"
    assert s.rag_top_k == 8


def test_rag_top_k_out_of_range_rejected(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("RAG_TOP_K", "0")
    with pytest.raises(ValidationError):
        Settings()
