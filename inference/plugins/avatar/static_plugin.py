import logging
from typing import AsyncIterator

import numpy as np

from inference.core.types import AudioChunk, PluginConfig, VideoChunk
from inference.plugins.avatar.base import AvatarPlugin

logger = logging.getLogger(__name__)


def _safe_int(value, default: int) -> int:
    try:
        return int(value)
    except Exception:
        return int(default)


def _parse_bg_color(value) -> tuple[int, int, int]:
    # Accept [r,g,b] or "r,g,b" or a single int.
    if value is None:
        return (128, 128, 128)
    if isinstance(value, (list, tuple)) and len(value) >= 3:
        try:
            r, g, b = int(value[0]), int(value[1]), int(value[2])
            return (max(0, min(r, 255)), max(0, min(g, 255)), max(0, min(b, 255)))
        except Exception:
            return (128, 128, 128)
    if isinstance(value, str) and "," in value:
        parts = [p.strip() for p in value.split(",")]
        if len(parts) >= 3:
            try:
                r, g, b = int(parts[0]), int(parts[1]), int(parts[2])
                return (max(0, min(r, 255)), max(0, min(g, 255)), max(0, min(b, 255)))
            except Exception:
                return (128, 128, 128)
    try:
        v = int(value)
        v = max(0, min(v, 255))
        return (v, v, v)
    except Exception:
        return (128, 128, 128)


def _num_audio_samples(chunk: AudioChunk) -> int:
    b = chunk.data or b""
    fmt = (chunk.format or "").strip().lower()
    if fmt in ("pcm_s16le", "s16le", "int16", "pcm16"):
        samples = len(b) // 2
    elif fmt in ("float32", "f32", "pcm_f32le"):
        samples = len(b) // 4
    else:
        # Best-effort: assume float32 if unknown.
        samples = len(b) // 4
    if chunk.channels and chunk.channels > 1:
        samples //= int(chunk.channels)
    return int(samples)


class StaticAvatarPlugin(AvatarPlugin):
    """A lightweight avatar plugin that outputs a static image as video frames.

    This is meant for local/dev environments where heavy GPU avatar backends
    (FlashHead/LiveAct) are not installed yet. It keeps the stack runnable.
    """

    name = "avatar.static"

    def __init__(self) -> None:
        self._fps = 25
        self._width = 512
        self._height = 512
        self._frames_per_chunk_fallback = 28
        self._bg_color = (128, 128, 128)
        self._frame: np.ndarray | None = None  # (H, W, 3) uint8
        self._chunk_index = 0

    async def initialize(self, config: PluginConfig) -> None:
        params = config.params or {}
        self._fps = max(_safe_int(params.get("fps", 25), 25), 1)
        self._width = max(_safe_int(params.get("width", 512), 512), 1)
        self._height = max(_safe_int(params.get("height", 512), 512), 1)
        self._frames_per_chunk_fallback = max(
            _safe_int(params.get("frames_per_chunk", 28), 28), 1
        )
        self._bg_color = _parse_bg_color(params.get("bg_color"))
        self._frame = np.full(
            (self._height, self._width, 3), self._bg_color, dtype=np.uint8
        )

        default_avatar = params.get("default_avatar")
        if default_avatar:
            try:
                await self.set_avatar(str(default_avatar), use_face_crop=False)
            except Exception:
                logger.exception("StaticAvatarPlugin failed to load default_avatar=%r", default_avatar)

        logger.info(
            "StaticAvatarPlugin initialized: %dx%d fps=%d",
            self._width,
            self._height,
            self._fps,
        )

    async def set_avatar(self, image_path: str, use_face_crop: bool = False) -> None:
        frame = None
        try:
            from PIL import Image

            img = Image.open(image_path).convert("RGB")
            # No face detection here; if requested, do a center square crop.
            if use_face_crop:
                w, h = img.size
                side = min(w, h)
                left = max((w - side) // 2, 0)
                top = max((h - side) // 2, 0)
                img = img.crop((left, top, left + side, top + side))
            img = img.resize((self._width, self._height), Image.BILINEAR)
            frame = np.asarray(img, dtype=np.uint8)
        except Exception:
            logger.exception("StaticAvatarPlugin failed to load avatar image: %s", image_path)

        if frame is None or frame.ndim != 3 or frame.shape[2] != 3:
            frame = np.full((self._height, self._width, 3), self._bg_color, dtype=np.uint8)

        self._frame = frame
        # Reset chunk counter so streams start at 1 after avatar change.
        self._chunk_index = 0

    def _frames_for_chunk(self, chunk: AudioChunk) -> int:
        # Prefer explicit duration if provided.
        if chunk.duration_ms and chunk.duration_ms > 0:
            n = int(round((chunk.duration_ms / 1000.0) * self._fps))
            return max(n, 1)

        sr = int(chunk.sample_rate or 16000)
        if sr <= 0:
            return self._frames_per_chunk_fallback

        samples = _num_audio_samples(chunk)
        if samples <= 0:
            return self._frames_per_chunk_fallback

        duration_s = float(samples) / float(sr)
        n = int(round(duration_s * self._fps))
        return max(n, 1)

    async def generate_stream(
        self, audio_stream: AsyncIterator[AudioChunk]
    ) -> AsyncIterator[VideoChunk]:
        async for audio_chunk in audio_stream:
            base = self._frame
            if base is None:
                base = np.full((self._height, self._width, 3), self._bg_color, dtype=np.uint8)

            n_frames = self._frames_for_chunk(audio_chunk)
            frames = np.repeat(base[None, ...], n_frames, axis=0)

            self._chunk_index += 1
            yield VideoChunk(
                frames=frames,
                fps=self._fps,
                chunk_index=self._chunk_index,
                is_final=audio_chunk.is_final,
            )

            if audio_chunk.is_final:
                break

    async def reset(self) -> None:
        self._chunk_index = 0

    def get_fps(self) -> int:
        return int(self._fps)

    async def shutdown(self) -> None:
        self._frame = None

