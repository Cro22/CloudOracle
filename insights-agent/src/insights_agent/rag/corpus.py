"""Load and chunk the FinOps knowledge corpus into LangChain documents.

The corpus is a set of curated markdown notes shipped inside the package
(`insights_agent/knowledge/*.md`). This module is deliberately free of any
embedding / database concern so the chunking logic is unit-testable offline —
the ingestion CLI (`rag.ingest`) layers the vector store on top.

Each source file becomes one or more `Document` chunks carrying `source`
(the file name) and `title` (the first H1) metadata, which the knowledge tool
turns into inline citations.
"""

from __future__ import annotations

from importlib import resources
from pathlib import Path

from langchain_core.documents import Document
from langchain_text_splitters import RecursiveCharacterTextSplitter

KNOWLEDGE_PACKAGE = "insights_agent.knowledge"

DEFAULT_CHUNK_SIZE = 1000
DEFAULT_CHUNK_OVERLAP = 150

# Split on markdown structure first (headings, then paragraphs) so a chunk
# tends to be a coherent section rather than an arbitrary character window.
_SEPARATORS = ["\n## ", "\n### ", "\n\n", "\n", " ", ""]


def _read_sources(directory: Path | None) -> list[tuple[str, str]]:
    """Return (file_name, text) for every markdown file in the corpus.

    With no directory, read the packaged corpus via importlib.resources so it
    works from an installed wheel regardless of CWD. A directory override is
    used by tests and by anyone pointing the ingester at a custom corpus.
    """
    out: list[tuple[str, str]] = []
    if directory is not None:
        for path in sorted(directory.glob("*.md")):
            out.append((path.name, path.read_text(encoding="utf-8")))
        return out

    for entry in sorted(
        resources.files(KNOWLEDGE_PACKAGE).iterdir(), key=lambda p: p.name
    ):
        if entry.name.endswith(".md") and entry.is_file():
            out.append((entry.name, entry.read_text(encoding="utf-8")))
    return out


def _first_heading(text: str) -> str | None:
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("# "):
            return stripped[2:].strip()
    return None


def load_markdown_documents(directory: Path | None = None) -> list[Document]:
    """Load each corpus file as one whole (un-chunked) Document with metadata."""
    docs: list[Document] = []
    for name, text in _read_sources(directory):
        if not text.strip():
            continue
        title = _first_heading(text) or Path(name).stem
        docs.append(
            Document(page_content=text, metadata={"source": name, "title": title})
        )
    return docs


def chunk_documents(
    docs: list[Document],
    *,
    chunk_size: int = DEFAULT_CHUNK_SIZE,
    chunk_overlap: int = DEFAULT_CHUNK_OVERLAP,
) -> list[Document]:
    """Split whole documents into overlapping chunks, preserving metadata."""
    splitter = RecursiveCharacterTextSplitter(
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
        separators=_SEPARATORS,
    )
    return splitter.split_documents(docs)


def load_corpus(
    directory: Path | None = None,
    *,
    chunk_size: int = DEFAULT_CHUNK_SIZE,
    chunk_overlap: int = DEFAULT_CHUNK_OVERLAP,
) -> list[Document]:
    """Load + chunk the corpus in one call — the ingester's entry point."""
    return chunk_documents(
        load_markdown_documents(directory),
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
    )
