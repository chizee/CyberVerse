from abc import abstractmethod
from typing import AsyncIterator

from inference.core.types import VoiceLLMOutputEvent, VoiceLLMSessionConfig
from inference.plugins.base import CyberVersePlugin


class VoiceLLMPlugin(CyberVersePlugin):
    @abstractmethod
    async def converse_stream(
        self,
        audio_stream: AsyncIterator[bytes],
        session_config: VoiceLLMSessionConfig | None = None,
    ) -> AsyncIterator[VoiceLLMOutputEvent]:
        ...

    async def interrupt(self) -> None:
        pass
