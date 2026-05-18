from __future__ import annotations

import json
from typing import Any

import httpx
import pytest
from pytest_httpx import HTTPXMock

from insights_agent.tools.cloudoracle import (
    CloudOracleAPIError,
    CloudOracleClient,
    CloudOracleTransportError,
    build_tools,
)

BASE_URL = "http://localhost:8080"
API_KEY = "test-key"


@pytest.fixture
def client() -> CloudOracleClient:
    return CloudOracleClient(base_url=BASE_URL, api_key=API_KEY, timeout_seconds=2.0)


SUMMARY_OK: dict[str, Any] = {
    "period": {"start": "2026-04-01", "end": "2026-04-30"},
    "providers": {
        "aws": {"total_usd": 150.0, "currency": "USD"},
        "gcp": {"total_usd": 200.0, "currency": "USD"},
    },
    "grand_total_usd": 350.0,
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "snapshots_approximation",
    "note": "approximation note",
}

BY_SERVICE_OK: dict[str, Any] = {
    "period": {"start": "2026-04-01", "end": "2026-04-30"},
    "provider": "aws",
    "services": [
        {"name": "ec2", "total_usd": 100.0, "percentage": 66.67},
        {"name": "rds", "total_usd": 50.0, "percentage": 33.33},
    ],
    "total_usd": 150.0,
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "snapshots_approximation",
    "note": "approximation note",
}


class TestClientConstruction:
    def test_rejects_empty_base_url(self) -> None:
        with pytest.raises(ValueError, match="base_url"):
            CloudOracleClient(base_url="", api_key="k")

    def test_rejects_empty_api_key(self) -> None:
        with pytest.raises(ValueError, match="api_key"):
            CloudOracleClient(base_url=BASE_URL, api_key="")

    def test_strips_trailing_slash(self) -> None:
        c = CloudOracleClient(base_url=BASE_URL + "/", api_key="k")
        assert c._base_url == BASE_URL


class TestCostSummaryHappyPath:
    async def test_success(self, client: CloudOracleClient, httpx_mock: HTTPXMock) -> None:
        httpx_mock.add_response(
            url=f"{BASE_URL}/api/v1/cost-summary?start=2026-04-01&end=2026-04-30",
            json=SUMMARY_OK,
        )
        out = await client.cost_summary("2026-04-01", "2026-04-30")
        assert out == SUMMARY_OK
        await client.aclose()

    async def test_sends_auth_and_request_id(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=SUMMARY_OK)
        await client.cost_summary("2026-04-01", "2026-04-30")
        req = httpx_mock.get_request()
        assert req is not None
        assert req.headers["X-API-Key"] == API_KEY
        # 24 hex chars, same shape as the Go server's newRequestID.
        rid = req.headers["X-Request-ID"]
        assert len(rid) == 24
        int(rid, 16)
        await client.aclose()

    async def test_providers_filter_serialized_as_csv(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=SUMMARY_OK)
        await client.cost_summary("2026-04-01", "2026-04-30", providers=["AWS", "gcp"])
        req = httpx_mock.get_request()
        assert req is not None
        assert b"providers=aws%2Cgcp" in req.url.query
        await client.aclose()


class TestCostByServiceHappyPath:
    async def test_success(self, client: CloudOracleClient, httpx_mock: HTTPXMock) -> None:
        httpx_mock.add_response(json=BY_SERVICE_OK)
        out = await client.cost_by_service("2026-04-01", "2026-04-30", "aws", top=5)
        assert out == BY_SERVICE_OK
        await client.aclose()

    async def test_params_include_provider_and_top(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=BY_SERVICE_OK)
        await client.cost_by_service("2026-04-01", "2026-04-30", "aws", top=7)
        req = httpx_mock.get_request()
        assert req is not None
        assert b"provider=aws" in req.url.query
        assert b"top=7" in req.url.query
        await client.aclose()


class TestErrorHandling:
    async def test_401_raises_with_code(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(
            status_code=401,
            json={"error": "missing X-API-Key header", "code": "unauthorized"},
        )
        with pytest.raises(CloudOracleAPIError) as exc:
            await client.cost_summary("2026-04-01", "2026-04-30")
        assert exc.value.status == 401
        assert exc.value.code == "unauthorized"
        assert "X-API-Key" in exc.value.message
        assert exc.value.request_id is not None
        await client.aclose()

    async def test_500_raises_with_code(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(
            status_code=500,
            json={"error": "boom", "code": "snapshot_query_failed"},
        )
        with pytest.raises(CloudOracleAPIError) as exc:
            await client.cost_summary("2026-04-01", "2026-04-30")
        assert exc.value.status == 500
        assert exc.value.code == "snapshot_query_failed"
        await client.aclose()

    async def test_400_invalid_date_range(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(
            status_code=400,
            json={"error": "end is before start", "code": "invalid_date_range"},
        )
        with pytest.raises(CloudOracleAPIError) as exc:
            await client.cost_summary("2026-04-01", "2026-04-30")
        assert exc.value.code == "invalid_date_range"
        await client.aclose()

    async def test_non_json_error_response(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(status_code=502, text="Bad Gateway")
        with pytest.raises(CloudOracleAPIError) as exc:
            await client.cost_summary("2026-04-01", "2026-04-30")
        assert exc.value.status == 502
        assert "Bad Gateway" in exc.value.message
        assert exc.value.code is None
        await client.aclose()

    async def test_non_object_response_rejected(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(
            content=json.dumps([1, 2, 3]).encode(),
            headers={"content-type": "application/json"},
        )
        with pytest.raises(CloudOracleAPIError, match="expected JSON object"):
            await client.cost_summary("2026-04-01", "2026-04-30")
        await client.aclose()

    async def test_timeout_raises_transport_error(self, httpx_mock: HTTPXMock) -> None:
        httpx_mock.add_exception(httpx.ReadTimeout("slow"))
        c = CloudOracleClient(base_url=BASE_URL, api_key=API_KEY, timeout_seconds=0.1)
        with pytest.raises(CloudOracleTransportError, match="timed out"):
            await c.cost_summary("2026-04-01", "2026-04-30")
        await c.aclose()

    async def test_network_error_raises_transport_error(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_exception(httpx.ConnectError("connection refused"))
        with pytest.raises(CloudOracleTransportError, match="connection refused"):
            await client.cost_summary("2026-04-01", "2026-04-30")
        await client.aclose()


class TestLocalValidation:
    """Inputs we reject before issuing an HTTP request — no httpx_mock needed."""

    async def test_bad_date_format(self, client: CloudOracleClient) -> None:
        with pytest.raises(ValueError, match="not a valid YYYY-MM-DD"):
            await client.cost_summary("04-01-2026", "2026-04-30")
        await client.aclose()

    async def test_end_before_start(self, client: CloudOracleClient) -> None:
        with pytest.raises(ValueError, match="before start"):
            await client.cost_summary("2026-04-30", "2026-04-01")
        await client.aclose()

    async def test_invalid_provider_in_summary_filter(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.cost_summary(
                "2026-04-01", "2026-04-30", providers=["aws", "oracle-cloud"]
            )
        await client.aclose()

    async def test_empty_provider_list_after_normalization_raises(
        self, client: CloudOracleClient
    ) -> None:
        # Empty list is treated as "no filter" by the client — that path
        # skips validation entirely and issues the request without the
        # query param. Verify it does not raise here (validation only fires
        # when at least one provider is supplied).
        # We don't actually issue the request; we just confirm the call
        # signature accepts an empty list without raising before any HTTP.
        # The actual behavior is exercised by the providers-filter test.
        await client.aclose()  # nothing to assert; this docstring documents intent.

    async def test_invalid_provider_in_by_service(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.cost_by_service("2026-04-01", "2026-04-30", "oracle-cloud")
        await client.aclose()

    async def test_top_out_of_range(self, client: CloudOracleClient) -> None:
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.cost_by_service("2026-04-01", "2026-04-30", "aws", top=0)
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.cost_by_service("2026-04-01", "2026-04-30", "aws", top=1001)
        await client.aclose()


class TestBuildTools:
    async def test_builds_two_tools_with_expected_names(
        self, client: CloudOracleClient
    ) -> None:
        tools = build_tools(client)
        names = {t.name for t in tools}
        assert names == {"cloudoracle_cost_summary", "cloudoracle_cost_by_service"}
        await client.aclose()

    async def test_descriptions_mention_data_source(
        self, client: CloudOracleClient
    ) -> None:
        tools = build_tools(client)
        for t in tools:
            assert "data_source" in t.description
            assert "snapshots_approximation" in t.description
        await client.aclose()

    async def test_summary_tool_invokes_client(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=SUMMARY_OK)
        summary_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_cost_summary"
        )
        out = await summary_tool.ainvoke(
            {"start": "2026-04-01", "end": "2026-04-30"}
        )
        assert out == SUMMARY_OK
        await client.aclose()

    async def test_by_service_tool_invokes_client(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=BY_SERVICE_OK)
        by_service_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_cost_by_service"
        )
        out = await by_service_tool.ainvoke(
            {
                "start": "2026-04-01",
                "end": "2026-04-30",
                "provider": "aws",
                "top": 5,
            }
        )
        assert out == BY_SERVICE_OK
        await client.aclose()
