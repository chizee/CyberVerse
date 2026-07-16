import pytest

from inference.rag.engine import HashEmbeddings
from inference.rag.vector_store import (
    ChromaBackend,
    MilvusBackend,
    _l2_similarity,
    create_vector_store_backend,
    safe_collection_name,
)


def _make(settings, tmp_path):
    return create_vector_store_backend(
        settings,
        character_id="char-1",
        character_dir=str(tmp_path),
        embedding_function=HashEmbeddings(16),
    )


def test_backend_defaults_to_chroma(tmp_path):
    backend = _make({}, tmp_path)

    assert isinstance(backend, ChromaBackend)
    assert backend.name == "chroma"


def test_backend_selects_milvus_from_string(tmp_path):
    backend = _make({"vector_store": "milvus"}, tmp_path)

    assert isinstance(backend, MilvusBackend)
    assert backend.name == "milvus"


def test_backend_selects_milvus_from_mapping(tmp_path):
    backend = _make(
        {"vector_store": {"provider": "milvus", "uri": "http://milvus:19530"}},
        tmp_path,
    )

    assert isinstance(backend, MilvusBackend)
    # A remote uri means no local Milvus Lite file is used.
    assert backend._uri == "http://milvus:19530"
    assert backend._local_db is None


def test_milvus_backend_defaults_to_local_lite_file(tmp_path):
    backend = _make({"vector_store": "milvus"}, tmp_path)

    assert backend._local_db is not None
    assert backend._local_db.suffix == ".db"
    assert "milvus" in backend._local_db.parts


def test_unknown_backend_raises(tmp_path):
    with pytest.raises(ValueError, match="unknown RAG vector store backend"):
        _make({"vector_store": "pinecone"}, tmp_path)


def test_l2_similarity_is_normalized_and_monotonic():
    assert _l2_similarity(0.0) == 1.0
    assert _l2_similarity(-5.0) == 1.0  # clamps negative distances
    assert 0.0 < _l2_similarity(10.0) < _l2_similarity(1.0) < 1.0


def test_safe_collection_name_is_portable():
    # Sanitized, prefixed, and valid for both Chroma and Milvus.
    assert safe_collection_name("角色-A/1") == "cv_A_1"
    assert safe_collection_name("") == "cv_default"
    assert len(safe_collection_name("x" * 1000)) <= 512
