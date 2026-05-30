"""Tests for the hand-rolled supervisor graph (graph/supervisor.py).

Driven by the same scripted-fake-model approach as test_graph.py: one shared
script is consumed in node-execution order — supervisor routes, workers run
their ReAct loop, supervisor routes again, then synthesize. No Gemini, no DB.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any

import pytest
from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.embeddings import DeterministicFakeEmbedding
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.tools import BaseTool
from langchain_core.vectorstores import InMemoryVectorStore
from pydantic import Field
from pytest_httpx import HTTPXMock

from insights_agent.graph import supervisor as sup
from insights_agent.graph.supervisor import (
    COST_ANALYST,
    FINISH,
    _run_react,
    _to_text,
    ask_supervisor,
    build_supervisor_graph,
)
from insights_agent.rag.ingest import ingest_corpus
from insights_agent.rag.store import build_retriever
from insights_agent.tools.cloudoracle import CloudOracleClient, build_tools
from insights_agent.tools.knowledge import build_knowledge_tool

BASE_URL = "http://localhost:8080"
API_KEY = "test-key"

SUMMARY_PAYLOAD: dict[str, Any] = {
    "period": {"start": "2026-04-01", "end": "2026-04-30"},
    "providers": {"aws": {"total_usd": 150.0, "currency": "USD"}},
    "grand_total_usd": 150.0,
    "generated_at": "2026-05-18T12:00:00Z",
    "data_source": "snapshots_approximation",
    "note": "approximation note",
}


class ScriptedChatModel(BaseChatModel):
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
            raise RuntimeError("ScriptedChatModel exhausted")
        return ChatResult(generations=[ChatGeneration(message=self.script.pop(0))])

    async def _agenerate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: Any = None,
        **kwargs: Any,
    ) -> ChatResult:
        return self._generate(messages, stop, run_manager, **kwargs)


def _route(name: str) -> AIMessage:
    return AIMessage(content="", tool_calls=[{"name": name, "args": {}, "id": f"r-{name}"}])


def _call(name: str, args: dict[str, Any], cid: str = "c1") -> AIMessage:
    return AIMessage(content="", tool_calls=[{"name": name, "args": args, "id": cid}])


def _say(text: str) -> AIMessage:
    return AIMessage(content=text)


@pytest.fixture
def client() -> CloudOracleClient:
    return CloudOracleClient(base_url=BASE_URL, api_key=API_KEY, timeout_seconds=2.0)


def _knowledge_tool() -> BaseTool:
    store = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
    ingest_corpus(store)
    return build_knowledge_tool(build_retriever(store, k=2))


async def test_routes_to_cost_analyst_then_synthesizes(
    client: CloudOracleClient, httpx_mock: HTTPXMock
) -> None:
    httpx_mock.add_response(json=SUMMARY_PAYLOAD)
    model = ScriptedChatModel(
        script=[
            _route(COST_ANALYST),
            _call("cloudoracle_cost_summary", {"start": "2026-04-01", "end": "2026-04-30"}),
            _say("Found ~$150 on AWS (snapshots_approximation)."),
            _route(FINISH),
            _say("You spent about $150 on AWS in April 2026 — a snapshot approximation, not the final bill."),
        ]
    )
    graph = build_supervisor_graph(model, build_tools(client))

    result = await ask_supervisor(graph, "How much did I spend on AWS in April 2026?")

    assert [c["name"] for c in result.tool_calls] == ["cloudoracle_cost_summary"]
    assert result.tool_calls[0]["args"] == {"start": "2026-04-01", "end": "2026-04-30"}
    assert "$150" in result.answer
    assert "snapshot" in result.answer.lower()
    # The cost_analyst worker contributed a named message to the transcript.
    assert any(getattr(m, "name", None) == COST_ANALYST for m in result.messages)
    await client.aclose()


async def test_routes_across_two_specialists(
    client: CloudOracleClient, httpx_mock: HTTPXMock
) -> None:
    httpx_mock.add_response(json=SUMMARY_PAYLOAD)
    tools = [*build_tools(client), _knowledge_tool()]
    model = ScriptedChatModel(
        script=[
            _route(COST_ANALYST),
            _call("cloudoracle_cost_summary", {"start": "2026-04-01", "end": "2026-04-30"}),
            _say("AWS ~$150 in April."),
            _route("concept_expert"),
            _call("finops_knowledge_search", {"query": "rightsizing"}),
            _say("Rightsizing matches capacity to demand (per the rightsizing guide)."),
            _route(FINISH),
            _say("You spent ~$150 on AWS; rightsizing means matching capacity to demand."),
        ]
    )
    graph = build_supervisor_graph(model, tools)

    result = await ask_supervisor(graph, "What did I spend on AWS and what is rightsizing?")

    assert [c["name"] for c in result.tool_calls] == [
        "cloudoracle_cost_summary",
        "finops_knowledge_search",
    ]
    assert "$150" in result.answer
    assert "rightsizing" in result.answer.lower()
    await client.aclose()


async def test_offscope_finishes_without_a_worker(client: CloudOracleClient) -> None:
    model = ScriptedChatModel(
        script=[
            _route(FINISH),
            _say("I only help with cloud cost and FinOps questions."),
        ]
    )
    graph = build_supervisor_graph(model, build_tools(client))

    result = await ask_supervisor(graph, "What's the weather today?")

    assert result.tool_calls == []
    assert "FinOps" in result.answer or "cloud cost" in result.answer
    await client.aclose()


async def test_hop_cap_forces_synthesis(
    client: CloudOracleClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    # Supervisor that never says finish: always routes to cost_analyst, whose
    # worker answers without a tool. The hop cap must end the loop at synthesis.
    monkeypatch.setattr(sup, "MAX_HOPS", 2)
    model = ScriptedChatModel(
        script=[
            _route(COST_ANALYST),
            _say("partial 1"),
            _route(COST_ANALYST),
            _say("partial 2"),
            _route(COST_ANALYST),  # hops becomes 3 > 2 → decide() goes to synthesize
            _say("final synthesized answer"),
        ]
    )
    graph = build_supervisor_graph(model, build_tools(client))

    result = await ask_supervisor(graph, "loop forever?")
    assert result.answer == "final synthesized answer"
    await client.aclose()


class TestRunReact:
    async def test_unknown_tool_becomes_observation(self) -> None:
        model = ScriptedChatModel(
            script=[_call("nope", {}), _say("done after observing the error")]
        )
        answer, calls = await _run_react(model, [], "system", [])
        assert answer == "done after observing the error"
        assert calls == [{"name": "nope", "args": {}}]

    async def test_direct_answer_without_tools(self) -> None:
        model = ScriptedChatModel(script=[_say("just an answer")])
        answer, calls = await _run_react(model, [], "system", [])
        assert answer == "just an answer"
        assert calls == []


class TestToText:
    def test_passthrough_string(self) -> None:
        assert _to_text("hi") == "hi"

    def test_dict_to_json(self) -> None:
        assert _to_text({"a": 1}) == '{"a": 1}'

    def test_non_serializable_falls_back_to_str(self) -> None:
        assert _to_text({1, 2}) in ("{1, 2}", "{2, 1}")
