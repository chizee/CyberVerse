from unittest.mock import patch

import pytest

pytest.importorskip("torch")

from inference.core.types import PluginConfig
from inference.plugins.avatar.live_act_plugin import LiveActAvatarPlugin


def _config(*, warmup_enabled: bool) -> PluginConfig:
    return PluginConfig(
        plugin_name="avatar.live_act",
        params={
            "world_size": 1,
            "size": "320*480",
            "fps": 20,
            "seed": 42,
            "audio_cfg": 1.0,
            "t5_cpu": True,
            "fp8_kv_cache": False,
            "offload_cache": False,
            "block_offload": False,
            "mean_memory": False,
            "default_prompt": "一个人在说话",
            "default_avatar_image": "models/SoulX-LiveAct/examples/image/1.png",
            "ckpt_dir": "/tmp/liveact",
            "wav2vec_dir": "/tmp/wav2vec",
        },
        shared={
            "warmup": {
                "enabled": warmup_enabled,
                "distributed": {"enabled": True, "timeout_s": 30},
            }
        },
    )


def test_init_sync_runs_warmup_after_avatar_setup():
    plugin = LiveActAvatarPlugin()
    order: list[str] = []

    with patch.object(plugin, "_load_models"):
        with patch.object(plugin, "_init_kv_cache"):
            with patch.object(
                plugin,
                "_resolve_default_avatar_path",
                return_value=("/tmp/avatar.png", False),
            ):
                with patch.object(
                    plugin,
                    "_set_avatar_sync_local",
                    side_effect=lambda image_path: (
                        order.append("avatar"),
                        setattr(plugin, "_avatar_initialized", True),
                    ),
                ):
                    with patch.object(
                        plugin,
                        "_warmup",
                        side_effect=lambda: order.append("warmup"),
                    ) as warmup:
                        plugin._init_sync(_config(warmup_enabled=True))

    warmup.assert_called_once_with()
    assert order == ["avatar", "warmup"]


def test_init_sync_skips_warmup_when_disabled():
    plugin = LiveActAvatarPlugin()
    order: list[str] = []

    with patch.object(plugin, "_load_models"):
        with patch.object(plugin, "_init_kv_cache"):
            with patch.object(
                plugin,
                "_resolve_default_avatar_path",
                return_value=("/tmp/avatar.png", False),
            ):
                with patch.object(
                    plugin,
                    "_set_avatar_sync_local",
                    side_effect=lambda image_path: (
                        order.append("avatar"),
                        setattr(plugin, "_avatar_initialized", True),
                    ),
                ):
                    with patch.object(plugin, "_warmup") as warmup:
                        plugin._init_sync(_config(warmup_enabled=False))

    warmup.assert_not_called()
    assert order == ["avatar"]
