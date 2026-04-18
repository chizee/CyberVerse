from inference.core.registry import PluginRegistry
from inference.generated import asr_pb2, asr_pb2_grpc
from inference.plugins.asr.base import ASRPlugin


class ASRGRPCService(asr_pb2_grpc.ASRServiceServicer):

    def __init__(self, registry: PluginRegistry) -> None:
        self.registry = registry

    def _get_plugin(self) -> ASRPlugin:
        plugin = self.registry.get_by_category("asr")
        if plugin is None:
            raise RuntimeError("No ASR plugin initialized")
        return plugin

    async def TranscribeStream(self, request_iterator, context):
        plugin = self._get_plugin()

        async def audio_stream():
            async for chunk in request_iterator:
                yield chunk.data

        async for event in plugin.transcribe_stream(audio_stream()):
            yield asr_pb2.TranscriptEvent(
                text=event.text,
                is_final=event.is_final,
                language=event.language,
                confidence=event.confidence,
            )
