"""structlog wiring that mirrors the Go side's slog output.

Go uses `slog.NewTextHandler` / `slog.NewJSONHandler` against stderr with
key=value (text) or JSON-per-line (json) shapes. We match: same stream, same
two formats, same `LOG_LEVEL`/`LOG_FORMAT` semantics. That way a combined
stderr tail of the Python CLI and the Go server reads coherently when both
are debugged together.
"""

from __future__ import annotations

import logging
import sys
from typing import Any, cast

import structlog
from structlog.typing import Processor


def setup(level: str = "INFO", fmt: str = "text") -> None:
    """Wire structlog + stdlib logging.

    Idempotent: re-calling overrides the previous configuration, which keeps
    tests that build per-test settings simple.
    """
    log_level = getattr(logging, level.upper(), logging.INFO)

    logging.basicConfig(
        format="%(message)s",
        stream=sys.stderr,
        level=log_level,
        force=True,
    )

    shared_processors: list[Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso", utc=True),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
    ]

    renderer: Processor
    if fmt == "json":
        renderer = structlog.processors.JSONRenderer()
    else:
        renderer = structlog.dev.ConsoleRenderer(colors=sys.stderr.isatty())

    structlog.configure(
        processors=[*shared_processors, renderer],
        wrapper_class=structlog.make_filtering_bound_logger(log_level),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(file=sys.stderr),
        cache_logger_on_first_use=True,
    )


def get_logger(name: str | None = None, **initial_values: Any) -> structlog.stdlib.BoundLogger:
    """Return a logger optionally bound to initial context (e.g. request_id).

    `structlog.get_logger` is typed as `Any`, so we cast the result to keep
    the return type informative for callers (autocomplete on `.info`, etc.).
    """
    log = structlog.get_logger(name) if name else structlog.get_logger()
    if initial_values:
        log = log.bind(**initial_values)
    return cast(structlog.stdlib.BoundLogger, log)
