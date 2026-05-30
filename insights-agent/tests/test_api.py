"""HTTP surface tests — an injected fake runner, no Gemini / Go server / DB."""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from insights_agent.api.app import create_app
from insights_agent.config import Settings
from insights_agent.guardrails.runner import GuardedResult
from insights_agent.guardrails.validation import ValidationResult


class FakeRunner:
    def __init__(self, result: GuardedResult) -> None:
        self._result = result
        self.queries: list[str] = []
        self.closed = False

    async def ask(self, query: str) -> GuardedResult:
        self.queries.append(query)
        return self._result

    async def aclose(self) -> None:
        self.closed = True


def _ok_result() -> GuardedResult:
    return GuardedResult(
        answer="You spent $150 on AWS.",
        tool_calls=[{"name": "cloudoracle_cost_summary", "args": {"start": "x", "end": "y"}}],
        observations=[{"name": "cloudoracle_cost_summary", "output": "{}"}],
        validation=ValidationResult(valid=True, layer="deterministic"),
        fallback_used=False,
    )


def test_health() -> None:
    with TestClient(create_app(runner=FakeRunner(_ok_result()))) as c:
        r = c.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


def test_ask_returns_answer_and_metadata() -> None:
    runner = FakeRunner(_ok_result())
    with TestClient(create_app(runner=runner)) as c:
        r = c.post("/ask", json={"query": "How much did I spend on AWS?"})
    assert r.status_code == 200
    body = r.json()
    assert body["answer"] == "You spent $150 on AWS."
    assert body["tool_calls"][0]["name"] == "cloudoracle_cost_summary"
    assert body["fallback_used"] is False
    assert body["validation"] == {"valid": True, "layer": "deterministic", "reason": ""}
    assert runner.queries == ["How much did I spend on AWS?"]


def test_ask_rejects_empty_query() -> None:
    with TestClient(create_app(runner=FakeRunner(_ok_result()))) as c:
        r = c.post("/ask", json={"query": ""})
    assert r.status_code == 422  # pydantic min_length


def test_ask_passes_through_fallback() -> None:
    result = GuardedResult(answer="couldn't verify", fallback_used=True, error="boom")
    with TestClient(create_app(runner=FakeRunner(result))) as c:
        r = c.post("/ask", json={"query": "x"})
    body = r.json()
    assert body["fallback_used"] is True
    assert body["validation"] is None


def test_no_auth_required_when_key_unset() -> None:
    # settings is None → api_key None → open endpoint.
    with TestClient(create_app(runner=FakeRunner(_ok_result()))) as c:
        assert c.post("/ask", json={"query": "x"}).status_code == 200


def test_auth_enforced_when_key_set(
    valid_env: None, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("AGENT_API_KEY", "s3cret")
    settings = Settings()
    runner = FakeRunner(_ok_result())
    with TestClient(create_app(runner=runner, settings=settings)) as c:
        assert c.post("/ask", json={"query": "x"}).status_code == 401
        assert c.post(
            "/ask", json={"query": "x"}, headers={"X-API-Key": "wrong"}
        ).status_code == 401
        ok = c.post("/ask", json={"query": "x"}, headers={"X-API-Key": "s3cret"})
    assert ok.status_code == 200
    assert ok.json()["answer"] == "You spent $150 on AWS."


def test_injected_runner_not_closed_by_app() -> None:
    # The app must not close a runner it didn't build (the injector owns it).
    runner = FakeRunner(_ok_result())
    with TestClient(create_app(runner=runner)) as c:
        c.get("/health")
    assert runner.closed is False
