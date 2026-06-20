import asyncio
import base64
from contextlib import suppress
import json
import logging
import time
from typing import Any, AsyncIterator

from inference.core.types import ASRRequestConfig, PluginConfig, TranscriptEvent
from inference.plugins.asr.base import ASRPlugin
from inference.plugins.qwen_endpoint import dashscope_realtime_ws_url

logger = logging.getLogger(__name__)

_SEND_EOF = "eof"
_SEND_ROLLOVER = "rollover"


def _connection_closed_error_type(websockets_module: Any) -> type[BaseException]:
    exceptions = getattr(websockets_module, "exceptions", None)
    if exceptions is not None and hasattr(exceptions, "ConnectionClosedError"):
        return exceptions.ConnectionClosedError

    from websockets.exceptions import ConnectionClosedError

    return ConnectionClosedError


class QwenASRPlugin(ASRPlugin):
    """DashScope Qwen realtime ASR plugin."""

    name = "asr.qwen"

    def __init__(self) -> None:
        self.api_key = ""
        self.model = "qwen3-asr-flash-realtime"
        self.ws_url = ""
        self.language = "auto"
        self.sample_rate = 16000
        self.vad_threshold = 0.5
        self.vad_silence_duration_ms = 1000
        self.max_session_seconds = 540.0
        self.rollover_drain_seconds = 5.0
        self.audio_queue_maxsize = 64

    async def initialize(self, config: PluginConfig) -> None:
        self.api_key = config.params.get("api_key", "")
        self.model = config.params.get("model", self.model)
        self.ws_url = dashscope_realtime_ws_url(self.model, "DASHSCOPE_ASR_WS_URL")
        self.language = config.params.get("language", self.language)
        self.sample_rate = int(config.params.get("sample_rate", self.sample_rate))
        self.vad_threshold = float(
            config.params.get("vad_threshold", self.vad_threshold)
        )
        self.vad_silence_duration_ms = int(
            config.params.get(
                "vad_silence_duration_ms", self.vad_silence_duration_ms
            )
        )
        self.max_session_seconds = float(
            config.params.get("max_session_seconds", self.max_session_seconds)
        )
        self.rollover_drain_seconds = float(
            config.params.get(
                "rollover_drain_seconds", self.rollover_drain_seconds
            )
        )
        self.audio_queue_maxsize = int(
            config.params.get("audio_queue_maxsize", self.audio_queue_maxsize)
        )

    async def transcribe_stream(
        self,
        audio_stream: AsyncIterator[bytes],
        request_config: ASRRequestConfig | None = None,
    ) -> AsyncIterator[TranscriptEvent]:
        import websockets

        connection_closed_error = _connection_closed_error_type(websockets)
        language = (request_config.language if request_config else "") or self.language
        session_id = (request_config.session_id if request_config else "") or ""
        transcription_params: dict[str, Any] = {}
        if language and language != "auto":
            transcription_params["language"] = language

        audio_queue: asyncio.Queue[bytes | None] = asyncio.Queue(
            maxsize=max(self.audio_queue_maxsize, 1)
        )
        producer_task = asyncio.create_task(
            self._queue_audio(audio_stream, audio_queue)
        )
        try:
            while True:
                first_chunk = await audio_queue.get()
                if first_chunk is None:
                    break
                if not first_chunk:
                    continue

                ws = None
                sender_task: asyncio.Task | None = None
                receiver_task: asyncio.Task | None = None
                try:
                    ws = await self._connect(websockets)
                    await self._configure_session(
                        ws, session_id, transcription_params
                    )
                    event_queue: asyncio.Queue[TranscriptEvent] = asyncio.Queue()
                    sender_task = asyncio.create_task(
                        self._send_session_audio(
                            ws,
                            audio_queue,
                            session_id,
                            first_chunk,
                            time.monotonic() + self.max_session_seconds,
                        )
                    )
                    receiver_task = asyncio.create_task(
                        self._receive_transcript_events(
                            ws, language, event_queue
                        )
                    )

                    send_result = ""
                    while not send_result:
                        if not event_queue.empty():
                            yield event_queue.get_nowait()
                            continue

                        event_task = asyncio.create_task(event_queue.get())
                        done, _ = await asyncio.wait(
                            {event_task, sender_task, receiver_task},
                            return_when=asyncio.FIRST_COMPLETED,
                        )

                        if event_task in done:
                            yield event_task.result()
                        else:
                            event_task.cancel()
                            with suppress(asyncio.CancelledError):
                                await event_task

                        if sender_task in done:
                            send_result = await sender_task

                        if receiver_task in done:
                            await receiver_task
                            if sender_task.done():
                                send_result = send_result or await sender_task
                            else:
                                raise RuntimeError(
                                    "Qwen ASR WebSocket closed before audio stream ended"
                                )

                    drain_timeout = (
                        self.rollover_drain_seconds
                        if send_result == _SEND_ROLLOVER
                        else None
                    )
                    async for event in self._drain_session_events(
                        receiver_task, event_queue, drain_timeout
                    ):
                        yield event

                    if send_result == _SEND_EOF:
                        break
                except connection_closed_error as exc:
                    raise RuntimeError(
                        f"Qwen ASR WebSocket closed unexpectedly: {exc}"
                    ) from exc
                finally:
                    if sender_task and not sender_task.done():
                        sender_task.cancel()
                        with suppress(asyncio.CancelledError):
                            await sender_task
                    if receiver_task and not receiver_task.done():
                        receiver_task.cancel()
                        with suppress(asyncio.CancelledError):
                            await receiver_task
                    if ws is not None:
                        await ws.close()
        finally:
            if producer_task.done():
                await producer_task
            else:
                producer_task.cancel()
                with suppress(asyncio.CancelledError):
                    await producer_task

    async def _queue_audio(
        self,
        audio_stream: AsyncIterator[bytes],
        audio_queue: asyncio.Queue[bytes | None],
    ) -> None:
        cancelled = False
        try:
            async for chunk in audio_stream:
                await audio_queue.put(chunk)
        except asyncio.CancelledError:
            cancelled = True
            raise
        finally:
            if cancelled:
                with suppress(asyncio.QueueFull):
                    audio_queue.put_nowait(None)
            else:
                await audio_queue.put(None)

    async def _configure_session(
        self,
        ws: Any,
        session_id: str,
        transcription_params: dict[str, Any],
    ) -> None:
        await self._send_json(
            ws,
            {
                "type": "session.update",
                "event_id": self._event_id(session_id, "session"),
                "session": {
                    "input_audio_format": "pcm",
                    "sample_rate": self.sample_rate,
                    "input_audio_transcription": transcription_params,
                    "turn_detection": {
                        "type": "server_vad",
                        "threshold": self.vad_threshold,
                        "silence_duration_ms": self.vad_silence_duration_ms,
                    },
                },
            },
        )

    async def _send_session_audio(
        self,
        ws: Any,
        audio_queue: asyncio.Queue[bytes | None],
        session_id: str,
        first_chunk: bytes,
        deadline: float,
    ) -> str:
        await self._send_audio_chunk(ws, first_chunk, session_id)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                await self._finish_session(ws, session_id)
                return _SEND_ROLLOVER

            try:
                chunk = await asyncio.wait_for(
                    audio_queue.get(), timeout=remaining
                )
            except asyncio.TimeoutError:
                await self._finish_session(ws, session_id)
                return _SEND_ROLLOVER

            if chunk is None:
                await self._finish_session(ws, session_id)
                return _SEND_EOF
            if not chunk:
                continue
            await self._send_audio_chunk(ws, chunk, session_id)

    async def _send_audio_chunk(
        self,
        ws: Any,
        chunk: bytes,
        session_id: str,
    ) -> None:
        await self._send_json(
            ws,
            {
                "type": "input_audio_buffer.append",
                "event_id": self._event_id(session_id, "audio"),
                "audio": base64.b64encode(chunk).decode("ascii"),
            },
        )

    async def _finish_session(self, ws: Any, session_id: str) -> None:
        await self._send_json(
            ws,
            {
                "type": "session.finish",
                "event_id": self._event_id(session_id, "finish"),
            },
        )

    async def _receive_transcript_events(
        self,
        ws: Any,
        language: str,
        event_queue: asyncio.Queue[TranscriptEvent],
    ) -> None:
        async for message in ws:
            event = json.loads(message)
            event_type = event.get("type", "")
            if event_type == "error":
                raise RuntimeError(f"Qwen ASR error: {event}")

            transcript = self._extract_transcript(event)
            if not transcript:
                continue

            is_final = self._is_final_event(event)
            await event_queue.put(
                TranscriptEvent(
                    text=transcript,
                    is_final=is_final,
                    language=event.get(
                        "language", language if language != "auto" else ""
                    ),
                    confidence=float(event.get("confidence", 0.0) or 0.0),
                )
            )

    async def _drain_session_events(
        self,
        receiver_task: asyncio.Task,
        event_queue: asyncio.Queue[TranscriptEvent],
        timeout_seconds: float | None,
    ) -> AsyncIterator[TranscriptEvent]:
        deadline = (
            time.monotonic() + max(timeout_seconds, 0.0)
            if timeout_seconds is not None
            else None
        )
        while True:
            while not event_queue.empty():
                yield event_queue.get_nowait()

            if receiver_task.done():
                await receiver_task
                while not event_queue.empty():
                    yield event_queue.get_nowait()
                return

            event_task = asyncio.create_task(event_queue.get())
            if deadline is None:
                done, _ = await asyncio.wait(
                    {event_task, receiver_task},
                    return_when=asyncio.FIRST_COMPLETED,
                )
            else:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    event_task.cancel()
                    with suppress(asyncio.CancelledError):
                        await event_task
                    return
                done, _ = await asyncio.wait(
                    {event_task, receiver_task},
                    timeout=remaining,
                    return_when=asyncio.FIRST_COMPLETED,
                )

            if event_task in done:
                yield event_task.result()
            else:
                event_task.cancel()
                with suppress(asyncio.CancelledError):
                    await event_task

    async def _connect(self, websockets: Any):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        try:
            return await websockets.connect(
                self.ws_url,
                additional_headers=headers,
            )
        except TypeError:
            return await websockets.connect(
                self.ws_url,
                extra_headers=headers,
            )

    @staticmethod
    async def _send_json(ws: Any, payload: dict[str, Any]) -> None:
        await ws.send(json.dumps(payload, ensure_ascii=False))

    @staticmethod
    def _event_id(session_id: str, suffix: str) -> str:
        base = session_id or "qwen_asr"
        return f"{base}_{suffix}_{int(time.time() * 1000)}"

    @classmethod
    def _extract_transcript(cls, event: dict[str, Any]) -> str:
        for key in ("transcript", "stash", "text", "delta"):
            value = event.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()

        for key in ("item", "result", "payload", "output"):
            nested = event.get(key)
            if isinstance(nested, dict):
                text = cls._extract_transcript(nested)
                if text:
                    return text

        choices = event.get("choices")
        if isinstance(choices, list):
            for choice in choices:
                if isinstance(choice, dict):
                    text = cls._extract_transcript(choice)
                    if text:
                        return text

        return ""

    @staticmethod
    def _is_final_event(event: dict[str, Any]) -> bool:
        event_type = str(event.get("type", "")).lower()
        if event.get("is_final") is True or event.get("final") is True:
            return True
        return any(token in event_type for token in ("completed", "final", "done"))

    async def shutdown(self) -> None:
        return None
