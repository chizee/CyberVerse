import asyncio
import json
import sys
from types import SimpleNamespace
from unittest.mock import patch

import pytest

from inference.core.types import ASRRequestConfig, PluginConfig
from inference.plugins.asr.qwen_asr_plugin import QwenASRPlugin


class FakeConnectionClosedError(Exception):
    pass


class FakeWebSocket:
    def __init__(self, index: int) -> None:
        self.index = index
        self.sent: list[dict] = []
        self.messages: asyncio.Queue[str | None] = asyncio.Queue()
        self.closed = False

    async def send(self, payload: str) -> None:
        event = json.loads(payload)
        self.sent.append(event)
        if event.get("type") == "session.finish":
            await self.messages.put(
                json.dumps(
                    {
                        "type": "response.done",
                        "transcript": f"final-{self.index}",
                        "is_final": True,
                        "language": "zh",
                    }
                )
            )
            await self.messages.put(None)

    def __aiter__(self):
        return self

    async def __anext__(self) -> str:
        message = await self.messages.get()
        if message is None:
            raise StopAsyncIteration
        return message

    async def close(self) -> None:
        self.closed = True


class ClosingWebSocket(FakeWebSocket):
    async def __anext__(self) -> str:
        raise FakeConnectionClosedError("received 1011 Response stream timeout")


class FakeWebSockets:
    exceptions = SimpleNamespace(ConnectionClosedError=FakeConnectionClosedError)

    def __init__(self, ws_type=FakeWebSocket) -> None:
        self.ws_type = ws_type
        self.connections: list[FakeWebSocket] = []

    async def connect(self, *args, **kwargs):
        ws = self.ws_type(len(self.connections) + 1)
        self.connections.append(ws)
        return ws


@pytest.fixture
def plugin() -> QwenASRPlugin:
    plugin = QwenASRPlugin()
    plugin.api_key = "test-key"
    plugin.ws_url = "wss://example.invalid/asr"
    plugin.max_session_seconds = 0.01
    plugin.rollover_drain_seconds = 0.05
    return plugin


async def test_transcribe_stream_rolls_over_before_timeout(plugin):
    fake_websockets = FakeWebSockets()

    async def audio_stream():
        yield b"first"
        await asyncio.sleep(0.03)
        yield b"second"

    with patch.dict(sys.modules, {"websockets": fake_websockets}):
        events = [
            event
            async for event in plugin.transcribe_stream(
                audio_stream(),
                ASRRequestConfig(language="zh", session_id="session-1"),
            )
        ]

    assert [event.text for event in events] == ["final-1", "final-2"]
    assert len(fake_websockets.connections) == 2
    assert _sent_types(fake_websockets.connections[0]) == [
        "session.update",
        "input_audio_buffer.append",
        "session.finish",
    ]
    assert _sent_types(fake_websockets.connections[1]) == [
        "session.update",
        "input_audio_buffer.append",
        "session.finish",
    ]
    assert _sent_audio(fake_websockets.connections[0]) == ["Zmlyc3Q="]
    assert _sent_audio(fake_websockets.connections[1]) == ["c2Vjb25k"]


async def test_transcribe_stream_wraps_connection_closed_error(plugin):
    fake_websockets = FakeWebSockets(ClosingWebSocket)

    async def audio_stream():
        yield b"audio"

    with patch.dict(sys.modules, {"websockets": fake_websockets}):
        with pytest.raises(
            RuntimeError, match="Qwen ASR WebSocket closed unexpectedly"
        ):
            [
                event
                async for event in plugin.transcribe_stream(
                    audio_stream(),
                    ASRRequestConfig(language="zh", session_id="session-1"),
                )
            ]


async def test_initialize_reads_session_rollover_config():
    plugin = QwenASRPlugin()

    await plugin.initialize(
        PluginConfig(
            plugin_name="asr.qwen",
            params={
                "api_key": "test-key",
                "max_session_seconds": "500",
                "rollover_drain_seconds": "3",
                "audio_queue_maxsize": "8",
            },
        )
    )

    assert plugin.max_session_seconds == 500
    assert plugin.rollover_drain_seconds == 3
    assert plugin.audio_queue_maxsize == 8


def _sent_types(ws: FakeWebSocket) -> list[str]:
    return [event["type"] for event in ws.sent]


def _sent_audio(ws: FakeWebSocket) -> list[str]:
    return [
        event["audio"]
        for event in ws.sent
        if event["type"] == "input_audio_buffer.append"
    ]
