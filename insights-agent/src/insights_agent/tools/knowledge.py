"""RAG retrieval tool over the FinOps knowledge corpus.

`build_knowledge_tool(retriever)` wraps any LangChain retriever as a
`finops_knowledge_search` tool. The agent calls it for conceptual / policy /
how-to questions (vs. the cloudoracle_* tools, which fetch numbers). Results
are formatted with `[source: <file> — <title>]` headers so the model can cite
where guidance came from.

The tool returns formatted text (not structured data) because retrieved context
is meant to be read and synthesized by the model, then cited in the answer.
"""

from __future__ import annotations

from langchain_core.documents import Document
from langchain_core.retrievers import BaseRetriever
from langchain_core.tools import StructuredTool, ToolException

_NO_RESULTS = "No relevant FinOps knowledge was found for that query."


def build_knowledge_tool(retriever: BaseRetriever) -> StructuredTool:
    async def _search(query: str) -> str:
        try:
            docs = await retriever.ainvoke(query)
        except Exception as e:  # surface any retriever failure to the model
            # ToolException is caught by the ReAct loop and shown to the model
            # as an observation, so a transient store error doesn't abort the
            # whole run — the agent can answer from its own knowledge instead.
            raise ToolException(f"knowledge search failed: {e}") from e
        if not docs:
            return _NO_RESULTS
        return _format_documents(docs)

    return StructuredTool.from_function(
        coroutine=_search,
        name="finops_knowledge_search",
        description=_KNOWLEDGE_DESC,
        handle_tool_error=True,
    )


def _format_documents(docs: list[Document]) -> str:
    blocks: list[str] = []
    for doc in docs:
        source = doc.metadata.get("source", "unknown")
        title = doc.metadata.get("title")
        header = f"[source: {source}" + (f" — {title}]" if title else "]")
        blocks.append(f"{header}\n{doc.page_content.strip()}")
    return "\n\n---\n\n".join(blocks)


_KNOWLEDGE_DESC = """Search CloudOracle's curated FinOps knowledge base for guidance and definitions.

Use this for conceptual, policy, how-to, or "what does X mean?" questions —
e.g. "what is rightsizing?", "should I buy reserved instances?", "how accurate
are these cost numbers?", "explain showback vs chargeback", "what does
data_source mean?". Do NOT use it to fetch a user's actual numbers — the
cloudoracle_cost_summary / cost_by_service / cost_trends / inventory /
recommendations tools do that.

Args:
    query: A natural-language question or topic to look up.

Returns:
    Relevant excerpts from the knowledge base, each prefixed with a
    `[source: <file> — <title>]` header, separated by `---`. If nothing
    matches, a short "no results" message.

When you use an excerpt in your answer, briefly cite the source (e.g. "per the
rightsizing guide"). If the excerpts don't cover the question, say so rather
than inventing FinOps guidance."""
