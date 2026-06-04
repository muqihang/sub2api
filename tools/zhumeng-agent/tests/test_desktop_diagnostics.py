import json

import pytest
import platform
from pathlib import Path

import zhumeng_agent.cli as cli
from zhumeng_agent.cli import main


@pytest.fixture(autouse=True)
def restore_cli_defaults():
    originals = {
        name: getattr(cli, name)
        for name in (
            "default_state_store",
            "default_http_client",
            "default_config_manager",
            "generate_loopback_secret",
            "choose_local_proxy_port",
            "ensure_proxy_running",
            "default_codex_app_path",
            "build_codex_launch_command",
            "select_cdp_port",
            "launch_codex_process",
            "inspect_codex_enhancements",
            "patch_codex_enhancements",
            "restore_codex_enhancements",
            "resolve_codex_home",
            "codex_doctor_report",
        )
        if hasattr(cli, name)
    }
    yield
    for name, value in originals.items():
        setattr(cli, name, value)


def parse_output(capsys):
    out = capsys.readouterr().out.strip()
    assert out
    return json.loads(out)


class Store:
    def read(self):
        return {
            "status": "configured",
            "client": "codex",
            "access_token": "access-token-secret",
            "refresh_token": "refresh-token-secret",
            "loopback_secret": "loopback-secret-secret",
            "server_base_url": "https://example.com",
            "proxy_port": 18081,
            "user_email": "alice@example.com",
            "codex_home": str(Path.home() / ".codex"),
        }


def test_desktop_diagnose_redacts_token_email_machine_name_and_home(capsys):
    cli.default_state_store = lambda: Store()
    cli.resolve_codex_home = lambda: Path.home() / ".codex"
    cli.default_codex_app_path = lambda: Path("/Applications/Codex.app")
    cli.codex_doctor_report = lambda *args, **kwargs: {
        "client": "codex",
        "message": f"alice@example.com {platform.node()} {Path.home()} access-token-secret refresh-token-secret loopback-secret-secret",
    }

    exit_code = main(["desktop", "diagnose", "--redacted", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    assert payload["ok"] is True
    assert payload["command"] == "desktop diagnose"
    dumped = json.dumps(payload)
    assert "access-token-secret" not in dumped
    assert "refresh-token-secret" not in dumped
    assert "loopback-secret-secret" not in dumped
    assert "alice@example.com" not in dumped
    assert platform.node() not in dumped
    assert str(Path.home()) not in dumped
    assert "<redacted" in dumped


def test_doctor_reports_model_catalog_freshness_and_skills_evidence(tmp_path: Path):
    import zhumeng_agent.doctor as doctor

    codex_home = tmp_path / ".codex"
    catalog = codex_home / "zhumeng-codex-models.json"
    (codex_home / "skills" / "local-skill").mkdir(parents=True)
    (codex_home / "superpowers" / "skills" / "systematic-debugging").mkdir(parents=True)
    plugin_skill = codex_home / "plugins" / "cache" / "openai-bundled" / "computer-use" / "1" / "skills" / "computer-use"
    plugin_skill.mkdir(parents=True)
    codex_home.mkdir(parents=True, exist_ok=True)
    catalog.write_text(json.dumps({
        "models": [
            {"slug": "deepseek-v4-pro", "base_instructions": "skills, plugins, MCP servers, or tool routing guidance"},
            {"slug": "deepseek-v4-flash", "base_instructions": "skills, plugins, MCP servers, or tool routing guidance"},
            {"slug": "claude-sonnet-4-6", "base_instructions": ""},
        ]
    }), encoding="utf-8")
    marketplace_source = codex_home / ".tmp" / "bundled-marketplaces" / "openai-bundled"
    (codex_home / "config.toml").write_text(
        f'model = "deepseek-v4-pro"\n'
        f'model_catalog_json = "{catalog}"\n\n'
        '[marketplaces.openai-bundled]\n'
        'source_type = "local"\n'
        f'source = "{marketplace_source}"\n\n'
        '[plugins."computer-use@openai-bundled"]\n'
        'enabled = true\n',
        encoding="utf-8",
    )

    report = doctor.codex_doctor_report(codex_home, state={"catalog_hash_after": "stale", "restart_required": True})

    freshness = report["model_catalog_freshness"]
    assert freshness["catalog_has_deepseek"] is True
    assert freshness["deepseek_models_present"] == ["deepseek-v4-flash", "deepseek-v4-pro"]
    assert freshness["active_default_model"] == "deepseek-v4-pro"
    assert freshness["catalog_hash"]
    assert freshness["catalog_mtime"]
    assert freshness["restart_required"] is True
    assert "catalog_hash_changed" in freshness["restart_required_reasons"]

    skills = report["skills_evidence"]
    assert skills["configured_marketplaces"] == ["openai-bundled"]
    assert skills["enabled_plugins"] == ["computer-use@openai-bundled"]
    assert skills["skills_dirs"][0]["present"] is True
    assert skills["superpowers_skills_dirs"][0]["present"] is True
    assert skills["plugin_cache_skill_paths"][0]["present"] is True
    assert skills["base_instruction_routing_guidance_present"] is True
    assert skills["deepseek_models_with_routing_guidance"] == ["deepseek-v4-flash", "deepseek-v4-pro"]


def test_desktop_diagnose_includes_model_catalog_freshness(capsys, tmp_path: Path):
    codex_home = tmp_path / ".codex"
    catalog = codex_home / "zhumeng-codex-models.json"
    codex_home.mkdir(parents=True)
    catalog.write_text(json.dumps({"models": [{"slug": "deepseek-v4-pro", "base_instructions": "skills, plugins, MCP servers, or tool routing guidance"}]}), encoding="utf-8")
    (codex_home / "config.toml").write_text(f'model = "deepseek-v4-pro"\nmodel_catalog_json = "{catalog}"\n', encoding="utf-8")

    class FreshStore:
        path = tmp_path / "state.json"
        def read(self):
            return {"status": "configured", "client": "codex", "codex_home": str(codex_home)}

    cli.default_state_store = lambda: FreshStore()
    cli.resolve_codex_home = lambda: codex_home
    cli.default_codex_app_path = lambda: None

    exit_code = main(["desktop", "diagnose", "--redacted", "--json"])

    assert exit_code == 0
    payload = parse_output(capsys)
    freshness = payload["data"]["doctor"]["model_catalog_freshness"]
    assert freshness["catalog_has_deepseek"] is True
    assert freshness["active_default_model"] == "deepseek-v4-pro"
    assert payload["data"]["doctor"]["skills_evidence"]["base_instruction_routing_guidance_present"] is True
