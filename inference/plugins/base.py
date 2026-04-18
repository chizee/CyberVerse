from abc import ABC, abstractmethod
from typing import AsyncIterator

from inference.core.types import PluginConfig


class CyberVersePlugin(ABC):
    name: str = ""
    version: str = "0.1.0"

    @abstractmethod
    async def initialize(self, config: PluginConfig) -> None:
        ...

    @abstractmethod
    async def shutdown(self) -> None:
        ...

    def __repr__(self) -> str:
        return f"<{self.__class__.__name__} name={self.name} v={self.version}>"
