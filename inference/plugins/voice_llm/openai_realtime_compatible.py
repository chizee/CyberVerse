import asyncio
import base64
import json
import logging
import time
from dataclasses import dataclass
from typing import Any, AsyncIterator
from urllib.parse import urlencode

from inference.core.types import (
    AudioChunk,
    PluginConfig,
    VoiceLLMInputEvent,
    VoiceLLMOutputEvent,
    VoiceLLMSessionConfig,
)
from inference.plugins.voice_llm.base import VoiceCheckError, VoiceLLMPlugin

logger = logging.getLogger(__name__)


class OpenAIRealtimeCompatiblePlugin(VoiceLLMPlugin):
    """Realtime voice plugin for providers using OpenAI-compatible events."""

    name = "omni.openai_realtime_compatible"

    def __init__(self) -> None:
        self.provider_label = "openai_realtime_compatible"
        self.api_key = ""
        self.model = ""
        self.ws_url = ""
        self.voice = ""
        self.system_prompt = ""
        self.input_sample_rate = 16000
        self.output_sample_rate = 24000
        self.input_audio_format = "audio/pcm"
        self.output_audio_format = "audio/pcm"
        self.input_source_sample_rate = 16000
        self.vad_type: str | None = "server_vad"
        self.vad_threshold = 0.85
        self.vad_silence_duration_ms = 800
        self.vad_prefix_padding_ms = 300
        self.vad_create_response = True
        self.vad_interrupt_response = True
        self.vad_eagerness = ""
        self.input_transcription_model = ""
        self.input_transcription_language = ""
        self.input_transcription_prompt = ""
        self.reasoning_effort = ""
        self.session_schema = "legacy"
        self.session_audio_schema = "nested"
        self.proxy: str | None = None
        self.websocket_compression: str | None = None
        self.max_message_size = 10_000_000
        self.connect_attempts = 1
        self.connect_retry_delay_seconds = 1.0
        self._active_ws: Any | None = None

    async def initialize(self, config: PluginConfig) -> None:
        params = config.params
        self.provider_label = str(params.get("provider_label") or self.provider_label)
        self.api_key = str(params.get("api_key") or self.api_key)
        self.model = str(params.get("model") or self.model)
        self.ws_url = str(params.get("ws_url") or self.ws_url)
        self.voice = str(params.get("voice") or self.voice)
        self.system_prompt = str(params.get("system_prompt") or self.system_prompt)
        self.input_sample_rate = int(params.get("input_sample_rate", self.input_sample_rate))
        self.output_sample_rate = int(params.get("output_sample_rate", self.output_sample_rate))
        self.input_audio_format = str(params.get("input_audio_format") or self.input_audio_format)
        self.output_audio_format = str(params.get("output_audio_format") or self.output_audio_format)
        self.input_source_sample_rate = int(
            params.get("input_source_sample_rate", self.input_source_sample_rate)
        )
        self.vad_type = self._optional_str(params.get("vad_type"), self.vad_type)
        self.vad_threshold = float(params.get("vad_threshold", self.vad_threshold))
        self.vad_silence_duration_ms = int(
            params.get("vad_silence_duration_ms", self.vad_silence_duration_ms)
        )
        self.vad_prefix_padding_ms = int(
            params.get("vad_prefix_padding_ms", self.vad_prefix_padding_ms)
        )
        self.vad_create_response = self._bool(
            params.get("vad_create_response"),
            self.vad_create_response,
        )
        self.vad_interrupt_response = self._bool(
            params.get("vad_interrupt_response"),
            self.vad_interrupt_response,
        )
        self.vad_eagerness = str(params.get("vad_eagerness") or self.vad_eagerness)
        self.input_transcription_model = str(
            params.get("input_transcription_model") or self.input_transcription_model
        )
        self.input_transcription_language = str(
            params.get("input_transcription_language") or self.input_transcription_language
        )
        self.input_transcription_prompt = str(
            params.get("input_transcription_prompt") or self.input_transcription_prompt
        )
        self.reasoning_effort = str(params.get("reasoning_effort") or self.reasoning_effort)
        self.session_schema = str(params.get("session_schema") or self.session_schema)
        self.session_audio_schema = str(params.get("session_audio_schema") or self.session_audio_schema)
        self.proxy = self._optional_str(params.get("proxy"), self.proxy)
        self.websocket_compression = self._optional_str(
            params.get("websocket_compression"),
            self.websocket_compression,
        )
        self.max_message_size = int(params.get("max_message_size", self.max_message_size))
        self.connect_attempts = max(1, int(params.get("connect_attempts", self.connect_attempts)))
        self.connect_retry_delay_seconds = float(
            params.get("connect_retry_delay_seconds", self.connect_retry_delay_seconds)
        )
        if not self.api_key:
            raise RuntimeError(f"{self.provider_label} api_key is required")
        if not self.model:
            raise RuntimeError(f"{self.provider_label} model is required")
        if not self.ws_url:
            raise RuntimeError(f"{self.provider_label} ws_url is required")

    async def check_voice(
        self,
        session_config: VoiceLLMSessionConfig | None = None,
    ) -> None:
        import websockets

        ws = await self._connect(websockets)
        try:
            await self._configure_session(ws, session_config or VoiceLLMSessionConfig())
        except RuntimeError as exc:
            raise VoiceCheckError(str(exc)) from exc
        finally:
            await ws.close()

    async def converse_stream(
        self,
        input_stream: AsyncIterator[VoiceLLMInputEvent],
        session_config: VoiceLLMSessionConfig | None = None,
    ) -> AsyncIterator[VoiceLLMOutputEvent]:
        import websockets

        config = session_config or VoiceLLMSessionConfig()
        ws = await self._connect(websockets)
        self._active_ws = ws
        output_queue: asyncio.Queue[VoiceLLMOutputEvent | Exception | None] = asyncio.Queue()
        response_done = asyncio.Event()
        sender_task: asyncio.Task | None = None
        receiver_task: asyncio.Task | None = None
        try:
            await self._configure_session(ws, config)
            sender_task = asyncio.create_task(
                self._send_inputs(ws, input_stream, config.session_id, output_queue, response_done)
            )
            receiver_task = asyncio.create_task(self._receive_events(ws, config.session_id, output_queue, response_done))
            while True:
                item = await output_queue.get()
                if item is None:
                    break
                if isinstance(item, Exception):
                    raise item
                yield item
        finally:
            for task in (sender_task, receiver_task):
                if task and not task.done():
                    task.cancel()
                    try:
                        await task
                    except asyncio.CancelledError:
                        pass
            if self._active_ws is ws:
                self._active_ws = None
            await ws.close()

    async def interrupt(self) -> None:
        ws = self._active_ws
        if ws is None:
            return
        for event_type in ("response.cancel", "input_audio_buffer.clear"):
            try:
                await self._send_json(ws, {"type": event_type, "event_id": self._event_id("interrupt")})
            except Exception:
                logger.debug("Failed to send %s interrupt event", self.provider_label, exc_info=True)

    async def _connect(self, websockets: Any):
        headers = {"Authorization": f"Bearer {self.api_key}"}
        url = self._connection_url()
        kwargs = {
            "compression": self.websocket_compression,
            "max_size": self.max_message_size,
        }
        if self.proxy is not None:
            kwargs["proxy"] = self.proxy
        last_error: Exception | None = None
        for attempt in range(1, self.connect_attempts + 1):
            try:
                try:
                    return await websockets.connect(url, additional_headers=headers, **kwargs)
                except TypeError:
                    return await websockets.connect(url, extra_headers=headers, **kwargs)
            except Exception as exc:
                last_error = exc
                if attempt >= self.connect_attempts:
                    break
                logger.warning(
                    "%s realtime connect attempt %d/%d failed: %s",
                    self.provider_label,
                    attempt,
                    self.connect_attempts,
                    exc,
                )
                await asyncio.sleep(self.connect_retry_delay_seconds)
        assert last_error is not None
        raise last_error

    def _connection_url(self) -> str:
        if "{model}" in self.ws_url:
            return self.ws_url.replace("{model}", self.model)
        separator = "&" if "?" in self.ws_url else "?"
        return f"{self.ws_url}{separator}{urlencode({'model': self.model})}"

    async def _configure_session(
        self,
        ws: Any,
        session_config: VoiceLLMSessionConfig,
    ) -> None:
        await self._send_json(
            ws,
            {
                "type": "session.update",
                "event_id": self._event_id("session"),
                "session": self._session_payload(session_config),
            },
        )
        while True:
            event = self._decode_message(await ws.recv())
            event_type = event.get("type", "")
            if event_type == "session.updated":
                return
            if event_type == "error":
                raise RuntimeError(self._error_message(event))

    async def _send_inputs(
        self,
        ws: Any,
        input_stream: AsyncIterator[VoiceLLMInputEvent],
        session_id: str,
        output_queue: asyncio.Queue[VoiceLLMOutputEvent | Exception | None],
        response_done: asyncio.Event,
    ) -> None:
        expects_response = False
        try:
            async for event in input_stream:
                if event.text:
                    expects_response = True
                    response_done.clear()
                    await self._send_text(ws, session_id, event.text)
                    continue
                if event.audio:
                    audio = self._prepare_input_audio(event.audio)
                    await self._send_json(
                        ws,
                        {
                            "type": "input_audio_buffer.append",
                            "event_id": self._event_id("audio"),
                            "audio": base64.b64encode(audio).decode("ascii"),
                        },
                    )
            if expects_response:
                await asyncio.wait_for(response_done.wait(), timeout=60.0)
        except Exception as exc:
            await output_queue.put(exc)
        finally:
            try:
                await ws.close()
            except Exception:
                pass

    async def _send_text(self, ws: Any, session_id: str, text: str) -> None:
        await self._send_json(
            ws,
            {
                "type": "conversation.item.create",
                "event_id": self._event_id("text"),
                "item": {
                    "type": "message",
                    "role": "user",
                    "content": [{"type": "input_text", "text": text}],
                },
            },
        )
        await self._send_json(
            ws,
            {
                "type": "response.create",
                "event_id": self._event_id("text_response"),
                "response": self._response_payload(),
            },
        )

    async def _receive_events(
        self,
        ws: Any,
        session_id: str,
        output_queue: asyncio.Queue[VoiceLLMOutputEvent | Exception | None],
        response_done: asyncio.Event,
    ) -> None:
        turn_state = _RealtimeTurnState(session_id=session_id or self.provider_label)
        try:
            async for message in ws:
                event = self._decode_message(message)
                self._log_server_event(session_id, event)
                event_type = event.get("type", "")
                if event_type == "error":
                    raise RuntimeError(self._error_message(event))

                if event_type == "input_audio_buffer.speech_started":
                    turn_state.start_next_turn()
                    await output_queue.put(
                        VoiceLLMOutputEvent(
                            barge_in=True,
                            question_id=turn_state.question_id,
                            reply_id=turn_state.reply_id,
                        )
                    )
                    continue

                if event_type == "response.created":
                    response = event.get("response")
                    if isinstance(response, dict):
                        response_id = str(response.get("id") or "")
                        turn_state.ensure_turn()
                        if response_id:
                            turn_state.reply_id = response_id
                    continue

                if event_type in {
                    "conversation.item.input_audio_transcription.delta",
                    "conversation.item.input_audio_transcription.completed",
                    "conversation.item.input_audio_transcription.updated",
                }:
                    transcript = str(event.get("transcript") or "")
                    if not transcript:
                        transcript = str(event.get("delta") or "")
                    if event_type == "conversation.item.input_audio_transcription.delta":
                        if transcript:
                            turn_state.ensure_turn()
                            turn_state.user_text += transcript
                        continue
                    if not transcript:
                        transcript = turn_state.user_text
                    if transcript:
                        turn_state.ensure_turn()
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                user_transcript=transcript,
                                question_id=turn_state.question_id,
                                reply_id=turn_state.reply_id,
                            )
                        )
                    continue

                if event_type == "conversation.item.input_audio_transcription.failed":
                    logger.warning(
                        "%s input audio transcription failed session=%s fields=%s",
                        self.provider_label,
                        session_id or self.provider_label,
                        json.dumps(
                            self._server_event_log_fields(event),
                            ensure_ascii=False,
                            sort_keys=True,
                        ),
                    )
                    continue

                if event_type in {"response.output_audio_transcript.delta", "response.audio_transcript.delta"}:
                    delta = str(event.get("delta") or "")
                    if delta:
                        turn_state.ensure_turn()
                        turn_state.assistant_text += delta
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                transcript=delta,
                                question_id=turn_state.question_id,
                                reply_id=turn_state.reply_id,
                            )
                        )
                    continue

                if event_type in {"response.output_audio_transcript.done", "response.audio_transcript.done"}:
                    transcript = str(event.get("transcript") or "")
                    if transcript:
                        turn_state.assistant_text = transcript
                    continue

                if event_type in {"response.output_audio.delta", "response.audio.delta"}:
                    delta = str(event.get("delta") or "")
                    if not delta:
                        continue
                    audio_payload = base64.b64decode(delta)
                    if audio_payload:
                        turn_state.ensure_turn()
                        turn_state.has_audio = True
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                audio=AudioChunk(
                                    data=audio_payload,
                                    sample_rate=self.output_sample_rate,
                                    channels=1,
                                    format="pcm_s16le",
                                ),
                                question_id=turn_state.question_id,
                                reply_id=turn_state.reply_id,
                            )
                        )
                    continue

                if event_type == "response.done":
                    if turn_state.has_content:
                        await output_queue.put(
                            VoiceLLMOutputEvent(
                                audio=AudioChunk(
                                    data=b"",
                                    sample_rate=self.output_sample_rate,
                                    channels=1,
                                    format="pcm_s16le",
                                    is_final=True,
                                )
                                if turn_state.has_audio
                                else None,
                                transcript=turn_state.assistant_text,
                                is_final=True,
                                question_id=turn_state.question_id,
                                reply_id=turn_state.reply_id,
                            )
                        )
                    turn_state.reset()
                    response_done.set()
                    continue
        except Exception as exc:
            if not getattr(ws, "closed", False):
                await output_queue.put(exc)
        finally:
            await output_queue.put(None)

    def _session_payload(self, session_config: VoiceLLMSessionConfig) -> dict[str, Any]:
        if self.session_schema == "openai_realtime_current":
            return self._openai_current_session_payload(session_config)
        payload: dict[str, Any] = {
            "modalities": ["text", "audio"],
            "voice": session_config.voice or self.voice,
            "instructions": self._instructions(session_config),
            "turn_detection": (
                None
                if self.vad_type is None
                else {
                    "type": self.vad_type,
                    "threshold": self.vad_threshold,
                    "silence_duration_ms": self.vad_silence_duration_ms,
                }
            ),
        }
        if self.session_audio_schema == "nested":
            payload["audio"] = {
                "input": {"format": {"type": self.input_audio_format, "rate": self.input_sample_rate}},
                "output": {"format": {"type": self.output_audio_format, "rate": self.output_sample_rate}},
            }
        else:
            payload["input_audio_format"] = self._legacy_audio_format(self.input_audio_format)
            payload["output_audio_format"] = self._legacy_audio_format(self.output_audio_format)
        if self.reasoning_effort:
            payload["reasoning"] = {"effort": self.reasoning_effort}
        return payload

    def _openai_current_session_payload(self, session_config: VoiceLLMSessionConfig) -> dict[str, Any]:
        audio_input: dict[str, Any] = {
            "format": {"type": self.input_audio_format, "rate": self.input_sample_rate},
        }
        transcription = self._input_transcription_payload()
        if transcription is not None:
            audio_input["transcription"] = transcription
        if self.vad_type is None:
            audio_input["turn_detection"] = None
        else:
            audio_input["turn_detection"] = self._openai_current_turn_detection_payload()

        payload: dict[str, Any] = {
            "type": "realtime",
            "model": self.model,
            "output_modalities": ["audio"],
            "instructions": self._instructions(session_config),
            "audio": {
                "input": audio_input,
                "output": {
                    "format": {"type": self.output_audio_format, "rate": self.output_sample_rate},
                    "voice": session_config.voice or self.voice,
                },
            },
        }
        if self.reasoning_effort:
            payload["reasoning"] = {"effort": self.reasoning_effort}
        return payload

    def _input_transcription_payload(self) -> dict[str, Any] | None:
        model = self.input_transcription_model.strip()
        if not model:
            return None
        payload: dict[str, Any] = {"model": model}
        language = self.input_transcription_language.strip()
        if language:
            payload["language"] = language
        prompt = self.input_transcription_prompt.strip()
        if prompt:
            payload["prompt"] = prompt
        return payload

    def _openai_current_turn_detection_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "type": self.vad_type,
            "create_response": self.vad_create_response,
            "interrupt_response": self.vad_interrupt_response,
        }
        if self.vad_type == "server_vad":
            payload["threshold"] = self.vad_threshold
            payload["prefix_padding_ms"] = self.vad_prefix_padding_ms
            payload["silence_duration_ms"] = self.vad_silence_duration_ms
        if self.vad_type == "semantic_vad" and self.vad_eagerness.strip():
            payload["eagerness"] = self.vad_eagerness.strip()
        return payload

    def _response_payload(self) -> dict[str, Any]:
        if self.session_schema == "openai_realtime_current":
            return {"output_modalities": ["audio"]}
        return {"modalities": ["text", "audio"]}

    def _instructions(self, session_config: VoiceLLMSessionConfig) -> str:
        parts: list[str] = []
        if session_config.bot_name:
            parts.append(f"名字：{session_config.bot_name}")
        parts.append(session_config.system_prompt or self.system_prompt)
        if session_config.speaking_style:
            parts.append(f"说话风格：{session_config.speaking_style}")
        if session_config.dialog_context:
            parts.append("以下是最近的对话上下文，请在回答时保持连续性：")
            for item in session_config.dialog_context:
                role = "用户" if item.role == "user" else "助手"
                parts.append(f"{role}：{item.text}")
        return "\n".join(part for part in parts if part.strip())

    @staticmethod
    def _legacy_audio_format(value: str) -> str:
        normalized = value.strip().lower()
        if normalized == "audio/pcm":
            return "pcm"
        if normalized == "audio/pcmu":
            return "g711_ulaw"
        if normalized == "audio/pcma":
            return "g711_alaw"
        return normalized or "pcm"

    def _prepare_input_audio(self, audio: bytes) -> bytes:
        if self.input_source_sample_rate == self.input_sample_rate:
            return audio
        if self.input_audio_format != "audio/pcm":
            return audio
        return self._resample_pcm16_mono(audio, self.input_source_sample_rate, self.input_sample_rate)

    @staticmethod
    def _resample_pcm16_mono(audio: bytes, source_rate: int, target_rate: int) -> bytes:
        if not audio or source_rate <= 0 or target_rate <= 0 or source_rate == target_rate:
            return audio
        sample_count = len(audio) // 2
        if sample_count == 0:
            return audio
        samples = [
            int.from_bytes(audio[index : index + 2], byteorder="little", signed=True)
            for index in range(0, sample_count * 2, 2)
        ]
        target_count = max(1, round(sample_count * target_rate / source_rate))
        if sample_count == 1:
            return int(samples[0]).to_bytes(2, byteorder="little", signed=True) * target_count
        ratio = source_rate / target_rate
        output = bytearray(target_count * 2)
        for out_index in range(target_count):
            position = out_index * ratio
            left = int(position)
            right = min(left + 1, sample_count - 1)
            fraction = position - left
            value = round(samples[left] * (1.0 - fraction) + samples[right] * fraction)
            value = max(-32768, min(32767, value))
            output[out_index * 2 : out_index * 2 + 2] = int(value).to_bytes(
                2,
                byteorder="little",
                signed=True,
            )
        return bytes(output)

    @staticmethod
    async def _send_json(ws: Any, payload: dict[str, Any]) -> None:
        await ws.send(json.dumps(payload, ensure_ascii=False))

    @staticmethod
    def _decode_message(message: str | bytes) -> dict[str, Any]:
        if isinstance(message, bytes):
            message = message.decode("utf-8")
        return json.loads(message)

    def _event_id(self, suffix: str) -> str:
        return f"{self.provider_label}_{suffix}_{int(time.time() * 1000)}"

    def _error_message(self, event: dict[str, Any]) -> str:
        error = event.get("error")
        if isinstance(error, dict):
            message = error.get("message") or error.get("msg") or error.get("code")
            if message:
                return str(message)
        if isinstance(error, str):
            return error
        return f"{self.provider_label} error: {event}"

    @staticmethod
    def _optional_str(value: Any, default: str | None = "") -> str | None:
        if value is None:
            return default
        text = str(value).strip()
        if text.lower() == "null":
            return None
        if text.startswith("${") and text.endswith("}"):
            return None
        return text

    @staticmethod
    def _bool(value: Any, default: bool) -> bool:
        if value is None:
            return default
        if isinstance(value, bool):
            return value
        text = str(value).strip().lower()
        if text in {"1", "true", "yes", "on"}:
            return True
        if text in {"0", "false", "no", "off"}:
            return False
        return default

    @classmethod
    def _server_event_log_fields(cls, event: dict[str, Any]) -> dict[str, Any]:
        event_type = str(event.get("type") or "")
        fields: dict[str, Any] = {}
        for key in ("response_id", "item_id", "call_id", "name", "output_index"):
            if key in event and event.get(key) not in (None, ""):
                fields[key] = event.get(key)
        if event_type in {"response.output_audio.delta", "response.audio.delta"}:
            fields["audio_delta_b64_len"] = len(str(event.get("delta") or ""))
        elif "delta" in event:
            fields["delta"] = cls._clip_text(event.get("delta"))
        if "transcript" in event:
            fields["transcript"] = cls._clip_text(event.get("transcript"))
        response = event.get("response")
        if isinstance(response, dict):
            fields["response"] = {
                key: response.get(key)
                for key in ("id", "status")
                if response.get(key) not in (None, "")
            }
        error = event.get("error")
        if error:
            fields["error"] = cls._clip_text(error)
        return fields

    @staticmethod
    def _clip_text(value: Any, limit: int = 180) -> str:
        text = str(value or "")
        if len(text) <= limit:
            return text
        return text[:limit] + "..."

    @classmethod
    def _server_event_log_level(cls, event: dict[str, Any]) -> int:
        event_type = str(event.get("type") or "")
        if event_type == "error":
            return logging.ERROR
        if event_type in {
            "conversation.created",
            "session.created",
            "session.updated",
            "input_audio_buffer.speech_started",
            "conversation.item.input_audio_transcription.completed",
            "response.created",
            "response.output_audio.done",
            "response.output_audio_transcript.done",
            "response.done",
        }:
            return logging.INFO
        return logging.DEBUG

    def _log_server_event(self, session_id: str, event: dict[str, Any]) -> None:
        level = self._server_event_log_level(event)
        if not logger.isEnabledFor(level):
            return
        logger.log(
            level,
            "%s model event session=%s type=%s fields=%s",
            self.provider_label,
            session_id or self.provider_label,
            str(event.get("type") or "unknown"),
            json.dumps(self._server_event_log_fields(event), ensure_ascii=False, sort_keys=True),
        )

    async def shutdown(self) -> None:
        if self._active_ws is not None:
            await self._active_ws.close()
            self._active_ws = None


@dataclass
class _RealtimeTurnState:
    session_id: str
    turn_index: int = 0
    question_id: str = ""
    reply_id: str = ""
    user_text: str = ""
    assistant_text: str = ""
    has_audio: bool = False

    @property
    def has_content(self) -> bool:
        return self.has_audio or bool(self.assistant_text)

    def start_next_turn(self) -> None:
        self.turn_index += 1
        timestamp = int(time.time() * 1000)
        self.question_id = f"{self.session_id}_q_{self.turn_index}_{timestamp}"
        self.reply_id = ""
        self.user_text = ""
        self.assistant_text = ""
        self.has_audio = False

    def ensure_turn(self) -> None:
        if not self.question_id:
            self.start_next_turn()

    def reset(self) -> None:
        self.question_id = ""
        self.reply_id = ""
        self.user_text = ""
        self.assistant_text = ""
        self.has_audio = False
