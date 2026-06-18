from typing import AsyncIterator

from inference.core.types import LLMResponseChunk, PluginConfig
from inference.plugins.llm.base import LLMPlugin
from inference.plugins.llm.openai_plugin import SENTENCE_ENDERS


class LiteLLMPlugin(LLMPlugin):
    """LLM plugin using LiteLLM to access 100+ providers via a unified API."""

    name = "llm.litellm"
    supports_images = True

    def __init__(self) -> None:
        self.model = "gpt-4o"
        self.temperature = 0.7
        self.system_prompt = ""
        self.api_key: str | None = None
        self.api_base: str | None = None

    async def initialize(self, config: PluginConfig) -> None:
        self.model = config.params.get("model", "gpt-4o")
        self.temperature = float(config.params.get("temperature", 0.7))
        self.system_prompt = config.params.get("system_prompt", "")
        self.api_key = config.params.get("api_key")
        self.api_base = config.params.get("api_base") or config.params.get("base_url")

    async def generate_stream(
        self, messages: list[dict]
    ) -> AsyncIterator[LLMResponseChunk]:
        import litellm

        full_messages = list(messages)
        has_system = any(m.get("role") == "system" for m in full_messages)
        if self.system_prompt and not has_system:
            full_messages = [{"role": "system", "content": self.system_prompt}] + full_messages

        kwargs: dict = {
            "model": self.model,
            "messages": full_messages,
            "temperature": self.temperature,
            "stream": True,
            "drop_params": True,
        }
        if self.api_key:
            kwargs["api_key"] = self.api_key
        if self.api_base:
            kwargs["api_base"] = self.api_base

        accumulated = ""
        response = await litellm.acompletion(**kwargs)
        async for chunk in response:
            choices = chunk.get("choices") or []
            if not choices:
                continue
            delta = choices[0].get("delta") or {}
            token = delta.get("content") or ""
            if not token:
                continue
            accumulated += token
            is_sentence_end = any(token.endswith(p) for p in SENTENCE_ENDERS)
            yield LLMResponseChunk(
                token=token,
                accumulated_text=accumulated,
                is_sentence_end=is_sentence_end,
                is_final=False,
            )

        yield LLMResponseChunk(
            token="",
            accumulated_text=accumulated,
            is_sentence_end=True,
            is_final=True,
        )

    async def shutdown(self) -> None:
        pass
