"""Tests for config loader with regex-based env var substitution."""
import os
from pathlib import Path
import tempfile

import pytest

from inference.core.config import load_config


def test_load_config_basic():
    config = load_config("infra/cyberverse_config.example.yaml")
    assert config["server"]["http_port"] == 8080
    assert config["inference"]["avatar"]["enabled"] is True
    assert config["inference"]["avatar"]["default"] in {"flash_head", "live_act"}
    assert set(config["inference"]["avatar"]["runtime"].keys()) == {
        "cuda_visible_devices",
        "world_size",
    }
    assert set(config["warmup"].keys()) == {"enabled", "distributed"}
    flash_head = config["inference"]["avatar"]["flash_head"]
    assert flash_head["compile_model"] is True
    assert flash_head["compile_vae"] is True
    assert flash_head["dist_worker_main_thread"] is True
    infer_params = config["inference"]["avatar"]["flash_head"]["infer_params"]
    assert set(infer_params.keys()) == {
        "frame_num",
        "motion_frames_latent_num",
        "tgt_fps",
        "sample_rate",
        "sample_shift",
        "color_correction_strength",
        "cached_audio_duration",
        "num_heads",
        "height",
        "width",
    }
    live_act_infer_params = config["inference"]["avatar"]["live_act"]["infer_params"]
    live_act = config["inference"]["avatar"]["live_act"]
    assert live_act["dist_worker_main_thread"] is True
    assert live_act["fp8_gemm"] is True
    assert live_act["fp4_gemm"] is False
    assert set(live_act_infer_params.keys()) == {
        "size",
        "fps",
        "audio_cfg",
    }
    assert "ws_url" not in config["inference"]["omni"]["doubao"]
    assert "base_url" not in config["inference"]["llm"]["qwen"]
    assert "system_prompt" not in config["inference"]["llm"]["qwen"]
    assert "system_prompt" not in config["inference"]["llm"]["openai"]
    assert "ws_url" not in config["inference"]["tts"]["qwen"]
    assert "ws_url" not in config["inference"]["asr"]["qwen"]


def test_env_var_substitution():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("key: ${TEST_CYBERVERSE_VAR}\n")
        f.flush()
        os.environ["TEST_CYBERVERSE_VAR"] = "hello_world"
        try:
            config = load_config(f.name)
            assert config["key"] == "hello_world"
        finally:
            del os.environ["TEST_CYBERVERSE_VAR"]
            os.unlink(f.name)


def test_load_config_loads_dotenv_next_to_config(monkeypatch, tmp_path):
    monkeypatch.delenv("TEST_CYBERVERSE_DOTENV_VAR", raising=False)
    monkeypatch.delenv("TEST_CYBERVERSE_DOTENV_QUOTED", raising=False)
    (tmp_path / ".env").write_text(
        """
# ignored
TEST_CYBERVERSE_DOTENV_VAR=from_dotenv
TEST_CYBERVERSE_DOTENV_QUOTED='quoted value'
ignored_line
""",
        encoding="utf-8",
    )
    config_path = tmp_path / "config.yaml"
    config_path.write_text(
        """
key: ${TEST_CYBERVERSE_DOTENV_VAR}
quoted: ${TEST_CYBERVERSE_DOTENV_QUOTED}
""",
        encoding="utf-8",
    )

    config = load_config(config_path)

    assert config["key"] == "from_dotenv"
    assert config["quoted"] == "quoted value"


def test_unmatched_env_var_preserved():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("key: ${NONEXISTENT_CYBERVERSE_VAR_12345}\n")
        f.flush()
        try:
            config = load_config(f.name)
            assert config["key"] == "${NONEXISTENT_CYBERVERSE_VAR_12345}"
        finally:
            os.unlink(f.name)


def test_path_not_substituted():
    """Ensure common env vars like PATH are NOT blindly substituted."""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("my_path: /usr/local/bin\nother: literal_${not_a_var\n")
        f.flush()
        try:
            config = load_config(f.name)
            assert config["my_path"] == "/usr/local/bin"
        finally:
            os.unlink(f.name)


def test_missing_config_file():
    with pytest.raises(FileNotFoundError):
        load_config("/nonexistent/path.yaml")


def test_multiple_vars_in_one_line():
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write("url: ${TEST_HOST}:${TEST_PORT}\n")
        f.flush()
        os.environ["TEST_HOST"] = "localhost"
        os.environ["TEST_PORT"] = "8080"
        try:
            config = load_config(f.name)
            assert config["url"] == "localhost:8080"
        finally:
            del os.environ["TEST_HOST"]
            del os.environ["TEST_PORT"]
            os.unlink(f.name)


def test_avatar_model_config_dir_is_merged_relative_to_main_config():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        model_dir = root / "avatar_models"
        model_dir.mkdir()
        (model_dir / "flash_head.yaml").write_text(
            """
flash_head:
  plugin_class: pkg.FlashHead
  checkpoint_dir: /models/flash
  infer_params:
    width: 512
    height: 288
""",
            encoding="utf-8",
        )
        config_path = root / "cyberverse_config.yaml"
        config_path.write_text(
            """
inference:
  avatar:
    default: flash_head
    model_config_dir: avatar_models
""",
            encoding="utf-8",
        )

        config = load_config(config_path)

    assert config["inference"]["avatar"]["flash_head"]["plugin_class"] == "pkg.FlashHead"
    assert config["inference"]["avatar"]["flash_head"]["infer_params"]["height"] == 288


def test_inline_avatar_model_config_wins_over_external_config():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        model_dir = root / "avatar_models"
        model_dir.mkdir()
        (model_dir / "flash_head.yaml").write_text(
            """
flash_head:
  plugin_class: pkg.External
  compile_model: false
""",
            encoding="utf-8",
        )
        config_path = root / "cyberverse_config.yaml"
        config_path.write_text(
            """
inference:
  avatar:
    model_config_dir: avatar_models
    flash_head:
      plugin_class: pkg.Inline
      compile_model: true
""",
            encoding="utf-8",
        )

        config = load_config(config_path)

    assert config["inference"]["avatar"]["flash_head"]["plugin_class"] == "pkg.Inline"
    assert config["inference"]["avatar"]["flash_head"]["compile_model"] is True


def test_avatar_model_config_dir_expands_env_vars():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        model_dir = root / "avatar_models"
        model_dir.mkdir()
        (model_dir / "live_act.yaml").write_text(
            """
live_act:
  ckpt_dir: ${TEST_CYBERVERSE_MODEL_DIR}
""",
            encoding="utf-8",
        )
        config_path = root / "cyberverse_config.yaml"
        config_path.write_text(
            """
inference:
  avatar:
    model_config_dir: avatar_models
""",
            encoding="utf-8",
        )
        os.environ["TEST_CYBERVERSE_MODEL_DIR"] = "/models/live_act"
        try:
            config = load_config(config_path)
        finally:
            del os.environ["TEST_CYBERVERSE_MODEL_DIR"]

    assert config["inference"]["avatar"]["live_act"]["ckpt_dir"] == "/models/live_act"


def test_avatar_model_config_dir_missing_raises():
    with tempfile.TemporaryDirectory() as tmp:
        config_path = Path(tmp) / "cyberverse_config.yaml"
        config_path.write_text(
            """
inference:
  avatar:
    model_config_dir: avatar_models
""",
            encoding="utf-8",
        )

        with pytest.raises(FileNotFoundError):
            load_config(config_path)


def test_avatar_model_config_file_requires_one_top_level_model():
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        model_dir = root / "avatar_models"
        model_dir.mkdir()
        (model_dir / "bad.yaml").write_text(
            """
flash_head: {}
live_act: {}
""",
            encoding="utf-8",
        )
        config_path = root / "cyberverse_config.yaml"
        config_path.write_text(
            """
inference:
  avatar:
    model_config_dir: avatar_models
""",
            encoding="utf-8",
        )

        with pytest.raises(ValueError):
            load_config(config_path)
