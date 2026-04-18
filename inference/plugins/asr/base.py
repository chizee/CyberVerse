from abc import abstractmethod
from typing import AsyncIterator

from inference.core.types import TranscriptEvent
from inference.plugins.base import CyberVersePlugin


class ASRPlugin(CyberVersePlugin):
    @abstractmethod
    async def transcribe_stream(
        self, audio_stream: AsyncIterator[bytes]
    ) -> AsyncIterator[TranscriptEvent]:
        ...
