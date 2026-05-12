from agent_runtime.llm import (
    AgentLLMConfig,
    agent_llm_config_from_cyberverse_config,
    build_agent_llm,
    init_chat_model_kwargs,
)


def test_agent_llm_uses_inference_llm_default_config():
    config = {
        "inference": {
            "llm": {
                "default": "qwen",
                "qwen": {
                    "api_key": "dashscope-key",
                    "model": "qwen3.6-plus",
                    "temperature": 0.7,
                    "extra_body": {"enable_thinking": False},
                },
            }
        }
    }

    llm_config = agent_llm_config_from_cyberverse_config(config)

    assert llm_config.provider == "qwen"
    assert llm_config.api_key == "dashscope-key"
    assert llm_config.model == "qwen3.6-plus"
    assert llm_config.temperature == 0.7
    assert llm_config.extra_body == {"enable_thinking": False}


def test_agent_llm_persona_override_keeps_provider_config_defaults():
    config = {
        "inference": {
            "persona": {
                "persona": {
                    "llm": {
                        "provider": "qwen",
                        "model": "qwen-max",
                        "temperature": 0.1,
                    }
                }
            },
            "llm": {
                "default": "qwen",
                "qwen": {
                    "api_key": "dashscope-key",
                    "model": "qwen3.6-plus",
                    "temperature": 0.7,
                    "extra_body": {"enable_thinking": False},
                },
            },
        }
    }

    llm_config = agent_llm_config_from_cyberverse_config(config)

    assert llm_config.provider == "qwen"
    assert llm_config.api_key == "dashscope-key"
    assert llm_config.model == "qwen-max"
    assert llm_config.temperature == 0.1
    assert llm_config.extra_body == {"enable_thinking": False}


def test_init_chat_model_kwargs_maps_qwen_to_openai_compatible_provider():
    kwargs = init_chat_model_kwargs(
        AgentLLMConfig(
            provider="qwen",
            model="qwen3.6-plus",
            api_key="fake-key",
            base_url="https://example.com/v1",
            temperature=0.1,
            extra_body={"enable_thinking": False},
        )
    )

    assert kwargs == {
        "model": "qwen3.6-plus",
        "model_provider": "openai",
        "api_key": "fake-key",
        "base_url": "https://example.com/v1",
        "temperature": 0.1,
        "extra_body": {"enable_thinking": False},
    }


def test_build_agent_llm_uses_langchain_init_chat_model(monkeypatch):
    calls = []
    fake_model = object()

    def fake_init_chat_model(**kwargs):
        calls.append(kwargs)
        return fake_model

    monkeypatch.setattr("inference.plugins.voice_llm.persona.llm.init_chat_model", fake_init_chat_model)

    model = build_agent_llm(
        AgentLLMConfig(
            provider="qwen",
            model="qwen3.6-plus",
            api_key="fake-key",
            base_url="https://example.com/v1",
            temperature=0.2,
            extra_body={"enable_thinking": False},
        )
    )

    assert model is fake_model
    assert calls == [
        {
            "model": "qwen3.6-plus",
            "model_provider": "openai",
            "api_key": "fake-key",
            "base_url": "https://example.com/v1",
            "temperature": 0.2,
            "extra_body": {"enable_thinking": False},
        }
    ]
