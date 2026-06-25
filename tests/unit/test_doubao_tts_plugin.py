import asyncio
import json
import sys
from types import SimpleNamespace
from unittest.mock import patch

import numpy as np
import pytest

from inference.core.types import PluginConfig, TTSRequestConfig
from inference.plugins.tts.doubao_tts_plugin import DoubaoTTSPlugin
from inference.plugins.voice_llm.doubao_protocol import (
    DoubaoEvent,
    MSGTYPE_AUDIO_ONLY_SERVER,
    MSGTYPE_FULL_SERVER,
    SERIALIZATION_JSON,
    SERIALIZATION_RAW,
    encode_frame,
)


class FakeDoubaoTTSWebSocket:
    def __init__(self) -> None:
        self.sent: list[dict] = []
        self.messages: asyncio.Queue[bytes] = asyncio.Queue()
        self.closed = False

    async def send(self, payload: bytes) -> None:
        from inference.plugins.voice_llm.doubao_protocol import decode_frame

        frame = decode_frame(payload)
        data = json.loads(frame.payload.decode("utf-8") or "{}")
        self.sent.append({"event": frame.event, "payload": data})
        if frame.event == DoubaoEvent.START_CONNECTION:
            await self.messages.put(self._server_event(DoubaoEvent.CONNECTION_STARTED))
        elif frame.event == DoubaoEvent.START_SESSION:
            await self.messages.put(self._server_event(DoubaoEvent.SESSION_STARTED))
        elif frame.event == DoubaoEvent.TASK_REQUEST:
            pcm = np.array([0, 32767], dtype=np.int16).tobytes()
            await self.messages.put(
                encode_frame(
                    msg_type_bits=MSGTYPE_AUDIO_ONLY_SERVER,
                    serialization_bits=SERIALIZATION_RAW,
                    event=DoubaoEvent.AUDIO_DATA,
                    session_id="session",
                    payload=pcm,
                )
            )
        elif frame.event == DoubaoEvent.FINISH_SESSION:
            await self.messages.put(self._server_event(DoubaoEvent.SESSION_FINISHED))
        elif frame.event == DoubaoEvent.FINISH_CONNECTION:
            await self.messages.put(self._server_event(DoubaoEvent.CONNECTION_FINISHED))

    async def recv(self) -> bytes:
        return await self.messages.get()

    async def close(self) -> None:
        self.closed = True

    @staticmethod
    def _server_event(event: int) -> bytes:
        return encode_frame(
            msg_type_bits=MSGTYPE_FULL_SERVER,
            serialization_bits=SERIALIZATION_JSON,
            event=event,
            session_id=None if event in (DoubaoEvent.CONNECTION_STARTED, DoubaoEvent.CONNECTION_FINISHED) else "session",
            connect_id="connect" if event in (DoubaoEvent.CONNECTION_STARTED, DoubaoEvent.CONNECTION_FINISHED) else None,
            payload=b"{}",
        )


class FakeWebSockets:
    def __init__(self) -> None:
        self.ws = FakeDoubaoTTSWebSocket()
        self.connect_url = ""
        self.connect_headers: dict[str, str] = {}

    async def connect(self, url: str, **kwargs):
        self.connect_url = url
        self.connect_headers = kwargs.get("additional_headers") or kwargs.get(
            "extra_headers",
            {},
        )
        return self.ws


@pytest.mark.asyncio
async def test_doubao_tts_streams_text_to_bidirectional_websocket():
    plugin = DoubaoTTSPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="tts.doubao",
            params={
                "api_key": "doubao-key",
                "model": "seed-tts-2.0",
                "voice": "zh_female_xiaohe_uranus_bigtts",
                "sample_rate": 16000,
                "target_sample_rate": 16000,
                "rechunk_samples": 2,
            },
        )
    )
    fake_websockets = FakeWebSockets()

    async def text_stream():
        yield "  你好  "

    with patch.dict(
        sys.modules,
        {"websockets": SimpleNamespace(connect=fake_websockets.connect)},
    ):
        chunks = [
            chunk
            async for chunk in plugin.synthesize_stream(
                text_stream(),
                TTSRequestConfig(
                    model="seed-tts-2.0",
                    voice="zh_female_vv_uranus_bigtts",
                    speaking_style="温柔自然",
                ),
            )
        ]

    assert fake_websockets.connect_url == "wss://openspeech.bytedance.com/api/v3/tts/bidirection"
    assert fake_websockets.connect_headers["X-Api-Key"] == "doubao-key"
    assert "X-Api-App-Id" not in fake_websockets.connect_headers
    assert "X-Api-Access-Key" not in fake_websockets.connect_headers
    assert fake_websockets.connect_headers["X-Api-Resource-Id"] == "seed-tts-2.0"
    assert fake_websockets.connect_headers["X-Control-Require-Usage-Tokens-Return"] == "*"
    assert fake_websockets.ws.closed is True

    start_session = next(item for item in fake_websockets.ws.sent if item["event"] == DoubaoEvent.START_SESSION)
    req_params = start_session["payload"]["req_params"]
    assert req_params["speaker"] == "zh_female_vv_uranus_bigtts"
    assert req_params["audio_params"] == {"format": "pcm", "sample_rate": 16000}
    assert req_params["context_texts"] == ["温柔自然"]

    task = next(item for item in fake_websockets.ws.sent if item["event"] == DoubaoEvent.TASK_REQUEST)
    assert task["payload"]["req_params"]["text"] == "你好"

    assert len(chunks) == 1
    assert chunks[0].sample_rate == 16000
    np.testing.assert_allclose(
        np.frombuffer(chunks[0].data, dtype=np.float32),
        np.array([0.0, 32767 / 32768], dtype=np.float32),
    )


@pytest.mark.asyncio
async def test_doubao_tts_supports_legacy_console_auth_headers():
    plugin = DoubaoTTSPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="tts.doubao",
            params={
                "appid": "legacy-app",
                "token": "legacy-token",
                "model": "seed-tts-2.0",
                "voice": "zh_female_xiaohe_uranus_bigtts",
                "sample_rate": 16000,
                "target_sample_rate": 16000,
                "rechunk_samples": 2,
            },
        )
    )
    fake_websockets = FakeWebSockets()

    async def text_stream():
        yield "你好"

    with patch.dict(
        sys.modules,
        {"websockets": SimpleNamespace(connect=fake_websockets.connect)},
    ):
        chunks = [
            chunk
            async for chunk in plugin.synthesize_stream(
                text_stream(),
                TTSRequestConfig(model="seed-tts-2.0"),
            )
        ]

    assert "X-Api-Key" not in fake_websockets.connect_headers
    assert fake_websockets.connect_headers["X-Api-App-Id"] == "legacy-app"
    assert fake_websockets.connect_headers["X-Api-Access-Key"] == "legacy-token"
    assert fake_websockets.connect_headers["X-Api-Resource-Id"] == "seed-tts-2.0"
    assert len(chunks) == 1


@pytest.mark.asyncio
async def test_doubao_tts_ignores_unresolved_api_key_placeholder_for_legacy_auth():
    plugin = DoubaoTTSPlugin()
    await plugin.initialize(
        PluginConfig(
            plugin_name="tts.doubao",
            params={
                "api_key": "${DOUBAO_API_KEY}",
                "app_id": "legacy-app",
                "access_token": "legacy-token",
                "model": "seed-tts-2.0",
                "sample_rate": 16000,
                "target_sample_rate": 16000,
                "rechunk_samples": 2,
            },
        )
    )
    fake_websockets = FakeWebSockets()

    async def text_stream():
        yield "你好"

    with patch.dict(
        sys.modules,
        {"websockets": SimpleNamespace(connect=fake_websockets.connect)},
    ):
        chunks = [
            chunk
            async for chunk in plugin.synthesize_stream(
                text_stream(),
                TTSRequestConfig(model="seed-tts-2.0"),
            )
        ]

    assert "X-Api-Key" not in fake_websockets.connect_headers
    assert fake_websockets.connect_headers["X-Api-App-Id"] == "legacy-app"
    assert fake_websockets.connect_headers["X-Api-Access-Key"] == "legacy-token"
    assert len(chunks) == 1


@pytest.mark.asyncio
async def test_doubao_tts_legacy_auth_requires_app_id_and_access_token():
    plugin = DoubaoTTSPlugin()

    with pytest.raises(ValueError, match="app_id/access_token"):
        await plugin.initialize(
            PluginConfig(
                plugin_name="tts.doubao",
                params={"access_token": "legacy-token"},
            )
        )
