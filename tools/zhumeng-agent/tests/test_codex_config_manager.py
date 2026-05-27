import json
import os
import stat
from pathlib import Path

from zhumeng_agent.adapters.codex.config_manager import (
    COMMON_PROXY_PORTS,
    CODEX_BASE_INSTRUCTIONS,
    CodexConfigManager,
    choose_local_proxy_port,
    discover_git_project_path,
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


def test_existing_project_trust_entries_are_preserved(tmp_path: Path):
    current_repo = "/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main"
    old_repo = "/Users/muqihang/chelingxi_workspace/sub2api"
    (tmp_path / "config.toml").write_text(
        "\n".join(
            [
                'model_provider = "legacy"',
                "",
                f'[projects."{current_repo}"]',
                'trust_level = "trusted"',
                "",
                f'[projects."{old_repo}"]',
                'trust_level = "trusted"',
                "",
            ]
        ),
        encoding="utf-8",
    )
    manager = CodexConfigManager(tmp_path)

    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    assert 'model_provider = "zhumeng-codex"' in config_text
    assert f'[projects."{current_repo}"]' in config_text
    assert f'[projects."{old_repo}"]' in config_text
    parsed = __import__("tomllib").loads(config_text)
    assert parsed["projects"][current_repo]["trust_level"] == "trusted"
    assert parsed["projects"][old_repo]["trust_level"] == "trusted"


def test_requested_project_trust_entry_is_added(tmp_path: Path):
    current_repo = "/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main"
    manager = CodexConfigManager(tmp_path)

    plan = manager.plan_configure(
        DEFAULT_PROFILE,
        18081,
        "loopback-secret",
        SAMPLE_MODEL_CATALOG,
        trusted_project_paths=[current_repo],
    )
    manager.apply_configure(plan)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    parsed = __import__("tomllib").loads(config_text)
    assert parsed["projects"][current_repo]["trust_level"] == "trusted"


def test_configure_keeps_bundled_plugins_available_when_marketplace_exists(tmp_path: Path):
    marketplace = tmp_path / ".tmp" / "bundled-marketplaces" / "openai-bundled"
    marketplace.mkdir(parents=True)
    (marketplace / ".agents" / "plugins").mkdir(parents=True)
    (tmp_path / "config.toml").write_text(
        "\n".join(
            [
                '[plugins."hyperframes@openai-curated"]',
                "enabled = true",
                "",
            ]
        ),
        encoding="utf-8",
    )
    manager = CodexConfigManager(tmp_path)

    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)
    manager.apply_configure(plan)

    parsed = __import__("tomllib").loads((tmp_path / "config.toml").read_text(encoding="utf-8"))
    assert parsed["features"]["plugins"] is True
    assert parsed["marketplaces"]["openai-bundled"]["source"] == str(marketplace)
    assert parsed["plugins"]["computer-use@openai-bundled"]["enabled"] is True
    assert parsed["plugins"]["browser@openai-bundled"]["enabled"] is True
    assert parsed["plugins"]["chrome@openai-bundled"]["enabled"] is True
    assert parsed["plugins"]["hyperframes@openai-curated"]["enabled"] is True


def test_repair_preserves_current_managed_model_selection(tmp_path: Path):
    (tmp_path / "config.toml").write_text(
        "\n".join(
            [
                'model_provider = "zhumeng-codex"',
                'model = "deepseek-v4-flash"',
                'model_reasoning_effort = "xhigh"',
                "",
            ]
        ),
        encoding="utf-8",
    )
    manager = CodexConfigManager(tmp_path)

    manager.repair(DEFAULT_PROFILE, 18081, "loopback-secret", SAMPLE_MODEL_CATALOG)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    parsed = __import__("tomllib").loads(config_text)
    assert parsed["model"] == "deepseek-v4-flash"
    assert parsed["model_reasoning_effort"] == "xhigh"


def test_discover_git_project_path_returns_repository_root(tmp_path: Path):
    repo = tmp_path / "repo"
    nested = repo / "a" / "b"
    nested.mkdir(parents=True)
    (repo / ".git").mkdir()

    assert discover_git_project_path(nested) == repo
    assert discover_git_project_path(tmp_path / "outside") is None


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
                "shell_type": "local",
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
    assert model["shell_type"] == "local"
    assert model["experimental_supported_tools"] == ["function", "namespace", "custom"]
    assert model["supports_search_tool"] is True
    assert model["web_search_tool_type"] == "text"
    assert saved["models"][1]["web_search_tool_type"] == "text_and_image"
    assert "For multi-line file creation or rewrites" in saved["models"][0]["base_instructions"]
    assert "skills, plugins, MCP servers, or tool routing guidance" in saved["models"][0]["base_instructions"]
    assert "clearly matches" in saved["models"][0]["base_instructions"]
    assert "Do not load unrelated skills" in saved["models"][0]["base_instructions"]
    assert saved["models"][0]["base_instructions"] == CODEX_BASE_INSTRUCTIONS
    assert saved["models"][0]["model_messages"]["instructions_template"] == CODEX_BASE_INSTRUCTIONS


def test_model_catalog_adds_routing_bridge_only_for_non_openai_fallback_models(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    catalog = manager.build_model_catalog({
        "models": [
            {
                "slug": "gpt-5.5",
                "display_name": "GPT 5.5",
                "provider": "openai",
            },
            {
                "slug": "claude-opus-4-7",
                "display_name": "Claude Opus 4.7",
                "provider": "anthropic",
            },
            {
                "slug": "deepseek-v4-pro",
                "display_name": "DeepSeek V4 Pro",
                "provider_id": "deepseek",
            },
            {
                "slug": "legacy-managed-deepseek",
                "display_name": "Legacy Managed DeepSeek",
                "provider_id": "zhumeng",
                "provider": "deepseek",
            },
            {
                "slug": "claude-compatible-openai-model",
                "display_name": "Claude-compatible OpenAI Model",
                "provider": "openai",
            },
        ]
    })

    by_slug = {model["slug"]: model for model in catalog["models"]}
    assert "skills, plugins, MCP servers, or tool routing guidance" not in by_slug["gpt-5.5"]["base_instructions"]
    assert "skills, plugins, MCP servers, or tool routing guidance" in by_slug["claude-opus-4-7"]["base_instructions"]
    assert "skills, plugins, MCP servers, or tool routing guidance" in by_slug["deepseek-v4-pro"]["base_instructions"]
    assert "skills, plugins, MCP servers, or tool routing guidance" in by_slug["legacy-managed-deepseek"]["base_instructions"]
    assert "skills, plugins, MCP servers, or tool routing guidance" not in by_slug["claude-compatible-openai-model"]["base_instructions"]


def test_model_catalog_preserves_gateway_cli_catalog_payload(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    gateway_catalog = {
        "models": [
            {
                "slug": "claude-opus-4-7",
                "display_name": "Claude Opus 4.7",
                "base_instructions": "You are Codex, based on GPT-5.",
                "model_messages": {
                    "instructions_template": "You are Codex, based on GPT-5.",
                    "instructions_variables": {},
                },
                "supported_reasoning_levels": [{"effort": "high", "description": "Greater reasoning depth"}],
                "visibility": "list",
                "shell_type": "local",
            }
        ]
    }

    catalog = manager.build_model_catalog(gateway_catalog)

    model = catalog["models"][0]
    assert model["base_instructions"] == "You are Codex, based on GPT-5."
    assert model["model_messages"]["instructions_template"] == "You are Codex, based on GPT-5."


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
    assert model["shell_type"] == "local"


def test_common_proxy_ports_are_avoided():
    for port in COMMON_PROXY_PORTS:
        chosen = choose_local_proxy_port(port)
        assert chosen not in COMMON_PROXY_PORTS

def test_build_model_catalog_preserves_origin_capabilities_and_pricing(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    catalog = manager.build_model_catalog({
        "models": [
            {
                "slug": "gpt-5.5",
                "display_name": "GPT-5.5",
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {
                    "responses": True,
                    "streaming": True,
                    "tool_calls": True,
                    "image_input": True,
                    "cache_pricing": True,
                    "context_continuation": True,
                },
                "pricing": {
                    "input_price": "2.50",
                    "output_price": "15.00",
                    "cached_input_price": "0.25",
                    "cache_write_price": "2.50",
                    "currency": "USD",
                    "unit": "per_1m_tokens",
                    "updated_at": "2026-05-21T00:00:00Z",
                    "source": "database_model_pricing",
                },
            },
            {
                "slug": "missing-price-model",
                "display_name": "Missing Price Model",
                "origin": "zhumeng",
                "provider_id": "zhumeng",
                "capabilities": {"responses": True},
                "pricing": None,
            },
        ]
    })

    assert catalog["models"][0]["origin"] == "zhumeng"
    assert catalog["models"][0]["provider_id"] == "zhumeng"
    assert catalog["models"][0]["capabilities"]["tool_calls"] is True
    assert catalog["models"][0]["pricing"]["source"] == "database_model_pricing"
    assert catalog["models"][1]["pricing"] is None


def test_build_model_catalog_preserves_deepseek_hosted_image_and_search_capabilities(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    catalog = manager.build_model_catalog({
        "models": [
            {
                "slug": "deepseek-v4-pro",
                "display_name": "DeepSeek V4 Pro",
                "provider_id": "deepseek",
                "origin": "deepseek",
                "capabilities": {
                    "responses": True,
                    "streaming": True,
                    "tool_calls": True,
                    "image_input": True,
                    "context_continuation": True,
                },
                "input_modalities": ["text", "image"],
                "supports_image_detail_original": True,
                "supports_search_tool": True,
                "web_search_tool_type": "text_and_image",
                "experimental_supported_tools": ["function", "namespace", "custom"],
            }
        ]
    })

    model = catalog["models"][0]
    assert model["provider_id"] == "deepseek"
    assert model["origin"] == "deepseek"
    assert model["input_modalities"] == ["text", "image"]
    assert model["supports_image_detail_original"] is True
    assert model["supports_search_tool"] is True
    assert model["web_search_tool_type"] == "text_and_image"
    assert model["experimental_supported_tools"] == ["function", "namespace", "custom"]
    assert model["capabilities"]["tool_calls"] is True
    assert model["capabilities"]["image_input"] is True
