from __future__ import annotations

from dataclasses import dataclass

from inference.core.types import PluginConfig

_TRUE_VALUES = {"1", "true", "yes", "on"}
_FALSE_VALUES = {"0", "false", "no", "off", ""}


def _parse_bool(value: object, *, default: bool) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)

    normalized = str(value).strip().lower()
    if normalized in _TRUE_VALUES:
        return True
    if normalized in _FALSE_VALUES:
        return False
    raise ValueError(f"Invalid boolean value: {value!r}")


@dataclass(frozen=True)
class AvatarWarmupPolicy:
    enabled: bool
    global_enabled: bool
    distributed_enabled: bool
    timeout_s: int | None


def resolve_avatar_warmup_policy(
    config: PluginConfig, *, world_size: int
) -> AvatarWarmupPolicy:
    warmup_cfg = config.shared.get("warmup", {})
    if not isinstance(warmup_cfg, dict):
        warmup_cfg = {}

    distributed_cfg = warmup_cfg.get("distributed", {})
    if not isinstance(distributed_cfg, dict):
        distributed_cfg = {}

    global_enabled = _parse_bool(warmup_cfg.get("enabled"), default=False)
    distributed_enabled = _parse_bool(
        distributed_cfg.get("enabled"),
        default=True,
    )
    timeout_raw = distributed_cfg.get("timeout_s")
    timeout_s = int(timeout_raw) if timeout_raw is not None else None

    enabled = global_enabled and (world_size <= 1 or distributed_enabled)
    return AvatarWarmupPolicy(
        enabled=enabled,
        global_enabled=global_enabled,
        distributed_enabled=distributed_enabled,
        timeout_s=timeout_s,
    )
