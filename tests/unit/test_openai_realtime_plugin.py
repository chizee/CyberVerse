import base64
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest

from inference.core.config import load_config
from inference.core.types import PluginConfig, VoiceLLMInputEvent, VoiceLLMSessionConfig
from inference.plugins.voice_llm.openai_realtime_compatible import OpenAIRealtimeCompatiblePlugin


class FakeOpenAIRealtimeWS:
    def __init__(self, events):
        self.events = list(events)
        self.sent = []
        self.closed = False

    async def send(self, payload: str):
        self.sent.append(json.loads(payload))

    async def recv(self):
        if not self.events:
            raise RuntimeError("no fake websocket events left")
        return json.dumps(self.events.pop(0), ensure_ascii=False)

    def __aiter__(self):
        return self

    async def __anext__(self):
        if not self.events:
            raise StopAsyncIteration
        return json.dumps(self.events.pop(0), ensure_ascii=False)

    async def close(self):
        self.closed = True


def test_openai_realtime_template_config_enables_transcription_and_server_vad():
    config = load_config(Path("infra/config/cyberverse.yaml"))
    model_config = config["inference"]["omni"]["openai_realtime"]

    assert model_config["model"] == "gpt-realtime-1.5"
    assert model_config["input_transcription_model"] == "gpt-realtime-whisper"
    assert model_config["vad_type"] == "server_vad"
    assert model_config["vad_create_response"] is True
    assert model_config["vad_interrupt_response"] is True


@pytest.mark.asyncio
async def test_openai_current_session_payload_enables_server_vad_transcription_and_barge_in():
    plugin = OpenAIRealtimeCompatiblePlugin()

    await plugin.initialize(
        PluginConfig(
            plugin_name="omni.openai_realtime",
            params={
                "api_key": "openai-key",
                "model": "gpt-realtime-1.5",
                "ws_url": "wss://api.openai.com/v1/realtime",
                "voice": "marin",
                "input_transcription_model": "gpt-realtime-whisper",
                "vad_type": "server_vad",
                "vad_threshold": 0.5,
                "vad_prefix_padding_ms": 300,
                "vad_silence_duration_ms": 500,
                "vad_create_response": True,
                "vad_interrupt_response": True,
                "session_schema": "openai_realtime_current",
            },
        )
    )

    payload = plugin._session_payload(VoiceLLMSessionConfig(voice="cedar"))

    assert payload["type"] == "realtime"
    assert payload["model"] == "gpt-realtime-1.5"
    assert payload["output_modalities"] == ["audio"]
    assert payload["audio"]["output"]["voice"] == "cedar"
    assert payload["audio"]["input"]["transcription"] == {
        "model": "gpt-realtime-whisper",
    }
    assert payload["audio"]["input"]["turn_detection"] == {
        "type": "server_vad",
        "create_response": True,
        "interrupt_response": True,
        "threshold": 0.5,
        "prefix_padding_ms": 300,
        "silence_duration_ms": 500,
    }


@pytest.mark.asyncio
async def test_openai_current_session_payload_supports_semantic_vad():
    plugin = OpenAIRealtimeCompatiblePlugin()

    await plugin.initialize(
        PluginConfig(
            plugin_name="omni.openai_realtime",
            params={
                "api_key": "openai-key",
                "model": "gpt-realtime-1.5",
                "ws_url": "wss://api.openai.com/v1/realtime",
                "voice": "marin",
                "input_transcription_model": "gpt-realtime-whisper",
                "vad_type": "semantic_vad",
                "vad_eagerness": "auto",
                "vad_create_response": True,
                "vad_interrupt_response": True,
                "session_schema": "openai_realtime_current",
            },
        )
    )

    payload = plugin._session_payload(VoiceLLMSessionConfig(voice="cedar"))

    assert payload["audio"]["input"]["turn_detection"] == {
        "type": "semantic_vad",
        "create_response": True,
        "interrupt_response": True,
        "eagerness": "auto",
    }


@pytest.mark.asyncio
async def test_openai_converse_stream_emits_user_transcript_from_delta_and_completed():
    plugin = OpenAIRealtimeCompatiblePlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="omni.openai_realtime",
            params={
                "api_key": "openai-key",
                "model": "gpt-realtime-1.5",
                "ws_url": "wss://api.openai.com/v1/realtime",
                "voice": "marin",
                "input_transcription_model": "gpt-realtime-whisper",
                "session_schema": "openai_realtime_current",
            },
        )
    )
    audio_bytes = b"\x01\x00\x02\x00"
    ws = FakeOpenAIRealtimeWS(
        [
            {"type": "session.updated"},
            {"type": "input_audio_buffer.speech_started"},
            {
                "type": "conversation.item.input_audio_transcription.delta",
                "delta": "你",
            },
            {
                "type": "conversation.item.input_audio_transcription.delta",
                "delta": "好",
            },
            {
                "type": "conversation.item.input_audio_transcription.completed",
                "transcript": "",
            },
            {"type": "response.output_audio_transcript.delta", "delta": "收到"},
            {
                "type": "response.output_audio.delta",
                "delta": base64.b64encode(audio_bytes).decode("ascii"),
            },
            {"type": "response.output_audio_transcript.done", "transcript": "收到"},
            {"type": "response.done"},
        ]
    )
    websockets = SimpleNamespace(connect=AsyncMock(return_value=ws))

    async def inputs():
        yield VoiceLLMInputEvent(audio=b"\x03\x00")

    with patch.dict("sys.modules", {"websockets": websockets}):
        outputs = [
            event
            async for event in plugin.converse_stream(
                inputs(),
                VoiceLLMSessionConfig(session_id="session-1", voice="marin"),
            )
        ]

    sent_audio = [event for event in ws.sent if event["type"] == "input_audio_buffer.append"]
    assert sent_audio
    assert base64.b64decode(sent_audio[0]["audio"]) == b"\x03\x00"

    assert outputs[0].barge_in is True
    assert outputs[1].user_transcript == "你好"
    assert outputs[2].transcript == "收到"
    assert outputs[3].audio is not None
    assert outputs[3].audio.data == audio_bytes
    assert outputs[3].audio.sample_rate == 24000
    assert outputs[4].is_final is True
    assert outputs[4].transcript == "收到"
    assert outputs[4].audio is not None
    assert outputs[4].audio.is_final is True
