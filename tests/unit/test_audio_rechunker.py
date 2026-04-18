import numpy as np

from inference.plugins.tts.base import AudioRechunker


def test_rechunker_exact_chunk():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    audio = np.ones(100, dtype=np.float32)
    chunks = rechunker.feed(audio)
    assert len(chunks) == 1
    assert len(chunks[0].data) == 100 * 4  # float32 = 4 bytes


def test_rechunker_multiple_chunks():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    audio = np.ones(250, dtype=np.float32)
    chunks = rechunker.feed(audio)
    assert len(chunks) == 2  # 250 // 100 = 2, remainder 50


def test_rechunker_accumulates():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    chunks1 = rechunker.feed(np.ones(60, dtype=np.float32))
    assert len(chunks1) == 0
    chunks2 = rechunker.feed(np.ones(60, dtype=np.float32))
    assert len(chunks2) == 1  # 60+60=120 >= 100


def test_rechunker_flush_pads_silence():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    rechunker.feed(np.ones(30, dtype=np.float32))
    chunk = rechunker.flush()
    assert chunk is not None
    assert chunk.is_final is True
    data = np.frombuffer(chunk.data, dtype=np.float32)
    assert len(data) == 100
    assert np.all(data[:30] == 1.0)
    assert np.all(data[30:] == 0.0)


def test_rechunker_flush_empty():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    assert rechunker.flush() is None


def test_rechunker_reset():
    rechunker = AudioRechunker(chunk_samples=100, sample_rate=16000)
    rechunker.feed(np.ones(50, dtype=np.float32))
    rechunker.reset()
    assert rechunker.flush() is None


def test_rechunker_default_avatar_alignment():
    """Test with FlashHead's actual chunk size: 28 frames / 25fps = 1.12s = 17920 samples."""
    rechunker = AudioRechunker()  # defaults: 17920, 16000
    audio = np.random.randn(17920 * 3 + 5000).astype(np.float32)
    chunks = rechunker.feed(audio)
    assert len(chunks) == 3
    for c in chunks:
        data = np.frombuffer(c.data, dtype=np.float32)
        assert len(data) == 17920
        assert c.duration_ms == 1120  # 17920/16000*1000
