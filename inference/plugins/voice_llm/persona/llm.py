from __future__ import annotations

import os
import re
from dataclasses import dataclass, field
from typing import Any

from langchain.chat_models import init_chat_model
from langchain_core.language_models.chat_models import BaseChatModel

AgentLLM = BaseChatModel


_ENV_PLACEHOLDER_RE = re.compile(r"^\$\{[A-Za-z_][A-Za-z0-9_]*\}$")


@dataclass
class AgentLLMConfig:
    provider: str = "qwen"
    model: str = "qwen3.6-plus"
    api_key: str = ""
    base_url: str = ""
    temperature: float = 0.2
    extra_body: dict[str, Any] = field(default_factory=dict)


def _clean_config_string(value: Any) -> str:
    text = str(value or "").strip()
    if _ENV_PLACEHOLDER_RE.match(text):
        return ""
    return text


def _optional_float(value: Any, default: float) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def _dashscope_base_url() -> str:
    try:
        from inference.plugins.qwen_endpoint import dashscope_base_url

        return dashscope_base_url()
    except Exception:
        return "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"


def agent_llm_config_from_env() -> AgentLLMConfig:
    provider = _clean_config_string(os.getenv("AGENT_LLM_PROVIDER")) or "qwen"
    model = _clean_config_string(os.getenv("AGENT_LLM_MODEL")) or (
        "qwen3.6-plus" if provider == "qwen" else "gpt-4o"
    )
    api_key = _clean_config_string(os.getenv("AGENT_LLM_API_KEY"))
    if not api_key:
        api_key = _clean_config_string(
            os.getenv("DASHSCOPE_API_KEY") if provider == "qwen" else os.getenv("OPENAI_API_KEY")
        )
    base_url = _clean_config_string(os.getenv("AGENT_LLM_BASE_URL"))
    if not base_url and provider == "qwen":
        base_url = _dashscope_base_url()
    return AgentLLMConfig(
        provider=provider,
        model=model,
        api_key=api_key,
        base_url=base_url,
        temperature=_optional_float(os.getenv("AGENT_LLM_TEMPERATURE"), 0.2),
        extra_body={"enable_thinking": False} if provider == "qwen" else {},
    )


def agent_llm_config_from_cyberverse_config(config: dict[str, Any] | None) -> AgentLLMConfig:
    if not isinstance(config, dict):
        return agent_llm_config_from_env()

    inference = config.get("inference", {})
    if not isinstance(inference, dict):
        return agent_llm_config_from_env()

    persona_conf = inference.get("persona_agent", {})
    persona_llm = persona_conf.get("llm", {}) if isinstance(persona_conf, dict) else {}
    persona_section = inference.get("persona", {})
    persona_plugin_conf = persona_section.get("persona", {}) if isinstance(persona_section, dict) else {}
    if not persona_llm and isinstance(persona_plugin_conf, dict):
        persona_llm = persona_plugin_conf.get("llm", {})
    persona_llm = persona_llm if isinstance(persona_llm, dict) else {}

    llm_section = inference.get("llm", {})
    llm_section = llm_section if isinstance(llm_section, dict) else {}
    provider = _clean_config_string(persona_llm.get("provider")) or _clean_config_string(llm_section.get("default")) or "qwen"
    provider_conf = llm_section.get(provider, {})
    provider_conf = provider_conf if isinstance(provider_conf, dict) else {}
    merged = {**provider_conf, **persona_llm}

    model = _clean_config_string(merged.get("model")) or ("qwen3.6-plus" if provider == "qwen" else "gpt-4o")
    api_key = _clean_config_string(merged.get("api_key"))
    if not api_key:
        api_key = _clean_config_string(
            os.getenv("DASHSCOPE_API_KEY") if provider == "qwen" else os.getenv("OPENAI_API_KEY")
        )
    base_url = _clean_config_string(merged.get("base_url"))
    if not base_url and provider == "qwen":
        base_url = _dashscope_base_url()
    extra_body = merged.get("extra_body")
    if not isinstance(extra_body, dict):
        extra_body = {"enable_thinking": False} if provider == "qwen" else {}

    return AgentLLMConfig(
        provider=provider,
        model=model,
        api_key=api_key,
        base_url=base_url,
        temperature=_optional_float(merged.get("temperature"), 0.2),
        extra_body=extra_body,
    )


def _langchain_model_provider(provider: str) -> str:
    normalized = _clean_config_string(provider).lower()
    if normalized in {"qwen", "dashscope", "openai"}:
        return "openai"
    return normalized or "openai"


def init_chat_model_kwargs(config: AgentLLMConfig) -> dict[str, Any]:
    kwargs: dict[str, Any] = {
        "model": config.model,
        "model_provider": _langchain_model_provider(config.provider),
        "temperature": config.temperature,
    }
    if config.api_key:
        kwargs["api_key"] = config.api_key
    if config.base_url:
        kwargs["base_url"] = config.base_url
    if config.extra_body:
        kwargs["extra_body"] = config.extra_body
    return kwargs


def build_agent_llm(config: AgentLLMConfig | None = None) -> BaseChatModel:
    return init_chat_model(**init_chat_model_kwargs(config or agent_llm_config_from_env()))


def build_agent_llm_from_runtime_config(config: dict[str, Any] | None = None) -> BaseChatModel:
    return build_agent_llm(agent_llm_config_from_cyberverse_config(config))
