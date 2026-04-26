import asyncio
import io
import json
import logging
import wave
from typing import AsyncIterator
from urllib import request

import numpy as np

from inference.core.types import AudioChunk, PluginConfig
from inference.plugins.tts.base import AudioRechunker, TTSPlugin

logger = logging.getLogger(__name__)

DEFAULT_BASE_URL = "https://dashscope.aliyuncs.com/api/v1"
DEFAULT_MODEL = "qwen3-tts-flash"
DEFAULT_VOICE = "Cherry"
LEGACY_VOICE_MAP = {
    "alloy": DEFAULT_VOICE,
    "echo": DEFAULT_VOICE,
    "fable": DEFAULT_VOICE,
    "nova": DEFAULT_VOICE,
    "onyx": DEFAULT_VOICE,
    "shimmer": DEFAULT_VOICE,
}


class QwenTTSPlugin(TTSPlugin):
    name = "tts.qwen"

    def __init__(self) -> None:
        self.api_key = ""
        self.base_url = DEFAULT_BASE_URL
        self.voice = DEFAULT_VOICE
        self.model = DEFAULT_MODEL
        self.language_type = "Chinese"
        self.rechunker = AudioRechunker()
        self._qwen_sample_rate = 24000

    async def initialize(self, config: PluginConfig) -> None:
        self.api_key = str(config.params.get("api_key", "") or "").strip()
        if not self.api_key:
            raise ValueError("api_key is required for tts.qwen")

        self.base_url = str(config.params.get("base_url", DEFAULT_BASE_URL) or DEFAULT_BASE_URL).rstrip("/")
        self.voice = str(config.params.get("voice", DEFAULT_VOICE) or DEFAULT_VOICE).strip() or DEFAULT_VOICE
        self.model = str(config.params.get("model", DEFAULT_MODEL) or DEFAULT_MODEL).strip() or DEFAULT_MODEL
        self.language_type = str(config.params.get("language_type", "Chinese") or "Chinese").strip() or "Chinese"
        self.rechunker = AudioRechunker(
            chunk_samples=17920,
            sample_rate=16000,
        )

    async def synthesize_stream(
        self, text_stream: AsyncIterator[str]
    ) -> AsyncIterator[AudioChunk]:
        async for sentence in text_stream:
            if not sentence.strip():
                continue

            try:
                wav_bytes = await asyncio.to_thread(self._synthesize_once, sentence)
                audio_np, sample_rate = self._wav_to_float32(wav_bytes)
            except Exception:
                logger.exception("Qwen TTS API call failed for: %s", sentence[:50])
                continue

            if sample_rate != 16000:
                audio_np = self._resample(audio_np, sample_rate, 16000)

            chunks = self.rechunker.feed(audio_np)
            for chunk in chunks:
                yield chunk

        final_chunk = self.rechunker.flush()
        if final_chunk:
            yield final_chunk

    def _synthesize_once(self, sentence: str) -> bytes:
        payload = {
            "model": self.model,
            "input": {
                "text": sentence,
                "voice": self._resolve_voice(self.voice),
                "language_type": self._resolve_language_type(sentence),
            },
        }
        req = request.Request(
            url=f"{self.base_url}/services/aigc/multimodal-generation/generation",
            data=json.dumps(payload).encode("utf-8"),
            headers={
                "Authorization": f"Bearer {self.api_key}",
                "Content-Type": "application/json",
            },
            method="POST",
        )
        with request.urlopen(req, timeout=90) as resp:
            data = json.loads(resp.read().decode("utf-8"))

        audio_url = (((data or {}).get("output") or {}).get("audio") or {}).get("url")
        if not audio_url:
            raise RuntimeError(f"Qwen TTS response missing output.audio.url: {data}")

        with request.urlopen(audio_url, timeout=90) as resp:
            return resp.read()

    def _resolve_voice(self, voice: str) -> str:
        normalized = voice.strip()
        if not normalized:
            return DEFAULT_VOICE
        return LEGACY_VOICE_MAP.get(normalized.lower(), normalized)

    def _resolve_language_type(self, sentence: str) -> str:
        if self.language_type.lower() != "auto":
            return self.language_type
        return "Chinese" if any("\u4e00" <= ch <= "\u9fff" for ch in sentence) else "English"

    @staticmethod
    def _wav_to_float32(wav_bytes: bytes) -> tuple[np.ndarray, int]:
        with wave.open(io.BytesIO(wav_bytes), "rb") as wav_file:
            sample_rate = wav_file.getframerate()
            sample_width = wav_file.getsampwidth()
            channels = wav_file.getnchannels()
            raw = wav_file.readframes(wav_file.getnframes())

        if sample_width != 2:
            raise ValueError(f"Unsupported Qwen TTS sample width: {sample_width}")

        audio = np.frombuffer(raw, dtype=np.int16).astype(np.float32) / 32768.0
        if channels > 1:
            audio = audio.reshape(-1, channels).mean(axis=1)
        return audio, sample_rate

    @staticmethod
    def _resample(audio: np.ndarray, orig_sr: int, target_sr: int) -> np.ndarray:
        if orig_sr == target_sr:
            return audio

        try:
            from math import gcd
            from scipy.signal import resample_poly

            g = gcd(orig_sr, target_sr)
            up = target_sr // g
            down = orig_sr // g
            return resample_poly(audio, up, down).astype(np.float32)
        except Exception:
            if audio.size == 0 or orig_sr <= 0 or target_sr <= 0:
                return np.array([], dtype=np.float32)

            n_src = int(audio.shape[0])
            n_dst = max(int(round(n_src * target_sr / orig_sr)), 1)
            if n_src == 1 or n_dst == 1:
                return np.array([float(audio[0])], dtype=np.float32)

            x_old = np.linspace(0.0, 1.0, n_src, dtype=np.float64)
            x_new = np.linspace(0.0, 1.0, n_dst, dtype=np.float64)
            y_new = np.interp(x_new, x_old, audio.astype(np.float64, copy=False))
            return y_new.astype(np.float32)

    async def shutdown(self) -> None:
        self.rechunker.reset()
