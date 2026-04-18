"""Tests for the Whisper ASR plugin."""
import asyncio
from unittest.mock import MagicMock, patch

import numpy as np
import pytest

from inference.core.types import PluginConfig, TranscriptEvent
from inference.plugins.asr.whisper_plugin import WhisperASRPlugin


@pytest.fixture
def plugin():
    return WhisperASRPlugin()


@pytest.fixture
def config():
    return PluginConfig(
        plugin_name="asr.whisper",
        params={
            "model_size": "base",
            "device": "cpu",
            "language": "auto",
            "min_audio_seconds": "0.5",
            "silence_threshold": "0.01",
            "silence_duration": "0.3",
        },
    )


class TestWhisperASRPluginInit:
    async def test_initialize_loads_model(self, plugin, config):
        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model") as mock_load:
            await plugin.initialize(config)
            mock_load.assert_called_once()
            assert plugin.model_size == "base"
            assert plugin.device == "cpu"
            assert plugin.language is None  # "auto" -> None

    async def test_initialize_specific_language(self, plugin):
        config = PluginConfig(
            plugin_name="asr.whisper",
            params={"model_size": "small", "device": "cuda:0", "language": "zh"},
        )
        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)
            assert plugin.model_size == "small"
            assert plugin.device == "cuda:0"
            assert plugin.language == "zh"

    async def test_initialize_defaults(self, plugin):
        config = PluginConfig(plugin_name="asr.whisper", params={})
        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)
            assert plugin.model_size == "base"
            assert plugin.device == "cpu"
            assert plugin.language is None


class TestWhisperASRPluginTranscribe:
    async def test_transcribe_stream_yields_events(self, plugin, config):
        # Setup mock model
        mock_model = MagicMock()
        mock_model.transcribe.return_value = {
            "text": "Hello world",
            "language": "en",
            "segments": [{"no_speech_prob": 0.1}],
        }

        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)
        plugin.model = mock_model

        # Create audio stream with speech followed by silence
        async def audio_stream():
            # Some speech audio (non-silent)
            speech = np.random.randn(16000).astype(np.float32) * 0.5
            yield speech.tobytes()
            # Silence to trigger transcription
            silence = np.zeros(8000, dtype=np.float32)
            yield silence.tobytes()

        events = []
        async for event in plugin.transcribe_stream(audio_stream()):
            events.append(event)

        assert len(events) >= 1
        assert events[0].text == "Hello world"
        assert events[0].is_final is True
        assert events[0].language == "en"

    async def test_transcribe_empty_stream(self, plugin, config):
        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)

        async def empty_stream():
            return
            yield  # Make it an async generator

        events = []
        async for event in plugin.transcribe_stream(empty_stream()):
            events.append(event)

        assert len(events) == 0

    async def test_transcribe_flush_remaining(self, plugin, config):
        mock_model = MagicMock()
        mock_model.transcribe.return_value = {
            "text": "Remaining audio",
            "language": "en",
            "segments": [],
        }

        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)
        plugin.model = mock_model

        # Only speech, no silence -- should flush at end
        async def audio_stream():
            speech = np.random.randn(8000).astype(np.float32) * 0.5
            yield speech.tobytes()

        events = []
        async for event in plugin.transcribe_stream(audio_stream()):
            events.append(event)

        assert len(events) == 1
        assert events[0].text == "Remaining audio"
        assert events[0].is_final is True


class TestWhisperASRPluginShutdown:
    async def test_shutdown_clears_model(self, plugin, config):
        with patch("inference.plugins.asr.whisper_plugin.WhisperASRPlugin._load_model"):
            await plugin.initialize(config)
        plugin.model = MagicMock()

        await plugin.shutdown()
        assert plugin.model is None


class TestWhisperASRPluginMetadata:
    def test_name(self, plugin):
        assert plugin.name == "asr.whisper"
