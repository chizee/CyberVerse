from typing import AsyncIterator

from inference.core.types import LLMResponseChunk, PluginConfig
from inference.plugins.llm.base import LLMPlugin

SENTENCE_ENDERS = {"。", "！", "？", ".", "!", "?", "；", ";", "\n"}
DEFAULT_BASE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
LEGACY_MODEL_PREFIXES = ("gpt-", "o1", "o3", "o4")


class QwenLLMPlugin(LLMPlugin):
    name = "llm.qwen"

    def __init__(self) -> None:
        self.client = None
        self.model = "qwen-plus"
        self.temperature = 0.7
        self.system_prompt = ""
        self.base_url = DEFAULT_BASE_URL

    async def initialize(self, config: PluginConfig) -> None:
        from openai import AsyncOpenAI

        api_key = str(config.params.get("api_key", "") or "").strip()
        if not api_key:
            raise ValueError("api_key is required for llm.qwen")

        self.base_url = str(config.params.get("base_url", DEFAULT_BASE_URL) or DEFAULT_BASE_URL).rstrip("/")
        configured_model = str(config.params.get("model", "qwen-plus") or "qwen-plus").strip()
        if configured_model.lower().startswith(LEGACY_MODEL_PREFIXES):
            configured_model = "qwen-plus"

        self.client = AsyncOpenAI(
            api_key=api_key,
            base_url=self.base_url,
        )
        self.model = configured_model
        self.temperature = float(config.params.get("temperature", 0.7))
        self.system_prompt = config.params.get("system_prompt", "")

    async def generate_stream(
        self, messages: list[dict]
    ) -> AsyncIterator[LLMResponseChunk]:
        full_messages = messages
        if self.system_prompt:
            full_messages = [{"role": "system", "content": self.system_prompt}] + messages

        accumulated = ""
        stream = await self.client.chat.completions.create(
            model=self.model,
            messages=full_messages,
            temperature=self.temperature,
            stream=True,
        )
        async for chunk in stream:
            if chunk.choices and chunk.choices[0].delta.content:
                token = chunk.choices[0].delta.content
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
        self.client = None
