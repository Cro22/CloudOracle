"""Corpus loading + chunking — fully offline (no embeddings, no DB)."""

from __future__ import annotations

from pathlib import Path

from insights_agent.rag.corpus import (
    chunk_documents,
    load_corpus,
    load_markdown_documents,
)


class TestPackagedCorpus:
    def test_loads_the_seed_corpus(self) -> None:
        docs = load_markdown_documents()
        # The five seed notes shipped with the package.
        names = {d.metadata["source"] for d in docs}
        assert names == {
            "rightsizing.md",
            "commitment-discounts.md",
            "data-sources-and-caveats.md",
            "cost-allocation-and-tagging.md",
            "finops-glossary.md",
        }

    def test_title_metadata_is_the_h1(self) -> None:
        docs = load_markdown_documents()
        by_source = {d.metadata["source"]: d for d in docs}
        assert by_source["rightsizing.md"].metadata["title"] == "Rightsizing cloud resources"

    def test_chunking_preserves_metadata_and_splits(self) -> None:
        chunks = load_corpus()
        # Chunking a multi-section corpus yields more pieces than source files.
        assert len(chunks) >= 5
        for c in chunks:
            assert c.metadata.get("source", "").endswith(".md")
            assert c.metadata.get("title")


class TestDirectoryOverride:
    def test_reads_from_a_directory(self, tmp_path: Path) -> None:
        (tmp_path / "a.md").write_text("# Alpha\n\nbody a", encoding="utf-8")
        (tmp_path / "b.md").write_text("# Beta\n\nbody b", encoding="utf-8")
        (tmp_path / "ignore.txt").write_text("not markdown", encoding="utf-8")

        docs = load_markdown_documents(tmp_path)
        assert {d.metadata["source"] for d in docs} == {"a.md", "b.md"}
        assert {d.metadata["title"] for d in docs} == {"Alpha", "Beta"}

    def test_blank_file_is_skipped(self, tmp_path: Path) -> None:
        (tmp_path / "empty.md").write_text("   \n\n", encoding="utf-8")
        (tmp_path / "real.md").write_text("# Real\n\nbody", encoding="utf-8")
        docs = load_markdown_documents(tmp_path)
        assert [d.metadata["source"] for d in docs] == ["real.md"]

    def test_falls_back_to_filename_when_no_h1(self, tmp_path: Path) -> None:
        (tmp_path / "no-heading.md").write_text("just text, no heading", encoding="utf-8")
        docs = load_markdown_documents(tmp_path)
        assert docs[0].metadata["title"] == "no-heading"


class TestChunkSizing:
    def test_long_doc_splits_into_multiple_chunks(self, tmp_path: Path) -> None:
        body = "\n\n".join(f"Paragraph number {i} with some filler text." for i in range(60))
        (tmp_path / "long.md").write_text(f"# Long\n\n{body}", encoding="utf-8")
        docs = load_markdown_documents(tmp_path)
        chunks = chunk_documents(docs, chunk_size=200, chunk_overlap=20)
        assert len(chunks) > 1
        for c in chunks:
            # Allow a little slack for separator boundaries.
            assert len(c.page_content) <= 260
