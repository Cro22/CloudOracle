"""CLI surface tests.

We don't drive the full agent here — `test_graph.py` already covers the
async pipeline. These tests target only the CLI shell: arg parsing,
config-error exit code, formatting of --verbose / --json output.
"""

from __future__ import annotations

from typing import Any

import pytest

from insights_agent.config import Settings
from insights_agent.graph.basic import AgentResult
from insights_agent.main import (
    EXIT_CONFIG,
    EXIT_INTERRUPTED,
    EXIT_OK,
    EXIT_RUNTIME,
    _build_arg_parser,
    cli_entrypoint,
)
from insights_agent.runtime import maybe_build_knowledge_tool


def test_arg_parser_requires_query() -> None:
    p = _build_arg_parser()
    with pytest.raises(SystemExit):
        p.parse_args([])


def test_arg_parser_default_flags() -> None:
    args = _build_arg_parser().parse_args(["hello"])
    assert args.query == "hello"
    assert args.verbose is False
    assert args.as_json is False


def test_arg_parser_flags_parsed() -> None:
    args = _build_arg_parser().parse_args(["q", "--verbose", "--json"])
    assert args.verbose is True
    assert args.as_json is True


def test_cli_missing_config_returns_2(
    capsys: pytest.CaptureFixture[str],
) -> None:
    # `_isolate_settings_env` (autouse) stripped the required env vars, so
    # Settings() will fail. We exercise the top-level error handler.
    rc = cli_entrypoint(["test query"])
    assert rc == EXIT_CONFIG
    err = capsys.readouterr().err
    assert "Configuration error" in err
    assert "GEMINI_API_KEY" in err


def test_cli_happy_path_prints_answer(
    valid_env: None,
    capsys: pytest.CaptureFixture[str],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Patch the _run coroutine to skip the LLM/HTTP layer."""

    async def fake_run(query: str, *, as_json: bool, verbose: bool) -> AgentResult:
        result = AgentResult(
            answer="$150 in AWS",
            tool_calls=[
                {"name": "cloudoracle_cost_summary", "args": {"start": "x", "end": "y"}}
            ],
        )
        if verbose:
            import sys

            print(f"Tool calls made: {len(result.tool_calls)}", file=sys.stderr)
        if as_json:
            import json
            import sys

            print(
                json.dumps({"answer": result.answer, "tool_calls": result.tool_calls}),
                file=sys.stdout,
            )
        else:
            print(result.answer)
        return result

    monkeypatch.setattr("insights_agent.main._run", fake_run)
    rc = cli_entrypoint(["What did I spend on AWS?"])
    out = capsys.readouterr()
    assert rc == EXIT_OK
    assert "$150 in AWS" in out.out


def test_cli_runtime_error_returns_1(
    valid_env: None,
    capsys: pytest.CaptureFixture[str],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    async def boom(*_: Any, **__: Any) -> AgentResult:
        raise RuntimeError("kaboom")

    monkeypatch.setattr("insights_agent.main._run", boom)
    rc = cli_entrypoint(["q"])
    assert rc == EXIT_RUNTIME
    assert "kaboom" in capsys.readouterr().err


class _SpyLog:
    def __init__(self) -> None:
        self.events: list[str] = []

    def info(self, event: str, **_: Any) -> None:
        self.events.append(event)


def test_knowledge_tool_disabled_without_database_url(valid_env: None) -> None:
    # No DATABASE_URL → the RAG tool is skipped and the reason is logged, so
    # the agent runs with just the cost/inventory/recommendation tools.
    settings = Settings()
    log = _SpyLog()
    tool = maybe_build_knowledge_tool(settings, log)
    assert tool is None
    assert "rag.disabled" in log.events


def test_cli_interrupt_returns_130(
    valid_env: None,
    capsys: pytest.CaptureFixture[str],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    async def interrupt(*_: Any, **__: Any) -> AgentResult:
        raise KeyboardInterrupt

    monkeypatch.setattr("insights_agent.main._run", interrupt)
    rc = cli_entrypoint(["q"])
    assert rc == EXIT_INTERRUPTED
    assert "Interrupted" in capsys.readouterr().err
