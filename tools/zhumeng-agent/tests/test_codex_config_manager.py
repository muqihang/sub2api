import json
import os
import stat
from pathlib import Path

from zhumeng_agent.adapters.codex.config_manager import (
    COMMON_PROXY_PORTS,
    CodexConfigManager,
    choose_local_proxy_port,
)


DEFAULT_PROFILE = {
    "model_provider": "zhumeng-codex",
    "wire_api": "responses",
    "requires_openai_auth": True,
    "supports_websockets": False,
}


SAMPLE_MODEL_CATALOG = {
    "models": [
        {
            "slug": "deepseek-v4-pro",
            "display_name": "DeepSeek V4 Pro",
            "default_reasoning_level": "xhigh",
            "supported_reasoning_levels": [
                {"effort": "high", "description": "Greater reasoning depth"},
                {"effort": "xhigh", "description": "Extra high reasoning"},
            ],
            "context_window": 1000000,
            "auto_compact_token_limit": 850000,
        }
    ]
}


def test_no_existing_config_creates_managed_config(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    auth = json.loads((tmp_path / "auth.json").read_text(encoding="utf-8"))
    catalog = json.loads((tmp_path / "zhumeng-codex-models.json").read_text(encoding="utf-8"))

    assert 'model_provider = "zhumeng-codex"' in config_text
    assert 'model = "deepseek-v4-pro"' in config_text
    assert "model_context_window = 1000000" in config_text
    assert "model_auto_compact_token_limit = 850000" in config_text
    assert 'model_catalog_json = "' in config_text
    assert '[model_providers.zhumeng-codex]' in config_text
    assert 'name = "Zhumeng Codex"' in config_text
    assert "[features]" in config_text
    assert "responses_websockets_v2 = false" in config_text
    assert 'base_url = "http://127.0.0.1:18081/v1"' in config_text
    assert "supports_websockets = false" in config_text
    assert auth["OPENAI_API_KEY"] == "zhumeng-local-managed-loopback-secret"
    assert catalog["models"][0]["slug"] == "deepseek-v4-pro"
    assert catalog["models"][0].get("context_window") != 400000


def test_websocket_support_is_disabled_for_legacy_profiles(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    legacy_profile = dict(DEFAULT_PROFILE)
    legacy_profile["supports_websockets"] = True

    plan = manager.plan_configure(legacy_profile, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    assert "responses_websockets_v2 = false" in config_text
    assert "supports_websockets = false" in config_text


def test_existing_config_is_backed_up(tmp_path: Path):
    (tmp_path / "config.toml").write_text('model_provider = "legacy"\n', encoding="utf-8")
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    backups = list((tmp_path / "backups").glob("config.toml.*.bak"))
    assert backups


def test_invalid_toml_is_backed_up_before_repair(tmp_path: Path):
    (tmp_path / "config.toml").write_text('model_provider = "broken', encoding="utf-8")
    manager = CodexConfigManager(tmp_path)

    manager.repair(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)

    backups = list((tmp_path / "backups").glob("config.toml.*.bak"))
    assert backups
    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    assert 'model_provider = "zhumeng-codex"' in config_text


def test_auth_json_never_contains_raw_api_key(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    auth_text = (tmp_path / "auth.json").read_text(encoding="utf-8")
    assert "sk-" not in auth_text
    assert "zhumeng-local-managed-loopback-secret" in auth_text
    if os.name == "posix":
        mode = stat.S_IMODE((tmp_path / "auth.json").stat().st_mode)
        assert mode == 0o600


def test_model_catalog_is_generated_from_gateway_models(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    catalog = manager.build_model_catalog({
        "models": [
            {
                "slug": "custom-web-model",
                "display_name": "Custom Web Model",
                "provider": "deepseek",
                "visibility": "visible",
                "supported_in_api": True,
                "priority": 77,
                "default_reasoning_level": "high",
                "supported_reasoning_levels": ["high", "xhigh"],
                "context_window": 123456,
                "auto_compact_token_limit": 100000,
                "max_output_tokens": 4096,
                "input_modalities": ["text"],
                "experimental_supported_tools": ["function", "namespace", "custom"],
                "supports_search_tool": True,
                "web_search_tool_type": "openai",
            },
            {
                "slug": "custom-vision-model",
                "display_name": "Custom Vision Model",
                "input_modalities": ["text", "image"],
                "supports_search_tool": True,
                "web_search_tool_type": "openai",
            }
        ]
    })

    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", catalog)
    manager.apply_configure(plan)

    saved = json.loads((tmp_path / "zhumeng-codex-models.json").read_text(encoding="utf-8"))
    model = saved["models"][0]
    assert model["slug"] == "custom-web-model"
    assert model["display_name"] == "Custom Web Model"
    assert model["visibility"] == "list"
    assert model["supported_reasoning_levels"][0]["effort"] == "high"
    assert model["context_window"] == 123456
    assert model["auto_compact_token_limit"] == 100000
    assert model["max_context_window"] == 123456
    assert model["effective_context_window_percent"] == 95
    assert model["experimental_supported_tools"] == ["function", "namespace", "custom"]
    assert model["supports_search_tool"] is True
    assert model["web_search_tool_type"] == "text"
    assert saved["models"][1]["web_search_tool_type"] == "text_and_image"


def test_model_catalog_tolerates_invalid_numeric_fields(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    catalog = manager.build_model_catalog({
        "models": [
            {
                "slug": "",
                "display_name": "Broken Model",
                "priority": "bad",
                "context_window": "unknown",
                "max_context_window": " ",
                "effective_context_window_percent": "bad",
                "max_output_tokens": "nan",
            }
        ]
    })

    model = catalog["models"][0]
    assert model["slug"] == "unknown-model"
    assert model["priority"] == 0
    assert model["context_window"] == 0
    assert model["auto_compact_token_limit"] == 0
    assert model["max_context_window"] == 0
    assert model["effective_context_window_percent"] == 95
    assert model["max_output_tokens"] == 128000


def test_common_proxy_ports_are_avoided():
    for port in COMMON_PROXY_PORTS:
        chosen = choose_local_proxy_port(port)
        assert chosen not in COMMON_PROXY_PORTS
