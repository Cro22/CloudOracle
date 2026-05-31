"""Gemini implementation of LLMProvider.

Defaults to `gemini-2.5-flash` to match the Go side (`internal/llm`). Keeping
both languages on the same model avoids drift when comparing dashboard
narratives (Go) against agent answers (Python) on the same period.
"""

from __future__ import annotations

from functools import cached_property

from langchain_core.language_models import BaseChatModel
from langchain_google_genai import ChatGoogleGenerativeAI

from insights_agent.llm.base import LLMProvider


class GeminiProvider(LLMProvider):
    def __init__(
        self,
        *,
        api_key: str,
        model: str = "gemini-2.5-flash",
        temperature: float = 0.2,
    ) -> None:
        if not api_key:
            raise ValueError("GeminiProvider requires a non-empty api_key")
        self._api_key = api_key
        self._model = model
        self._temperature = temperature

    @cached_property
    def _chat(self) -> ChatGoogleGenerativeAI:
        return ChatGoogleGenerativeAI(
            model=self._model,
            google_api_key=self._api_key,
            temperature=self._temperature,
        )

    def get_chat_model(self) -> BaseChatModel:
        return self._chat

    @property
    def provider_name(self) -> str:
        return "gemini"

    @property
    def model_name(self) -> str:
        return self._model
