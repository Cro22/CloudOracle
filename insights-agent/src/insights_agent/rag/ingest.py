"""Ingest the FinOps corpus into the pgvector store.

Two layers:

  - `ingest_corpus(store, ...)` is store-agnostic (chunk → add_documents) and
    unit-tested against an in-memory store.
  - `ingest_entrypoint` is the `insights-agent-ingest` console script: it reads
    settings, builds the Gemini embeddings + PGVector store, and calls
    `ingest_corpus`. It touches Postgres and the embeddings API, so it is not
    unit-tested (the smoke test in the README covers it end-to-end).

Run it once after `docker compose up` (with pgvector) and whenever the corpus
changes:

    uv run insights-agent-ingest            # add/refresh the corpus
    uv run insights-agent-ingest --recreate # drop the collection first
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from langchain_core.vectorstores import VectorStore

from insights_agent.rag.corpus import (
    DEFAULT_CHUNK_OVERLAP,
    DEFAULT_CHUNK_SIZE,
    load_corpus,
)


def ingest_corpus(
    store: VectorStore,
    *,
    directory: Path | None = None,
    chunk_size: int = DEFAULT_CHUNK_SIZE,
    chunk_overlap: int = DEFAULT_CHUNK_OVERLAP,
) -> int:
    """Chunk the corpus and add it to `store`. Returns the number of chunks."""
    chunks = load_corpus(
        directory, chunk_size=chunk_size, chunk_overlap=chunk_overlap
    )
    if not chunks:
        return 0
    store.add_documents(chunks)
    return len(chunks)


def ingest_entrypoint(argv: list[str] | None = None) -> int:  # pragma: no cover
    """Console-script entry point. Builds the real PGVector store and ingests."""
    parser = argparse.ArgumentParser(
        prog="insights-agent-ingest",
        description="Embed the FinOps knowledge corpus into the pgvector store.",
    )
    parser.add_argument(
        "--recreate",
        action="store_true",
        help="Drop and recreate the collection before ingesting.",
    )
    args = parser.parse_args(argv)

    # Imports are local so the module stays importable (for ingest_corpus) even
    # if optional settings/credentials aren't present in a test environment.
    from pydantic import ValidationError

    from insights_agent.config import Settings
    from insights_agent.logging import get_logger, setup
    from insights_agent.rag.embeddings import GeminiEmbeddingsProvider
    from insights_agent.rag.store import build_vector_store

    try:
        settings = Settings()  # type: ignore[call-arg]
    except ValidationError as e:
        print(f"Configuration error:\n{e}", file=sys.stderr)
        return 2

    if not settings.database_url:
        print(
            "DATABASE_URL is not set — RAG ingestion needs a pgvector-enabled "
            "Postgres. See insights-agent/README.md.",
            file=sys.stderr,
        )
        return 2

    setup(level=settings.log_level, fmt=settings.log_format)
    log = get_logger("insights_agent.rag.ingest")

    embeddings = GeminiEmbeddingsProvider(
        api_key=settings.gemini_api_key,
        model=settings.embeddings_model,
    ).get_embeddings()
    store = build_vector_store(
        connection=settings.database_url,
        embeddings=embeddings,
        collection=settings.knowledge_collection,
    )
    if args.recreate:
        store.drop_tables()
        store.create_tables_if_not_exists()
        log.info("rag.collection_recreated", collection=settings.knowledge_collection)

    count = ingest_corpus(store)
    log.info("rag.ingested", chunks=count, collection=settings.knowledge_collection)
    print(f"Ingested {count} chunks into '{settings.knowledge_collection}'.")
    return 0


if __name__ == "__main__":  # pragma: no cover
    sys.exit(ingest_entrypoint())
