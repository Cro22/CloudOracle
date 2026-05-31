"""FastAPI surface for the insights agent.

A thin HTTP shell over the shared `GeminiAgentRunner` + guardrails: `POST /ask`
runs a query through the same guarded pipeline the CLI uses, `GET /health` is a
liveness probe. The agent stack is built once in the lifespan and reused across
requests.

`create_app(runner=...)` injects a ready runner so the surface can be tested
without Gemini / a live Go server; production goes through the lifespan, which
builds a `GeminiAgentRunner` from settings and closes it on shutdown.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import Any, Protocol

from fastapi import FastAPI, Header, HTTPException, Request
from pydantic import BaseModel, Field

from insights_agent.config import Settings
from insights_agent.guardrails.runner import GuardedResult
from insights_agent.logging import get_logger, setup


class AgentRunner(Protocol):
    """Minimal interface the HTTP layer needs from an agent runtime."""

    async def ask(self, query: str) -> GuardedResult: ...

    async def aclose(self) -> None: ...


class AskRequest(BaseModel):
    query: str = Field(min_length=1, max_length=4000)


class ValidationModel(BaseModel):
    valid: bool
    layer: str
    reason: str = ""


class AskResponse(BaseModel):
    answer: str
    tool_calls: list[dict[str, Any]] = Field(default_factory=list)
    fallback_used: bool = False
    validation: ValidationModel | None = None


def _to_validation(result: GuardedResult) -> ValidationModel | None:
    if result.validation is None:
        return None
    v = result.validation
    return ValidationModel(valid=v.valid, layer=v.layer, reason=v.reason)


def create_app(
    *,
    runner: AgentRunner | None = None,
    settings: Settings | None = None,
) -> FastAPI:
    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        if runner is not None:
            # Injected runner (tests / embedding): we don't own its lifecycle.
            app.state.runner = runner
            app.state.api_key = settings.agent_api_key if settings else None
            yield
            return

        from insights_agent.runtime import GeminiAgentRunner

        resolved = settings or Settings()  # type: ignore[call-arg]
        setup(level=resolved.log_level, fmt=resolved.log_format)
        log = get_logger("insights_agent.api")
        log.info("api.starting", model=resolved.gemini_model)
        built = GeminiAgentRunner(resolved, log)
        app.state.runner = built
        app.state.api_key = resolved.agent_api_key
        try:
            yield
        finally:
            await built.aclose()

    app = FastAPI(
        title="CloudOracle Insights Agent",
        version="0.1.0",
        lifespan=lifespan,
    )

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/ask", response_model=AskResponse)
    async def ask(
        body: AskRequest,
        request: Request,
        x_api_key: str | None = Header(default=None, alias="X-API-Key"),
    ) -> AskResponse:
        expected = getattr(request.app.state, "api_key", None)
        if expected and x_api_key != expected:
            raise HTTPException(status_code=401, detail="invalid or missing X-API-Key")

        agent: AgentRunner = request.app.state.runner
        result = await agent.ask(body.query)
        return AskResponse(
            answer=result.answer,
            tool_calls=result.tool_calls,
            fallback_used=result.fallback_used,
            validation=_to_validation(result),
        )

    return app
