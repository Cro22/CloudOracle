"""Shared agent runtime used by both the CLI and the HTTP surface.

`GeminiAgentRunner` assembles the whole stack once — Gemini model, HTTP client,
tools (incl. the optional RAG tool), the supervisor graph and the run limits —
and exposes a single `ask()` that runs a query through the guardrails. Centralizing
it here keeps `main.py` (CLI) and `api/app.py` (HTTP) thin and identical in
behavior.
"""

from __future__ import annotations

from typing import Any

from langchain_core.tools import BaseTool

from insights_agent.config import Settings
from insights_agent.graph.supervisor import build_supervisor_graph
from insights_agent.guardrails.runner import GuardedResult, run_guarded
from insights_agent.llm import GeminiProvider
from insights_agent.tools.cloudoracle import CloudOracleClient, build_tools


def maybe_build_knowledge_tool(settings: Settings, log: Any) -> BaseTool | None:
    """Build the RAG knowledge tool when a pgvector DB is configured.

    Returns None (and logs why) when database_url is unset, so the agent runs
    with just the cost/inventory/recommendation tools and no DB dependency.
    Imports are deferred so the heavier RAG/db stack is only loaded when used.
    """
    if not settings.database_url:
        log.info("rag.disabled", reason="database_url not set")
        return None

    from insights_agent.rag.embeddings import GeminiEmbeddingsProvider
    from insights_agent.rag.store import build_retriever, build_vector_store
    from insights_agent.tools.knowledge import build_knowledge_tool

    embeddings = GeminiEmbeddingsProvider(
        api_key=settings.gemini_api_key,
        model=settings.embeddings_model,
    ).get_embeddings()
    store = build_vector_store(
        connection=settings.database_url,
        embeddings=embeddings,
        collection=settings.knowledge_collection,
    )
    retriever = build_retriever(store, k=settings.rag_top_k)
    log.info(
        "rag.enabled",
        collection=settings.knowledge_collection,
        embeddings_model=settings.embeddings_model,
        top_k=settings.rag_top_k,
    )
    return build_knowledge_tool(retriever)


class GeminiAgentRunner:
    """Owns the assembled agent and runs guarded queries. Async-closeable."""

    def __init__(self, settings: Settings, log: Any) -> None:
        self._settings = settings
        self._chat = GeminiProvider(
            api_key=settings.gemini_api_key,
            model=settings.gemini_model,
        ).get_chat_model()
        self._client = CloudOracleClient(
            base_url=settings.cloudoracle_base_url,
            api_key=settings.cloudoracle_api_key,
            timeout_seconds=settings.http_timeout_seconds,
        )
        tools: list[BaseTool] = list(build_tools(self._client))
        knowledge_tool = maybe_build_knowledge_tool(settings, log)
        if knowledge_tool is not None:
            tools.append(knowledge_tool)
        self._graph = build_supervisor_graph(self._chat, tools, settings.run_limits)

    async def ask(self, query: str) -> GuardedResult:
        return await run_guarded(
            self._graph,
            query,
            validate=self._settings.enable_answer_validation,
            judge_model=self._chat if self._settings.enable_llm_judge else None,
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> GeminiAgentRunner:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()
