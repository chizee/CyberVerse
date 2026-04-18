from inference.core.registry import PluginRegistry
from inference.generated import common_pb2, tts_pb2, tts_pb2_grpc
from inference.plugins.tts.base import TTSPlugin


class TTSGRPCService(tts_pb2_grpc.TTSServiceServicer):

    def __init__(self, registry: PluginRegistry) -> None:
        self.registry = registry

    def _get_plugin(self) -> TTSPlugin:
        plugin = self.registry.get_by_category("tts")
        if plugin is None:
            raise RuntimeError("No TTS plugin initialized")
        return plugin

    async def SynthesizeStream(self, request_iterator, context):
        plugin = self._get_plugin()

        async def text_stream():
            async for chunk in request_iterator:
                yield chunk.text

        async for audio_chunk in plugin.synthesize_stream(text_stream()):
            yield common_pb2.AudioChunk(
                data=audio_chunk.data,
                sample_rate=audio_chunk.sample_rate,
                channels=audio_chunk.channels,
                format=audio_chunk.format,
                is_final=audio_chunk.is_final,
            )

    async def ListVoices(self, request, context):
        return tts_pb2.ListVoicesResponse(voices=[])
