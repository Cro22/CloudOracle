from __future__ import annotations

import logging

import structlog

from insights_agent.logging import get_logger, setup


def test_setup_text_format_does_not_raise() -> None:
    setup(level="DEBUG", fmt="text")
    log = get_logger("test")
    log.info("hello", key="value")


def test_setup_json_format_does_not_raise() -> None:
    setup(level="INFO", fmt="json")
    log = get_logger("test")
    log.warning("careful", n=1)


def test_get_logger_binds_initial_values() -> None:
    setup(level="INFO", fmt="text")
    log = get_logger("test", request_id="abc123")
    bound = structlog.get_context(log)
    assert bound.get("request_id") == "abc123"


def test_setup_is_idempotent() -> None:
    setup(level="INFO", fmt="text")
    setup(level="DEBUG", fmt="json")
    assert logging.getLogger().level == logging.DEBUG
