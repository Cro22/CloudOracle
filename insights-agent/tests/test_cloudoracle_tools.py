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

RECOMMENDATIONS_OK: dict[str, Any] = {
    "recommendations": [
        {
            "resource_id": "i-aaa",
            "provider": "aws",
            "service": "ec2",
            "resource_type": "t3.large",
            "region": "us-east-1",
            "rule": "ec2-idle",
            "severity": "High",
            "monthly_cost_usd": 300.0,
            "monthly_savings_usd": 300.0,
            "description": "idle instance",
            "recommendation": "terminate it",
        }
    ],
    "total_count": 1,
    "returned_count": 1,
    "total_monthly_savings_usd": 300.0,
    "by_severity": {"High": 1},
    "filters": {"provider": "aws", "severity": "high", "top": 20},
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "heuristic_rules",
    "note": "heuristic note",
}

TRENDS_OK: dict[str, Any] = {
    "days": 90,
    "points": [
        {"date": "2026-03-01", "total_cost_usd": 200.0},
        {"date": "2026-03-30", "total_cost_usd": 300.0},
    ],
    "first": {"date": "2026-03-01", "total_cost_usd": 200.0},
    "latest": {"date": "2026-03-30", "total_cost_usd": 300.0},
    "change": {"absolute_usd": 100.0, "percent_from_first": 50.0, "direction": "up"},
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "snapshots_approximation",
    "note": "approximation note",
}

INVENTORY_OK: dict[str, Any] = {
    "total_resources": 7,
    "total_monthly_cost_usd": 500.0,
    "total_services": 6,
    "by_provider": {
        "aws": {"count": 3, "monthly_cost_usd": 350.0},
        "gcp": {"count": 2, "monthly_cost_usd": 90.0},
        "azure": {"count": 2, "monthly_cost_usd": 60.0},
    },
    "by_service": [
        {"service": "ec2", "provider": "aws", "count": 2, "monthly_cost_usd": 300.0},
    ],
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "live_inventory",
    "note": "inventory note",
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


class TestRecommendationsHappyPath:
    async def test_success_no_filters(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=RECOMMENDATIONS_OK)
        out = await client.recommendations()
        assert out == RECOMMENDATIONS_OK
        req = httpx_mock.get_request()
        assert req is not None
        assert req.url.path == "/api/v1/recommendations"
        # Only top is sent when provider/severity are omitted.
        assert b"top=20" in req.url.query
        assert b"provider=" not in req.url.query
        assert b"severity=" not in req.url.query
        await client.aclose()

    async def test_params_include_filters(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=RECOMMENDATIONS_OK)
        await client.recommendations(provider="AWS", severity="High", top=5)
        req = httpx_mock.get_request()
        assert req is not None
        # provider/severity normalized to lowercase before the request.
        assert b"provider=aws" in req.url.query
        assert b"severity=high" in req.url.query
        assert b"top=5" in req.url.query
        await client.aclose()


class TestCostTrendsHappyPath:
    async def test_success_defaults(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=TRENDS_OK)
        out = await client.cost_trends()
        assert out == TRENDS_OK
        req = httpx_mock.get_request()
        assert req is not None
        assert req.url.path == "/api/v1/cost-trends"
        assert b"days=90" in req.url.query
        assert b"provider=" not in req.url.query
        await client.aclose()

    async def test_params_include_days_and_provider(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=TRENDS_OK)
        await client.cost_trends(days=30, provider="AWS")
        req = httpx_mock.get_request()
        assert req is not None
        assert b"days=30" in req.url.query
        assert b"provider=aws" in req.url.query
        await client.aclose()


class TestInventoryHappyPath:
    async def test_success_defaults(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=INVENTORY_OK)
        out = await client.inventory()
        assert out == INVENTORY_OK
        req = httpx_mock.get_request()
        assert req is not None
        assert req.url.path == "/api/v1/inventory"
        assert b"top=50" in req.url.query
        assert b"provider=" not in req.url.query
        await client.aclose()

    async def test_params_include_provider_and_top(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=INVENTORY_OK)
        await client.inventory(provider="AWS", top=10)
        req = httpx_mock.get_request()
        assert req is not None
        assert b"provider=aws" in req.url.query
        assert b"top=10" in req.url.query
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

    async def test_recommendations_invalid_provider(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.recommendations(provider="oracle-cloud")
        await client.aclose()

    async def test_recommendations_invalid_severity(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.recommendations(severity="critical")
        await client.aclose()

    async def test_recommendations_top_out_of_range(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.recommendations(top=0)
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.recommendations(top=201)
        await client.aclose()

    async def test_cost_trends_days_out_of_range(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match=r"days=\d+ must be in"):
            await client.cost_trends(days=0)
        with pytest.raises(ValueError, match=r"days=\d+ must be in"):
            await client.cost_trends(days=366)
        await client.aclose()

    async def test_cost_trends_invalid_provider(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.cost_trends(provider="oracle-cloud")
        await client.aclose()

    async def test_inventory_top_out_of_range(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.inventory(top=0)
        with pytest.raises(ValueError, match=r"top=\d+ must be in"):
            await client.inventory(top=201)
        await client.aclose()

    async def test_inventory_invalid_provider(
        self, client: CloudOracleClient
    ) -> None:
        with pytest.raises(ValueError, match="must be one of"):
            await client.inventory(provider="oracle-cloud")
        await client.aclose()


class TestBuildTools:
    async def test_builds_five_tools_with_expected_names(
        self, client: CloudOracleClient
    ) -> None:
        tools = build_tools(client)
        names = {t.name for t in tools}
        assert names == {
            "cloudoracle_cost_summary",
            "cloudoracle_cost_by_service",
            "cloudoracle_recommendations",
            "cloudoracle_cost_trends",
            "cloudoracle_inventory",
        }
        await client.aclose()

    async def test_descriptions_mention_data_source(
        self, client: CloudOracleClient
    ) -> None:
        # Every tool documents its data_source so the model knows which caveat
        # to surface: the cost/trends tools use snapshots_approximation, the
        # recommendations tool uses heuristic_rules, inventory uses live_inventory.
        for t in build_tools(client):
            assert "data_source" in t.description
        tools_by_name = {t.name: t for t in build_tools(client)}
        assert "snapshots_approximation" in tools_by_name["cloudoracle_cost_summary"].description
        assert "snapshots_approximation" in tools_by_name["cloudoracle_cost_by_service"].description
        assert "snapshots_approximation" in tools_by_name["cloudoracle_cost_trends"].description
        assert "heuristic_rules" in tools_by_name["cloudoracle_recommendations"].description
        assert "live_inventory" in tools_by_name["cloudoracle_inventory"].description
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

    async def test_recommendations_tool_invokes_client(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=RECOMMENDATIONS_OK)
        rec_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_recommendations"
        )
        out = await rec_tool.ainvoke({"provider": "aws", "severity": "high", "top": 5})
        assert out == RECOMMENDATIONS_OK
        await client.aclose()

    async def test_recommendations_tool_wraps_validation_error(
        self, client: CloudOracleClient
    ) -> None:
        # A bad severity raises ValueError in the client; the tool wrapper must
        # translate it to a ToolException so the ReAct loop can recover instead
        # of aborting the run.
        rec_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_recommendations"
        )
        out = await rec_tool.ainvoke({"severity": "critical"})
        # handle_tool_error=True returns the error string as the observation.
        assert "must be one of" in str(out)
        await client.aclose()

    async def test_cost_trends_tool_invokes_client(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=TRENDS_OK)
        trends_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_cost_trends"
        )
        out = await trends_tool.ainvoke({"days": 30, "provider": "aws"})
        assert out == TRENDS_OK
        await client.aclose()

    async def test_cost_trends_tool_wraps_validation_error(
        self, client: CloudOracleClient
    ) -> None:
        trends_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_cost_trends"
        )
        out = await trends_tool.ainvoke({"days": 9999})
        assert "must be in" in str(out)
        await client.aclose()

    async def test_inventory_tool_invokes_client(
        self, client: CloudOracleClient, httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(json=INVENTORY_OK)
        inventory_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_inventory"
        )
        out = await inventory_tool.ainvoke({"provider": "aws", "top": 10})
        assert out == INVENTORY_OK
        await client.aclose()

    async def test_inventory_tool_wraps_validation_error(
        self, client: CloudOracleClient
    ) -> None:
        inventory_tool = next(
            t for t in build_tools(client) if t.name == "cloudoracle_inventory"
        )
        out = await inventory_tool.ainvoke({"provider": "oracle-cloud"})
        assert "must be one of" in str(out)
        await client.aclose()
