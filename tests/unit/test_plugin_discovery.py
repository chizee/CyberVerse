"""Tests for config-driven plugin discovery and dynamic import."""
import pytest

from inference.core.registry import PluginRegistry, import_plugin_class
from inference.core.types import PluginConfig
from inference.plugins.base import CyberVersePlugin


# --- import_plugin_class tests ---

def test_import_valid_plugin_class():
    cls = import_plugin_class(
        "inference.plugins.avatar.flash_head_plugin.FlashHeadAvatarPlugin"
    )
    assert issubclass(cls, CyberVersePlugin)
    assert cls.name == "avatar.flash_head"


def test_import_llm_plugin_class():
    cls = import_plugin_class(
        "inference.plugins.llm.openai_plugin.OpenAILLMPlugin"
    )
    assert issubclass(cls, CyberVersePlugin)
    assert cls.name == "llm.openai"


def test_import_invalid_module_raises():
    with pytest.raises(ImportError):
        import_plugin_class("nonexistent.module.SomeClass")


def test_import_invalid_class_raises():
    with pytest.raises(AttributeError):
        import_plugin_class("inference.plugins.base.NonExistentClass")


def test_import_non_plugin_raises():
    with pytest.raises(TypeError, match="not a CyberVersePlugin subclass"):
        import_plugin_class("inference.core.types.AudioChunk")


def test_import_no_module_path_raises():
    with pytest.raises(ImportError, match="Invalid plugin class path"):
        import_plugin_class("JustAClassName")


# --- Config-driven registration tests ---

def test_config_driven_registration():
    """Simulate server._register_plugins logic with config dict."""
    config = {
        "inference": {
            "avatar": {
                "default": "flash_head",
                "flash_head": {
                    "plugin_class": "inference.plugins.avatar.flash_head_plugin.FlashHeadAvatarPlugin",
                    "checkpoint_dir": "/tmp/test",
                },
            },
            "llm": {
                "default": "openai",
                "openai": {
                    "plugin_class": "inference.plugins.llm.openai_plugin.OpenAILLMPlugin",
                    "api_key": "test",
                },
            },
        }
    }

    registry = PluginRegistry()
    for category in ("avatar", "llm", "tts", "asr", "voice_llm"):
        section = config.get("inference", {}).get(category, {})
        for name, conf in section.items():
            if name == "default" or not isinstance(conf, dict):
                continue
            class_path = conf.get("plugin_class")
            if not class_path:
                continue
            cls = import_plugin_class(class_path)
            registry.register(f"{category}.{name}", cls)

    assert "avatar.flash_head" in registry.registered_names
    assert "llm.openai" in registry.registered_names
    assert len(registry.registered_names) == 2


def test_missing_plugin_class_skipped():
    """Plugins without plugin_class should be silently skipped."""
    config_section = {
        "default": "flash_head",
        "flash_head": {
            "checkpoint_dir": "/tmp/test",
            # no plugin_class
        },
    }

    registry = PluginRegistry()
    for name, conf in config_section.items():
        if name == "default" or not isinstance(conf, dict):
            continue
        class_path = conf.get("plugin_class")
        if not class_path:
            continue  # should skip here
        cls = import_plugin_class(class_path)
        registry.register(f"avatar.{name}", cls)

    assert len(registry.registered_names) == 0


def test_invalid_plugin_class_warning(caplog):
    """Invalid plugin_class should be caught, not crash."""
    import logging

    config_section = {
        "default": "broken",
        "broken": {
            "plugin_class": "nonexistent.module.BrokenPlugin",
        },
    }

    registry = PluginRegistry()
    for name, conf in config_section.items():
        if name == "default" or not isinstance(conf, dict):
            continue
        class_path = conf.get("plugin_class")
        if not class_path:
            continue
        try:
            cls = import_plugin_class(class_path)
            registry.register(f"avatar.{name}", cls)
        except (ImportError, AttributeError, TypeError):
            pass  # gracefully skip

    assert len(registry.registered_names) == 0


def test_plugin_class_stripped_from_params():
    """plugin_class should not be passed to the plugin's initialize()."""
    conf = {
        "plugin_class": "inference.plugins.avatar.flash_head_plugin.FlashHeadAvatarPlugin",
        "checkpoint_dir": "/tmp/test",
        "model_type": "lite",
    }
    params = {k: v for k, v in conf.items() if k != "plugin_class"}
    plugin_config = PluginConfig(plugin_name="avatar.flash_head", params=params)

    assert "plugin_class" not in plugin_config.params
    assert plugin_config.params["checkpoint_dir"] == "/tmp/test"
    assert plugin_config.params["model_type"] == "lite"
