"""Tests for graph/basic.py with a hand-rolled fake chat model.

We don't use Gemini in CI — too slow, costs money, and tests would couple
to model quirks. Instead, we drive `create_react_agent` with a custom
`BaseChatModel` that:

  - implements `bind_tools` (returns self, so the agent loop's call
    survives without changing the response queue), and
  - returns a pre-scripted sequence of AIMessages on each `_agenerate` call.

That lets us assert: (1) tools are invoked in the expected order with the
expected args, (2) the final answer text bubbles up correctly, and
(3) when the model emits no tool call, the graph terminates immediately.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any

import pytest
from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from pydantic import Field
from pytest_httpx import HTTPXMock

from insights_agent.graph.basic import _stringify_content, ask, build_graph
from insights_agent.tools.cloudoracle import CloudOracleClient, build_tools

BASE_URL = "http://localhost:8080"
API_KEY = "test-key"


class ScriptedChatModel(BaseChatModel):
    """Returns the next message from `script` on every model call.

    `script` is consumed left-to-right. We also stash the most recent
    `messages` passed to the model so tests can assert the system prompt
    was actually plumbed through.
    """

    script: list[AIMessage] = Field(default_factory=list)
    last_messages: list[BaseMessage] | None = None

    @property
    def _llm_type(self) -> str:
        return "scripted-test"

    def bind_tools(self, tools: Sequence[Any], **kwargs: Any) -> ScriptedChatModel:
        return self

    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        self.last_messages = messages
        if not self.script:
            raise RuntimeError("ScriptedChatModel exhausted: graph asked for one more turn than scripted")
        msg = self.script.pop(0)
        return ChatResult(generations=[ChatGeneration(message=msg)])

    async def _agenerate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: Any = None,
        **kwargs: Any,
    ) -> ChatResult:
        return self._generate(messages, stop, run_manager, **kwargs)


SUMMARY_PAYLOAD: dict[str, Any] = {
    "period": {"start": "2026-04-01", "end": "2026-04-30"},
    "providers": {"aws": {"total_usd": 150.0, "currency": "USD"}},
    "grand_total_usd": 150.0,
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "snapshots_approximation",
    "note": "approximation note",
}


@pytest.fixture
def client() -> CloudOracleClient:
    return CloudOracleClient(base_url=BASE_URL, api_key=API_KEY, timeout_seconds=2.0)


async def test_graph_invokes_summary_tool_then_returns_answer(
    client: CloudOracleClient, httpx_mock: HTTPXMock
) -> None:
    httpx_mock.add_response(json=SUMMARY_PAYLOAD)

    model = ScriptedChatModel(
        script=[
            # Turn 1: ask the agent to call cloudoracle_cost_summary.
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "cloudoracle_cost_summary",
                        "args": {"start": "2026-04-01", "end": "2026-04-30"},
                        "id": "call-1",
                    }
                ],
            ),
            # Turn 2: deliver a final answer that references the snapshot caveat.
            AIMessage(
                content=(
                    "You spent approximately $150 on AWS in April 2026 "
                    "(snapshots-based approximation, not the final bill)."
                )
            ),
        ]
    )
    tools = build_tools(client)
    graph = build_graph(model, tools)

    result = await ask(graph, "How much did I spend on AWS in April 2026?")

    assert len(result.tool_calls) == 1
    assert result.tool_calls[0]["name"] == "cloudoracle_cost_summary"
    assert result.tool_calls[0]["args"] == {"start": "2026-04-01", "end": "2026-04-30"}
    assert "$150" in result.answer
    assert "snapshots" in result.answer.lower()

    # System prompt was prepended on every call.
    assert model.last_messages is not None
    assert model.last_messages[0].type == "system"
    assert "FinOps" in str(model.last_messages[0].content)

    await client.aclose()


async def test_graph_invokes_two_tools_in_order(
    client: CloudOracleClient, httpx_mock: HTTPXMock
) -> None:
    httpx_mock.add_response(json=SUMMARY_PAYLOAD)
    httpx_mock.add_response(
        json={
            "period": {"start": "2026-04-01", "end": "2026-04-30"},
            "provider": "aws",
            "services": [{"name": "ec2", "total_usd": 100.0, "percentage": 66.67}],
            "total_usd": 100.0,
            "generated_at": "2026-05-18T12:00:00Z",
            "data_source": "snapshots_approximation",
            "note": "approximation note",
        }
    )

    model = ScriptedChatModel(
        script=[
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "cloudoracle_cost_summary",
                        "args": {"start": "2026-04-01", "end": "2026-04-30"},
                        "id": "call-1",
                    }
                ],
            ),
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "cloudoracle_cost_by_service",
                        "args": {
                            "start": "2026-04-01",
                            "end": "2026-04-30",
                            "provider": "aws",
                            "top": 5,
                        },
                        "id": "call-2",
                    }
                ],
            ),
            AIMessage(content="Top service: EC2 at $100."),
        ]
    )
    tools = build_tools(client)
    graph = build_graph(model, tools)

    result = await ask(graph, "Break down AWS spend.")

    names = [c["name"] for c in result.tool_calls]
    assert names == ["cloudoracle_cost_summary", "cloudoracle_cost_by_service"]
    assert "EC2" in result.answer
    await client.aclose()


async def test_graph_no_tool_call_returns_direct_answer(
    client: CloudOracleClient,
) -> None:
    """Off-scope question: the model should reply without calling a tool."""
    model = ScriptedChatModel(
        script=[
            AIMessage(content="Sorry, I only help with cloud cost questions."),
        ]
    )
    tools = build_tools(client)
    graph = build_graph(model, tools)

    result = await ask(graph, "What's the weather today?")

    assert result.tool_calls == []
    assert "cloud cost" in result.answer
    await client.aclose()


class TestStringifyContent:
    """Exercise the multimodal fallback paths Gemini returns for some replies."""

    def test_string_passthrough(self) -> None:
        assert _stringify_content("hello") == "hello"

    def test_list_of_strings(self) -> None:
        assert _stringify_content(["a", "b"]) == "ab"

    def test_list_of_text_blocks(self) -> None:
        assert (
            _stringify_content([{"type": "text", "text": "x"}, {"type": "text", "text": "y"}])
            == "xy"
        )

    def test_mixed_list(self) -> None:
        blocks = [
            "head ",
            {"type": "image_url", "image_url": "..."},  # non-text ignored
            {"type": "text", "text": "tail"},
            {"type": "text"},  # malformed: no text key
        ]
        assert _stringify_content(blocks) == "head tail"

    def test_unknown_type_returns_empty(self) -> None:
        assert _stringify_content(42) == ""


async def test_graph_surfaces_tool_error_to_llm(
    client: CloudOracleClient, httpx_mock: HTTPXMock
) -> None:
    """If the Go API returns 401, the tool raises; the LLM sees the error
    string and can compose a graceful answer."""
    httpx_mock.add_response(
        status_code=401,
        json={"error": "invalid API key", "code": "unauthorized"},
    )

    model = ScriptedChatModel(
        script=[
            AIMessage(
                content="",
                tool_calls=[
                    {
                        "name": "cloudoracle_cost_summary",
                        "args": {"start": "2026-04-01", "end": "2026-04-30"},
                        "id": "call-1",
                    }
                ],
            ),
            AIMessage(
                content="I couldn't fetch the data: the API rejected the key. Please check the CloudOracle API key."
            ),
        ]
    )
    tools = build_tools(client)
    graph = build_graph(model, tools)

    result = await ask(graph, "AWS spend in April?")
    assert "rejected the key" in result.answer
    await client.aclose()
