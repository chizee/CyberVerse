import grpc

from inference.core.types import TTSRequestConfig
from inference.core.registry import PluginRegistry
from inference.generated import common_pb2, tts_pb2, tts_pb2_grpc
from inference.plugins.tts.base import TTSPlugin

_MODEL_PROVIDER_MARKER = "::model="
_DEFAULT_TTS_PROVIDER = "qwen"


class TTSGRPCService(tts_pb2_grpc.TTSServiceServicer):

    def __init__(self, registry: PluginRegistry) -> None:
        self.registry = registry

    def _get_plugin(self, provider: str = "") -> TTSPlugin:
        requested = provider.strip()
        provider = requested or _DEFAULT_TTS_PROVIDER
        if provider:
            try:
                return self.registry.get(f"tts.{provider}")
            except KeyError:
                if requested:
                    raise
        plugin = self.registry.get_by_category("tts")
        if plugin is None:
            raise RuntimeError("No TTS plugin initialized")
        return plugin

    async def SynthesizeStream(self, request_iterator, context):
        iterator = request_iterator.__aiter__()
        try:
            first = await anext(iterator)
        except StopAsyncIteration:
            return

        request_config = self._request_config(first.config if first.HasField("config") else None)
        provider, model = self._split_provider_model(request_config.provider)
        request_config.provider = provider
        if model:
            request_config.model = model
        try:
            plugin = self._get_plugin(provider)
        except (KeyError, RuntimeError) as exc:
            await context.abort(grpc.StatusCode.INVALID_ARGUMENT, str(exc))

        async def text_stream():
            if first.text:
                yield first.text
            async for chunk in iterator:
                if chunk.text:
                    yield chunk.text

        async for audio_chunk in plugin.synthesize_stream(text_stream(), request_config):
            yield common_pb2.AudioChunk(
                data=audio_chunk.data,
                sample_rate=audio_chunk.sample_rate,
                channels=audio_chunk.channels,
                format=audio_chunk.format,
                is_final=audio_chunk.is_final,
            )

    async def ListVoices(self, request, context):
        return tts_pb2.ListVoicesResponse(voices=[])

    @staticmethod
    def _request_config(config) -> TTSRequestConfig:
        if config is None:
            return TTSRequestConfig()
        return TTSRequestConfig(
            provider=config.provider,
            voice=config.voice,
            speaking_style=config.speaking_style,
            language=config.language,
            session_id=config.session_id,
        )

    @staticmethod
    def _split_provider_model(provider: str) -> tuple[str, str]:
        provider = provider.strip()
        if _MODEL_PROVIDER_MARKER not in provider:
            return provider, ""
        base, model = provider.split(_MODEL_PROVIDER_MARKER, 1)
        return base.strip(), model.strip()
