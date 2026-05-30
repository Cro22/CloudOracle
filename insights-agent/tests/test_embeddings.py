"""Embeddings provider — construction only, no network calls."""

from __future__ import annotations

import pytest
from langchain_core.embeddings import Embeddings

from insights_agent.rag.embeddings import (
    DEFAULT_EMBEDDINGS_MODEL,
    GeminiEmbeddingsProvider,
)


def test_rejects_empty_api_key() -> None:
    with pytest.raises(ValueError, match="api_key"):
        GeminiEmbeddingsProvider(api_key="")


def test_model_name_defaults() -> None:
    p = GeminiEmbeddingsProvider(api_key="k")
    assert p.model_name == DEFAULT_EMBEDDINGS_MODEL


def test_model_name_override() -> None:
    p = GeminiEmbeddingsProvider(api_key="k", model="models/custom")
    assert p.model_name == "models/custom"


def test_get_embeddings_returns_embeddings_object() -> None:
    p = GeminiEmbeddingsProvider(api_key="k")
    emb = p.get_embeddings()
    assert isinstance(emb, Embeddings)
    # cached_property: the same object is returned on repeat access.
    assert p.get_embeddings() is emb
