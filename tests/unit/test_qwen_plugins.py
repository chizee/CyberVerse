"""Tests for Qwen LLM and TTS plugins."""
import io
import wave
from unittest.mock import AsyncMock, MagicMock

import numpy as np
import pytest

from inference.core.types import PluginConfig
from inference.plugins.llm.qwen_plugin import QwenLLMPlugin, SENTENCE_ENDERS
from inference.plugins.tts.qwen_tts_plugin import QwenTTSPlugin


def _pcm_wav_bytes(samples: np.ndarray, sample_rate: int = 24000) -> bytes:
    buf = io.BytesIO()
    with wave.open(buf, "wb") as wav_file:
        wav_file.setnchannels(1)
        wav_file.setsampwidth(2)
        wav_file.setframerate(sample_rate)
        wav_file.writeframes(samples.astype(np.int16).tobytes())
    return buf.getvalue()


class TestQwenLLMPlugin:
    def test_name(self):
        assert QwenLLMPlugin.name == "llm.qwen"

    def test_sentence_enders(self):
        assert "." in SENTENCE_ENDERS
        assert "。" in SENTENCE_ENDERS
        assert "!" in SENTENCE_ENDERS

    @pytest.mark.asyncio
    async def test_generate_stream_with_mock(self):
        plugin = QwenLLMPlugin()
        plugin.client = MagicMock()
        plugin.model = "qwen-plus"
        plugin.temperature = 0.7
        plugin.system_prompt = "You are helpful."

        mock_chunk1 = MagicMock()
        mock_chunk1.choices = [MagicMock()]
        mock_chunk1.choices[0].delta.content = "Hello"

        mock_chunk2 = MagicMock()
        mock_chunk2.choices = [MagicMock()]
        mock_chunk2.choices[0].delta.content = " world."

        async def mock_stream():
            yield mock_chunk1
            yield mock_chunk2

        plugin.client.chat.completions.create = AsyncMock(return_value=mock_stream())

        messages = [{"role": "user", "content": "Hi"}]
        results = []
        async for chunk in plugin.generate_stream(messages):
            results.append(chunk)

        assert len(results) == 3
        assert results[0].token == "Hello"
        assert results[1].token == " world."
        assert results[1].is_sentence_end is True
        assert results[2].is_final is True
        assert results[2].accumulated_text == "Hello world."

    @pytest.mark.asyncio
    async def test_shutdown(self):
        plugin = QwenLLMPlugin()
        plugin.client = MagicMock()
        await plugin.shutdown()
        assert plugin.client is None


class TestQwenTTSPlugin:
    def test_name(self):
        assert QwenTTSPlugin.name == "tts.qwen"

    def test_legacy_voice_mapping(self):
        plugin = QwenTTSPlugin()
        assert plugin._resolve_voice("nova") == "Cherry"
        assert plugin._resolve_voice("CustomVoice") == "CustomVoice"

    def test_language_type_auto_detects_chinese(self):
        plugin = QwenTTSPlugin()
        plugin.language_type = "auto"
        assert plugin._resolve_language_type("你好，世界") == "Chinese"
        assert plugin._resolve_language_type("Hello world") == "English"

    @pytest.mark.asyncio
    async def test_synthesize_stream_with_mock(self):
        plugin = QwenTTSPlugin()
        plugin.api_key = "test"
        plugin.rechunker.chunk_samples = 1000
        fake_audio = np.zeros(3000, dtype=np.int16)
        plugin._synthesize_once = MagicMock(return_value=_pcm_wav_bytes(fake_audio, 24000))

        async def text_stream():
            yield "Hello world."

        results = []
        async for chunk in plugin.synthesize_stream(text_stream()):
            results.append(chunk)

        assert len(results) == 2

    @pytest.mark.asyncio
    async def test_synthesize_skips_empty(self):
        plugin = QwenTTSPlugin()
        plugin.api_key = "test"
        plugin.rechunker.chunk_samples = 1000
        plugin._synthesize_once = MagicMock()

        async def text_stream():
            yield ""
            yield "   "

        results = []
        async for chunk in plugin.synthesize_stream(text_stream()):
            results.append(chunk)

        assert len(results) == 0
        plugin._synthesize_once.assert_not_called()

    @pytest.mark.asyncio
    async def test_synthesize_handles_api_error(self):
        plugin = QwenTTSPlugin()
        plugin.api_key = "test"
        plugin.rechunker.chunk_samples = 1000
        plugin._synthesize_once = MagicMock(side_effect=Exception("rate limited"))

        async def text_stream():
            yield "Hello world."

        results = []
        async for chunk in plugin.synthesize_stream(text_stream()):
            results.append(chunk)

        assert len(results) == 0

    @pytest.mark.asyncio
    async def test_shutdown(self):
        plugin = QwenTTSPlugin()
        plugin.rechunker.feed(np.ones(8, dtype=np.float32))
        await plugin.shutdown()
        assert plugin.rechunker.buffer.size == 0
