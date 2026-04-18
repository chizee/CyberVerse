from inference.core.types import PluginConfig
from inference.plugins.avatar.warmup import resolve_avatar_warmup_policy


def _config(warmup: dict) -> PluginConfig:
    return PluginConfig(
        plugin_name="avatar.test",
        shared={"warmup": warmup},
    )


def test_resolve_avatar_warmup_policy_global_disabled():
    policy = resolve_avatar_warmup_policy(
        _config({"enabled": False, "distributed": {"enabled": True, "timeout_s": 30}}),
        world_size=1,
    )

    assert policy.enabled is False
    assert policy.global_enabled is False
    assert policy.distributed_enabled is True
    assert policy.timeout_s == 30


def test_resolve_avatar_warmup_policy_single_gpu_enabled():
    policy = resolve_avatar_warmup_policy(
        _config({"enabled": True, "distributed": {"enabled": False, "timeout_s": 30}}),
        world_size=1,
    )

    assert policy.enabled is True
    assert policy.global_enabled is True
    assert policy.distributed_enabled is False
    assert policy.timeout_s == 30


def test_resolve_avatar_warmup_policy_multi_gpu_enabled():
    policy = resolve_avatar_warmup_policy(
        _config({"enabled": True, "distributed": {"enabled": True, "timeout_s": 30}}),
        world_size=2,
    )

    assert policy.enabled is True
    assert policy.global_enabled is True
    assert policy.distributed_enabled is True
    assert policy.timeout_s == 30


def test_resolve_avatar_warmup_policy_multi_gpu_skipped_when_distributed_disabled():
    policy = resolve_avatar_warmup_policy(
        _config({"enabled": True, "distributed": {"enabled": False, "timeout_s": 30}}),
        world_size=2,
    )

    assert policy.enabled is False
    assert policy.global_enabled is True
    assert policy.distributed_enabled is False
    assert policy.timeout_s == 30
