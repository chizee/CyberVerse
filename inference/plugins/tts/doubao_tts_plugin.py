import asyncio
import json
import logging
import os
import uuid
from math import gcd
from typing import Any, AsyncIterator

import numpy as np

from inference.core.types import AudioChunk, PluginConfig, TTSRequestConfig
from inference.plugins.tts.base import AudioRechunker, TTSPlugin
from inference.plugins.voice_llm.doubao_protocol import (
    COMPRESSION_GZIP,
    COMPRESSION_NONE,
    DoubaoEvent,
    MSGTYPE_FULL_CLIENT,
    SERIALIZATION_JSON,
    compress_payload,
    decode_frame,
    decompress_payload,
    encode_frame,
)

logger = logging.getLogger(__name__)

DEFAULT_DOUBAO_TTS_WS_URL = "wss://openspeech.bytedance.com/api/v3/tts/bidirection"
DEFAULT_DOUBAO_TTS_RESOURCE_ID = "seed-tts-2.0"
DEFAULT_DOUBAO_TTS_VOICE = "zh_female_xiaohe_uranus_bigtts"


class DoubaoTTSPlugin(TTSPlugin):
    """Doubao-TTS 2.0 bidirectional WebSocket plugin."""

    name = "tts.doubao"

    def __init__(self) -> None:
        self.api_key = ""
        self.app_id = ""
        self.access_token = ""
        self.app_key = ""
        self.ws_url = DEFAULT_DOUBAO_TTS_WS_URL
        self.resource_id = DEFAULT_DOUBAO_TTS_RESOURCE_ID
        self.speaker_model = ""
        self.voice = DEFAULT_DOUBAO_TTS_VOICE
        self.audio_format = "pcm"
        self.sample_rate = 24000
        self.target_sample_rate = 16000
        self.rechunk_samples = 17920
        self.speech_rate: int | None = None
        self.loudness_rate: int | None = None
        self.bit_rate: int | None = None
        self.explicit_dialect = ""
        self.use_cache: bool | None = None
        self.text_type: int | None = None
        self.use_tag_parser = False
        self.context_texts: list[str] = []
        self.use_speaking_style_context = True
        self.compression = COMPRESSION_NONE

    async def initialize(self, config: PluginConfig) -> None:
        params = config.params
        self.api_key = self._config_string(
            params.get("api_key"), os.environ.get("DOUBAO_API_KEY")
        )
        self.app_id = self._config_string(
            params.get("app_id")
            or params.get("appid")
            or params.get("appId"),
            os.environ.get("DOUBAO_APP_ID"),
        )
        self.access_token = self._config_string(
            params.get("access_token")
            or params.get("access_key")
            or params.get("token"),
            os.environ.get("DOUBAO_ACCESS_TOKEN")
            or os.environ.get("DOUBAO_ACCESS_KEY")
            or os.environ.get("DOUBAO_TOKEN")
        )
        self.app_key = self._config_string(
            params.get("app_key"), os.environ.get("DOUBAO_APP_KEY")
        )
        if not self.api_key and (not self.app_id or not self.access_token):
            raise ValueError(
                "api_key or legacy app_id/access_token is required for Doubao TTS"
            )

        self.ws_url = str(params.get("ws_url") or self.ws_url)
        self.resource_id = str(
            params.get("resource_id") or params.get("model") or self.resource_id
        )
        self.speaker_model = str(
            params.get("speaker_model") or params.get("voice_model") or ""
        )
        self.voice = str(params.get("voice") or self.voice)
        self.audio_format = str(params.get("format") or self.audio_format)
        self.sample_rate = int(params.get("sample_rate", self.sample_rate))
        self.target_sample_rate = int(
            params.get("target_sample_rate", self.target_sample_rate)
        )
        self.rechunk_samples = int(params.get("rechunk_samples", self.rechunk_samples))
        self.speech_rate = self._optional_int(params.get("speech_rate"))
        self.loudness_rate = self._optional_int(params.get("loudness_rate"))
        self.bit_rate = self._optional_int(params.get("bit_rate"))
        self.explicit_dialect = str(params.get("explicit_dialect") or "")
        self.use_cache = self._optional_bool(params.get("use_cache"))
        self.text_type = self._optional_int(params.get("text_type"))
        self.use_tag_parser = self._optional_bool(params.get("use_tag_parser")) or False
        self.context_texts = self._context_texts(params.get("context_texts"))
        self.use_speaking_style_context = (
            self._optional_bool(params.get("use_speaking_style_context"))
            if "use_speaking_style_context" in params
            else True
        )
        self.compression = (
            COMPRESSION_GZIP
            if str(params.get("compression", "")).strip().lower() == "gzip"
            else COMPRESSION_NONE
        )

    async def synthesize_stream(
        self,
        text_stream: AsyncIterator[str],
        request_config: TTSRequestConfig | None = None,
    ) -> AsyncIterator[AudioChunk]:
        if self.audio_format.strip().lower() != "pcm":
            raise ValueError("Doubao TTS realtime avatar output requires pcm format")

        import websockets

        session_id = (request_config.session_id if request_config else "") or str(uuid.uuid4())
        connect_id = str(uuid.uuid4())
        resource_id = self._request_resource_id(request_config)
        voice = ((request_config.voice if request_config else "") or self.voice).strip()
        context_texts = self._request_context_texts(request_config)
        rechunker = AudioRechunker(
            chunk_samples=self.rechunk_samples,
            sample_rate=self.target_sample_rate,
        )

        ws = await self._connect(websockets, self._headers(connect_id, resource_id))
        sender_task: asyncio.Task | None = None
        try:
            await self._send_event(ws, DoubaoEvent.START_CONNECTION, None, {})
            await self._recv_expected(ws, DoubaoEvent.CONNECTION_STARTED, "connection")

            base_request = self._base_request(voice, context_texts)
            await self._send_event(
                ws,
                DoubaoEvent.START_SESSION,
                session_id,
                {**base_request, "event": DoubaoEvent.START_SESSION},
            )
            await self._recv_expected(ws, DoubaoEvent.SESSION_STARTED, "session")

            sender_task = asyncio.create_task(
                self._send_text(ws, text_stream, session_id, base_request)
            )
            async for audio in self._receive_audio(ws, rechunker):
                yield audio

            await sender_task
            final_chunk = rechunker.flush()
            if final_chunk:
                yield final_chunk
        finally:
            if sender_task and not sender_task.done():
                sender_task.cancel()
                try:
                    await sender_task
                except asyncio.CancelledError:
                    pass
            try:
                await self._send_event(ws, DoubaoEvent.FINISH_CONNECTION, None, {})
                await self._recv_expected(
                    ws, DoubaoEvent.CONNECTION_FINISHED, "finish connection"
                )
            except Exception:
                logger.debug("Doubao TTS finish connection skipped", exc_info=True)
            await ws.close()

    async def _send_text(
        self,
        ws: Any,
        text_stream: AsyncIterator[str],
        session_id: str,
        base_request: dict[str, Any],
    ) -> None:
        try:
            async for text in text_stream:
                text = text.strip()
                if not text:
                    continue
                request = {
                    **base_request,
                    "event": DoubaoEvent.TASK_REQUEST,
                    "req_params": {
                        **base_request["req_params"],
                        "text": text,
                    },
                }
                await self._send_event(ws, DoubaoEvent.TASK_REQUEST, session_id, request)
        finally:
            await self._send_event(ws, DoubaoEvent.FINISH_SESSION, session_id, {})

    async def _receive_audio(
        self,
        ws: Any,
        rechunker: AudioRechunker,
    ) -> AsyncIterator[AudioChunk]:
        while True:
            frame = await ws.recv()
            if isinstance(frame, str):
                logger.debug("Doubao TTS ignored text frame: %s", frame[:200])
                continue
            decoded = decode_frame(frame)
            if decoded.is_error():
                raise RuntimeError(self._error_message(decoded, "response"))
            if decoded.is_audio():
                payload = decompress_payload(decoded.payload, decoded.compression_bits)
                audio = np.frombuffer(payload, dtype=np.int16).astype(np.float32) / 32768.0
                if self.sample_rate != self.target_sample_rate:
                    audio = self._resample(audio, self.sample_rate, self.target_sample_rate)
                for chunk in rechunker.feed(audio):
                    yield chunk
                continue
            if decoded.is_full_server():
                if decoded.event == DoubaoEvent.SESSION_FINISHED:
                    return
                if decoded.event == DoubaoEvent.SESSION_FAILED:
                    raise RuntimeError(self._error_message(decoded, "session"))
                continue
            raise RuntimeError(f"Doubao TTS returned unexpected frame type={decoded.msg_type_bits}")

    async def _send_event(
        self,
        ws: Any,
        event: int,
        session_id: str | None,
        payload: dict[str, Any],
    ) -> None:
        payload_bytes = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        await ws.send(
            encode_frame(
                msg_type_bits=MSGTYPE_FULL_CLIENT,
                serialization_bits=SERIALIZATION_JSON,
                event=event,
                session_id=session_id,
                payload=compress_payload(payload_bytes, self.compression),
                compression_bits=self.compression,
            )
        )

    async def _recv_expected(self, ws: Any, event: int, stage: str) -> None:
        frame = await ws.recv()
        if isinstance(frame, str):
            raise RuntimeError(f"Doubao TTS {stage} returned text frame unexpectedly")
        decoded = decode_frame(frame)
        if decoded.is_error():
            raise RuntimeError(self._error_message(decoded, stage))
        if not decoded.is_full_server() or decoded.event != event:
            raise RuntimeError(
                f"Doubao TTS {stage} returned unexpected event={decoded.event}, expected={event}"
            )

    def _base_request(self, voice: str, context_texts: list[str]) -> dict[str, Any]:
        req_params: dict[str, Any] = {
            "speaker": voice,
            "audio_params": {
                "format": self.audio_format,
                "sample_rate": self.sample_rate,
            },
        }
        if self.speaker_model:
            req_params["model"] = self.speaker_model
        if self.speech_rate is not None:
            req_params["speech_rate"] = self.speech_rate
        if self.loudness_rate is not None:
            req_params["loudness_rate"] = self.loudness_rate
        if self.bit_rate is not None:
            req_params["bit_rate"] = self.bit_rate
        if self.explicit_dialect:
            req_params["explicit_dialect"] = self.explicit_dialect
        if self.use_cache is not None:
            req_params["use_cache"] = self.use_cache
        if self.text_type is not None:
            req_params["text_type"] = self.text_type
        if self.use_tag_parser:
            req_params["use_tag_parser"] = True
        if context_texts:
            req_params["context_texts"] = context_texts
        return {"req_params": req_params}

    def _headers(self, connect_id: str, resource_id: str) -> dict[str, str]:
        headers = {
            "X-Api-Resource-Id": resource_id,
            "X-Api-Connect-Id": connect_id,
            "X-Control-Require-Usage-Tokens-Return": "*",
        }
        if self.api_key:
            headers["X-Api-Key"] = self.api_key
            return headers

        if self.app_id:
            headers["X-Api-App-Id"] = self.app_id
        if self.access_token:
            headers["X-Api-Access-Key"] = self.access_token
        if self.app_key:
            headers["X-Api-App-Key"] = self.app_key
        return headers

    async def _connect(self, websockets: Any, headers: dict[str, str]) -> Any:
        try:
            return await websockets.connect(
                self.ws_url,
                additional_headers=headers,
                max_size=10 * 1024 * 1024,
                proxy=None,
            )
        except TypeError:
            return await websockets.connect(
                self.ws_url,
                extra_headers=headers,
                max_size=10 * 1024 * 1024,
            )

    def _request_resource_id(self, request_config: TTSRequestConfig | None) -> str:
        model = ((request_config.model if request_config else "") or "").strip()
        if model.startswith("seed-tts-") or model.startswith("seed-icl-"):
            return model
        return self.resource_id

    def _request_context_texts(
        self, request_config: TTSRequestConfig | None
    ) -> list[str]:
        context_texts = list(self.context_texts)
        speaking_style = ((request_config.speaking_style if request_config else "") or "").strip()
        if self.use_speaking_style_context and speaking_style:
            context_texts.append(speaking_style)
        return context_texts

    @staticmethod
    def _error_message(decoded: Any, stage: str) -> str:
        try:
            payload = decompress_payload(decoded.payload, decoded.compression_bits)
        except Exception:
            payload = decoded.payload
        text = (
            payload.decode("utf-8", errors="ignore")
            if isinstance(payload, (bytes, bytearray))
            else str(payload)
        )
        return f"Doubao TTS {stage} failed: code={decoded.error_code} payload={text}"

    @staticmethod
    def _context_texts(value: Any) -> list[str]:
        if value is None:
            return []
        if isinstance(value, str):
            items = [value]
        elif isinstance(value, list):
            items = [str(item) for item in value]
        else:
            items = [str(value)]
        return [item.strip() for item in items if item.strip()]

    @staticmethod
    def _config_string(*values: Any) -> str:
        for value in values:
            text = str(value or "").strip()
            if text and not (text.startswith("${") and text.endswith("}")):
                return text
        return ""

    @staticmethod
    def _optional_int(value: Any) -> int | None:
        if value is None or value == "":
            return None
        return int(value)

    @staticmethod
    def _optional_bool(value: Any) -> bool | None:
        if value is None or value == "":
            return None
        if isinstance(value, bool):
            return value
        if isinstance(value, (int, float)):
            return bool(value)
        return str(value).strip().lower() in {"1", "true", "yes", "on"}

    @staticmethod
    def _resample(audio: np.ndarray, orig_sr: int, target_sr: int) -> np.ndarray:
        if orig_sr == target_sr:
            return audio.astype(np.float32)
        from scipy.signal import resample_poly

        divisor = gcd(orig_sr, target_sr)
        return resample_poly(
            audio,
            target_sr // divisor,
            orig_sr // divisor,
        ).astype(np.float32)

    async def shutdown(self) -> None:
        return None
