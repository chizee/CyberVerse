import asyncio
import json
import logging
import uuid
from typing import AsyncIterator

from inference.core.types import AudioChunk, PluginConfig, VoiceLLMOutputEvent, VoiceLLMSessionConfig
from inference.plugins.voice_llm.base import VoiceLLMPlugin
from inference.plugins.voice_llm.doubao_config import DoubaoSessionConfig
from inference.plugins.voice_llm.doubao_protocol import (
    DecodedFrame,
    DoubaoEvent,
    MSGTYPE_AUDIO_ONLY_CLIENT,
    MSGTYPE_FULL_CLIENT,
    SERIALIZATION_JSON,
    SERIALIZATION_RAW,
    compress_payload,
    decode_frame,
    decompress_payload,
    encode_frame,
)

logger = logging.getLogger(__name__)
_MAX_OUTPUT_QUEUE = 64


class DoubaoRealtimePlugin(VoiceLLMPlugin):
    """Doubao realtime voice LLM plugin (WebSocket binary protocol)."""

    name = "voice_llm.doubao"

    def __init__(self) -> None:
        self._config: DoubaoSessionConfig | None = None
        self._ws = None
        self._session_id: str | None = None
        self._interrupting = False

    async def initialize(self, config: PluginConfig) -> None:
        self._config = DoubaoSessionConfig.from_plugin_config(config)

    async def converse_stream(
        self,
        audio_stream: AsyncIterator[bytes],
        session_config: VoiceLLMSessionConfig | None = None,
    ) -> AsyncIterator[VoiceLLMOutputEvent]:
        import websockets

        assert self._config is not None
        effective_config = self._config
        if session_config is not None:
            effective_config = self._config.with_overrides(session_config)

        attempt = 0
        last_error = None
        while attempt <= effective_config.max_retries:
            try:
                async for event in self._converse_stream_inner(audio_stream, effective_config):
                    yield event
                return
            except websockets.InvalidStatus as e:
                status_code = getattr(getattr(e, "response", None), "status_code", None)
                if status_code == 401:
                    raise RuntimeError(
                        "Doubao authentication failed (HTTP 401): "
                        "check DOUBAO_ACCESS_TOKEN / DOUBAO_APP_ID"
                    ) from e
                raise RuntimeError(
                    f"Doubao WebSocket handshake failed (HTTP {status_code})"
                ) from e
            except (websockets.ConnectionClosed, ConnectionError, OSError) as e:
                attempt += 1
                last_error = e
                if attempt > effective_config.max_retries:
                    break
                backoff = min(
                    effective_config.retry_backoff_base * (2 ** (attempt - 1)),
                    effective_config.retry_backoff_max,
                )
                logger.warning(
                    "Doubao connection failed (attempt %d/%d), retrying in %.1fs: %s",
                    attempt,
                    effective_config.max_retries,
                    backoff,
                    e,
                )
                await asyncio.sleep(backoff)
        raise RuntimeError(
            f"Doubao connection failed after {attempt} attempts: {last_error}"
        )

    async def _converse_stream_inner(
        self, audio_stream: AsyncIterator[bytes], config: DoubaoSessionConfig
    ) -> AsyncIterator[VoiceLLMOutputEvent]:
        import websockets

        output_queue: asyncio.Queue[VoiceLLMOutputEvent | None] = asyncio.Queue(
            maxsize=_MAX_OUTPUT_QUEUE
        )
        done = asyncio.Event()

        session_id = str(uuid.uuid4())
        connect_id = str(uuid.uuid4())

        headers = config.build_ws_headers(connect_id)

        async with websockets.connect(
            config.ws_url, additional_headers=headers
        ) as ws:
            self._ws = ws
            self._session_id = session_id

            # 1) StartConnection (event=1)
            await ws.send(
                encode_frame(
                    msg_type_bits=MSGTYPE_FULL_CLIENT,
                    serialization_bits=SERIALIZATION_JSON,
                    event=DoubaoEvent.START_CONNECTION,
                    session_id=None,
                    payload=compress_payload(
                        b"{}", config.compression_bits
                    ),
                    compression_bits=config.compression_bits,
                )
            )

            # 2) Wait ConnectionStarted (event=50)
            first = await ws.recv()
            if isinstance(first, str):
                raise RuntimeError("Doubao handshake returned text frame unexpectedly")
            _ = decode_frame(first)

            # 3) StartSession (event=100)
            start_session_payload = config.build_start_session_payload()
            await ws.send(
                encode_frame(
                    msg_type_bits=MSGTYPE_FULL_CLIENT,
                    serialization_bits=SERIALIZATION_JSON,
                    event=DoubaoEvent.START_SESSION,
                    session_id=session_id,
                    payload=compress_payload(
                        json.dumps(start_session_payload, ensure_ascii=False).encode(
                            "utf-8"
                        ),
                        config.compression_bits,
                    ),
                    compression_bits=config.compression_bits,
                )
            )

            # 4) Wait SessionStarted (event=150)
            second = await ws.recv()
            if isinstance(second, str):
                raise RuntimeError(
                    "Doubao StartSession returned text frame unexpectedly"
                )
            _ = decode_frame(second)

            # 5) SayHello (event=300) only when the character explicitly defines one.
            if config.has_welcome_message:
                say_hello_payload = config.build_say_hello_payload()
                await ws.send(
                    encode_frame(
                        msg_type_bits=MSGTYPE_FULL_CLIENT,
                        serialization_bits=SERIALIZATION_JSON,
                        event=DoubaoEvent.SAY_HELLO,
                        session_id=session_id,
                        payload=compress_payload(
                            json.dumps(say_hello_payload, ensure_ascii=False).encode(
                                "utf-8"
                            ),
                            config.compression_bits,
                        ),
                        compression_bits=config.compression_bits,
                    )
                )

            sender_task = asyncio.create_task(
                self._send_audio(ws, audio_stream, session_id, config)
            )
            receiver_task = asyncio.create_task(
                self._receive_audio(ws, output_queue, done, config)
            )

            def _on_task_done(task: asyncio.Task) -> None:
                if task.cancelled():
                    return
                exc = task.exception()
                if exc is not None:
                    logger.error("Doubao task failed: %s", exc)
                    done.set()
                    try:
                        output_queue.put_nowait(None)
                    except asyncio.QueueFull:
                        pass

            sender_task.add_done_callback(_on_task_done)
            receiver_task.add_done_callback(_on_task_done)

            try:
                while True:
                    try:
                        event = await asyncio.wait_for(output_queue.get(), timeout=1.0)
                    except TimeoutError:
                        if done.is_set():
                            break
                        continue
                    if event is None:
                        break
                    yield event
            finally:
                for task in (sender_task, receiver_task):
                    task.cancel()
                for task in (sender_task, receiver_task):
                    try:
                        await task
                    except (asyncio.CancelledError, Exception):
                        pass

    async def _send_audio(
        self, ws, audio_stream: AsyncIterator[bytes], session_id: str,
        config: DoubaoSessionConfig,
    ) -> None:
        try:
            chunk_count = 0
            async for chunk_bytes in audio_stream:
                if not chunk_bytes:
                    continue
                chunk_count += 1
                await ws.send(
                    encode_frame(
                        msg_type_bits=MSGTYPE_AUDIO_ONLY_CLIENT,
                        serialization_bits=SERIALIZATION_RAW,
                        event=DoubaoEvent.TASK_REQUEST,
                        session_id=session_id,
                        payload=compress_payload(
                            chunk_bytes, config.compression_bits
                        ),
                        compression_bits=config.compression_bits,
                    )
                )
            await ws.send(
                encode_frame(
                    msg_type_bits=MSGTYPE_FULL_CLIENT,
                    serialization_bits=SERIALIZATION_JSON,
                    event=DoubaoEvent.FINISH_SESSION,
                    session_id=session_id,
                    payload=compress_payload(
                        b"{}", config.compression_bits
                    ),
                    compression_bits=config.compression_bits,
                )
            )
        except Exception:
            logger.exception("Failed to send audio to Doubao")
            raise

    async def _receive_audio(
        self,
        ws,
        output_queue: asyncio.Queue[VoiceLLMOutputEvent | None],
        done: asyncio.Event,
        config: DoubaoSessionConfig,
    ) -> None:
        turn_has_audio = False
        turn_final_sent = False
        turn_transcript = ""
        last_was_idle_timeout = False
        try:
            async for message in ws:
                if isinstance(message, str):
                    continue
                frame = message
                try:
                    decoded = decode_frame(frame)
                except Exception:
                    logger.warning("Failed to decode Doubao frame (%d bytes)", len(frame))
                    continue

                if decoded.is_audio():
                    audio_payload = decompress_payload(
                        decoded.payload, decoded.compression_bits
                    )
                    logger.debug(
                        "Doubao recv: audio frame, event=%s, %d bytes",
                        decoded.event,
                        len(audio_payload),
                    )
                    await output_queue.put(
                        VoiceLLMOutputEvent(
                            audio=AudioChunk(
                                data=audio_payload,
                                sample_rate=config.output_sample_rate,
                                channels=1,
                                format=config.output_audio_format,
                                is_final=False,
                            ),
                        )
                    )
                    if len(audio_payload) > 0:
                        turn_has_audio = True
                        turn_final_sent = False
                elif decoded.is_full_server():
                    try:
                        text_payload = decompress_payload(
                            decoded.payload, decoded.compression_bits
                        )
                        data = json.loads(text_payload)
                    except (json.JSONDecodeError, Exception):
                        data = {}
                    logger.debug(
                        "Doubao recv: FullServer event=%s payload=%s", decoded.event, data
                    )
                    # Extract transcript from relevant events:
                    # - 351 (TTS_SENTENCE_DONE): 'text' = assistant sentence
                    # - 451 (ASR_RESULT): 'results[0].text' = user speech
                    # - 550 (LLM_TOKEN): 'content' = LLM streaming token
                    assistant_text = ""
                    user_text = ""

                    if decoded.event == DoubaoEvent.TTS_SENTENCE_DONE:
                        assistant_text = data.get("text", "")
                    elif decoded.event == DoubaoEvent.ASR_RESULT:
                        results = data.get("results", [])
                        if results:
                            user_text = results[0].get("text", "")
                            is_interim = results[0].get("is_interim", True)
                            if user_text and not is_interim:
                                await output_queue.put(
                                    VoiceLLMOutputEvent(
                                        user_transcript=user_text,
                                    )
                                )
                    elif decoded.event == DoubaoEvent.LLM_TOKEN:
                        assistant_text = data.get("content", "")

                    # LLM tokens provide incremental text for the happy path.
                    # When Doubao only returns sentence-done text with no audio
                    # frames, keep that text as the turn transcript so the Go
                    # side can fall back to local TTS.
                    if assistant_text and decoded.event == DoubaoEvent.LLM_TOKEN:
                        turn_transcript += assistant_text
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                transcript=assistant_text,
                            )
                        )
                    elif (
                        assistant_text
                        and decoded.event == DoubaoEvent.TTS_SENTENCE_DONE
                        and not turn_transcript
                    ):
                        turn_transcript = assistant_text

                    # event 359 (REPLY_DONE) = assistant reply audio fully sent
                    if decoded.event == DoubaoEvent.REPLY_DONE:
                        if turn_has_audio and not turn_final_sent:
                            logger.debug(
                                "Doubao reply done (event=359), emit turn_final marker"
                            )
                            await output_queue.put(
                                VoiceLLMOutputEvent(
                                    audio=AudioChunk(
                                        data=b"",
                                        sample_rate=config.output_sample_rate,
                                        channels=1,
                                        format=config.output_audio_format,
                                        is_final=True,
                                    ),
                                    transcript=turn_transcript,
                                    is_final=True,
                                )
                            )
                            turn_final_sent = True
                            turn_transcript = ""
                        elif turn_transcript and not turn_final_sent:
                            logger.debug(
                                "Doubao reply done without audio, emit transcript-only final"
                            )
                            await output_queue.put(
                                VoiceLLMOutputEvent(
                                    transcript=turn_transcript,
                                    is_final=True,
                                )
                            )
                            turn_final_sent = True
                            turn_transcript = ""
                    elif decoded.event in (
                        DoubaoEvent.SESSION_FINISHED,
                        DoubaoEvent.SESSION_FAILED,
                    ):
                        # Handle interrupt: if we initiated the finish, reset and don't terminate
                        if (
                            self._interrupting
                            and decoded.event == DoubaoEvent.SESSION_FINISHED
                        ):
                            self._interrupting = False
                            continue
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                audio=AudioChunk(
                                    data=b"",
                                    sample_rate=config.output_sample_rate,
                                    channels=1,
                                    format=config.output_audio_format,
                                    is_final=True,
                                ),
                                transcript=turn_transcript,
                                is_final=True,
                            )
                        )
                        await output_queue.put(None)
                        break
                elif decoded.is_error():
                    try:
                        err_text = decompress_payload(decoded.payload, decoded.compression_bits)
                    except Exception:
                        err_text = decoded.payload[:200]
                    err_text_str = (
                        err_text.decode("utf-8", errors="ignore")
                        if isinstance(err_text, (bytes, bytearray))
                        else str(err_text)
                    )
                    is_idle_timeout = "DialogAudioIdleTimeoutError" in err_text_str

                    if is_idle_timeout:
                        if turn_transcript and not turn_final_sent:
                            logger.info(
                                "Doubao idle timeout with transcript-only reply, emit final transcript"
                            )
                            await output_queue.put(
                                VoiceLLMOutputEvent(
                                    transcript=turn_transcript,
                                    is_final=True,
                                )
                            )
                        logger.info(
                            "Doubao idle timeout: keep session open for next turn, payload=%s",
                            err_text_str,
                        )
                        turn_has_audio = False
                        turn_final_sent = False
                        turn_transcript = ""
                        last_was_idle_timeout = True
                        continue

                    if turn_final_sent:
                        # Reply already completed; idle timeout is expected (e.g. welcome greeting
                        # with no user audio). Log at INFO and skip emitting a duplicate final.
                        logger.info(
                            "Doubao post-reply error (expected idle timeout): code=%s payload=%s",
                            decoded.error_code,
                            err_text,
                        )
                    else:
                        logger.error(
                            "Doubao recv: Error code=%s payload=%s",
                            decoded.error_code, err_text,
                        )
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                audio=AudioChunk(
                                    data=b"",
                                    sample_rate=config.output_sample_rate,
                                    channels=1,
                                    format=config.output_audio_format,
                                    is_final=True,
                                ),
                                is_final=True,
                            )
                        )
                    await output_queue.put(None)
                    break
        except Exception as exc:
            import websockets
            if isinstance(exc, websockets.ConnectionClosedError) and last_was_idle_timeout:
                logger.info(
                    "Doubao WebSocket closed after idle timeout (expected), ending stream gracefully"
                )
            else:
                logger.exception("Failed to receive audio from Doubao")
                raise
        finally:
            done.set()
            try:
                output_queue.put_nowait(None)
            except asyncio.QueueFull:
                pass

    async def interrupt(self) -> None:
        ws = self._ws
        session_id = self._session_id
        if ws is None or session_id is None:
            return
        self._interrupting = True
        try:
            await ws.send(
                encode_frame(
                    msg_type_bits=MSGTYPE_FULL_CLIENT,
                    serialization_bits=SERIALIZATION_JSON,
                    event=DoubaoEvent.FINISH_SESSION,
                    session_id=session_id,
                    payload=compress_payload(
                        b"{}", self._config.compression_bits
                    ),
                    compression_bits=self._config.compression_bits,
                )
            )
        except Exception:
            logger.warning("Failed to send interrupt frame to Doubao")

    async def shutdown(self) -> None:
        return
