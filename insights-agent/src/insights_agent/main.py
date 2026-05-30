"""CLI entry point.

Single-turn agent run: read a question from argv, build the graph, print the
answer to stdout. Logs go to stderr so callers can pipe the answer cleanly
into other tools.

Exit codes:
  0   success
  1   unexpected runtime failure
  2   configuration problem (missing env var, bad URL, etc.)
  130 user-cancelled (Ctrl-C)

Two output modes:
  default        natural-language answer on stdout
  --json         {"answer": "...", "tool_calls": [...]} on stdout
  --verbose      additionally streams tool-call summary to stderr
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from typing import Any

from langchain_core.tools import BaseTool
from pydantic import ValidationError

from insights_agent.config import Settings
from insights_agent.graph.basic import AgentResult
from insights_agent.graph.supervisor import ask_supervisor, build_supervisor_graph
from insights_agent.llm import GeminiProvider
from insights_agent.logging import get_logger, setup
from insights_agent.tools.cloudoracle import CloudOracleClient, build_tools

EXIT_OK = 0
EXIT_RUNTIME = 1
EXIT_CONFIG = 2
EXIT_INTERRUPTED = 130


def _build_arg_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog="insights-agent",
        description=(
            "Ask CloudOracle's FinOps agent a question. The agent will pick the "
            "right /api/v1 tool calls against your running CloudOracle Go server "
            "and answer in natural language."
        ),
    )
    p.add_argument("query", help="The question to ask, in quotes.")
    p.add_argument(
        "--verbose",
        action="store_true",
        help="Print the tool calls the agent made to stderr.",
    )
    p.add_argument(
        "--json",
        dest="as_json",
        action="store_true",
        help="Print structured JSON instead of natural-language answer.",
    )
    return p


def _maybe_build_knowledge_tool(settings: Settings, log: Any) -> BaseTool | None:
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


async def _run(query: str, *, as_json: bool, verbose: bool) -> AgentResult:
    # pydantic-settings populates required fields from the environment;
    # mypy's call-arg check doesn't understand env-based construction
    # without the pydantic plugin, so we silence it locally.
    settings = Settings()  # type: ignore[call-arg]  # may raise ValidationError
    setup(level=settings.log_level, fmt=settings.log_format)
    log = get_logger("insights_agent.main")
    log.info(
        "starting",
        provider="gemini",
        model=settings.gemini_model,
        base_url=settings.cloudoracle_base_url,
    )

    provider = GeminiProvider(
        api_key=settings.gemini_api_key,
        model=settings.gemini_model,
    )
    async with CloudOracleClient(
        base_url=settings.cloudoracle_base_url,
        api_key=settings.cloudoracle_api_key,
        timeout_seconds=settings.http_timeout_seconds,
    ) as client:
        tools: list[BaseTool] = list(build_tools(client))
        knowledge_tool = _maybe_build_knowledge_tool(settings, log)
        if knowledge_tool is not None:
            tools.append(knowledge_tool)
        graph = build_supervisor_graph(provider.get_chat_model(), tools)
        result = await ask_supervisor(graph, query)

    if verbose and result.tool_calls:
        print("Tool calls made:", file=sys.stderr)
        for i, call in enumerate(result.tool_calls, 1):
            print(f"  {i}. {call['name']}({call['args']})", file=sys.stderr)

    if as_json:
        print(json.dumps({"answer": result.answer, "tool_calls": result.tool_calls}, ensure_ascii=False))
    else:
        print(result.answer)
    return result


def cli_entrypoint(argv: list[str] | None = None) -> int:
    args = _build_arg_parser().parse_args(argv)
    try:
        asyncio.run(_run(args.query, as_json=args.as_json, verbose=args.verbose))
        return EXIT_OK
    except ValidationError as e:
        # We don't have a configured logger yet at this point — Settings()
        # failure is what would have configured it. Fall back to stderr so
        # the operator sees what was missing.
        print(f"Configuration error:\n{e}", file=sys.stderr)
        print(
            "\nCheck your .env or environment for "
            "GEMINI_API_KEY, CLOUDORACLE_API_URL, CLOUDORACLE_API_KEY.",
            file=sys.stderr,
        )
        return EXIT_CONFIG
    except KeyboardInterrupt:
        print("Interrupted.", file=sys.stderr)
        return EXIT_INTERRUPTED
    except Exception as e:
        # Top-level catch is intentional: this is the CLI boundary, anything
        # not handled by the inner code is a runtime failure we want to
        # report cleanly and exit non-zero instead of dumping a Python
        # traceback to the operator.
        print(f"Error: {e}", file=sys.stderr)
        return EXIT_RUNTIME


if __name__ == "__main__":
    sys.exit(cli_entrypoint())
