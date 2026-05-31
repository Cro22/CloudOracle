"""Knowledge retrieval tool — offline via an in-memory vector store.

We avoid pgvector entirely: a `DeterministicFakeEmbedding` + `InMemoryVectorStore`
exercise the real retrieval + formatting path that the production PGVector store
would drive, without a database or network.
"""

from __future__ import annotations

import pytest
from langchain_core.documents import Document
from langchain_core.embeddings import DeterministicFakeEmbedding
from langchain_core.retrievers import BaseRetriever
from langchain_core.vectorstores import InMemoryVectorStore

from insights_agent.rag.ingest import ingest_corpus
from insights_agent.rag.store import build_retriever
from insights_agent.tools.knowledge import build_knowledge_tool


@pytest.fixture
def retriever() -> BaseRetriever:
    store = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
    # Ingest the real packaged corpus through the same code path the CLI uses.
    n = ingest_corpus(store)
    assert n >= 5
    return build_retriever(store, k=3)


class TestKnowledgeTool:
    def test_tool_name_and_description(self, retriever: BaseRetriever) -> None:
        tool = build_knowledge_tool(retriever)
        assert tool.name == "finops_knowledge_search"
        assert "FinOps" in tool.description

    async def test_returns_cited_snippets(self, retriever: BaseRetriever) -> None:
        tool = build_knowledge_tool(retriever)
        out = await tool.ainvoke({"query": "how should I rightsize idle instances?"})
        assert isinstance(out, str)
        # Each retrieved chunk is prefixed with a [source: ...] citation header.
        assert "[source:" in out
        # k=3 retriever → up to three blocks joined by the --- separator.
        assert out.count("[source:") <= 3

    async def test_no_results_message(self) -> None:
        # An empty store retrieves nothing → the friendly no-results string.
        empty = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
        tool = build_knowledge_tool(build_retriever(empty, k=3))
        out = await tool.ainvoke({"query": "anything"})
        assert "No relevant FinOps knowledge" in out

    async def test_retriever_error_becomes_observation(self) -> None:
        class BoomRetriever(BaseRetriever):
            def _get_relevant_documents(self, query: str, *, run_manager=None):  # type: ignore[no-untyped-def]
                raise RuntimeError("store down")

        tool = build_knowledge_tool(BoomRetriever())
        # handle_tool_error=True turns the ToolException into the observation
        # string instead of raising — the ReAct loop can then recover.
        out = await tool.ainvoke({"query": "x"})
        assert "knowledge search failed" in out
        assert "store down" in out


class TestIngestCorpus:
    def test_ingests_into_a_store(self) -> None:
        store = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
        count = ingest_corpus(store)
        assert count >= 5

    def test_empty_directory_adds_nothing(self, tmp_path) -> None:  # type: ignore[no-untyped-def]
        store = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
        assert ingest_corpus(store, directory=tmp_path) == 0

    def test_custom_directory_is_used(self, tmp_path) -> None:  # type: ignore[no-untyped-def]
        (tmp_path / "one.md").write_text("# One\n\nhello world", encoding="utf-8")
        store = InMemoryVectorStore(DeterministicFakeEmbedding(size=64))
        docs_added = ingest_corpus(store, directory=tmp_path)
        assert docs_added == 1
        results = store.similarity_search("hello", k=1)
        assert results and results[0].metadata["source"] == "one.md"


def test_format_documents_handles_missing_title() -> None:
    from insights_agent.tools.knowledge import _format_documents

    docs = [Document(page_content="body", metadata={"source": "x.md"})]
    out = _format_documents(docs)
    assert out == "[source: x.md]\nbody"
