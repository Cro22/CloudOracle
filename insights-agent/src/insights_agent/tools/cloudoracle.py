"""LangChain tools that call the CloudOracle v1 cost endpoints.

The Go API exposes two snapshot-derived cost endpoints behind `X-API-Key`:

  - GET /api/v1/cost-summary       → totals by provider
  - GET /api/v1/cost-by-service    → per-service breakdown for one provider

Both return a `data_source` field tagging the response as
`"snapshots_approximation"` until the real billing-API integration lands
(milestone 8.7). The tool docstrings tell the LLM to surface that caveat to
the user — `note` carries the long-form disclaimer text the Go side curates.

Errors are propagated as exceptions. LangGraph's ReAct loop catches them
and feeds the message back to the LLM as tool output, which lets the model
recover (e.g. retry with a corrected date) instead of confabulating
numbers from a silent empty-dict.
"""

from __future__ import annotations

import secrets
from collections.abc import Sequence
from datetime import date, datetime
from typing import Any

import httpx
import structlog
from langchain_core.tools import StructuredTool, ToolException

logger = structlog.get_logger(__name__)

VALID_PROVIDERS: frozenset[str] = frozenset({"aws", "gcp", "azure"})
VALID_SEVERITIES: frozenset[str] = frozenset({"high", "medium", "low"})
_DATE_FMT = "%Y-%m-%d"


class CloudOracleAPIError(RuntimeError):
    """Raised when the Go API returns a non-2xx response.

    The `code` field mirrors the machine-readable error code the Go side
    returns (`invalid_date_range`, `unauthorized`, `snapshot_query_failed`,
    ...). It lets downstream code branch deterministically without parsing
    the human message.
    """

    def __init__(
        self,
        status: int,
        message: str,
        code: str | None = None,
        request_id: str | None = None,
    ) -> None:
        self.status = status
        self.message = message
        self.code = code
        self.request_id = request_id
        suffix = f" (code={code})" if code else ""
        rid = f" [request_id={request_id}]" if request_id else ""
        super().__init__(f"CloudOracle API {status}: {message}{suffix}{rid}")


class CloudOracleTransportError(RuntimeError):
    """Raised when the HTTP request itself fails (timeout, DNS, conn reset)."""

    def __init__(self, message: str, request_id: str | None = None) -> None:
        self.message = message
        self.request_id = request_id
        rid = f" [request_id={request_id}]" if request_id else ""
        super().__init__(f"CloudOracle transport error: {message}{rid}")


class CloudOracleClient:
    """Thin async wrapper around `httpx.AsyncClient` for the v1 cost endpoints.

    Owns the auth header and base URL so call sites don't repeat boilerplate.
    A fresh `X-Request-ID` is generated per request (24 hex chars, same
    convention as `internal/api/middleware.go:newRequestID`) and echoed in
    logs so a Python-side trace can be cross-referenced with the Go logs
    without manual correlation.
    """

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        timeout_seconds: float = 10.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        if not base_url:
            raise ValueError("base_url must be non-empty")
        if not api_key:
            raise ValueError("api_key must be non-empty")
        self._base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            base_url=self._base_url,
            timeout=timeout_seconds,
            headers={"X-API-Key": api_key, "Accept": "application/json"},
            transport=transport,
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> CloudOracleClient:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()

    async def _get(self, path: str, params: dict[str, str]) -> dict[str, Any]:
        request_id = _new_request_id()
        log = logger.bind(request_id=request_id, path=path)
        try:
            resp = await self._client.get(
                path, params=params, headers={"X-Request-ID": request_id}
            )
        except httpx.TimeoutException as e:
            log.warning("cloudoracle.timeout", error=str(e))
            raise CloudOracleTransportError(f"request timed out: {e}", request_id) from e
        except httpx.HTTPError as e:
            log.warning("cloudoracle.transport_error", error=str(e))
            raise CloudOracleTransportError(str(e), request_id) from e

        if resp.status_code >= 400:
            code, message = _extract_error(resp)
            log.warning("cloudoracle.api_error", status=resp.status_code, code=code)
            raise CloudOracleAPIError(resp.status_code, message, code, request_id)

        log.info("cloudoracle.ok", status=resp.status_code)
        data: Any = resp.json()
        if not isinstance(data, dict):
            # The v1 endpoints always return an object; defensively reject
            # anything else so we don't pass an unexpected shape upstream.
            raise CloudOracleAPIError(
                resp.status_code,
                f"expected JSON object, got {type(data).__name__}",
                request_id=request_id,
            )
        return data

    async def cost_summary(
        self,
        start: str,
        end: str,
        providers: Sequence[str] | None = None,
    ) -> dict[str, Any]:
        _validate_date(start, "start")
        _validate_date(end, "end")
        _validate_date_order(start, end)

        params: dict[str, str] = {"start": start, "end": end}
        if providers:
            normalized = _validate_and_normalize_providers(providers)
            params["providers"] = ",".join(normalized)
        return await self._get("/api/v1/cost-summary", params)

    async def cost_by_service(
        self,
        start: str,
        end: str,
        provider: str,
        top: int = 10,
    ) -> dict[str, Any]:
        _validate_date(start, "start")
        _validate_date(end, "end")
        _validate_date_order(start, end)
        normalized = _validate_provider(provider)
        if not 1 <= top <= 1000:
            raise ValueError(f"top={top} must be in [1, 1000]")

        params: dict[str, str] = {
            "start": start,
            "end": end,
            "provider": normalized,
            "top": str(top),
        }
        return await self._get("/api/v1/cost-by-service", params)

    async def recommendations(
        self,
        provider: str | None = None,
        severity: str | None = None,
        top: int = 20,
    ) -> dict[str, Any]:
        if not 1 <= top <= 200:
            raise ValueError(f"top={top} must be in [1, 200]")

        params: dict[str, str] = {"top": str(top)}
        if provider is not None:
            params["provider"] = _validate_provider(provider)
        if severity is not None:
            params["severity"] = _validate_severity(severity)
        return await self._get("/api/v1/recommendations", params)


def build_tools(client: CloudOracleClient) -> list[StructuredTool]:
    """Wrap the client methods as LangChain `StructuredTool`s.

    The wrappers translate `CloudOracleAPIError` / `CloudOracleTransportError`
    / `ValueError` into `ToolException`. `ToolException` is the canonical
    "this tool failed but the agent should keep going" signal — LangGraph's
    ToolNode catches it and surfaces the message back to the LLM as a tool
    observation, so the model can either retry with corrected args or
    explain the failure to the user. Letting the original RuntimeError
    propagate would abort the whole graph run, which is the wrong UX for
    a transient 5xx or a malformed date.
    """

    async def _summary(
        start: str,
        end: str,
        providers: list[str] | None = None,
    ) -> dict[str, Any]:
        try:
            return await client.cost_summary(start, end, providers)
        except (CloudOracleAPIError, CloudOracleTransportError, ValueError) as e:
            raise ToolException(str(e)) from e

    async def _by_service(
        start: str,
        end: str,
        provider: str,
        top: int = 10,
    ) -> dict[str, Any]:
        try:
            return await client.cost_by_service(start, end, provider, top)
        except (CloudOracleAPIError, CloudOracleTransportError, ValueError) as e:
            raise ToolException(str(e)) from e

    async def _recommendations(
        provider: str | None = None,
        severity: str | None = None,
        top: int = 20,
    ) -> dict[str, Any]:
        try:
            return await client.recommendations(provider, severity, top)
        except (CloudOracleAPIError, CloudOracleTransportError, ValueError) as e:
            raise ToolException(str(e)) from e

    summary_tool = StructuredTool.from_function(
        coroutine=_summary,
        name="cloudoracle_cost_summary",
        description=_COST_SUMMARY_DESC,
        handle_tool_error=True,
    )
    by_service_tool = StructuredTool.from_function(
        coroutine=_by_service,
        name="cloudoracle_cost_by_service",
        description=_COST_BY_SERVICE_DESC,
        handle_tool_error=True,
    )
    recommendations_tool = StructuredTool.from_function(
        coroutine=_recommendations,
        name="cloudoracle_recommendations",
        description=_RECOMMENDATIONS_DESC,
        handle_tool_error=True,
    )
    return [summary_tool, by_service_tool, recommendations_tool]


_COST_SUMMARY_DESC = """Return aggregated cloud cost totals per provider for a date range.

Args:
    start: Inclusive period start, ISO date `YYYY-MM-DD` (e.g. "2026-04-01").
    end:   Inclusive period end,   ISO date `YYYY-MM-DD`. Must be >= start.
    providers: Optional list filtering which providers to include. Allowed
               values: "aws", "gcp", "azure". If omitted, all configured
               providers are returned.

Returns:
    A dict with this shape:
      {
        "period":      {"start": "...", "end": "..."},
        "providers":   {"aws": {"total_usd": 150.0, "currency": "USD"}, ...},
        "grand_total_usd": 350.0,
        "generated_at":    "2026-05-18T12:00:00Z",
        "data_source":     "snapshots_approximation",
        "note":            "<long-form caveat about snapshot-based approximation>"
      }

IMPORTANT: When `data_source == "snapshots_approximation"`, the numbers come
from periodic CloudOracle cost snapshots, NOT a real billing API. Surface the
caveat to the user when the answer materially depends on accuracy — e.g.
prefix with "based on snapshot approximations, ..." or quote the `note`."""


_COST_BY_SERVICE_DESC = """Return a per-service cost breakdown for one provider.

Args:
    start: Inclusive period start, ISO date `YYYY-MM-DD`.
    end:   Inclusive period end,   ISO date `YYYY-MM-DD`. Must be >= start.
    provider: One of "aws", "gcp", "azure" (lowercase).
    top: Maximum services to return, sorted by cost descending. Default 10.
         Allowed range: 1..1000. Use 5-10 for executive summaries.

Returns:
    A dict with this shape:
      {
        "period":   {"start": "...", "end": "..."},
        "provider": "aws",
        "services": [
          {"name": "ec2", "total_usd": 100.0, "percentage": 66.67},
          {"name": "rds", "total_usd":  50.0, "percentage": 33.33}
        ],
        "total_usd":    150.0,
        "generated_at": "...",
        "data_source":  "snapshots_approximation",
        "note":         "<long-form caveat>"
      }

IMPORTANT: Same snapshot-approximation caveat as cloudoracle_cost_summary —
surface it to the user when accuracy matters for the answer."""


_RECOMMENDATIONS_DESC = """Return cost-optimization recommendations (where to save money).

Use this for "where can I save money?", "what's wasteful?", "show me my top
optimizations", or any savings / right-sizing / idle-resource question. This is
NOT a spend query — for "how much did I spend", use cloudoracle_cost_summary.

Args:
    provider: Optional filter, one of "aws", "gcp", "azure". Omit for all clouds.
    severity: Optional filter, one of "high", "medium", "low". Omit for all.
              "high" = biggest / most certain waste; start here for quick wins.
    top: Max recommendations to return, sorted by monthly savings descending.
         Default 20, range 1..200. Use 5-10 for an executive shortlist.

Returns:
    A dict with this shape:
      {
        "recommendations": [
          {
            "resource_id": "i-aaa", "provider": "aws", "service": "ec2",
            "resource_type": "t3.large", "region": "us-east-1",
            "rule": "ec2-idle", "severity": "High",
            "monthly_cost_usd": 300.0, "monthly_savings_usd": 300.0,
            "description": "EC2 i-aaa ... CPU usage 1.0% ...",
            "recommendation": "Consider shutting down or terminating ..."
          }
        ],
        "total_count": 12,            # full filtered set, before the top cap
        "returned_count": 10,         # after the top cap
        "total_monthly_savings_usd": 1450.0,   # sum over the full filtered set
        "by_severity": {"High": 3, "Medium": 5, "Low": 4},
        "filters": {"provider": "aws", "severity": "", "top": 10},
        "generated_at": "...",
        "data_source": "heuristic_rules",
        "note": "<caveat: heuristic upper-bound savings, validate before acting>"
      }

IMPORTANT: `data_source == "heuristic_rules"` means these come from a rule-based
analyzer over the current inventory, NOT real billing. `monthly_savings_usd` is
an estimated upper bound. When recommending action, tell the user to validate
against real usage first, and quote `total_monthly_savings_usd` for the headline
opportunity. If `returned_count < total_count`, mention the list was truncated to
the top N by savings."""


def _validate_date(value: str, field: str) -> date:
    try:
        return datetime.strptime(value, _DATE_FMT).date()
    except (TypeError, ValueError) as e:
        raise ValueError(
            f"{field}={value!r} is not a valid YYYY-MM-DD date"
        ) from e


def _validate_date_order(start: str, end: str) -> None:
    if _validate_date(end, "end") < _validate_date(start, "start"):
        raise ValueError(f"end={end!r} is before start={start!r}")


def _validate_provider(value: str) -> str:
    norm = value.strip().lower() if isinstance(value, str) else ""
    if norm not in VALID_PROVIDERS:
        raise ValueError(
            f"provider={value!r} must be one of {sorted(VALID_PROVIDERS)}"
        )
    return norm


def _validate_severity(value: str) -> str:
    norm = value.strip().lower() if isinstance(value, str) else ""
    if norm not in VALID_SEVERITIES:
        raise ValueError(
            f"severity={value!r} must be one of {sorted(VALID_SEVERITIES)}"
        )
    return norm


def _validate_and_normalize_providers(values: Sequence[str]) -> list[str]:
    out: list[str] = []
    for v in values:
        out.append(_validate_provider(v))
    if not out:
        raise ValueError("providers list cannot be empty when provided")
    return out


def _new_request_id() -> str:
    """24 hex chars — same length / encoding as `newRequestID` in the Go API."""
    return secrets.token_hex(12)


def _extract_error(resp: httpx.Response) -> tuple[str | None, str]:
    """Pull `code` + human message from the Go v1 error envelope.

    The v1 handlers always emit `{"error": "...", "code": "..."}`. If a
    legacy v0 handler ever leaks here it'll only have `error` — handle that
    gracefully so we still produce a useful exception.
    """
    try:
        body = resp.json()
    except ValueError:
        return None, resp.text or f"HTTP {resp.status_code}"
    if isinstance(body, dict):
        return body.get("code"), str(body.get("error") or body)
    return None, str(body)
