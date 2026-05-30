"""pgvector-backed vector store wiring.

`build_vector_store` is the only place that talks to Postgres; it's a thin
assembly of a `langchain_postgres.PGVector` and is exercised by the smoke /
integration path, not unit tests (it opens a real connection). `build_retriever`
is store-agnostic and unit-tested against an in-memory store.
"""

from __future__ import annotations

from langchain_core.embeddings import Embeddings
from langchain_core.vectorstores import VectorStore, VectorStoreRetriever
from langchain_postgres import PGVector


def build_vector_store(
    *,
    connection: str,
    embeddings: Embeddings,
    collection: str,
) -> PGVector:  # pragma: no cover - opens a real DB connection
    """Construct a PGVector store over the CloudOracle Postgres.

    `connection` is a SQLAlchemy/psycopg URL, e.g.
    `postgresql+psycopg://oracle:oracle_dev@localhost:5432/cloudoracle`.
    `use_jsonb=True` stores chunk metadata as JSONB so it can be filtered on.
    """
    return PGVector(
        embeddings=embeddings,
        collection_name=collection,
        connection=connection,
        use_jsonb=True,
    )


def build_retriever(store: VectorStore, *, k: int = 4) -> VectorStoreRetriever:
    """Wrap any vector store as a top-k similarity retriever."""
    return store.as_retriever(search_kwargs={"k": k})
