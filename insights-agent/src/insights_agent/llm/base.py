"""Abstract LLM provider used by the agent graph.

Designed so that adding AnthropicProvider / OpenAIProvider later is purely
additive: implement this ABC, register in a selector in `main.py`. No graph
changes required.
"""

from __future__ import annotations

from abc import ABC, abstractmethod

from langchain_core.language_models import BaseChatModel


class LLMProvider(ABC):
    """Vendor-agnostic chat-model factory."""

    @abstractmethod
    def get_chat_model(self) -> BaseChatModel:
        """Return a LangChain-compatible chat model bound to this provider."""

    @property
    @abstractmethod
    def provider_name(self) -> str:
        """Short identifier for logs and observability (e.g. 'gemini')."""

    @property
    @abstractmethod
    def model_name(self) -> str:
        """Current model id (e.g. 'gemini-2.5-flash')."""
