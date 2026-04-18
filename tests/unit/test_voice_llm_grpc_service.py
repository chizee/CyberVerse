"""Tests for VoiceLLM gRPC service (audio-only; avatar is AvatarService)."""

from unittest.mock import MagicMock

import pytest

from inference.core.types import AudioChunk, VoiceLLMOutputEvent
from inference.services.voice_llm_service import VoiceLLMGRPCService


@pytest.mark.asyncio
async def test_converse_yields_audio_only():
    reg = MagicMock()
    voice = MagicMock()

    async def fake_converse(_stream, session_config=None):
        yield VoiceLLMOutputEvent(
            audio=AudioChunk(
                data=b"\x01\x00",
                sample_rate=24000,
                format="pcm_s16le",
                is_final=False,
            ),
        )

    voice.converse_stream = fake_converse
    reg.get_by_category = MagicMock(return_value=voice)

    svc = VoiceLLMGRPCService(reg)

    from inference.generated import common_pb2, voice_llm_pb2

    async def requests():
        yield voice_llm_pb2.VoiceLLMInput(
            audio=common_pb2.AudioChunk(data=b"pcm", sample_rate=16000)
        )

    ctx = MagicMock()
    outs = []
    async for o in svc.Converse(requests(), ctx):
        outs.append(o)
    assert len(outs) == 1
    assert outs[0].audio.data == b"\x01\x00"
    assert outs[0].audio.sample_rate == 24000


@pytest.mark.asyncio
async def test_converse_without_voice_llm_plugin_raises():
    reg = MagicMock()
    reg.get_by_category = MagicMock(return_value=None)
    svc = VoiceLLMGRPCService(reg)

    from inference.generated import common_pb2, voice_llm_pb2

    async def requests():
        yield voice_llm_pb2.VoiceLLMInput(
            audio=common_pb2.AudioChunk(data=b"x", sample_rate=16000)
        )

    with pytest.raises(RuntimeError, match="No VoiceLLM"):
        async for _ in svc.Converse(requests(), MagicMock()):
            pass
