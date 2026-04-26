import logging

from inference.core.registry import PluginRegistry
from inference.core.types import AudioChunk, VoiceLLMSessionConfig
from inference.generated import common_pb2, voice_llm_pb2, voice_llm_pb2_grpc
from inference.plugins.voice_llm.base import VoiceLLMPlugin

logger = logging.getLogger(__name__)


def _audio_chunk_to_pb(ac: AudioChunk) -> common_pb2.AudioChunk:
    return common_pb2.AudioChunk(
        data=ac.data,
        sample_rate=ac.sample_rate,
        channels=ac.channels,
        format=ac.format or "",
        is_final=ac.is_final,
        timestamp_ms=ac.timestamp_ms,
    )


class VoiceLLMGRPCService(voice_llm_pb2_grpc.VoiceLLMServiceServicer):

    def __init__(self, registry: PluginRegistry) -> None:
        self.registry = registry

    def _get_plugin(self) -> VoiceLLMPlugin:
        plugin = self.registry.get_by_category("voice_llm")
        if plugin is None:
            raise RuntimeError("No VoiceLLM plugin initialized")
        return plugin

    async def Converse(self, request_iterator, context):
        """Stream user audio to VoiceLLM (e.g. Doubao); yield audio + transcripts only.

        Avatar video is produced by AvatarService.GenerateStream; the Go orchestrator
        composes VoiceLLM output with that stream.
        """
        plugin = self._get_plugin()

        # Phase 1: read the config message (always sent first by Go client)
        session_config: VoiceLLMSessionConfig | None = None
        async for msg in request_iterator:
            which = msg.WhichOneof("input")
            if which == "config":
                cfg = msg.config
                session_config = VoiceLLMSessionConfig(
                    session_id=cfg.session_id,
                    system_prompt=cfg.system_prompt,
                    voice=cfg.voice,
                    bot_name=cfg.bot_name,
                    speaking_style=cfg.speaking_style,
                    welcome_message=cfg.welcome_message,
                )
                logger.debug(
                    "VoiceLLM session config: voice=%r bot_name=%r system_prompt=%r welcome=%r",
                    session_config.voice,
                    session_config.bot_name,
                    session_config.system_prompt[:50] if session_config.system_prompt else "",
                    session_config.welcome_message[:50]
                    if session_config.welcome_message
                    else "",
                )
            break  # config (or first audio) consumed, proceed to audio phase

        # Phase 2: stream remaining messages as audio
        async def audio_stream():
            async for msg in request_iterator:
                which = msg.WhichOneof("input")
                if which == "audio":
                    yield msg.audio.data

        async for event in plugin.converse_stream(audio_stream(), session_config=session_config):
            output = voice_llm_pb2.VoiceLLMOutput(is_final=event.is_final)
            if event.audio:
                output.audio.CopyFrom(_audio_chunk_to_pb(event.audio))
            if event.transcript:
                output.transcript = event.transcript
            if event.user_transcript:
                output.user_transcript = event.user_transcript
            yield output

    async def Interrupt(self, request, context):
        plugin = self._get_plugin()
        await plugin.interrupt()
        return voice_llm_pb2.InterruptResponse(success=True)
