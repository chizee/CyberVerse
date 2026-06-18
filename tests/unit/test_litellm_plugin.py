"""Tests for LiteLLM LLM plugin with mocked litellm client."""
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from inference.core.types import PluginConfig
from inference.plugins.llm.litellm_plugin import LiteLLMPlugin
from inference.plugins.llm.openai_plugin import SENTENCE_ENDERS


class TestLiteLLMPlugin:
    def test_name(self):
        assert LiteLLMPlugin.name == "llm.litellm"

    def test_supports_images(self):
        assert LiteLLMPlugin.supports_images is True

    def test_sentence_enders(self):
        assert "." in SENTENCE_ENDERS
        assert "。" in SENTENCE_ENDERS
        assert "!" in SENTENCE_ENDERS

    @pytest.mark.asyncio
    async def test_initialize(self):
        plugin = LiteLLMPlugin()
        config = PluginConfig(
            plugin_name="llm.litellm",
            params={
                "model": "anthropic/claude-sonnet-4-6",
                "temperature": 0.3,
                "api_key": "sk-test",
                "system_prompt": "Be concise.",
            },
        )
        await plugin.initialize(config)
        assert plugin.model == "anthropic/claude-sonnet-4-6"
        assert plugin.temperature == 0.3
        assert plugin.api_key == "sk-test"
        assert plugin.system_prompt == "Be concise."

    @pytest.mark.asyncio
    async def test_initialize_defaults(self):
        plugin = LiteLLMPlugin()
        config = PluginConfig(plugin_name="llm.litellm", params={})
        await plugin.initialize(config)
        assert plugin.model == "gpt-4o"
        assert plugin.temperature == 0.7
        assert plugin.api_key is None
        assert plugin.system_prompt == ""

    @pytest.mark.asyncio
    async def test_generate_stream_with_mock(self):
        plugin = LiteLLMPlugin()
        plugin.model = "anthropic/claude-sonnet-4-6"
        plugin.temperature = 0.7
        plugin.system_prompt = "You are helpful."

        mock_chunk1 = {"choices": [{"delta": {"content": "Hello"}}]}
        mock_chunk2 = {"choices": [{"delta": {"content": " world."}}]}

        async def mock_stream():
            yield mock_chunk1
            yield mock_chunk2

        with patch("litellm.acompletion", new_callable=AsyncMock) as mock_acomp:
            mock_acomp.return_value = mock_stream()

            messages = [{"role": "user", "content": "Hi"}]
            results = []
            async for chunk in plugin.generate_stream(messages):
                results.append(chunk)

        assert len(results) == 3  # 2 tokens + 1 final
        assert results[0].token == "Hello"
        assert results[0].is_sentence_end is False
        assert results[1].token == " world."
        assert results[1].is_sentence_end is True
        assert results[2].is_final is True
        assert results[2].accumulated_text == "Hello world."

    @pytest.mark.asyncio
    async def test_system_prompt_prepended(self):
        plugin = LiteLLMPlugin()
        plugin.model = "gpt-4o"
        plugin.system_prompt = "Be concise."

        async def empty_stream():
            return
            yield  # make it an async generator

        with patch("litellm.acompletion", new_callable=AsyncMock) as mock_acomp:
            mock_acomp.return_value = empty_stream()

            messages = [{"role": "user", "content": "Hi"}]
            async for _ in plugin.generate_stream(messages):
                pass

            call_kwargs = mock_acomp.call_args.kwargs
            sent_messages = call_kwargs["messages"]
            assert sent_messages[0]["role"] == "system"
            assert sent_messages[0]["content"] == "Be concise."

    @pytest.mark.asyncio
    async def test_drop_params_true(self):
        plugin = LiteLLMPlugin()
        plugin.model = "gpt-4o"

        async def empty_stream():
            return
            yield

        with patch("litellm.acompletion", new_callable=AsyncMock) as mock_acomp:
            mock_acomp.return_value = empty_stream()

            messages = [{"role": "user", "content": "Hi"}]
            async for _ in plugin.generate_stream(messages):
                pass

            call_kwargs = mock_acomp.call_args.kwargs
            assert call_kwargs["drop_params"] is True
            assert call_kwargs["stream"] is True

    @pytest.mark.asyncio
    async def test_api_key_forwarded(self):
        plugin = LiteLLMPlugin()
        plugin.model = "gpt-4o"
        plugin.api_key = "sk-test-key"
        plugin.api_base = "http://localhost:4000"

        async def empty_stream():
            return
            yield

        with patch("litellm.acompletion", new_callable=AsyncMock) as mock_acomp:
            mock_acomp.return_value = empty_stream()

            messages = [{"role": "user", "content": "Hi"}]
            async for _ in plugin.generate_stream(messages):
                pass

            call_kwargs = mock_acomp.call_args.kwargs
            assert call_kwargs["api_key"] == "sk-test-key"
            assert call_kwargs["api_base"] == "http://localhost:4000"

    @pytest.mark.asyncio
    async def test_api_key_omitted_when_none(self):
        plugin = LiteLLMPlugin()
        plugin.model = "gpt-4o"
        plugin.api_key = None
        plugin.api_base = None

        async def empty_stream():
            return
            yield

        with patch("litellm.acompletion", new_callable=AsyncMock) as mock_acomp:
            mock_acomp.return_value = empty_stream()

            messages = [{"role": "user", "content": "Hi"}]
            async for _ in plugin.generate_stream(messages):
                pass

            call_kwargs = mock_acomp.call_args.kwargs
            assert "api_key" not in call_kwargs
            assert "api_base" not in call_kwargs

    @pytest.mark.asyncio
    async def test_shutdown(self):
        plugin = LiteLLMPlugin()
        await plugin.shutdown()  # should not raise
