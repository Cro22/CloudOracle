from __future__ import annotations

from unittest.mock import patch

import pytest
from langchain_core.language_models import BaseChatModel

from insights_agent.llm import GeminiProvider, LLMProvider


def test_provider_implements_abc() -> None:
    p = GeminiProvider(api_key="k", model="gemini-2.5-flash")
    assert isinstance(p, LLMProvider)


def test_metadata_properties() -> None:
    p = GeminiProvider(api_key="k", model="gemini-2.5-flash")
    assert p.provider_name == "gemini"
    assert p.model_name == "gemini-2.5-flash"


def test_custom_model_name() -> None:
    p = GeminiProvider(api_key="k", model="gemini-2.5-pro")
    assert p.model_name == "gemini-2.5-pro"


def test_empty_api_key_rejected() -> None:
    with pytest.raises(ValueError, match="non-empty api_key"):
        GeminiProvider(api_key="")


def test_get_chat_model_constructs_chatgooglegenerativeai() -> None:
    """Verify we pass the configured params to the LangChain wrapper.

    We patch the class to avoid the real SDK doing credential discovery —
    even with a fake key, instantiation can poke at the file system or env.
    """
    with patch("insights_agent.llm.gemini.ChatGoogleGenerativeAI") as mock_cls:
        instance = mock_cls.return_value
        # Mark the mock as a BaseChatModel so callers' isinstance checks pass.
        mock_cls.return_value.__class__ = BaseChatModel  # type: ignore[misc]
        p = GeminiProvider(api_key="k123", model="gemini-2.5-flash", temperature=0.5)
        got = p.get_chat_model()
        assert got is instance
        mock_cls.assert_called_once_with(
            model="gemini-2.5-flash",
            google_api_key="k123",
            temperature=0.5,
        )


def test_get_chat_model_is_cached() -> None:
    with patch("insights_agent.llm.gemini.ChatGoogleGenerativeAI") as mock_cls:
        p = GeminiProvider(api_key="k")
        first = p.get_chat_model()
        second = p.get_chat_model()
        assert first is second
        # The expensive wrapper is built exactly once.
        assert mock_cls.call_count == 1
