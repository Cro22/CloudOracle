"""Retrieval-augmented generation over the FinOps knowledge corpus."""

from insights_agent.rag.corpus import load_corpus, load_markdown_documents
from insights_agent.rag.embeddings import (
    EmbeddingsProvider,
    GeminiEmbeddingsProvider,
)
from insights_agent.rag.store import build_retriever, build_vector_store

__all__ = [
    "EmbeddingsProvider",
    "GeminiEmbeddingsProvider",
    "build_retriever",
    "build_vector_store",
    "load_corpus",
    "load_markdown_documents",
]
