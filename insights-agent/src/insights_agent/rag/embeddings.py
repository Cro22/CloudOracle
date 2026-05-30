"""Embeddings provider abstraction, mirroring the LLM provider pattern.

Adding another embeddings backend (OpenAI, a local model) later is purely
additive: implement `EmbeddingsProvider` and select it in the wiring code.
Gemini is the default so the agent stays on a single vendor / free tier for
both generation and retrieval.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from functools import cached_property

from langchain_core.embeddings import Embeddings
from langchain_google_genai import GoogleGenerativeAIEmbeddings

# Gemini's general-purpose text embedding model. 768 dimensions, covered by the
# free tier — same account/key as the chat model.
DEFAULT_EMBEDDINGS_MODEL = "models/text-embedding-004"


class EmbeddingsProvider(ABC):
    """Vendor-agnostic embeddings factory."""

    @abstractmethod
    def get_embeddings(self) -> Embeddings:
        """Return a LangChain-compatible Embeddings object."""

    @property
    @abstractmethod
    def model_name(self) -> str:
        """Current embeddings model id (e.g. 'models/text-embedding-004')."""


class GeminiEmbeddingsProvider(EmbeddingsProvider):
    def __init__(
        self,
        *,
        api_key: str,
        model: str = DEFAULT_EMBEDDINGS_MODEL,
    ) -> None:
        if not api_key:
            raise ValueError("GeminiEmbeddingsProvider requires a non-empty api_key")
        self._api_key = api_key
        self._model = model

    @cached_property
    def _embeddings(self) -> GoogleGenerativeAIEmbeddings:
        # google_api_key is a valid pydantic field (accepted at runtime via the
        # model's **data init) but mypy reads the typed signature and doesn't
        # see it — same accommodation the codebase makes for env-based pydantic.
        return GoogleGenerativeAIEmbeddings(
            model=self._model,
            google_api_key=self._api_key,  # type: ignore[call-arg]
        )

    def get_embeddings(self) -> Embeddings:
        return self._embeddings

    @property
    def model_name(self) -> str:
        return self._model
