"""Shared pytest fixtures.

Environment isolation: many of our tests instantiate Settings, which reads
.env / process env. We clear the Settings-relevant vars at session start so
a developer's real keys don't leak into test behavior.
"""

from __future__ import annotations

from collections.abc import Iterator

import pytest

SETTINGS_VARS = (
    "GEMINI_API_KEY",
    "CLOUDORACLE_API_URL",
    "CLOUDORACLE_API_KEY",
    "GEMINI_MODEL",
    "LOG_LEVEL",
    "LOG_FORMAT",
    "HTTP_TIMEOUT_SECONDS",
)


@pytest.fixture(autouse=True)
def _isolate_settings_env(monkeypatch: pytest.MonkeyPatch, tmp_path) -> Iterator[None]:  # type: ignore[no-untyped-def]
    """Strip Settings vars and chdir to a tmp dir so no local .env is picked up."""
    for var in SETTINGS_VARS:
        monkeypatch.delenv(var, raising=False)
    monkeypatch.chdir(tmp_path)
    yield


@pytest.fixture
def valid_env(monkeypatch: pytest.MonkeyPatch) -> None:
    """Set the minimum required Settings vars to a known-good state."""
    monkeypatch.setenv("GEMINI_API_KEY", "test-gemini-key")
    monkeypatch.setenv("CLOUDORACLE_API_URL", "http://localhost:8080")
    monkeypatch.setenv("CLOUDORACLE_API_KEY", "test-cloudoracle-key")


@pytest.fixture(autouse=True)
def _disable_real_network(monkeypatch: pytest.MonkeyPatch) -> None:
    """Belt-and-suspenders: keep `langchain_google_genai` from contacting Google."""
    monkeypatch.setenv("GOOGLE_API_USE_CLIENT_CERTIFICATE", "false")
