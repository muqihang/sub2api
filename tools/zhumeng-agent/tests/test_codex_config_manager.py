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
    "model_provider": "zhumeng-managed",
    "wire_api": "responses",
    "requires_openai_auth": True,
    "supports_websockets": True,
}


def test_no_existing_config_creates_managed_config(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret")
    manager.apply_configure(plan)

    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    auth = json.loads((tmp_path / "auth.json").read_text(encoding="utf-8"))

    assert 'model_provider = "zhumeng-managed"' in config_text
    assert "[features]" in config_text
    assert "responses_websockets_v2 = true" in config_text
    assert 'base_url = "http://127.0.0.1:18081/v1"' in config_text
    assert auth["OPENAI_API_KEY"] == "zhumeng-local-managed-loopback-secret"


def test_existing_config_is_backed_up(tmp_path: Path):
    (tmp_path / "config.toml").write_text('model_provider = "legacy"\n', encoding="utf-8")
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret")
    manager.apply_configure(plan)

    backups = list((tmp_path / "backups").glob("config.toml.*.bak"))
    assert backups


def test_invalid_toml_is_backed_up_before_repair(tmp_path: Path):
    (tmp_path / "config.toml").write_text('model_provider = "broken', encoding="utf-8")
    manager = CodexConfigManager(tmp_path)

    manager.repair(DEFAULT_PROFILE, 18081, "loopback-secret")

    backups = list((tmp_path / "backups").glob("config.toml.*.bak"))
    assert backups
    config_text = (tmp_path / "config.toml").read_text(encoding="utf-8")
    assert 'model_provider = "zhumeng-managed"' in config_text


def test_auth_json_never_contains_raw_api_key(tmp_path: Path):
    manager = CodexConfigManager(tmp_path)
    plan = manager.plan_configure(DEFAULT_PROFILE, 18081, "loopback-secret")
    manager.apply_configure(plan)

    auth_text = (tmp_path / "auth.json").read_text(encoding="utf-8")
    assert "sk-" not in auth_text
    assert "zhumeng-local-managed-loopback-secret" in auth_text
    if os.name == "posix":
        mode = stat.S_IMODE((tmp_path / "auth.json").stat().st_mode)
        assert mode == 0o600


def test_common_proxy_ports_are_avoided():
    for port in COMMON_PROXY_PORTS:
        chosen = choose_local_proxy_port(port)
        assert chosen not in COMMON_PROXY_PORTS
