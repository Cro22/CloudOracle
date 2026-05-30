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

from pydantic import ValidationError

from insights_agent.config import Settings
from insights_agent.guardrails.runner import GuardedResult
from insights_agent.logging import get_logger, setup
from insights_agent.runtime import GeminiAgentRunner

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


async def _run(query: str, *, as_json: bool, verbose: bool) -> GuardedResult:
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

    async with GeminiAgentRunner(settings, log) as runner:
        result = await runner.ask(query)

    if result.fallback_used:
        log.warning("fallback_used", error=result.error, validation=_validation_dict(result))
    if verbose and result.tool_calls:
        print("Tool calls made:", file=sys.stderr)
        for i, call in enumerate(result.tool_calls, 1):
            print(f"  {i}. {call['name']}({call['args']})", file=sys.stderr)

    if as_json:
        print(
            json.dumps(
                {
                    "answer": result.answer,
                    "tool_calls": result.tool_calls,
                    "fallback_used": result.fallback_used,
                    "validation": _validation_dict(result),
                },
                ensure_ascii=False,
            )
        )
    else:
        print(result.answer)
    return result


def _validation_dict(result: GuardedResult) -> dict[str, Any] | None:
    v = result.validation
    if v is None:
        return None
    return {"valid": v.valid, "layer": v.layer, "reason": v.reason}


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
