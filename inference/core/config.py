import os
import re
from pathlib import Path

import yaml

_ENV_VAR_PATTERN = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")
_AVATAR_MODEL_CONFIG_GLOBS = ("*.yaml", "*.yml")
_DEFAULT_CONFIG_PATH = Path("config/cyberverse.yaml")
_LEGACY_CONFIG_PATH = Path("cyberverse_config.yaml")


def _load_yaml_file(config_path: Path) -> dict:
    if not config_path.exists():
        raise FileNotFoundError(f"Config file not found: {config_path}")

    with open(config_path, encoding="utf-8") as f:
        raw = f.read()

    def _replace_env(match: re.Match) -> str:
        var_name = match.group(1)
        return os.environ.get(var_name, match.group(0))

    raw = _ENV_VAR_PATTERN.sub(_replace_env, raw)
    data = yaml.safe_load(raw)
    if data is None:
        return {}
    if not isinstance(data, dict):
        raise ValueError(f"Config root must be a mapping: {config_path}")
    return data


def load_dotenv(path: str | Path) -> None:
    """Load KEY=VALUE pairs from a dotenv file into the process environment."""
    env_path = Path(path)
    if not env_path.exists():
        return
    if not env_path.is_file():
        raise FileNotFoundError(f"Dotenv path is not a file: {env_path}")

    with open(env_path, encoding="utf-8") as f:
        for raw_line in f:
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            key, sep, value = line.partition("=")
            if not sep:
                continue
            key = key.strip()
            value = value.strip()
            if not key:
                continue
            if len(value) >= 2 and (
                (value[0] == '"' and value[-1] == '"')
                or (value[0] == "'" and value[-1] == "'")
            ):
                value = value[1:-1]
            os.environ[key] = value


def resolve_config_path(config_path: str | Path) -> Path:
    """Resolve the config path, falling back from the new default to the legacy file."""
    path = Path(config_path)
    if path.exists():
        return path
    if path == _DEFAULT_CONFIG_PATH and _LEGACY_CONFIG_PATH.exists():
        return _LEGACY_CONFIG_PATH
    return path


def _dotenv_paths(config_path: Path) -> list[Path]:
    config_dir = config_path.parent
    paths: list[Path] = []
    if config_dir.name == "config":
        paths.append(config_dir.parent / ".env")
    paths.extend([config_dir / ".env", config_dir / "env"])
    return paths


def _avatar_model_config_files(model_config_dir: Path) -> list[Path]:
    files: list[Path] = []
    for pattern in _AVATAR_MODEL_CONFIG_GLOBS:
        files.extend(model_config_dir.glob(pattern))
    return sorted({path.resolve(): path for path in files}.values(), key=lambda p: p.name)


def _merge_avatar_model_configs(config: dict, config_path: Path) -> dict:
    inference = config.get("inference")
    if not isinstance(inference, dict):
        return config
    avatar = inference.get("avatar")
    if not isinstance(avatar, dict):
        return config

    raw_model_config_dir = avatar.get("model_config_dir")
    if raw_model_config_dir is None or str(raw_model_config_dir).strip() == "":
        return config

    model_config_dir = Path(str(raw_model_config_dir)).expanduser()
    if not model_config_dir.is_absolute():
        model_config_dir = config_path.parent / model_config_dir
    if not model_config_dir.exists():
        raise FileNotFoundError(f"Avatar model config dir not found: {model_config_dir}")
    if not model_config_dir.is_dir():
        raise NotADirectoryError(f"Avatar model config dir is not a directory: {model_config_dir}")

    external_models: set[str] = set()
    for model_file in _avatar_model_config_files(model_config_dir):
        model_data = _load_yaml_file(model_file)
        if len(model_data) != 1:
            raise ValueError(
                "Avatar model config file must contain exactly one top-level model: "
                f"{model_file}"
            )
        model_name, model_config = next(iter(model_data.items()))
        if not isinstance(model_name, str) or not model_name.strip():
            raise ValueError(f"Avatar model name must be a non-empty string: {model_file}")
        if not isinstance(model_config, dict):
            raise ValueError(
                "Avatar model config value must be a mapping: "
                f"{model_file}"
            )
        if model_name in external_models:
            raise ValueError(f"Duplicate avatar model config for {model_name!r}")
        external_models.add(model_name)
        # Inline model configs in the main CyberVerse config are treated as local
        # overrides and remain the write target for that model.
        if model_name not in avatar:
            avatar[model_name] = model_config

    return config


def load_config(config_path: str | Path) -> dict:
    """Load YAML config with env substitution and external avatar model files.

    Only substitutes explicit ${VAR_NAME} patterns, not arbitrary env vars.
    Unmatched patterns are left as-is.
    """
    config_path = resolve_config_path(config_path)
    for dotenv_path in _dotenv_paths(config_path):
        load_dotenv(dotenv_path)
    os.environ["CYBERVERSE_CONFIG_DIR"] = str(config_path.parent.resolve())
    config = _load_yaml_file(config_path)

    return _merge_avatar_model_configs(config, config_path)
