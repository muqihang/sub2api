import json
import os
from pathlib import Path
import hashlib
import subprocess
import shutil
import sys
from types import SimpleNamespace

import pytest
from aiohttp.test_utils import TestClient, TestServer

import zhumeng_agent.cli as cli
from zhumeng_agent.adapters.codex.model_picker import ModelPickerPatchError
from zhumeng_agent.adapters.claude_code.launcher import _bridge_model_capabilities_json, build_bridge_effort_ui_probe_metadata
from zhumeng_agent.adapters.claude_code.runtime_installer import EFFORT_CAPABILITY_HOOK_NEEDLE, EFFORT_CAPABILITY_HOOK_REPLACEMENT, EXACT_EFFORT_LEVEL_UI_PATCH_POINT
from zhumeng_agent.cli import main

REPO_ROOT = Path(__file__).resolve().parents[3]

ORIGINAL_DEFAULT_CAPTURE_CONFIG = cli.default_capture_config
ORIGINAL_GENERATE_CAPTURE_REPORT = cli.generate_capture_report
ORIGINAL_ENSURE_CAPTURE_RECEIVER_RUNNING = cli.ensure_capture_receiver_running
ORIGINAL_ENSURE_CAPTURE_BRIDGE_RUNNING = cli.ensure_capture_bridge_running
ORIGINAL_ENSURE_PROXY_RUNNING = cli.ensure_proxy_running


@pytest.fixture(autouse=True)
def restore_cli_globals_after_test():
    originals = {
        name: getattr(cli, name)
        for name in (
            "default_capture_config",
            "generate_capture_report",
            "ensure_capture_receiver_running",
            "ensure_capture_bridge_running",
            "ensure_proxy_running",
            "default_state_store",
            "default_config_manager",
            "default_http_client",
            "default_codex_app_path",
            "resolve_codex_home",
            "codex_doctor_report",
            "detect_codex_app_path",
            "patch_codex_enhancements",
            "select_cdp_port",
            "launch_codex_process",
            "launch_claude_code_process",
            "capture_installation_enabled",
            "inject_capture_hook_via_cdp",
            "run_managed_claude_code",
            "resolve_active_managed_runtime",
        )
        if hasattr(cli, name)
    }
    yield
    for name, value in originals.items():
        setattr(cli, name, value)


def parse_output(capsys):
    output = capsys.readouterr().out.strip()
    assert output, "expected CLI to print JSON"
    return json.loads(output)


def test_setup_parses_codex_setup_args(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token",
                "refresh_token": "refresh-token",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {
                    "model_provider": "zhumeng-managed",
                    "wire_api": "responses",
                    "requires_openai_auth": True,
                    "supports_websockets": True,
                },
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_route_hint_secret": "server-route-hint-secret",
            }

        def list_codex_models(self, **kwargs):
            return {
                "models": [
                    {
                        "slug": "custom-web-model",
                        "display_name": "Custom Web Model",
                        "visibility": "visible",
                        "supported_reasoning_levels": ["high"],
                    }
                ]
            }

    class FakeStore:
        def __init__(self):
            self.payload = None

        def write(self, payload):
            self.payload = payload

    fake_store = FakeStore()
    auth_path = tmp_path / ".codex" / "auth.json"
    auth_path.parent.mkdir(parents=True, exist_ok=True)
    auth_path.write_text('{"OPENAI_API_KEY":"legacy-secret"}', encoding="utf-8")
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: fake_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 9999

    exit_code = main([
        "setup",
        "--client", "codex",
        "--code", "abc",
        "--server", "https://example.com",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "setup"
    assert data["client"] == "codex"
    assert data["server"] == "https://example.com"
    assert data["code_redacted"] is True
    assert data["status"] == "configured"
    assert fake_store.payload["device_id"] == 9
    assert fake_store.payload["prior_auth_json"]
    catalog = json.loads((tmp_path / ".codex" / "zhumeng-codex-models.json").read_text(encoding="utf-8"))
    assert catalog["models"][0]["slug"] == "custom-web-model"


def test_deeplink_invocation_dispatches_to_setup(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token",
                "refresh_token": "refresh-token",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {
                    "model_provider": "zhumeng-managed",
                    "wire_api": "responses",
                    "requires_openai_auth": True,
                    "supports_websockets": True,
                },
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
            }

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

    class FakeStore:
        def __init__(self):
            self.payload = None

        def write(self, payload):
            self.payload = payload

    fake_store = FakeStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: fake_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 9999

    exit_code = main(["zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "setup"
    assert data["client"] == "codex"


def test_deeplink_invocation_uses_sys_argv_when_run_as_module(capsys, tmp_path: Path, monkeypatch):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token",
                "refresh_token": "refresh-token",
                "managed_session_id": "sess-1",
                "expires_at": "2026-05-11T12:00:00Z",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://example.com",
                "config_profile": {
                    "model_provider": "zhumeng-managed",
                    "wire_api": "responses",
                    "requires_openai_auth": True,
                    "supports_websockets": True,
                },
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
            }

        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"}]}

    class FakeStore:
        def __init__(self):
            self.payload = None

        def write(self, payload):
            self.payload = payload

    fake_store = FakeStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: fake_store
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 9999
    monkeypatch.setattr(cli.sys, "argv", [
        "zhumeng-agent",
        "zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com",
    ])

    exit_code = main()

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "setup"
    assert data["client"] == "codex"


def test_doctor_returns_json(capsys, monkeypatch):
    monkeypatch.setattr(cli, "resolve_codex_home", lambda: Path("/tmp/fake-codex-home"))
    monkeypatch.setattr(cli, "codex_doctor_report", lambda *args, **kwargs: {"client": "codex", "plugins": {}})
    exit_code = main(["doctor", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "doctor"
    assert data["format"] == "json"


def test_status_reports_desktop_injection_fields(capsys):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "codex",
                "proxy_port": 18081,
                "desktop_enhancements": {
                    "status": "patched",
                    "model_picker": {"status": "patched"},
                    "plugin_auth_gate": {"status": "patched"},
                    "plugin_mention_marketplace": {"status": "patched"},
                },
                "model_catalog_meta": {"source": "gateway"},
            }

    cli.default_state_store = lambda: FakeStore()

    exit_code = main(["status", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "status"
    assert data["status"] == "configured"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"
    assert data["model_catalog"]["source"] == "gateway"


def test_status_reports_desktop_injection_fields_from_aggregate_items(capsys):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "codex",
                "proxy_port": 18081,
                "desktop_enhancements": {
                    "status": "patched",
                    "items": {
                        "model-picker": {"status": "patched"},
                        "plugin-auth-gate": {"status": "already_patched"},
                        "plugin-mention-marketplace": {"status": "patched"},
                    },
                },
            }

    cli.default_state_store = lambda: FakeStore()

    exit_code = main(["status", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "already_patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"


def test_status_includes_claude_code_operator_status(capsys, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "codex",
                "proxy_port": 18081,
                "claude_code_native": {
                    "configured": True,
                    "process": {"pid": 12345},
                    "guard": {"status": "running", "attested": True},
                    "profile": {"status": "ready", "profile_id": "real_claude_code_native_takeover_v1"},
                    "shape_healthcheck": {"status": "pass"},
                    "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                    "netwatch": {
                        "summary": {
                            "potential_guard_bypass_count": 0,
                            "official_or_public_bypass_count": 0,
                            "remote_host_buckets": {"loopback": 1},
                            "stores_payload": False,
                            "stores_headers": False,
                        }
                    },
                },
            }

    cli.default_state_store = lambda: FakeStore()
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: pid == 12345)

    exit_code = main(["status", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["claude_code"]["status"] == "running"
    assert data["adapters"]["claude_code"]["status"] == "running"
    assert data["claude_code"]["guard"]["attested"] is True


def test_status_claude_code_guard_bypass_is_raw_safe(capsys):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "codex",
                "claude_code_native": {
                    "configured": True,
                    "raw_prompt": "raw-prompt-marker",
                    "entry_api_token": "raw-token-marker",
                    "guard": {"status": "running", "attested": True},
                    "netwatch": {
                        "summary": {
                            "connection_count": 2,
                            "potential_guard_bypass_count": 1,
                            "official_or_public_bypass_count": 1,
                            "remote_host_buckets": {"loopback": 1, "anthropic_or_claude": 1},
                            "stores_payload": False,
                            "stores_headers": False,
                        }
                    },
                },
            }

    cli.default_state_store = lambda: FakeStore()

    exit_code = main(["status", "--json"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["claude_code"]["status"] == "guard_bypass"
    dumped = json.dumps(data, sort_keys=True)
    assert "raw-prompt-marker" not in dumped
    assert "raw-token-marker" not in dumped
    assert "api.anthropic.com" not in dumped




class CP0MemoryStore:
    def __init__(self, payload=None):
        self.payload = dict(payload or {})

    def read(self):
        return dict(self.payload)

    def write(self, payload):
        self.payload = dict(payload)

    def update(self, patch):
        self.payload.update(patch)
        return dict(self.payload)

def test_setup_prefers_server_provisioned_claude_code_native_attestation_secret(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token-secret",
                "refresh_token": "refresh-token-secret",
                "managed_session_id": "sess-1",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://gateway.zhumeng.example",
                "config_profile": {"model_provider": "zhumeng-codex"},
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_route_hint_secret": "server-route-hint-secret",
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = CP0MemoryStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    secrets = iter(["loopback-secret", "native-attestation-secret"])
    cli.generate_loopback_secret = lambda: next(secrets)
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 123

    exit_code = main(["setup", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com"])

    assert exit_code == 0
    parse_output(capsys)
    assert store.payload["loopback_secret"] == "loopback-secret"
    assert store.payload["claude_code_native_attestation_secret"] == "server-native-attestation-secret"
    assert store.payload["claude_code_route_hint_secret"] == "server-route-hint-secret"
    assert store.payload["claude_code_route_hint_secret_source"] == "server"


def test_setup_fails_closed_when_server_omits_claude_code_native_attestation_secret(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "access-token-secret",
                "refresh_token": "refresh-token-secret",
                "managed_session_id": "sess-1",
                "device_id": 9,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://gateway.zhumeng.example",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = CP0MemoryStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "loopback-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 123

    exit_code = main(["setup", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com"])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "claude_code_native_attestation_secret" in data["message"]
    assert store.payload == {}


def test_reauth_prefers_server_provisioned_claude_code_native_attestation_secret(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "new-access-token",
                "refresh_token": "new-refresh-token",
                "managed_session_id": "sess-2",
                "device_id": 10,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://gateway.zhumeng.example",
                "config_profile": {"model_provider": "zhumeng-codex"},
                "claude_code_native_attestation_secret": "server-rotated-native-secret",
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = CP0MemoryStore({
        "status": "configured",
        "client": "codex",
        "gateway_base_url": "https://old-gateway.example",
        "access_token": "old-access",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "refresh_token": "old-refresh",
        "config_profile": {"model_provider": "zhumeng-codex"},
        "proxy_port": 18081,
        "loopback_secret": "loopback-secret",
        "claude_code_native_attestation_secret": "existing-native-secret",
        "claude_code_native_attestation_secret_source": "server",
    })
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.choose_local_proxy_port = lambda preferred=None: 18081
    cli.ensure_proxy_running = lambda store: 123

    exit_code = main(["desktop", "reauth", "--client", "codex", "--code", "fresh-code", "--server", "https://example.com", "--json"])

    assert exit_code == 0
    parse_output(capsys)
    assert store.payload["claude_code_native_attestation_secret"] == "server-rotated-native-secret"


def test_reauth_preserves_existing_claude_code_native_attestation_secret(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "new-access-token",
                "refresh_token": "new-refresh-token",
                "managed_session_id": "sess-2",
                "device_id": 10,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://gateway.zhumeng.example",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = CP0MemoryStore({
        "status": "configured",
        "client": "codex",
        "gateway_base_url": "https://old-gateway.example",
        "access_token": "old-access",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "refresh_token": "old-refresh",
        "config_profile": {"model_provider": "zhumeng-codex"},
        "proxy_port": 18081,
        "loopback_secret": "loopback-secret",
        "claude_code_native_attestation_secret": "existing-native-secret",
        "claude_code_native_attestation_secret_source": "server",
    })
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "new-secret-should-not-be-used"
    cli.ensure_proxy_running = lambda store: 123
    cli.default_codex_app_path = lambda: None

    exit_code = main(["desktop", "reauth", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com", "--json"])

    assert exit_code == 0
    parse_output(capsys)
    assert store.payload["claude_code_native_attestation_secret"] == "existing-native-secret"

def test_reauth_does_not_promote_non_server_route_hint_secret(capsys, tmp_path: Path):
    class FakeClient:
        def exchange_setup_grant(self, **kwargs):
            return {
                "access_token": "new-access-token",
                "refresh_token": "new-refresh-token",
                "managed_session_id": "sess-2",
                "device_id": 10,
                "server_base_url": "https://example.com",
                "gateway_base_url": "https://gateway.zhumeng.example",
                "config_profile": {"model_provider": "zhumeng-codex"},
            }

        def list_codex_models(self, **kwargs):
            return {"models": []}

    store = CP0MemoryStore({
        "status": "configured",
        "client": "codex",
        "gateway_base_url": "https://old-gateway.example",
        "access_token": "old-access",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "refresh_token": "old-refresh",
        "config_profile": {"model_provider": "zhumeng-codex"},
        "proxy_port": 18081,
        "loopback_secret": "loopback-secret",
        "claude_code_native_attestation_secret": "existing-native-secret",
        "claude_code_native_attestation_secret_source": "server",
        "claude_code_route_hint_secret": "local-route-hint-secret",
        "claude_code_route_hint_secret_source": "local",
    })
    cli.default_http_client = lambda server: FakeClient()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: cli.CodexConfigManager(tmp_path / ".codex")
    cli.generate_loopback_secret = lambda: "new-secret-should-not-be-used"
    cli.ensure_proxy_running = lambda store: 123
    cli.default_codex_app_path = lambda: None

    exit_code = main(["desktop", "reauth", "--client", "codex", "--code", "one-time-code", "--server", "https://example.com", "--json"])

    assert exit_code == 0
    parse_output(capsys)
    assert cli.server_route_hint_secret(store.payload) is None
    assert store.payload.get("claude_code_route_hint_secret", "") == ""
    assert store.payload.get("claude_code_route_hint_secret_source", "") == ""

def write_fake_claude_runtime(runtime_root: Path, executable: Path, *, payload: bytes = b'k.enum(["sonnet","opus","haiku","fable"]).optional()', patches: dict[str, object] | None = None, upstream_version: str = "2.1.175") -> tuple[Path, str, str]:
    executable.parent.mkdir(parents=True, exist_ok=True)
    executable.write_bytes(payload)
    runtime_hash = "sha256:" + hashlib.sha256(payload).hexdigest()
    overlay_hash = "sha256:" + "2" * 64
    manifest_dir = runtime_root / "claude-code" / upstream_version
    manifest_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = manifest_dir / "manifest.json"
    manifest = {
        "runtime": "claude-code",
        "upstream_version": upstream_version,
        "zhumeng_runtime_version": "0.1.0",
        "source": f"npm:@anthropic-ai/claude-code@{upstream_version}",
        "upstream_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "patch_points": ["runtime_manifest", "hash_lock", "isolated_config", "guard_env"],
        "cch_profile": "claude_code_" + upstream_version.replace(".", "_"),
        "status": "ready",
        "executable_path": str(executable.resolve(strict=False)),
    }
    manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    patch_payload = {"runtime": "claude-code", "upstream_version": upstream_version, "patch_points": ["runtime_manifest", "hash_lock", "isolated_config", "guard_env"], "live_bridge_models_enabled": False}
    if patches:
        patch_payload.update(patches)
    patches_path = manifest_dir / "patches.json"
    patches_path.write_text(json.dumps(patch_payload, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    manifest_hash = "sha256:" + hashlib.sha256(manifest_path.read_bytes()).hexdigest()
    patches_hash = "sha256:" + hashlib.sha256(patches_path.read_bytes()).hexdigest()
    (manifest_dir / "hash.lock").write_text(
        json.dumps({
            "runtime": "claude-code",
            "upstream_version": upstream_version,
            "manifest_hash": manifest_hash,
            "locked_files": {"manifest.json": manifest_hash, "patches.json": patches_hash},
        }, sort_keys=True, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )
    active_pointer = runtime_root / "claude-code" / "active"
    active_pointer.write_text(
        json.dumps({"runtime": "claude-code", "status": "enabled", "active_version": upstream_version, "manifest_path": str(manifest_path)}, sort_keys=True, separators=(",", ":")) + "\n",
        encoding="utf-8",
    )
    return executable.resolve(strict=False), runtime_hash, overlay_hash


def test_active_runtime_bridge_live_models_fail_closed_on_missing_or_malformed_allowlist():
    assert cli._active_runtime_bridge_live_models({"live_bridge_models_enabled": True}) == ()
    assert cli._active_runtime_bridge_live_models({
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": "claude-code-bridge-gpt-5.5",
    }) == ()


def test_active_runtime_bridge_live_models_requires_catalog_metadata_not_model_version_hardcoding():
    assert cli._active_runtime_bridge_live_models({
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
    }) == ()


def test_active_runtime_bridge_live_models_defaults_to_openai_and_deepseek_scope():
    assert cli._active_runtime_bridge_live_models({
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": [
            "claude-code-bridge-gpt-5.5",
            "claude-code-bridge-deepseek-v4-pro",
            "claude-code-bridge-glm-5.2-1m",
            "claude-code-bridge-kimi-k2.7-code",
            "claude-opus-4-8",
            "unsafe-bridge",
            "claude-code-bridge-gpt-5.5",
        ],
        "live_bridge_model_catalog": {
            "claude-code-bridge-gpt-5.5": {
                "route": "openai_bridge",
                "client_type": "claude_code_bridge_openai",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-deepseek-v4-pro": {
                "route": "deepseek_bridge",
                "client_type": "claude_code_bridge_deepseek",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-glm-5.2-1m": {
                "route": "zai_glm_bridge",
                "client_type": "claude_code_bridge_zai_glm",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-kimi-k2.7-code": {
                "route": "kimi_bridge",
                "client_type": "claude_code_bridge_kimi",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-opus-4-8": {
                "route": "claude_code_native",
                "client_type": "claude_code_native",
                "live_enabled": True,
                "formal_pool_eligible": True,
            },
            "unsafe-bridge": {
                "route": "openai_bridge",
                "client_type": "claude_code_native",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
        },
    }) == ("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro")


def test_active_runtime_bridge_live_models_requires_strict_live_evidence_for_conditional_agnes():
    patches = {
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": ["claude-code-bridge-agnes-2.0-flash"],
        "live_bridge_model_catalog": {
            "claude-code-bridge-agnes-2.0-flash": {
                "route": "agnes_bridge",
                "client_type": "claude_code_bridge_agnes",
                "live_enabled": True,
                "formal_pool_eligible": False,
                "provider": "agnes",
            },
        },
    }
    assert cli._active_runtime_bridge_live_models(patches) == ()
    patches["bridge_provider_release_statuses"] = {"agnes": "strict-live-pass"}
    assert cli._active_runtime_bridge_live_models(patches) == ("claude-code-bridge-agnes-2.0-flash",)


def test_active_runtime_bridge_live_models_requires_expanded_scope_for_glm_and_kimi():
    patches = {
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": [
            "claude-code-bridge-glm-5.2-1m",
            "claude-code-bridge-kimi-k2.7-code",
        ],
        "bridge_provider_release_statuses": {
            "zai_glm": "strict-live-pass",
            "kimi": "strict-live-pass",
        },
        "live_bridge_model_catalog": {
            "claude-code-bridge-glm-5.2-1m": {
                "route": "zai_glm_bridge",
                "client_type": "claude_code_bridge_zai_glm",
                "live_enabled": True,
                "formal_pool_eligible": False,
                "provider": "zai_glm",
            },
            "claude-code-bridge-kimi-k2.7-code": {
                "route": "kimi_bridge",
                "client_type": "claude_code_bridge_kimi",
                "live_enabled": True,
                "formal_pool_eligible": False,
                "provider": "kimi",
            },
        },
    }
    assert cli._active_runtime_bridge_live_models(patches) == ()
    patches["bridge_live_expanded_providers"] = ["zai_glm", "kimi"]
    assert cli._active_runtime_bridge_live_models(patches) == ()
    patches["bridge_runtime_account_providers"] = ["zai_glm", "kimi"]
    assert cli._active_runtime_bridge_live_models(patches) == (
        "claude-code-bridge-glm-5.2-1m",
        "claude-code-bridge-kimi-k2.7-code",
    )


def test_active_runtime_bridge_live_models_rejects_legacy_client_type_as_route():
    assert cli._active_runtime_bridge_live_models({
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
        "live_bridge_model_catalog": {
            "claude-code-bridge-gpt-5.5": {
                "route": "claude_code_bridge_openai",
                "client_type": "claude_code_bridge_openai",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-deepseek-v4-pro": {
                "route": "deepseek_bridge",
                "client_type": "claude_code_bridge_deepseek",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
        },
    }) == ("claude-code-bridge-deepseek-v4-pro",)


def test_claude_code_start_applies_agent_schema_patch_before_launch(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def update(self, patch):
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    managed_executable, _, _ = write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b'k.enum(["sonnet","opus","haiku","fable"]).optional()',
    )
    captured = {}

    def fake_run_managed_claude_code(**kwargs):
        captured["executable_bytes_at_launch"] = Path(kwargs["executable"]).read_bytes()
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(command=[str(kwargs["executable"])], env={"ANTHROPIC_BASE_URL": "http://127.0.0.1:43117", "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117"}, cwd=kwargs["project_cwd"]),
            guard_plan=SimpleNamespace(command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"], config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117)),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["runtime"]["agent_model_schema_patch"]["status"] == "patched"
    assert b"k.enum" not in captured["executable_bytes_at_launch"]
    assert b"k.string().min(1).max(128).optional()" in captured["executable_bytes_at_launch"]
    assert Path(data["runtime"]["executable"]) == managed_executable


def test_claude_code_start_fails_closed_when_bridge_effort_ui_patch_missing(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def update(self, patch):
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b'k.enum(["sonnet","opus","haiku","fable"]).optional()',
        upstream_version="2.1.177",
        patches={
            "live_bridge_models_enabled": True,
            "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
            "live_bridge_model_catalog": {
                "claude-code-bridge-gpt-5.5": {
                    "route": "openai_bridge",
                    "client_type": "claude_code_bridge_openai",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
                "claude-code-bridge-deepseek-v4-pro": {
                    "route": "deepseek_bridge",
                    "client_type": "claude_code_bridge_deepseek",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
            },
        },
    )
    launched = False

    def fake_run_managed_claude_code(**kwargs):
        nonlocal launched
        launched = True
        raise AssertionError("start must fail closed before launching unpatched /model effort UI")

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
    ])

    data = parse_output(capsys)
    assert exit_code == 1
    assert data["status"] == "not_configured"
    assert "effort capability patch" in data["message"]
    assert launched is False


def test_claude_code_start_version_bypasses_bridge_effort_ui_patch_gate(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_native_access_token": "eyJ.native-token-for-version-only",
                "claude_code_native_managed_session_id": "native-session",
                "claude_code_native_device_id": 9,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def update(self, patch):
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b'k.enum(["sonnet","opus","haiku","fable"]).optional()',
        upstream_version="2.1.177",
        patches={
            "live_bridge_models_enabled": True,
            "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
            "live_bridge_model_catalog": {
                "claude-code-bridge-gpt-5.5": {
                    "route": "openai_bridge",
                    "client_type": "claude_code_bridge_openai",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
                "claude-code-bridge-deepseek-v4-pro": {
                    "route": "deepseek_bridge",
                    "client_type": "claude_code_bridge_deepseek",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
            },
        },
    )
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(env={"ANTHROPIC_BASE_URL": "http://127.0.0.1:43117", "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117"}, cwd=kwargs["project_cwd"]),
            guard_plan=SimpleNamespace(command=["--native-attestation", "--route-hint-secret-env"], config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117)),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    data = parse_output(capsys)
    assert exit_code == 0
    assert data["status"] == "exited"
    assert calls[0]["argv"] == ["--version"]
    assert calls[0]["bridge_live_models"] == ("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro")
    dumped = json.dumps(data)
    assert "native-token-for-version-only" not in dumped


def test_claude_code_runtime_effort_patch_requires_explicit_approval_and_updates_runtime(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtimes"
    managed_executable, _, _ = write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b"prefix " + EFFORT_CAPABILITY_HOOK_NEEDLE + b" suffix",
        upstream_version="2.1.177",
    )

    denied = main([
        "claude-code",
        "runtime-patch",
        "--runtime-root",
        str(runtime_root),
        "effort-capability",
    ])
    denied_payload = parse_output(capsys)
    assert denied == 1
    assert denied_payload["status"] == "not_configured"
    assert "explicit approval" in denied_payload["message"]
    assert EFFORT_CAPABILITY_HOOK_NEEDLE in managed_executable.read_bytes()

    approved = main([
        "claude-code",
        "runtime-patch",
        "--runtime-root",
        str(runtime_root),
        "--approve-managed-binary-patch",
        "effort-capability",
    ])

    data = parse_output(capsys)
    assert approved == 0
    assert data["command"] == "claude-code runtime-patch effort-capability"
    assert data["status"] == "patched"
    assert data["patch_point"] == "effort_capability_hook"
    assert EFFORT_CAPABILITY_HOOK_NEEDLE not in managed_executable.read_bytes()
    assert EFFORT_CAPABILITY_HOOK_REPLACEMENT.rstrip() in managed_executable.read_bytes()


def test_claude_code_effort_ui_patch_gate_scope():
    assert cli._bridge_effort_ui_models((
        "claude-code-bridge-agnes-2.0-flash",
        "claude-code-bridge-kimi-k2.7-code",
        "claude-code-bridge-glm-5.2-1m",
    )) == ("claude-code-bridge-glm-5.2-1m",)

    older_runtime = SimpleNamespace(
        upstream_version="2.1.175",
        manifest={},
        patches={},
    )
    cli._require_effort_capability_patch_for_bridge_ui(older_runtime, ("claude-code-bridge-gpt-5.5",))

    unpatched_runtime = SimpleNamespace(
        upstream_version="2.1.177",
        manifest={"patch_points": ["runtime_manifest"]},
        patches={"patch_points": ["runtime_manifest"]},
    )
    with pytest.raises(cli.RuntimeInstallerError, match="effort capability patch"):
        cli._require_effort_capability_patch_for_bridge_ui(unpatched_runtime, ("claude-code-bridge-glm-5.2-1m",))

    boolean_only_runtime = SimpleNamespace(
        upstream_version="2.1.177",
        runtime_hash="sha256:" + "1" * 64,
        manifest={"patch_points": ["runtime_manifest", "effort_capability_hook"]},
        patches={
            "patch_points": ["runtime_manifest", "effort_capability_hook"],
            "effort_capability_patch": {"env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON"},
        },
    )
    with pytest.raises(cli.RuntimeInstallerError, match="exact effort levels"):
        cli._require_effort_capability_patch_for_bridge_ui(boolean_only_runtime, ("claude-code-bridge-deepseek-v4-pro",))

    boolean_patch_with_probe_metadata = SimpleNamespace(
        upstream_version="2.1.177",
        runtime_hash="sha256:" + "1" * 64,
        manifest={"patch_points": ["runtime_manifest", "effort_capability_hook"]},
        patches={
            "patch_points": ["runtime_manifest", "effort_capability_hook"],
            "effort_capability_patch": {
                "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
                **build_bridge_effort_ui_probe_metadata(
                    capabilities_json=_bridge_model_capabilities_json(("claude-code-bridge-deepseek-v4-pro",)),
                    bridge_live_models=("claude-code-bridge-deepseek-v4-pro",),
                    runtime_hash="sha256:" + "1" * 64,
                ),
            },
        },
    )
    with pytest.raises(cli.RuntimeInstallerError, match="exact effort levels"):
        cli._require_effort_capability_patch_for_bridge_ui(boolean_patch_with_probe_metadata, ("claude-code-bridge-deepseek-v4-pro",))

    forged_exact_runtime = SimpleNamespace(
        upstream_version="2.1.177",
        runtime_hash="sha256:" + "1" * 64,
        manifest={"patch_points": ["runtime_manifest", "effort_capability_hook", "exact_effort_level_ui_patch"]},
        patches={
            "patch_points": ["runtime_manifest", "effort_capability_hook", "exact_effort_level_ui_patch"],
            "effort_capability_patch": {
                "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
                **build_bridge_effort_ui_probe_metadata(
                    capabilities_json=_bridge_model_capabilities_json(("claude-code-bridge-deepseek-v4-pro",)),
                    bridge_live_models=("claude-code-bridge-deepseek-v4-pro",),
                    runtime_hash="sha256:" + "1" * 64,
                ),
                "exact_effort_levels_supported": False,
                "boolean_only_hook_rejected": "true",
            },
        },
    )
    with pytest.raises(cli.RuntimeInstallerError, match="exact effort levels"):
        cli._require_effort_capability_patch_for_bridge_ui(forged_exact_runtime, ("claude-code-bridge-deepseek-v4-pro",))

    exact_level_runtime = SimpleNamespace(
        upstream_version="2.1.177",
        runtime_hash="sha256:" + "1" * 64,
        manifest={"patch_points": ["runtime_manifest", "effort_capability_hook", "exact_effort_level_ui_patch"]},
        patches={
            "patch_points": ["runtime_manifest", "effort_capability_hook", "exact_effort_level_ui_patch"],
            "effort_capability_patch": {
                "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
                **build_bridge_effort_ui_probe_metadata(
                    capabilities_json=_bridge_model_capabilities_json(("claude-code-bridge-deepseek-v4-pro",)),
                    bridge_live_models=("claude-code-bridge-deepseek-v4-pro",),
                    runtime_hash="sha256:" + "1" * 64,
                ),
            },
        },
    )
    cli._require_effort_capability_patch_for_bridge_ui(exact_level_runtime, ("claude-code-bridge-deepseek-v4-pro",))


def test_claude_code_start_allows_bridge_models_after_exact_effort_ui_patch(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def update(self, patch):
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    payload = b"exact-effort-ui-patched k.string().min(1).max(128).optional()                suffix"
    runtime_hash = "sha256:" + hashlib.sha256(payload).hexdigest()
    exact_patch_points = [
        "runtime_manifest",
        "hash_lock",
        "isolated_config",
        "guard_env",
        "effort_capability_hook",
        "exact_effort_level_ui_patch",
    ]
    managed_executable, _, _ = write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=payload,
        upstream_version="2.1.177",
        patches={
            "patch_points": exact_patch_points,
            "effort_capability_patch": {
                "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
                **build_bridge_effort_ui_probe_metadata(
                    capabilities_json=_bridge_model_capabilities_json((
                        "claude-code-bridge-gpt-5.5",
                        "claude-code-bridge-deepseek-v4-pro",
                    )),
                    bridge_live_models=("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"),
                    runtime_hash=runtime_hash,
                ),
            },
            "live_bridge_models_enabled": True,
            "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
            "live_bridge_model_catalog": {
                "claude-code-bridge-gpt-5.5": {
                    "route": "openai_bridge",
                    "client_type": "claude_code_bridge_openai",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
                "claude-code-bridge-deepseek-v4-pro": {
                    "route": "deepseek_bridge",
                    "client_type": "claude_code_bridge_deepseek",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
            },
        },
    )
    manifest_path = runtime_root / "claude-code" / "2.1.177" / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest["patch_points"] = exact_patch_points
    manifest_path.write_text(json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    hash_lock_path = runtime_root / "claude-code" / "2.1.177" / "hash.lock"
    hash_lock = json.loads(hash_lock_path.read_text(encoding="utf-8"))
    manifest_hash = "sha256:" + hashlib.sha256(manifest_path.read_bytes()).hexdigest()
    hash_lock["manifest_hash"] = manifest_hash
    hash_lock["locked_files"]["manifest.json"] = manifest_hash
    hash_lock_path.write_text(json.dumps(hash_lock, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")

    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                command=[str(kwargs["executable"])],
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                    "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON": "bridge-capabilities-json",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
    ])

    data = parse_output(capsys)
    assert exit_code == 0
    assert calls
    assert calls[0]["bridge_live_models"] == ("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro")
    assert Path(data["runtime"]["executable"]) == managed_executable
    assert data["runtime"]["version"] == "2.1.177"


def test_claude_code_start_real_path_starts_loopback_guard(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def update(self, patch):
            self.patch = patch
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    managed_executable, runtime_hash, overlay_hash = write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b"k.string().min(1).max(128).optional()               ",
        patches={
            "live_bridge_models_enabled": True,
            "live_bridge_model_allowlist": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro", "claude-code-bridge-agnes-2.0-flash"],
            "live_bridge_model_catalog": {
                "claude-code-bridge-gpt-5.5": {
                    "route": "openai_bridge",
                    "client_type": "claude_code_bridge_openai",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
                "claude-code-bridge-deepseek-v4-pro": {
                    "route": "deepseek_bridge",
                    "client_type": "claude_code_bridge_deepseek",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                },
            },
        },
    )

    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                command=[str(kwargs["executable"])],
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.generate_loopback_secret = lambda: "attestation-secret"
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--print",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "claude-code start"
    assert data["status"] == "exited"
    assert data["guard"]["listen"] == "http://127.0.0.1:43117"
    assert data["guard"]["route_hint_contract"] is True
    assert data["claude_base_url"] == "http://127.0.0.1:43117"
    assert calls[0]["executable"] == managed_executable
    assert calls[0]["runtime_hash"] == runtime_hash
    assert calls[0]["overlay_hash"] == overlay_hash
    assert data["runtime"]["version"] == "2.1.175"
    assert data["runtime"]["runtime_hash"] == runtime_hash
    assert calls[0]["bridge_live_models"] == ("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro")
    assert data["runtime"]["bridge_live_models"] == ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"]
    assert calls[0]["upstream_base"] == "http://127.0.0.1:18080"
    assert calls[0]["sub2api_auth"] == "test-zhumeng-claude-code-cli-key"
    assert calls[0]["managed_session_id"] == "managed-session"
    assert calls[0]["device_id"] == 9
    assert calls[0]["attestation_secret"] == "server-native-attestation-secret"
    assert calls[0]["route_hint_secret"] == "server-route-hint-secret"
    assert calls[0]["argv"] == ["--print"]
    assert calls[0]["guard_listen_port"] == 43117
    assert calls[0]["capability_profile"].profile_id == "native-prod"
    assert calls[0]["capability_profile"].tool_search_mode == "true"
    assert calls[0]["capability_profile"].tool_search_healthcheck_passed is False
    assert calls[0]["toolsearch_doctor_context"] is None
    dumped = json.dumps(data)
    assert "test-zhumeng-claude-code-cli-key" not in dumped
    assert "attestation-secret" not in dumped


def test_claude_code_start_passes_capability_profile_and_toolsearch_doctor(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
                "claude_code_capability_profile": {
                    "profile_id": "native-prod",
                    "claude_code_version_family": "2.1.x",
                    "persona_profile_id": "claude-code-native-prod",
                    "tool_search_mode": "true",
                    "control_plane_policy_version": "cp-v1",
                    "server_shape_healthcheck_version": "shape-v1",
                    "tool_search_healthcheck_passed": True,
                },
                "claude_code_toolsearch_doctor_context": {
                    "model": "claude-sonnet-4-6",
                    "has_mcp_deferred_tools": True,
                    "has_pending_mcp_server": False,
                    "disallowed_tools": [],
                    "model_supports_tool_reference": True,
                },
            }

        def update(self, patch):
            return {**self.read(), **patch}

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(
        runtime_root,
        tmp_path / "managed-runtime" / "claude",
        payload=b'k.enum(["sonnet","opus","haiku","fable"]).optional()',
    )
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                    "ZHUMENG_CLAUDE_TOOLSEARCH_STATUS_PATH": str(tmp_path / "toolsearch-status.json"),
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    capability = calls[0]["capability_profile"]
    doctor = calls[0]["toolsearch_doctor_context"]
    assert capability.profile_id == "native-prod"
    assert capability.tool_search_mode == "true"
    assert capability.tool_search_healthcheck_passed is True
    assert capability.control_plane_policy_version == "cp-v1"
    assert capability.server_shape_healthcheck_version == "shape-v1"
    assert doctor.model == "claude-sonnet-4-6"
    assert doctor.claude_code_version == data["runtime"]["version"]
    assert doctor.has_mcp_deferred_tools is True
    assert doctor.has_pending_mcp_server is False
    assert calls[0]["capability_profile"].kill_switches == ()
    assert data["toolsearch"]["status_path"].endswith("toolsearch-status.json")
    assert "test-zhumeng-claude-code-cli-key" not in json.dumps(data)


def test_claude_code_start_prefers_dedicated_sub2api_key_over_agent_jwt(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3013",
                "gateway_base_url": "http://127.0.0.1:3013",
                "access_token": "eyJ.agent-login-jwt",
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["sub2api_auth"] == "test-zhumeng-claude-code-cli-key"
    assert calls[0]["sub2api_auth"] != "eyJ.agent-login-jwt"
    assert "test-zhumeng-claude-code-cli-key" not in json.dumps(data)


def test_claude_code_start_uses_native_managed_credentials_separate_from_codex_gateway(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "access_token": "eyJ.codex-gateway-managed-token",
                "refresh_token": "codex-refresh-token",
                "managed_session_id": "codex-managed-session",
                "device_id": 31,
                "claude_code_native_access_token": "eyJ.claude-code-native-token",
                "claude_code_native_refresh_token": "claude-code-refresh-token",
                "claude_code_native_managed_session_id": "claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["sub2api_auth"] == "test-zhumeng-claude-code-cli-key"
    assert calls[0]["native_managed_access_token"] == "eyJ.claude-code-native-token"
    assert calls[0]["managed_session_id"] == "claude-code-session"
    assert calls[0]["device_id"] == 32
    assert calls[0]["native_managed_access_token"] != "eyJ.codex-gateway-managed-token"
    assert calls[0]["managed_session_id"] != "codex-managed-session"
    assert calls[0]["device_id"] != 31
    assert "eyJ.claude-code-native-token" not in json.dumps(data)


def test_claude_code_start_reads_state_root_state_json_without_env_override(capsys, tmp_path: Path, monkeypatch):
    state_root = tmp_path / "canary-state-root"
    runtime_root = tmp_path / "runtime-root"
    state_root.mkdir()
    runtime_root.mkdir()
    (state_root / "state.json").write_text(json.dumps({
        "client": "claude_code_native",
        "server_base_url": "http://127.0.0.1:3017",
        "gateway_base_url": "http://127.0.0.1:3017",
        "managed_session_id": "session-from-state-root",
        "device_id": 42,
        "claude_code_native_access_token": "eyJ.state-root-native-token",
        "claude_code_native_refresh_token": "state-root-refresh-token",
        "claude_code_native_managed_session_id": "session-from-state-root",
        "claude_code_native_device_id": 42,
        "claude_code_sub2api_api_key": "sk-state-root-claude-code-cli",
        "claude_code_native_attestation_secret": "server-native-attestation-secret",
        "claude_code_native_attestation_secret_source": "server",
        "claude_code_route_hint_secret": "server-route-hint-secret",
        "claude_code_route_hint_secret_source": "server",
    }), encoding="utf-8")
    calls = []

    class FakeRuntime:
        executable = tmp_path / "runtime" / "claude"
        upstream_version = "2.1.177"
        manifest_path = tmp_path / "runtime" / "manifest.json"
        runtime_hash = "sha256:" + "1" * 64
        overlay_hash = "sha256:" + "2" * 64
        manifest = {
            "patch_points": ["runtime_manifest", "hash_lock", "isolated_config", "guard_env", "effort_capability_hook", "exact_effort_level_ui_patch"],
        }
        patches = {
            "patch_points": ["runtime_manifest", "hash_lock", "isolated_config", "guard_env", "effort_capability_hook", "exact_effort_level_ui_patch"],
            "effort_capability_patch": {
                "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
                **build_bridge_effort_ui_probe_metadata(
                    capabilities_json=_bridge_model_capabilities_json(("claude-code-bridge-deepseek-v4-pro",)),
                    bridge_live_models=("claude-code-bridge-deepseek-v4-pro",),
                    runtime_hash="sha256:" + "1" * 64,
                ),
            },
            "live_bridge_models_enabled": True,
            "live_bridge_model_allowlist": ["claude-code-bridge-deepseek-v4-pro"],
            "live_bridge_model_catalog": {
                "claude-code-bridge-deepseek-v4-pro": {
                    "route": "deepseek_bridge",
                    "client_type": "claude_code_bridge_deepseek",
                    "live_enabled": True,
                    "formal_pool_eligible": False,
                }
            },
        }

    FakeRuntime.executable.parent.mkdir(parents=True)
    FakeRuntime.executable.write_text("#!/bin/sh\n", encoding="utf-8")
    FakeRuntime.manifest_path.write_text("{}", encoding="utf-8")

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:18181"},
            guard_plan=SimpleNamespace(
                command=["python", "cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl"),
            ),
            launch_plan=SimpleNamespace(env={
                "ANTHROPIC_BASE_URL": "http://127.0.0.1:18181",
                "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:18181",
            }),
        )

    monkeypatch.delenv("ZHUMENG_AGENT_STATE_PATH", raising=False)
    monkeypatch.setattr(cli, "state_dir", lambda app_name="zhumeng-agent": tmp_path / "default-state", raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root_arg: FakeRuntime, raising=False)
    monkeypatch.setattr(
        cli,
        "apply_managed_runtime_agent_model_schema_patch",
        lambda runtime_root_arg, executable_arg: {"status": "already_patched", "runtime_hash_after": FakeRuntime.runtime_hash},
        raising=False,
    )
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)
    monkeypatch.setattr(cli, "choose_local_proxy_port", lambda preferred=None: 18181, raising=False)

    assert main([
        "claude-code",
        "start",
        "--state-root",
        str(state_root),
        "--runtime-root",
        str(runtime_root),
        "--project-cwd",
        str(tmp_path),
    ]) == 0
    data = parse_output(capsys)

    assert data["status"] == "exited"
    assert calls[0]["upstream_base"] == "http://127.0.0.1:3017"
    assert calls[0]["sub2api_auth"] == "sk-state-root-claude-code-cli"
    assert calls[0]["native_managed_access_token"] == "eyJ.state-root-native-token"


def test_claude_code_start_reads_env_state_path_for_canary_state(capsys, tmp_path: Path, monkeypatch):
    state_path = tmp_path / "canary-state" / "state.json"
    state_path.parent.mkdir()
    state_path.write_text(
        json.dumps(
            {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "access_token": "eyJ.global-codex-token",
                "managed_session_id": "global-codex-session",
                "device_id": 31,
                "claude_code_native_access_token": "eyJ.canary-claude-code-token",
                "claude_code_native_refresh_token": "canary-refresh-token",
                "claude_code_native_managed_session_id": "canary-claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "sk-canary-claude-code-cli",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }
        ),
        encoding="utf-8",
    )
    monkeypatch.setenv("ZHUMENG_AGENT_STATE_PATH", str(state_path))
    runtime_root = tmp_path / "runtimes"
    managed_executable = tmp_path / "managed-runtime" / "claude"
    write_fake_claude_runtime(runtime_root, managed_executable)
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main(
        [
            "claude-code",
            "start",
            "--runtime-root",
            str(runtime_root),
            "--state-root",
            str(tmp_path / "canary-state"),
            "--project-cwd",
            str(tmp_path),
            "--",
            "--version",
        ]
    )

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["upstream_base"] == "http://127.0.0.1:3017"
    assert calls[0]["sub2api_auth"] == "sk-canary-claude-code-cli"
    assert calls[0]["native_managed_access_token"] == "eyJ.canary-claude-code-token"
    assert calls[0]["managed_session_id"] == "canary-claude-code-session"
    assert calls[0]["device_id"] == 32
    dumped = json.dumps(data)
    assert "sk-canary-claude-code-cli" not in dumped
    assert "eyJ.canary-claude-code-token" not in dumped


def test_claude_code_start_refreshes_expired_native_managed_credentials(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def __init__(self):
            self.state = {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "access_token": "eyJ.codex-gateway-managed-token",
                "refresh_token": "codex-refresh-token",
                "managed_session_id": "codex-managed-session",
                "device_id": 31,
                "claude_code_native_access_token": "eyJ.expired-claude-code-native-token",
                "claude_code_native_refresh_token": "claude-code-refresh-token",
                "claude_code_native_managed_session_id": "expired-claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_sub2api_api_key_configured": True,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }
            self.patches = []

        def read(self):
            return dict(self.state)

        def update(self, patch):
            self.patches.append(dict(patch))
            self.state.update(patch)
            return dict(self.state)

    class FakeClient:
        def __init__(self):
            self.refresh_calls = []

        def refresh_device_token(self, **kwargs):
            self.refresh_calls.append(kwargs)
            return {
                "access_token": "eyJ.refreshed-claude-code-native-token",
                "refresh_token": "next-claude-code-refresh-token",
                "managed_session_id": "refreshed-claude-code-session",
                "expires_at": "2026-06-18T13:00:00Z",
            }

    store = FakeStore()
    fake_client = FakeClient()
    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: store
    cli.default_http_client = lambda server: fake_client
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)
    monkeypatch.setattr(cli, "_managed_jwt_seconds_until_expiry", lambda token: -30 if token == "eyJ.expired-claude-code-native-token" else 3600)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert fake_client.refresh_calls == [{"device_id": 32, "refresh_token": "claude-code-refresh-token"}]
    assert calls[0]["native_managed_access_token"] == "eyJ.refreshed-claude-code-native-token"
    assert calls[0]["managed_session_id"] == "refreshed-claude-code-session"
    assert calls[0]["device_id"] == 32
    assert store.patches[-1]["claude_code_native_refresh_token"] == "next-claude-code-refresh-token"
    dumped = json.dumps(data)
    assert "eyJ.refreshed-claude-code-native-token" not in dumped
    assert "next-claude-code-refresh-token" not in dumped


def test_claude_code_start_version_tolerates_native_refresh_endpoint_unavailable(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def __init__(self):
            self.state = {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "access_token": "eyJ.codex-gateway-managed-token",
                "refresh_token": "codex-refresh-token",
                "managed_session_id": "codex-managed-session",
                "device_id": 31,
                "claude_code_native_access_token": "eyJ.expired-claude-code-native-token",
                "claude_code_native_refresh_token": "claude-code-refresh-token",
                "claude_code_native_managed_session_id": "expired-claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }
            self.patches = []

        def read(self):
            return dict(self.state)

        def update(self, patch):
            self.patches.append(dict(patch))
            self.state.update(patch)
            return dict(self.state)

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    store = FakeStore()
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(env={"ANTHROPIC_BASE_URL": "http://127.0.0.1:43117", "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117"}, cwd=kwargs["project_cwd"]),
            guard_plan=SimpleNamespace(command=["--native-attestation", "--route-hint-secret-env"], config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117)),
        )

    cli.default_state_store = lambda: store
    cli.default_http_client = lambda server: (_ for _ in ()).throw(ConnectionError("connection refused"))
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)
    monkeypatch.setattr(cli, "_managed_jwt_seconds_until_expiry", lambda token: -30 if token == "eyJ.expired-claude-code-native-token" else 3600)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["native_managed_access_token"] == "eyJ.expired-claude-code-native-token"
    assert calls[0]["managed_session_id"] == "expired-claude-code-session"
    assert store.patches == []
    dumped = json.dumps(data)
    assert "claude-code-refresh-token" not in dumped
    assert "eyJ.expired-claude-code-native-token" not in dumped


def test_claude_code_start_bare_version_does_not_bypass_native_refresh_failure(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "claude_code_native_access_token": "eyJ.expired-claude-code-native-token",
                "claude_code_native_refresh_token": "claude-code-refresh-token",
                "claude_code_native_managed_session_id": "expired-claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    cli.default_state_store = lambda: FakeStore()
    cli.default_http_client = lambda server: (_ for _ in ()).throw(ConnectionError("connection refused"))
    monkeypatch.setattr(cli, "_managed_jwt_seconds_until_expiry", lambda token: -30 if token == "eyJ.expired-claude-code-native-token" else 3600)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "version",
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "native token refresh failed" in data["message"]
    assert "claude-code-refresh-token" not in json.dumps(data)


def test_claude_code_start_real_session_fails_closed_when_native_refresh_unavailable(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "claude_code_native_access_token": "eyJ.expired-claude-code-native-token",
                "claude_code_native_refresh_token": "claude-code-refresh-token",
                "claude_code_native_managed_session_id": "expired-claude-code-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    cli.default_state_store = lambda: FakeStore()
    cli.default_http_client = lambda server: (_ for _ in ()).throw(ConnectionError("connection refused"))
    monkeypatch.setattr(cli, "_managed_jwt_seconds_until_expiry", lambda token: -30 if token == "eyJ.expired-claude-code-native-token" else 3600)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--print",
        "hello",
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "native token refresh failed" in data["message"]
    assert "claude-code-refresh-token" not in json.dumps(data)


def test_claude_code_start_reads_dedicated_sub2api_key_from_state_root_env_file(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3013",
                "gateway_base_url": "http://127.0.0.1:3013",
                "access_token": "eyJ.agent-login-jwt",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    state_root = tmp_path / "zhumeng-state"
    state_root.mkdir()
    (state_root / "claude-code-sub2api.env").write_text("SUB2API_API_KEY=sk-env-claude-code-cli\n", encoding="utf-8")
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setenv("SUB2API_API_KEY", "sk-global-should-not-win")
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(state_root),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["sub2api_auth"] == "sk-env-claude-code-cli"
    assert calls[0]["sub2api_auth"] != "eyJ.agent-login-jwt"
    assert "sk-env-claude-code-cli" not in json.dumps(data)


def test_claude_code_start_does_not_require_generic_codex_token_when_native_credentials_exist(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3017",
                "gateway_base_url": "http://127.0.0.1:3017",
                "claude_code_native_access_token": "eyJ.native-only-token",
                "claude_code_native_refresh_token": "native-refresh-token",
                "claude_code_native_managed_session_id": "native-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "exited"
    assert calls[0]["sub2api_auth"] == "test-zhumeng-claude-code-cli-key"
    assert calls[0]["native_managed_access_token"] == "eyJ.native-only-token"
    assert calls[0]["managed_session_id"] == "native-session"
    assert calls[0]["device_id"] == 32
    assert "eyJ.native-only-token" not in json.dumps(data)


def test_claude_code_native_refresh_uses_gateway_base_url_when_server_base_url_missing(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def __init__(self):
            self.state = {
                "status": "configured",
                "client": "claude_code_native",
                "gateway_base_url": "http://127.0.0.1:3017",
                "claude_code_native_access_token": "eyJ.expired-native-token",
                "claude_code_native_refresh_token": "native-refresh-token",
                "claude_code_native_managed_session_id": "expired-native-session",
                "claude_code_native_device_id": 32,
                "claude_code_sub2api_api_key": "test-zhumeng-claude-code-cli-key",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

        def read(self):
            return dict(self.state)

        def update(self, patch):
            self.state.update(patch)
            return dict(self.state)

    class FakeClient:
        def __init__(self):
            self.server = None
            self.refresh_calls = []

        def refresh_device_token(self, **kwargs):
            self.refresh_calls.append(kwargs)
            return {
                "access_token": "eyJ.refreshed-native-token",
                "refresh_token": "next-native-refresh",
                "managed_session_id": "refreshed-native-session",
            }

    store = FakeStore()
    client = FakeClient()
    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_default_http_client(server):
        client.server = server
        return client

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(env={"ANTHROPIC_BASE_URL": "http://127.0.0.1:43117", "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117"}, cwd=kwargs["project_cwd"]),
            guard_plan=SimpleNamespace(command=["--native-attestation", "--route-hint-secret-env"], config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117)),
        )

    cli.default_state_store = lambda: store
    cli.default_http_client = fake_default_http_client
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)
    monkeypatch.setattr(cli, "_managed_jwt_seconds_until_expiry", lambda token: -1 if token == "eyJ.expired-native-token" else 3600)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 0
    assert client.server == "http://127.0.0.1:3017"
    assert client.refresh_calls == [{"device_id": 32, "refresh_token": "native-refresh-token"}]
    assert calls[0]["native_managed_access_token"] == "eyJ.refreshed-native-token"


def test_claude_code_start_rejects_env_jwt_and_official_anthropic_fallback(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3013",
                "gateway_base_url": "http://127.0.0.1:3013",
                "access_token": "eyJ.agent-login-jwt",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []
    cli.default_state_store = lambda: FakeStore()
    monkeypatch.setenv("SUB2API_API_KEY", "eyJ.env-jwt-should-not-be-used")
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-official-should-not-be-used")
    monkeypatch.setattr(cli, "run_managed_claude_code", lambda **kwargs: calls.append(kwargs), raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "claude_code_sub2api_api_key" in data["message"]
    assert calls == []
    dumped = json.dumps(data)
    assert "env-jwt" not in dumped
    assert "official" not in dumped


def test_claude_code_start_rejects_legacy_access_token_as_sub2api_key(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "http://127.0.0.1:3013",
                "gateway_base_url": "http://127.0.0.1:3013",
                "access_token": "legacy-sub2api-entry-secret",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []

    def fake_run_managed_claude_code(**kwargs):
        calls.append(kwargs)
        return SimpleNamespace(
            returncode=0,
            guard_ready={"listen": "http://127.0.0.1:43117"},
            launch_plan=SimpleNamespace(
                env={
                    "ANTHROPIC_BASE_URL": "http://127.0.0.1:43117",
                    "CLAUDE_CODE_API_BASE_URL": "http://127.0.0.1:43117",
                },
                cwd=kwargs["project_cwd"],
            ),
            guard_plan=SimpleNamespace(
                command=["python", "tools/cli_control_plane_guard.py", "--native-attestation", "--route-hint-secret-env"],
                config=SimpleNamespace(summary_path=tmp_path / "summary.jsonl", listen_port=43117),
            ),
        )

    cli.default_state_store = lambda: FakeStore()
    cli.choose_local_proxy_port = lambda preferred=None: 43117
    monkeypatch.setattr(cli, "run_managed_claude_code", fake_run_managed_claude_code, raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--",
        "--version",
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "claude_code_sub2api_api_key" in data["message"]
    assert calls == []
    assert "legacy-sub2api-entry-secret" not in json.dumps(data)


def test_claude_code_start_rejects_explicit_executable_drift(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "sub2api-entry-secret",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "server-route-hint-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []
    cli.default_state_store = lambda: FakeStore()
    monkeypatch.setattr(cli, "run_managed_claude_code", lambda **kwargs: calls.append(kwargs), raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
        "--executable",
        str(tmp_path / "other-runtime" / "claude"),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "executable drift" in data["message"]
    assert calls == []


def test_claude_code_start_fails_closed_without_server_route_hint_secret(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "sub2api-entry-secret",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
                "claude_code_native_attestation_secret": "server-native-attestation-secret",
                "claude_code_native_attestation_secret_source": "server",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []
    cli.default_state_store = lambda: FakeStore()
    monkeypatch.setattr(cli, "run_managed_claude_code", lambda **kwargs: calls.append(kwargs), raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "claude_code_route_hint_secret" in data["message"]
    assert calls == []



def test_claude_code_start_fails_closed_without_server_native_attestation_secret(capsys, tmp_path: Path, monkeypatch):
    class FakeStore:
        def read(self):
            return {
                "status": "configured",
                "client": "claude_code_native",
                "server_base_url": "https://example.com",
                "gateway_base_url": "http://127.0.0.1:18080",
                "access_token": "sub2api-entry-secret",
                "managed_session_id": "managed-session",
                "device_id": 9,
                "config_profile": {"model_provider": "zhumeng-claude"},
                "proxy_port": 18081,
                "loopback_secret": "loopback-secret",
            }

    runtime_root = tmp_path / "runtimes"
    write_fake_claude_runtime(runtime_root, tmp_path / "managed-runtime" / "claude")
    calls = []
    cli.default_state_store = lambda: FakeStore()
    cli.generate_loopback_secret = lambda: "must-not-be-used"
    monkeypatch.setattr(cli, "run_managed_claude_code", lambda **kwargs: calls.append(kwargs), raising=False)

    exit_code = main([
        "claude-code",
        "start",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(tmp_path / "zhumeng-state"),
        "--project-cwd",
        str(tmp_path),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "not_configured"
    assert "claude_code_native_attestation_secret" in data["message"]
    assert calls == []


def test_launch_claude_code_process_detaches_stdio_for_fire_and_forget(monkeypatch, tmp_path: Path):
    calls = []

    def fake_popen(command, **kwargs):
        calls.append((command, kwargs))
        return SimpleNamespace(pid=6262)

    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)

    process = cli.launch_claude_code_process(
        ["python", "-m", "zhumeng_agent", "claude-code", "start"],
        env={"NO_PROXY": "127.0.0.1"},
        cwd=tmp_path,
        detach_stdio=True,
    )

    assert process.pid == 6262
    assert calls[0][0] == ["python", "-m", "zhumeng_agent", "claude-code", "start"]
    kwargs = calls[0][1]
    assert kwargs["cwd"] == str(tmp_path)
    assert kwargs["env"] == {"NO_PROXY": "127.0.0.1"}
    assert kwargs["stdin"] is cli.subprocess.DEVNULL
    assert kwargs["stdout"] is cli.subprocess.DEVNULL
    assert kwargs["stderr"] is cli.subprocess.DEVNULL
    assert kwargs["start_new_session"] is True


def test_zhumeng_claude_entrypoint_maps_to_claude_code_start(monkeypatch):
    calls = []
    monkeypatch.setattr(cli, "main", lambda argv=None: calls.append(argv) or 0)

    assert cli.zhumeng_claude_main(["--print"]) == 0
    assert cli.zhumeng_claude_main(["start", "--executable", "managed-claude", "--", "--print"]) == 0
    assert cli.zhumeng_claude_main(["status", "--runtime-root", "/tmp/runtime"]) == 0
    assert cli.zhumeng_claude_main(["restart", "--runtime-root", "/tmp/runtime"]) == 0
    assert cli.zhumeng_claude_main(["alias", "enable", "--shell-rc", "/tmp/rc"]) == 0
    assert cli.zhumeng_claude_main(["live-matrix", "--evidence", "/tmp/cp8.json"]) == 0

    assert calls == [
        ["claude-code", "start", "--", "--print"],
        ["claude-code", "start", "--executable", "managed-claude", "--", "--print"],
        ["claude-code", "status", "--runtime-root", "/tmp/runtime"],
        ["claude-code", "restart", "--runtime-root", "/tmp/runtime"],
        ["claude-code", "alias", "enable", "--shell-rc", "/tmp/rc"],
        ["claude-code", "live-matrix", "--evidence", "/tmp/cp8.json"],
    ]



def test_claude_code_preflight_writes_local_decision_freeze_without_live(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtime-root"
    state_root = tmp_path / "state-root"
    executable = tmp_path / "managed" / "claude"
    patches = {
        "runtime": "claude-code",
        "upstream_version": "2.1.177",
        "patch_points": [
            "runtime_manifest",
            "hash_lock",
            "isolated_config",
            "guard_env",
            "agent_model_schema",
            "effort_capability_hook",
            "exact_effort_level_ui_patch",
        ],
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": [
            "claude-code-bridge-gpt-5.5",
            "claude-code-bridge-deepseek-v4-pro",
        ],
        "live_bridge_model_catalog": {
            "claude-code-bridge-gpt-5.5": {
                "provider": "openai",
                "route": "openai_bridge",
                "client_type": "claude_code_bridge_openai",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-deepseek-v4-pro": {
                "provider": "deepseek",
                "route": "deepseek_bridge",
                "client_type": "claude_code_bridge_deepseek",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
        },
        "bridge_provider_release_statuses": {
            "openai": "fixture-pass-only",
            "deepseek": "fixture-pass-only",
        },
        "effort_capability_patch": {
            "status": "patched",
            "supported_models_exact": True,
            "probe_schema_version": "claude-code-2.1.177-bridge-effort-ui-probe-v1",
            "capabilities_hash": "sha256:" + "4" * 64,
        },
    }
    _, runtime_hash, overlay_hash = write_fake_claude_runtime(
        runtime_root,
        executable,
        payload=EFFORT_CAPABILITY_HOOK_REPLACEMENT,
        patches=patches,
        upstream_version="2.1.177",
    )
    patch_path = runtime_root / "claude-code" / "2.1.177" / "patches.json"
    patch_payload = json.loads(patch_path.read_text(encoding="utf-8"))
    patch_payload["patch_points"] = sorted(set(patch_payload.get("patch_points", [])) | {"effort_capability_hook", EXACT_EFFORT_LEVEL_UI_PATCH_POINT})
    patch_payload["effort_capability_patch"].update({
        "env": "ZHUMENG_CLAUDE_MODEL_CAPABILITIES_JSON",
        "schema": "exact_effort_levels_v1",
        "ui_probe": "claude_code_2_1_177_model_picker_exact_effort_levels",
        "patch_point": EXACT_EFFORT_LEVEL_UI_PATCH_POINT,
        "hook": "exact_effort_levels_ui",
        "exact_effort_levels_supported": True,
        "boolean_only_hook_rejected": False,
        "probe_status": "pass",
        "runtime_hash": runtime_hash,
        "live_bridge_models": ["claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"],
        "capabilities_hash": cli.bridge_effort_capabilities_hash(cli._bridge_model_capabilities_json(("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"))),
    })
    patch_path.write_text(json.dumps(patch_payload, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    manifest_path = runtime_root / "claude-code" / "2.1.177" / "manifest.json"
    manifest_payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest_payload["patch_points"] = sorted(set(manifest_payload.get("patch_points", [])) | {"effort_capability_hook", EXACT_EFFORT_LEVEL_UI_PATCH_POINT})
    manifest_path.write_text(json.dumps(manifest_payload, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    lock_path = runtime_root / "claude-code" / "2.1.177" / "hash.lock"
    lock_payload = json.loads(lock_path.read_text(encoding="utf-8"))
    lock_payload["manifest_hash"] = "sha256:" + hashlib.sha256(manifest_path.read_bytes()).hexdigest()
    lock_payload["locked_files"] = {
        "manifest.json": lock_payload["manifest_hash"],
        "patches.json": "sha256:" + hashlib.sha256(patch_path.read_bytes()).hexdigest(),
    }
    lock_path.write_text(json.dumps(lock_payload, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(
        json.dumps(
            {
                "gateway_base_url": "http://127.0.0.1:3017",
                "claude_code_capability_profile": {
                    "profile_id": "toolsearch-ready",
                    "claude_code_version_family": "2.1.x",
                    "tool_search_mode": "true",
                    "tool_search_healthcheck_passed": True,
                    "control_plane_policy_version": "cp-v1",
                    "server_shape_healthcheck_version": "shape-v1",
                },
                "claude_code_toolsearch_doctor_context": {
                    "model": "claude-sonnet-4-6",
                    "has_mcp_deferred_tools": True,
                    "has_pending_mcp_server": False,
                    "disallowed_tools": [],
                    "model_supports_tool_reference": True,
                },
            },
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )
    evidence = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"
    out_root = tmp_path / "artifacts"

    exit_code = main([
        "claude-code",
        "preflight",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(state_root),
        "--project-cwd",
        str(tmp_path),
        "--run-id",
        "cp18-local",
        "--evidence",
        str(evidence),
        "--output-root",
        str(out_root),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "claude-code preflight"
    assert data["status"] == "pass"
    assert data["phase"] == "local-runtime-verification"
    assert data["touches_3017"] is False
    assert data["touches_3012"] is False
    assert data["runtime"]["version"] == "2.1.177"
    assert data["runtime"]["runtime_hash"] == runtime_hash
    assert data["runtime"]["overlay_hash"] == overlay_hash
    assert data["catalog_hash"].startswith("sha256:")
    assert data["toolsearch"]["env_value"] == "true"
    assert data["effort_ui"]["probe_schema_version"] == "claude-code-2.1.177-bridge-effort-ui-probe-v1"
    assert data["cp8_local_matrix"]["status"] == "pass"
    manifest_artifact = out_root / "cp18-local" / "preflight" / "run-manifest.json"
    assert manifest_artifact.exists()
    manifest_payload = json.loads(manifest_artifact.read_text(encoding="utf-8"))
    assert manifest_payload["run_id"] == "cp18-local"
    assert manifest_payload["runtime_hash"] == runtime_hash
    assert manifest_payload["overlay_hash"] == overlay_hash
    assert manifest_payload["catalog_hash"] == data["catalog_hash"]
    assert "live-matrix" in manifest_payload["evidence_subdirs"]
    artifact = out_root / "cp18-local" / "preflight" / "decision-freeze.json"
    assert artifact.exists()
    artifact_payload = json.loads(artifact.read_text(encoding="utf-8"))
    assert artifact_payload["schema_version"] == "claude-code-l8-local-preflight-decision-freeze-v1"
    assert artifact_payload["run_id"] == "cp18-local"
    assert artifact_payload["runtime_hash"] == runtime_hash
    assert artifact_payload["overlay_hash"] == overlay_hash
    assert artifact_payload["catalog_hash"] == data["catalog_hash"]
    assert artifact_payload["provider_scope"]["l8_live_targets"] == ["claude_native", "openai", "deepseek"]
    assert artifact_payload["decisions"]["deepseek_cache_control_policy"] == "provider_ignored_audit_only"
    dumped = json.dumps(artifact_payload, sort_keys=True)
    assert "Authorization" not in dumped
    assert "raw_body" not in dumped
    assert "prompt_cache_key" not in dumped


def test_claude_code_preflight_fails_closed_when_cp8_evidence_has_sensitive_artifact(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    write_fake_claude_runtime(runtime_root, executable, upstream_version="2.1.177")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    fixture_root = tmp_path / "cp8"
    shutil.copytree(Path(__file__).parent / "fixtures" / "claude_code_cp8", fixture_root)
    artifact = fixture_root / "artifacts" / "cache_account_audit.json"
    payload = json.loads(artifact.read_text(encoding="utf-8"))
    payload["raw_body"] = "must not pass"
    artifact.write_text(json.dumps(payload, sort_keys=True), encoding="utf-8")
    matrix = json.loads((fixture_root / "live_matrix_pass.json").read_text(encoding="utf-8"))
    for scenario in matrix["scenarios"].values():
        for ref in scenario.get("artifact_refs", []):
            if ref.get("path") == "artifacts/cache_account_audit.json":
                ref["sha256"] = "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest()
    evidence = fixture_root / "live_matrix_pass.json"
    evidence.write_text(json.dumps(matrix, sort_keys=True), encoding="utf-8")

    exit_code = main([
        "claude-code",
        "preflight",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(state_root),
        "--project-cwd",
        str(tmp_path),
        "--run-id",
        "cp18-bad",
        "--evidence",
        str(evidence),
        "--output-root",
        str(tmp_path / "artifacts"),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code preflight"
    assert data["status"] == "fail"
    assert "sensitive_evidence_scan" in data["failed"]
    assert not (tmp_path / "artifacts" / "cp18-bad" / "preflight" / "decision-freeze.json").exists()


def test_claude_code_preflight_rejects_sensitive_cp8_payload_even_if_matrix_would_pass(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    write_fake_claude_runtime(runtime_root, executable, upstream_version="2.1.177")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    fixture_root = tmp_path / "cp8"
    shutil.copytree(Path(__file__).parent / "fixtures" / "claude_code_cp8", fixture_root)
    evidence = fixture_root / "live_matrix_pass.json"
    matrix = json.loads(evidence.read_text(encoding="utf-8"))
    matrix["diagnostic"] = {"authorization": "Bearer must-not-pass"}
    matrix["scenarios"]["workflow"]["raw_request"] = "must not pass"
    evidence.write_text(json.dumps(matrix, sort_keys=True), encoding="utf-8")

    exit_code = main([
        "claude-code",
        "preflight",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(state_root),
        "--project-cwd",
        str(tmp_path),
        "--run-id",
        "cp18-sensitive-payload",
        "--evidence",
        str(evidence),
        "--output-root",
        str(tmp_path / "artifacts"),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code preflight"
    assert data["status"] == "fail"
    assert "sensitive_evidence_scan" in data["failed"]
    assert not (tmp_path / "artifacts" / "cp18-sensitive-payload" / "preflight" / "decision-freeze.json").exists()


def test_claude_code_preflight_rejects_sensitive_non_cache_artifact_even_if_ref_claims_clean(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    write_fake_claude_runtime(runtime_root, executable, upstream_version="2.1.177")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    fixture_root = tmp_path / "cp8"
    shutil.copytree(Path(__file__).parent / "fixtures" / "claude_code_cp8", fixture_root)
    artifact = fixture_root / "artifacts" / "workflow_sensitive.json"
    artifact.write_text(json.dumps({"response_headers": {"authorization": "Bearer must-not-pass"}}, sort_keys=True), encoding="utf-8")
    evidence = fixture_root / "live_matrix_pass.json"
    matrix = json.loads(evidence.read_text(encoding="utf-8"))
    matrix["scenarios"]["workflow"].setdefault("artifact_refs", []).append({
        "path": "artifacts/workflow_sensitive.json",
        "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
        "sensitive_scan_clean": True,
    })
    evidence.write_text(json.dumps(matrix, sort_keys=True), encoding="utf-8")

    exit_code = main([
        "claude-code",
        "preflight",
        "--runtime-root",
        str(runtime_root),
        "--state-root",
        str(state_root),
        "--project-cwd",
        str(tmp_path),
        "--run-id",
        "cp18-sensitive-artifact",
        "--evidence",
        str(evidence),
        "--output-root",
        str(tmp_path / "artifacts"),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "fail"
    assert "sensitive_evidence_scan" in data["failed"]
    assert not (tmp_path / "artifacts" / "cp18-sensitive-artifact" / "preflight" / "decision-freeze.json").exists()


def test_claude_code_preflight_rejects_malformed_or_drifted_catalog_hash(capsys, tmp_path: Path, monkeypatch):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    write_fake_claude_runtime(runtime_root, executable, upstream_version="2.1.177")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    evidence = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"
    out_root = tmp_path / "artifacts"

    monkeypatch.setenv("ZHUMENG_CLAUDE_CATALOG_HASH", "not-a-sha")
    assert main([
        "claude-code", "preflight", "--runtime-root", str(runtime_root), "--state-root", str(state_root),
        "--project-cwd", str(tmp_path), "--run-id", "cp18-bad-catalog", "--evidence", str(evidence),
        "--output-root", str(out_root),
    ]) == 1
    malformed = parse_output(capsys)
    assert malformed["status"] == "not_configured"
    assert "catalog_hash" in malformed["message"]
    assert not (out_root / "cp18-bad-catalog" / "preflight" / "decision-freeze.json").exists()

    monkeypatch.setenv("ZHUMENG_CLAUDE_CATALOG_HASH", "sha256:" + "0" * 64)
    assert main([
        "claude-code", "preflight", "--runtime-root", str(runtime_root), "--state-root", str(state_root),
        "--project-cwd", str(tmp_path), "--run-id", "cp18-drift-catalog", "--evidence", str(evidence),
        "--output-root", str(out_root),
    ]) == 1
    drift = parse_output(capsys)
    assert drift["status"] == "not_configured"
    assert "catalog_hash" in drift["message"]
    assert not (out_root / "cp18-drift-catalog" / "preflight" / "decision-freeze.json").exists()


def test_claude_code_preflight_catalog_hash_includes_expanded_provider_scope(capsys, tmp_path: Path, monkeypatch):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    patches = {
        "live_bridge_models_enabled": True,
        "live_bridge_model_allowlist": ["claude-code-bridge-glm-5.2-1m", "claude-code-bridge-kimi-k2.7-code"],
        "bridge_provider_release_statuses": {"zai_glm": "strict-live-pass", "kimi": "strict-live-pass"},
        "bridge_live_expanded_providers": ["zai_glm", "kimi"],
        "bridge_runtime_account_providers": ["zai_glm", "kimi"],
        "live_bridge_model_catalog": {
            "claude-code-bridge-glm-5.2-1m": {
                "provider": "zai_glm",
                "route": "zai_glm_bridge",
                "client_type": "claude_code_bridge_zai_glm",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
            "claude-code-bridge-kimi-k2.7-code": {
                "provider": "kimi",
                "route": "kimi_bridge",
                "client_type": "claude_code_bridge_kimi",
                "live_enabled": True,
                "formal_pool_eligible": False,
            },
        },
    }
    write_fake_claude_runtime(runtime_root, executable, patches=patches, upstream_version="2.1.175")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    evidence = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"
    calls: list[dict[str, object]] = []

    def fake_route_catalog_hash(catalog_version: str, **kwargs):
        calls.append({"catalog_version": catalog_version, **kwargs})
        return "sha256:" + "6" * 64

    monkeypatch.setattr(cli, "route_catalog_content_hash_for_cp8_live", fake_route_catalog_hash)

    assert main([
        "claude-code", "preflight", "--runtime-root", str(runtime_root), "--state-root", str(state_root),
        "--project-cwd", str(tmp_path), "--run-id", "cp18-expanded", "--evidence", str(evidence),
        "--output-root", str(tmp_path / "artifacts"),
    ]) == 0
    data = parse_output(capsys)
    assert data["catalog_hash"] == "sha256:" + "6" * 64
    assert calls == [{
        "catalog_version": "cp4-cli-fixture-v1",
        "bridge_live_models": ("claude-code-bridge-glm-5.2-1m", "claude-code-bridge-kimi-k2.7-code"),
        "bridge_provider_release_statuses": {"kimi": "strict-live-pass", "zai_glm": "strict-live-pass"},
        "bridge_live_expanded_providers": ("kimi", "zai_glm"),
    }]


def test_claude_code_preflight_rejects_sensitive_provider_level_artifact_ref(capsys, tmp_path: Path):
    runtime_root = tmp_path / "runtime-root"
    executable = tmp_path / "managed" / "claude"
    write_fake_claude_runtime(runtime_root, executable, upstream_version="2.1.177")
    state_root = tmp_path / "state-root"
    state_root.mkdir(parents=True)
    (state_root / "state.json").write_text(json.dumps({"gateway_base_url": "http://127.0.0.1:3017"}), encoding="utf-8")
    fixture_root = tmp_path / "cp8"
    shutil.copytree(Path(__file__).parent / "fixtures" / "claude_code_cp8", fixture_root)
    artifact = fixture_root / "artifacts" / "provider_sensitive.json"
    artifact.write_text(json.dumps({"headers": {"authorization": "Bearer must-not-pass"}}, sort_keys=True), encoding="utf-8")
    evidence = fixture_root / "live_matrix_pass.json"
    matrix = json.loads(evidence.read_text(encoding="utf-8"))
    matrix["live_provenance"] = {
        "mode": "external_provider_live_matrix",
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp18-provider-artifact",
        "providers": {
            "openai": {
                "provider": "openai",
                "route": "openai_bridge",
                "client_type": "claude_code_bridge_openai",
                "model": "gpt-5.5",
                "request_model": "gpt-5.5",
                "endpoint": "https://api.openai.com/v1/responses",
                "status": 200,
                "request_id": "provider-artifact-openai",
                "live_provider_verified": True,
                "credential_scope": "bridge_pool",
                "artifact_refs": [{
                    "path": "artifacts/provider_sensitive.json",
                    "sha256": "sha256:" + hashlib.sha256(artifact.read_bytes()).hexdigest(),
                    "sensitive_scan_clean": True,
                }],
            }
        },
    }
    evidence.write_text(json.dumps(matrix, sort_keys=True), encoding="utf-8")

    exit_code = main([
        "claude-code", "preflight", "--runtime-root", str(runtime_root), "--state-root", str(state_root),
        "--project-cwd", str(tmp_path), "--run-id", "cp18-provider-artifact", "--evidence", str(evidence),
        "--output-root", str(tmp_path / "artifacts"),
    ])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "fail"
    assert "sensitive_evidence_scan" in data["failed"]
    assert any("provider_sensitive" in issue for issue in data["issues"])
    assert not (tmp_path / "artifacts" / "cp18-provider-artifact" / "preflight" / "decision-freeze.json").exists()

def test_claude_code_live_matrix_cli_fails_closed_for_invalid_evidence(capsys, tmp_path: Path):
    evidence = tmp_path / "bad-live-matrix.json"
    evidence.write_text(json.dumps({"checkpoint": "CP8", "schema_version": "stale"}), encoding="utf-8")

    assert main(["claude-code", "live-matrix", "--evidence", str(evidence)]) == 1
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "not_configured"
    assert "schema_version" in data["message"]

    evidence.write_text(json.dumps(["not", "an", "object"]), encoding="utf-8")
    assert main(["claude-code", "live-matrix", "--evidence", str(evidence)]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "not_configured"
    assert "JSON object" in data["message"]


def test_claude_code_live_matrix_cli_reports_cp8_release_gate(capsys):
    fixture = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"

    assert main(["claude-code", "live-matrix", "--evidence", str(fixture)]) == 0
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "pass"
    assert data["checkpoint"] == "CP8"
    assert data["release_gate"] == "manual_external_live_required"
    assert data["summary"]["required_scenarios_passed"] is True

    assert main(["claude-code", "live-matrix", "--evidence", str(fixture), "--strict-live"]) == 1
    strict = parse_output(capsys)
    assert strict["status"] == "fail"
    assert strict["release_gate"] == "blocked_missing_external_live"


def test_claude_code_live_matrix_cli_collects_provider_provenance(capsys, tmp_path: Path, monkeypatch):
    calls: list[dict[str, object]] = []

    def fake_collect_cp8_live_provider_provenance(**kwargs):
        calls.append(kwargs)
        return {
            "credential_backed": True,
            "loopback_only": False,
            "run_id": kwargs["run_id"],
            "providers": {
                "claude": {"credential_scope": "formal_pool", "live_provider_verified": True},
                "openai": {"credential_scope": "bridge_pool", "live_provider_verified": True},
                "deepseek": {"credential_scope": "bridge_pool", "live_provider_verified": True},
            },
        }

    monkeypatch.setattr(cli, "collect_cp8_live_provider_provenance", fake_collect_cp8_live_provider_provenance, raising=False)

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-provider-provenance",
        "--run-id",
        "cp8-cli-live",
        "--output-root",
        str(tmp_path),
    ]) == 0
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix collect-provider-provenance"
    assert data["status"] == "collected"
    assert data["live_provenance"]["credential_backed"] is True
    assert calls == [{"run_id": "cp8-cli-live", "output_root": tmp_path}]


def test_claude_code_live_matrix_module_entrypoint_executes_main_for_provider_provenance(tmp_path: Path):
    env = {
        key: value
        for key, value in os.environ.items()
        if key
        not in {
            "ANTHROPIC_API_KEY",
            "SUB2API_CLAUDE_CODE_LIVE_ANTHROPIC_API_KEY",
            "OPENAI_API_KEY",
            "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY",
            "DEEPSEEK_API_KEY",
            "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY",
        }
    }
    env["PYTHONPATH"] = f"{REPO_ROOT}:{REPO_ROOT / 'tools' / 'zhumeng-agent' / 'src'}"

    result = subprocess.run(
        [
            sys.executable,
            "-m",
            "zhumeng_agent.cli",
            "claude-code",
            "live-matrix",
            "--collect-provider-provenance",
            "--run-id",
            "cp8-module-entrypoint",
            "--output-root",
            str(tmp_path),
        ],
        cwd=REPO_ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )

    assert result.returncode == 1
    data = json.loads(result.stdout)
    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "not_configured"
    assert "missing live credential" in data["message"]


def test_claude_code_live_matrix_cli_collects_sub2api_gateway_provenance(capsys, tmp_path: Path, monkeypatch):
    calls: list[dict[str, object]] = []

    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        calls.append(kwargs)
        return {
            "mode": "sub2api_gateway_live_matrix",
            "credential_backed": True,
            "loopback_only": False,
            "gateway_base_url": kwargs["base_url"],
            "run_id": kwargs["run_id"],
            "providers": {
                "claude": {"credential_scope": "formal_pool", "live_provider_verified": True, "route": "claude_code_native"},
                "openai": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "openai_bridge"},
                "deepseek": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "deepseek_bridge"},
            },
        }

    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    monkeypatch.setattr(cli, "default_state_store", lambda: SimpleNamespace(read=lambda: {}), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: (_ for _ in ()).throw(cli.RuntimeInstallerError("not installed")), raising=False)
    monkeypatch.setattr(cli, "route_catalog_content_hash_for_cp8_live", lambda version, bridge_live_models=(), **kwargs: "sha256:" + "e" * 64, raising=False)
    monkeypatch.setenv("SUB2API_CP8_LIVE_BASE_URL", "http://127.0.0.1:3012")
    monkeypatch.setenv("SUB2API_CP8_LIVE_GATEWAY_TOKEN", "sub2api-env-token")

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-cli",
        "--output-root",
        str(tmp_path),
    ]) == 0
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix collect-sub2api-provenance"
    assert data["status"] == "collected"
    assert data["live_provenance"]["mode"] == "sub2api_gateway_live_matrix"
    assert calls == [{
        "run_id": "cp8-sub2api-cli",
        "output_root": tmp_path,
        "base_url": "http://127.0.0.1:3012",
        "gateway_token": "sub2api-env-token",
        "native_attestation_secret": "",
        "route_hint_secret": "",
        "runtime_hash": "",
        "overlay_hash": "",
        "catalog_hash": "sha256:" + "e" * 64,
        "catalog_version": "cp4-cli-fixture-v1",
    }]


def test_claude_code_live_matrix_cli_collects_sub2api_from_managed_state(capsys, tmp_path: Path, monkeypatch):
    calls: list[dict[str, object]] = []

    class FakeStateStore:
        path = tmp_path / "state.json"

        def read(self):
            return {
                "gateway_base_url": "http://127.0.0.1:3012",
                "access_token": "eyJ.generic-managed-jwt",
                "claude_code_sub2api_api_key": "test-dedicated-claude-code-sub2api",
                "claude_code_native_attestation_secret": "managed-native-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "managed-route-secret",
                "claude_code_route_hint_secret_source": "server",
                "catalog_hash_after": "sha256:" + "a" * 64,
                "claude_code_native_managed_session_id": "managed-session-cp8",
                "claude_code_native_device_id": 42,
                "config_profile": {"id": "default"},
            }

    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        calls.append(kwargs)
        return {
            "mode": "sub2api_gateway_live_matrix",
            "credential_backed": True,
            "loopback_only": False,
            "gateway_base_url": kwargs["base_url"],
            "run_id": kwargs["run_id"],
            "providers": {
                "claude": {"credential_scope": "formal_pool", "live_provider_verified": True, "route": "claude_code_native"},
                "openai": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "openai_bridge"},
                "deepseek": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "deepseek_bridge"},
            },
        }

    monkeypatch.setattr(cli, "default_state_store", lambda: FakeStateStore(), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: SimpleNamespace(
        runtime_hash="sha256:" + "b" * 64,
        overlay_hash="sha256:" + "c" * 64,
    ), raising=False)
    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    monkeypatch.setattr(cli, "route_catalog_content_hash_for_cp8_live", lambda version, bridge_live_models=(), **kwargs: "sha256:" + "e" * 64, raising=False)
    for key in (
        "SUB2API_CP8_LIVE_BASE_URL",
        "SUB2API_BASE_URL",
        "SUB2API_CP8_LIVE_GATEWAY_TOKEN",
        "SUB2API_API_KEY",
        "SUB2API_ACCESS_TOKEN",
        "ZHUMENG_CLAUDE_CODE_SUB2API_API_KEY",
        "SUB2API_CLAUDE_CODE_API_KEY",
        "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET",
        "SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET",
        "ZHUMENG_CLAUDE_RUNTIME_HASH",
        "SUB2API_CLAUDE_CODE_RUNTIME_HASH",
        "ZHUMENG_CLAUDE_OVERLAY_HASH",
        "SUB2API_CLAUDE_CODE_OVERLAY_HASH",
        "ZHUMENG_CLAUDE_CATALOG_HASH",
        "SUB2API_CLAUDE_CODE_CATALOG_HASH",
        "SUB2API_CLAUDE_CODE_ROUTE_HINT_CATALOG_VERSION",
        "ZHUMENG_CLAUDE_CATALOG_VERSION",
    ):
        monkeypatch.delenv(key, raising=False)

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-managed-cli",
        "--output-root",
        str(tmp_path),
    ]) == 0
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix collect-sub2api-provenance"
    assert data["status"] == "collected"
    assert calls == [{
        "run_id": "cp8-sub2api-managed-cli",
        "output_root": tmp_path,
        "base_url": "http://127.0.0.1:3012",
        "gateway_token": "test-dedicated-claude-code-sub2api",
        "native_attestation_secret": "managed-native-secret",
        "route_hint_secret": "managed-route-secret",
        "runtime_hash": "sha256:" + "b" * 64,
        "overlay_hash": "sha256:" + "c" * 64,
        "catalog_hash": "sha256:" + "e" * 64,
        "catalog_version": "cp4-cli-fixture-v1",
        "managed_session_id": "managed-session-cp8",
        "device_id": 42,
    }]


def test_claude_code_live_matrix_cli_derives_route_catalog_hash_not_model_catalog_hash(capsys, tmp_path: Path, monkeypatch):
    calls: list[dict[str, object]] = []
    model_catalog_hash = "d" * 64
    expected_route_hash = "sha256:" + "e" * 64

    class FakeStateStore:
        path = tmp_path / "state.json"

        def read(self):
            return {
                "gateway_base_url": "http://127.0.0.1:3012",
                "access_token": "managed-sub2api-token",
                "claude_code_sub2api_api_key": "test-dedicated-claude-code-sub2api",
                "claude_code_native_attestation_secret": "managed-native-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "managed-route-secret",
                "claude_code_route_hint_secret_source": "server",
                "catalog_hash_after": model_catalog_hash,
                "claude_code_route_hint_catalog_version": "cp4-cli-fixture-v1",
                "config_profile": {"id": "default"},
            }

    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        calls.append(kwargs)
        return {
            "mode": "sub2api_gateway_live_matrix",
            "credential_backed": True,
            "loopback_only": False,
            "gateway_base_url": kwargs["base_url"],
            "run_id": kwargs["run_id"],
            "providers": {
                "claude": {"credential_scope": "formal_pool", "live_provider_verified": True, "route": "claude_code_native"},
                "openai": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "openai_bridge"},
                "deepseek": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "deepseek_bridge"},
            },
        }

    monkeypatch.setattr(cli, "default_state_store", lambda: FakeStateStore(), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: SimpleNamespace(
        runtime_hash="sha256:" + "b" * 64,
        overlay_hash="sha256:" + "c" * 64,
        patches={"live_bridge_models_enabled": True, "bridge_live_models": ["gpt-5.5", "deepseek-v4-pro"]},
    ), raising=False)
    monkeypatch.setattr(cli, "route_catalog_content_hash_for_cp8_live", lambda version, bridge_live_models=(), **kwargs: expected_route_hash, raising=False)
    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    for key in ("ZHUMENG_CLAUDE_CATALOG_HASH", "SUB2API_CLAUDE_CODE_CATALOG_HASH"):
        monkeypatch.delenv(key, raising=False)

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-route-catalog-hash",
        "--output-root",
        str(tmp_path),
    ]) == 0
    parse_output(capsys)

    assert calls[0]["catalog_hash"] == expected_route_hash
    assert calls[0]["catalog_hash"] != model_catalog_hash
    assert calls[0]["catalog_version"] == "cp4-cli-fixture-v1"


def test_claude_code_live_matrix_cli_ignores_non_server_managed_runtime_secrets(capsys, tmp_path: Path, monkeypatch):
    calls: list[dict[str, object]] = []

    class FakeStateStore:
        path = tmp_path / "state.json"

        def read(self):
            return {
                "gateway_base_url": "http://127.0.0.1:3012",
                "access_token": "managed-sub2api-token",
                "claude_code_sub2api_api_key": "test-dedicated-claude-code-sub2api",
                "claude_code_native_attestation_secret": "local-native-secret",
                "claude_code_native_attestation_secret_source": "local",
                "claude_code_route_hint_secret": "local-route-secret",
                "claude_code_route_hint_secret_source": "local",
                "catalog_hash_after": "sha256:" + "a" * 64,
                "config_profile": {"id": "default"},
            }

    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        calls.append(kwargs)
        return {
            "mode": "sub2api_gateway_live_matrix",
            "credential_backed": True,
            "loopback_only": False,
            "gateway_base_url": kwargs["base_url"],
            "run_id": kwargs["run_id"],
            "providers": {
                "claude": {"credential_scope": "formal_pool", "live_provider_verified": True, "route": "claude_code_native"},
                "openai": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "openai_bridge"},
                "deepseek": {"credential_scope": "bridge_pool", "live_provider_verified": True, "route": "deepseek_bridge"},
            },
        }

    monkeypatch.setattr(cli, "default_state_store", lambda: FakeStateStore(), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: SimpleNamespace(
        runtime_hash="sha256:" + "b" * 64,
        overlay_hash="sha256:" + "c" * 64,
    ), raising=False)
    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    monkeypatch.delenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", raising=False)
    monkeypatch.delenv("SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET", raising=False)

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-managed-cli",
        "--output-root",
        str(tmp_path),
    ]) == 0
    parse_output(capsys)

    assert calls[0]["native_attestation_secret"] == ""
    assert calls[0]["route_hint_secret"] == ""


def test_claude_code_live_matrix_cli_sub2api_collection_requires_run_id_output_and_token(capsys, tmp_path: Path, monkeypatch):
    monkeypatch.delenv("SUB2API_CP8_LIVE_BASE_URL", raising=False)
    monkeypatch.delenv("SUB2API_BASE_URL", raising=False)
    monkeypatch.delenv("SUB2API_CP8_LIVE_GATEWAY_TOKEN", raising=False)
    monkeypatch.delenv("SUB2API_API_KEY", raising=False)
    monkeypatch.delenv("SUB2API_ACCESS_TOKEN", raising=False)
    monkeypatch.delenv("ZHUMENG_CLAUDE_CODE_SUB2API_API_KEY", raising=False)
    monkeypatch.delenv("SUB2API_CLAUDE_CODE_API_KEY", raising=False)
    monkeypatch.setattr(cli, "default_state_store", lambda: SimpleNamespace(path=tmp_path / "state.json", read=lambda: {}), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: (_ for _ in ()).throw(cli.RuntimeInstallerError("not installed")), raising=False)

    assert main(["claude-code", "live-matrix", "--collect-sub2api-provenance"]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix collect-sub2api-provenance"
    assert "requires --run-id and --output-root" in data["message"]

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-missing-token",
        "--output-root",
        str(tmp_path),
        "--sub2api-base-url",
        "http://127.0.0.1:3012",
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert "gateway token" in data["message"]


def test_claude_code_live_matrix_cli_does_not_use_managed_jwt_as_sub2api_gateway_token(capsys, tmp_path: Path, monkeypatch):
    class FakeStateStore:
        path = tmp_path / "state.json"

        def read(self):
            return {
                "gateway_base_url": "http://127.0.0.1:3012",
                "access_token": "eyJ.generic-managed-jwt",
                "claude_code_native_attestation_secret": "managed-native-secret",
                "claude_code_native_attestation_secret_source": "server",
                "claude_code_route_hint_secret": "managed-route-secret",
                "claude_code_route_hint_secret_source": "server",
            }

    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        if not kwargs["gateway_token"]:
            raise cli.CP8LiveMatrixError("CP8 Sub2API gateway token is required")
        raise AssertionError("managed JWT must not be reused as the Sub2API gateway token")

    monkeypatch.setattr(cli, "default_state_store", lambda: FakeStateStore(), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: (_ for _ in ()).throw(cli.RuntimeInstallerError("not installed")), raising=False)
    monkeypatch.setattr(cli, "route_catalog_content_hash_for_cp8_live", lambda version, bridge_live_models=(), **kwargs: "sha256:" + "e" * 64, raising=False)
    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    for key in (
        "SUB2API_CP8_LIVE_BASE_URL",
        "SUB2API_BASE_URL",
        "SUB2API_CP8_LIVE_GATEWAY_TOKEN",
        "SUB2API_API_KEY",
        "SUB2API_ACCESS_TOKEN",
        "ZHUMENG_CLAUDE_CODE_SUB2API_API_KEY",
        "SUB2API_CLAUDE_CODE_API_KEY",
        "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET",
        "SUB2API_CLAUDE_CODE_ROUTE_HINT_SECRET",
    ):
        monkeypatch.delenv(key, raising=False)

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-managed-jwt",
        "--output-root",
        str(tmp_path),
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert "gateway token" in data["message"]


def test_claude_code_live_matrix_cli_rejects_explicit_jwt_as_sub2api_gateway_token(capsys, tmp_path: Path, monkeypatch):
    def fake_collect_cp8_sub2api_gateway_live_provenance(**kwargs):
        if not kwargs["gateway_token"]:
            raise cli.CP8LiveMatrixError("CP8 Sub2API gateway token is required")
        raise AssertionError("collector must not receive a JWT-shaped Sub2API gateway token")

    monkeypatch.setattr(cli, "default_state_store", lambda: SimpleNamespace(path=tmp_path / "state.json", read=lambda: {}), raising=False)
    monkeypatch.setattr(cli, "resolve_active_managed_runtime", lambda runtime_root: (_ for _ in ()).throw(cli.RuntimeInstallerError("not installed")), raising=False)
    monkeypatch.setattr(cli, "collect_cp8_sub2api_gateway_live_provenance", fake_collect_cp8_sub2api_gateway_live_provenance, raising=False)
    monkeypatch.setenv("SUB2API_CP8_LIVE_BASE_URL", "http://127.0.0.1:3012")
    monkeypatch.setenv("SUB2API_CP8_LIVE_GATEWAY_TOKEN", "eyJ.explicit-jwt")

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-sub2api-explicit-jwt",
        "--output-root",
        str(tmp_path),
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert "gateway token" in data["message"]


def test_claude_code_live_matrix_cli_rejects_provider_and_sub2api_collection_conflict(capsys, tmp_path: Path):
    assert main([
        "claude-code",
        "live-matrix",
        "--collect-provider-provenance",
        "--collect-sub2api-provenance",
        "--run-id",
        "cp8-conflict",
        "--output-root",
        str(tmp_path),
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert "conflicting" in data["message"]
    assert "--collect-sub2api-provenance" in data["message"]


def test_claude_code_live_matrix_cli_assembles_external_matrix_without_promoting_loopback(capsys, tmp_path: Path):
    fixture = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"
    provenance = {
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp8-cli-assemble",
        "providers": {
            "claude": {
                "credential_scope": "formal_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.anthropic.com/v1/messages",
            },
            "openai": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.openai.com/v1/responses",
            },
            "deepseek": {
                "credential_scope": "bridge_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.deepseek.com/anthropic/v1/messages",
            },
        },
    }
    provenance_file = tmp_path / "live_provenance.json"
    provenance_file.write_text(json.dumps(provenance), encoding="utf-8")
    out = tmp_path / "external_matrix.json"

    assert main([
        "claude-code",
        "live-matrix",
        "--assemble-external",
        "--evidence",
        str(fixture),
        "--provenance",
        str(provenance_file),
        "--out",
        str(out),
    ]) == 0
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix assemble-external"
    assert data["status"] == "assembled"
    assembled = json.loads(out.read_text(encoding="utf-8"))
    assert assembled["mode"] == "external_provider_live_matrix"
    assert assembled["live_provenance"] == provenance
    assert all(scenario.get("live_provider_verified") is False for scenario in assembled["scenarios"].values())

    assert main(["claude-code", "live-matrix", "--evidence", str(out), "--strict-live"]) == 1
    strict = parse_output(capsys)
    assert strict["release_gate"] == "blocked_missing_external_live"


def test_claude_code_live_matrix_cli_rejects_conflicting_modes(capsys, tmp_path: Path):
    evidence = tmp_path / "matrix.json"
    provenance = tmp_path / "provenance.json"
    out = tmp_path / "out.json"
    evidence.write_text(json.dumps({"checkpoint": "CP8", "schema_version": "cp8-live-matrix-v1", "scenarios": {}}), encoding="utf-8")
    provenance.write_text(json.dumps({"credential_backed": True}), encoding="utf-8")

    assert main([
        "claude-code",
        "live-matrix",
        "--collect-provider-provenance",
        "--assemble-external",
        "--run-id",
        "cp8-conflict",
        "--output-root",
        str(tmp_path),
        "--evidence",
        str(evidence),
        "--provenance",
        str(provenance),
        "--out",
        str(out),
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "not_configured"
    assert "conflicting" in data["message"]

    assert main([
        "claude-code",
        "live-matrix",
        "--assemble-external",
        "--strict-live",
        "--evidence",
        str(evidence),
        "--provenance",
        str(provenance),
        "--out",
        str(out),
    ]) == 1
    data = parse_output(capsys)
    assert data["command"] == "claude-code live-matrix"
    assert "conflicting" in data["message"]


def test_claude_code_live_matrix_cli_rejects_sensitive_inline_provenance_without_writing(capsys, tmp_path: Path):
    fixture = Path(__file__).parent / "fixtures" / "claude_code_cp8" / "live_matrix_pass.json"
    provenance = tmp_path / "provenance.json"
    provenance.write_text(json.dumps({
        "credential_backed": True,
        "loopback_only": False,
        "run_id": "cp8-sensitive-cli",
        "providers": {
            "claude": {
                "credential_scope": "formal_pool",
                "live_provider_verified": True,
                "endpoint": "https://api.anthropic.com/v1/messages",
                "response_headers": {"authorization": "Bearer sk-must-not-persist"},
            },
        },
    }), encoding="utf-8")
    out = tmp_path / "external_matrix.json"

    assert main([
        "claude-code",
        "live-matrix",
        "--assemble-external",
        "--evidence",
        str(fixture),
        "--provenance",
        str(provenance),
        "--out",
        str(out),
    ]) == 1
    data = parse_output(capsys)

    assert data["command"] == "claude-code live-matrix"
    assert data["status"] == "not_configured"
    assert "sensitive inline" in data["message"]
    assert not out.exists()


def test_claude_code_runtime_install_status_rollback_and_alias_commands(capsys, tmp_path: Path, monkeypatch):
    runtime_root = tmp_path / "runtime"
    shell_rc = tmp_path / ".zshrc"
    shell_rc.write_text("# user rc\n", encoding="utf-8")

    class VersionRunner:
        def __call__(self, command, **kwargs):
            return SimpleNamespace(stdout="Claude Code v2.1.175\n", stderr="", returncode=0)

    monkeypatch.setattr(cli.subprocess, "run", VersionRunner())

    install_exit = main([
        "claude-code",
        "install",
        "--executable",
        str(tmp_path / "claude"),
        "--runtime-root",
        str(runtime_root),
    ])
    assert install_exit == 0
    install_data = parse_output(capsys)
    assert install_data["command"] == "claude-code install"
    assert install_data["status"] == "installed"
    assert install_data["active_version"] == "2.1.175"
    assert install_data["official_claude_unaffected"] is True

    assert main(["claude-code", "status", "--runtime-root", str(runtime_root)]) == 0
    status_data = parse_output(capsys)
    assert status_data["status"] == "enabled"
    assert status_data["active_version"] == "2.1.175"

    def fail_if_restart_spawns(*_args, **_kwargs):
        raise AssertionError("restart must not spawn a second unmanaged Claude Code process")

    cli.launch_claude_code_process = fail_if_restart_spawns
    assert main(["claude-code", "restart", "--runtime-root", str(runtime_root)]) == 1
    restart_data = parse_output(capsys)
    assert restart_data["status"] == "restart_unavailable"
    assert restart_data["active_version"] == "2.1.175"
    assert restart_data["official_claude_unaffected"] is True
    assert restart_data["nonblocking"] is False
    assert "no running managed Claude Code process" in restart_data["message"]
    assert (runtime_root / "claude-code" / "2.1.175" / "manifest.json").exists()

    assert main(["claude-code", "alias", "enable", "--shell-rc", str(shell_rc)]) == 0
    alias_data = parse_output(capsys)
    assert alias_data["status"] == "enabled"
    assert "alias claude=" not in shell_rc.read_text(encoding="utf-8")
    assert 'alias zhumeng-claude="zhumeng-claude"' in shell_rc.read_text(encoding="utf-8")

    assert main(["claude-code", "alias", "disable", "--shell-rc", str(shell_rc)]) == 0
    disable_alias_data = parse_output(capsys)
    assert disable_alias_data["status"] == "disabled"
    assert "zhumeng-claude alias disabled" in shell_rc.read_text(encoding="utf-8")

    assert main(["claude-code", "rollback", "--runtime-root", str(runtime_root)]) == 0
    rollback_data = parse_output(capsys)
    assert rollback_data["status"] == "disabled"
    assert rollback_data["rollback_action"] == "disable_active_pointer_without_delete"
    assert rollback_data["requires_user_confirmation_for_delete"] is True
    assert (runtime_root / "claude-code" / "2.1.175" / "manifest.json").exists()

    assert main(["claude-code", "uninstall", "--runtime-root", str(runtime_root)]) == 0
    uninstall_data = parse_output(capsys)
    assert uninstall_data["status"] == "disabled"
    assert uninstall_data["rollback_action"] == "disable_active_pointer_without_delete"
    assert uninstall_data["requires_user_confirmation_for_delete"] is True
    assert (runtime_root / "claude-code" / "2.1.175" / "manifest.json").exists()

    assert main(["claude-code", "doctor", "--runtime-root", str(runtime_root)]) == 0
    doctor_data = parse_output(capsys)
    assert doctor_data["status"] == "disabled"
    assert doctor_data["official_claude_unaffected"] is True
    assert doctor_data["destructive_cleanup_requires_confirmation"] is True


def test_fetch_codex_model_catalog_falls_back_on_non_auth_errors():
    class FakeClient:
        def list_codex_models(self, **kwargs):
            class Response:
                status_code = 404
            err = RuntimeError("not found")
            err.response = Response()
            raise err

    class FakeManager:
        def read_existing_model_catalog(self, profile):
            return {"models": [{"slug": "fallback-model"}]}

    catalog, meta = cli.fetch_codex_model_catalog(FakeClient(), FakeManager(), {
        "gateway_base_url": "https://example.com",
        "access_token": "access-token",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "config_profile": {"model_provider": "zhumeng-codex"},
    })

    assert catalog["models"][0]["slug"] == "fallback-model"
    assert meta["source"] == "existing"
    assert meta["reason"] == "upstream_fetch_failed"
    assert meta["status_code"] == 404


def test_fetch_codex_model_catalog_refreshes_managed_token_after_unauthorized():
    class FakeClient:
        def __init__(self):
            self.calls = 0

        def list_codex_models(self, **kwargs):
            self.calls += 1
            if self.calls == 1:
                class Response:
                    status_code = 401
                err = RuntimeError("unauthorized")
                err.response = Response()
                raise err
            assert kwargs["access_token"] == "fresh-access-token"
            assert kwargs["managed_session_id"] == "sess-2"
            return {
                "models": [
                    {
                        "slug": "gpt-5.5",
                        "display_name": "GPT-5.5",
                        "visibility": "visible",
                    }
                ]
            }

        def refresh_device_token(self, **kwargs):
            assert kwargs["device_id"] == 9
            assert kwargs["refresh_token"] == "refresh-token"
            return {
                "access_token": "fresh-access-token",
                "refresh_token": "refresh-token-2",
                "managed_session_id": "sess-2",
            }

    class FakeManager:
        def read_existing_model_catalog(self, profile):
            return {"models": [{"slug": "fallback-model"}]}

        def build_model_catalog(self, payload):
            return payload

    class FakeStore:
        def __init__(self):
            self.patch = None

        def update(self, patch):
            self.patch = patch
            return patch

    state = {
        "gateway_base_url": "https://example.com",
        "access_token": "stale-access-token",
        "refresh_token": "refresh-token",
        "managed_session_id": "sess-1",
        "device_id": 9,
        "config_profile": {"model_provider": "zhumeng-codex"},
    }
    store = FakeStore()

    catalog, meta = cli.fetch_codex_model_catalog(FakeClient(), FakeManager(), state, store)

    assert catalog["models"][0]["slug"] == "gpt-5.5"
    assert meta["source"] == "gateway"
    assert meta["refreshed"] is True
    assert state["access_token"] == "fresh-access-token"
    assert state["refresh_token"] == "refresh-token-2"
    assert state["managed_session_id"] == "sess-2"
    assert store.patch == {
        "access_token": "fresh-access-token",
        "refresh_token": "refresh-token-2",
        "managed_session_id": "sess-2",
        "status": "configured",
    }


def test_repair_requires_configured_state(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {}})()
    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "not_configured"


def test_repair_codex_patches_desktop_when_app_is_detected(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def read_existing_model_catalog(self, *args, **kwargs):
            return {"models": []}

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: Path("/Applications/Codex.app")
    cli.patch_codex_enhancements = lambda app_path, item="all": {
        "status": "patched",
        "app_path": str(app_path),
        "restart_required": True,
        "items": {
            "model-picker": {"status": "patched", "app_path": str(app_path)},
            "plugin-auth-gate": {"status": "already_patched", "app_path": str(app_path)},
            "plugin-mention-marketplace": {"status": "patched", "app_path": str(app_path)},
        },
    }

    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "repaired"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "already_patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"


def test_repair_codex_does_not_require_desktop_app_for_model_picker(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def read_existing_model_catalog(self, *args, **kwargs):
            return {"models": []}

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: None

    exit_code = main(["repair", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "repair"
    assert data["status"] == "repaired"
    assert data["model_picker"]["status"] == "app_not_found"
    assert data["plugin_auth_gate"]["status"] == "app_not_found"
    assert data["plugin_mention_marketplace"]["status"] == "app_not_found"


def test_ensure_proxy_running_reuses_existing_loopback_listener(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def __init__(self):
            self.updated = None

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.updated = patch
            return patch

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: False)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: port == 18081)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: True)
    monkeypatch.setattr(cli, "proxy_process_pids_for_state_file", lambda path: [])
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("proxy already running")))

    assert cli.ensure_proxy_running(store) == 0
    assert store.updated is None


def test_ensure_proxy_running_starts_detached_process(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 0,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    captured = {}

    class FakeProcess:
        pid = 12345

    def fake_popen(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return FakeProcess()

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: False)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: False)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli, "proxy_process_pids_for_state_file", lambda path: [])
    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)

    assert cli.ensure_proxy_running(store) == 12345
    assert captured["kwargs"]["start_new_session"] is True
    assert captured["kwargs"]["stdout"] == cli.subprocess.DEVNULL
    assert captured["kwargs"]["stderr"] == cli.subprocess.DEVNULL
    assert store.patch["proxy_pid"] == 12345


def test_ensure_proxy_running_restarts_when_listener_is_old_runtime(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    class FakeProcess:
        pid = 23456

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: True)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: True)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli, "proxy_process_pids_for_state_file", lambda path: [])
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: FakeProcess())

    assert cli.ensure_proxy_running(store) == 23456
    assert store.patch["proxy_pid"] == 23456


def test_ensure_proxy_running_terminates_stale_listener_before_restart(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.patch = patch
            return patch

    class FakeProcess:
        pid = 34567

    killed: list[tuple[int, int]] = []

    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: True)
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: True)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: False)
    monkeypatch.setattr(cli.os, "kill", lambda pid, sig: killed.append((pid, sig)))
    monkeypatch.setattr(cli, "proxy_process_pids_for_state_file", lambda path: [])
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: FakeProcess())

    assert cli.ensure_proxy_running(store) == 34567
    assert killed == [(999999, cli.signal.SIGTERM)]
    assert store.patch["proxy_pid"] == 34567


def test_ensure_proxy_running_terminates_duplicate_proxy_processes(monkeypatch: pytest.MonkeyPatch):
    class FakeStore:
        path = Path("/tmp/state.json")

        def __init__(self):
            self.updated = None

        def read(self):
            return {
                "gateway_base_url": "https://example.com",
                "device_id": 9,
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "loopback_secret": "loopback-secret",
                "proxy_port": 18081,
                "proxy_pid": 999999,
            }

        def update(self, patch):
            self.updated = patch
            return patch

    killed: list[tuple[int, int]] = []
    store = FakeStore()
    monkeypatch.setattr(cli, "ensure_proxy_running", ORIGINAL_ENSURE_PROXY_RUNNING)
    monkeypatch.setattr(cli, "is_process_alive", lambda pid: pid in {111111, 222222, 999999})
    monkeypatch.setattr(cli, "is_loopback_port_accepting_connections", lambda port: port == 18081)
    monkeypatch.setattr(cli, "proxy_matches_current_runtime", lambda port: True)
    monkeypatch.setattr(cli, "proxy_process_pids_for_state_file", lambda path: [111111, 999999, 222222])
    monkeypatch.setattr(cli.os, "kill", lambda pid, sig: killed.append((pid, sig)))
    monkeypatch.setattr(cli.subprocess, "Popen", lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("proxy already running")))

    assert cli.ensure_proxy_running(store) == 999999
    assert killed == [(111111, cli.signal.SIGTERM), (222222, cli.signal.SIGTERM)]
    assert store.updated is None


def test_proxy_matches_current_runtime_rejects_stale_runtime_signature(monkeypatch: pytest.MonkeyPatch):
    class FakeResponse:
        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def read(self):
            return json.dumps({
                "ok": True,
                "agent_version": cli.AGENT_VERSION,
                "source_root": cli.AGENT_SOURCE_ROOT,
                "runtime_signature": "stale-signature",
            }).encode("utf-8")

    monkeypatch.setattr(
        cli.urllib.request,
        "urlopen",
        lambda *args, **kwargs: FakeResponse(),
    )

    assert cli.proxy_matches_current_runtime(18081) is False


def test_launch_codex_patches_desktop_before_launch(capsys):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

    class FakeStore:
        def read(self):
            return {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
            }

        def update(self, patch):
            return patch

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: Path("/Applications/Codex.app")
    cli.patch_codex_enhancements = lambda app_path, item="all": {
        "status": "patched",
        "app_path": str(app_path),
        "restart_required": True,
        "items": {
            "model-picker": {"status": "patched", "app_path": str(app_path)},
            "plugin-auth-gate": {"status": "already_patched", "app_path": str(app_path)},
            "plugin-mention-marketplace": {"status": "patched", "app_path": str(app_path)},
        },
    }
    launched = {}
    cli.launch_codex_process = lambda command: launched.setdefault("command", command)

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "launch"
    assert data["status"] == "launched"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "already_patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"
    assert launched["command"]


@pytest.mark.asyncio
async def test_capture_receiver_accepts_cors_preflight_for_desktop_renderer(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.options(
            "/codex-desktop-capture-v2",
            headers={
                "Origin": "app://-",
                "Access-Control-Request-Method": "POST",
                "Access-Control-Request-Headers": "content-type",
            },
        )

        assert response.status == 204
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        assert "POST" in response.headers["Access-Control-Allow-Methods"]
        assert "content-type" in response.headers["Access-Control-Allow-Headers"].lower()
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_posts_with_desktop_origin_and_writes_shape_only_trace(tmp_path: Path):
    key = tmp_path / "correlation.key"
    key.write_bytes(b"shared")
    config = cli.CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)
    app = cli.create_capture_receiver_app(tmp_path, config)
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.post(
            "/codex-desktop-capture-v2",
            json={
                "type": "app_server_frame",
                "direction": "desktop_to_app_server",
                "frame_text": '{"id":1,"method":"model/list","params":{"x-client-request-id":"request-1"}}',
            },
            headers={"Origin": "app://-"},
        )

        assert response.status == 200
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        dumped = (tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8")
        assert "request-1" not in dumped
        assert "x_client_request_id_hash" in dumped
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_accepts_text_plain_beacon_payload(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        response = await client.post(
            "/codex-desktop-capture-v2",
            data='{"type":"model_picker","selected_model":"beacon-smoke"}',
            headers={
                "Origin": "app://-",
                "Content-Type": "text/plain;charset=UTF-8",
            },
        )

        assert response.status == 200
        assert response.headers["Access-Control-Allow-Origin"] == "app://-"
        dumped = (tmp_path / "model_picker.jsonl").read_text(encoding="utf-8")
        assert "beacon-smoke" in dumped
    finally:
        await client.close()
        await server.close()


@pytest.mark.asyncio
async def test_capture_receiver_accepts_websocket_events(tmp_path: Path):
    app = cli.create_capture_receiver_app(tmp_path, cli.default_capture_config())
    server = TestServer(app)
    await server.start_server()
    client = TestClient(server)
    await client.start_server()
    try:
        ws = await client.ws_connect("/codex-desktop-capture-v2/ws", headers={"Origin": "app://-"})
        await ws.send_str('{"type":"model_picker","selected_model":"websocket-smoke"}')
        await ws.close()

        dumped = (tmp_path / "model_picker.jsonl").read_text(encoding="utf-8")
        assert "websocket-smoke" in dumped
    finally:
        await client.close()
        await server.close()


def test_launch_codex_dry_run_returns_not_implemented_status(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"proxy_port": 18081}})()
    exit_code = main(["launch", "codex", "--dry-run"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "launch"
    assert data["client"] == "codex"
    assert data["status"] == "not_configured"


def test_launch_codex_dry_run_returns_plan_when_state_exists(capsys):
    cli.default_state_store = lambda: type("Store", (), {
        "read": lambda self: {
            "proxy_port": 18081,
            "config_profile": {"model_provider": "zhumeng-managed"},
            "loopback_secret": "loopback-secret",
        }
    })()
    exit_code = main(["launch", "codex", "--dry-run"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "planned"
    assert data["proxy_port"] == 18081


def test_codex_wrapper_parses_passthrough_args_without_launching(capsys):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"proxy_port": 18081}})()
    cli.default_config_manager = lambda: type("Manager", (), {"repair": lambda *args, **kwargs: None})()
    cli.run_codex_process = lambda args, env: 0
    cli.ensure_proxy_running = lambda store: 9999
    exit_code = main(["codex", "--", "--version"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex"
    assert data["args"] == ["--version"]
    assert data["status"] == "not_configured"


def test_codex_capture_status_reports_config(capsys, tmp_path: Path):
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"desktop_capture_enabled": True}})()
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.capture_install_manifest_state = lambda config: {"status": "installed"}

    exit_code = main(["codex", "capture", "status"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture status"
    assert data["config"]["enabled"] is True
    assert data["installation"]["status"] == "installed"
    assert data["config"]["raw_payloads"] is False


def test_codex_capture_baseline_delegates_to_baseline_generator(capsys, tmp_path: Path):
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.default_codex_app_path = lambda: tmp_path / "Codex.app"
    cli.generate_capture_baseline = lambda out_dir, app_path, config: {"status": "baseline_created", "out_dir": str(out_dir), "desktop_app_path_hash": "hmac-sha256:x"}

    exit_code = main(["codex", "capture", "baseline", "--out", str(tmp_path / "out")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture baseline"
    assert data["status"] == "baseline_created"
    assert "/Codex.app" not in json.dumps(data)


def test_codex_capture_install_and_uninstall_do_not_patch_model_picker(capsys, tmp_path: Path):
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.install_capture_hook = lambda app_path, config: {"status": "installed", "app_asar_modified": False, "hook_mode": "renderer_readonly"}
    cli.uninstall_capture_hook = lambda app_path, config: {"status": "uninstalled", "app_asar_modified": False}
    cli.patch_model_picker_app = lambda app_path: (_ for _ in ()).throw(AssertionError("capture install must not patch model picker"))
    updates = {}
    cli.default_state_store = lambda: type(
        "Store",
        (),
        {
            "read": lambda self: {},
            "update": lambda self, patch: updates.update(patch) or patch,
        },
    )()

    assert main(["codex", "capture", "install", "--app", str(tmp_path / "Codex.app")]) == 0
    install_data = parse_output(capsys)
    assert install_data["status"] == "installed"
    assert install_data["app_asar_modified"] is False
    assert updates["desktop_capture_enabled"] is True

    assert main(["codex", "capture", "uninstall", "--app", str(tmp_path / "Codex.app")]) == 0
    uninstall_data = parse_output(capsys)
    assert uninstall_data["status"] == "uninstalled"
    assert updates["desktop_capture_enabled"] is False


def test_codex_capture_report_reads_trace_dir(capsys, tmp_path: Path, monkeypatch):
    monkeypatch.setattr(cli, "generate_capture_report", lambda trace_dir, config=None, gateway_trace_dir=None: {"status": "reported", "trace_dir_hash": "hmac-sha256:x", "app_server_methods": ["model/list"]})

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(tmp_path / "traces")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture report"
    assert data["status"] == "reported"
    assert "model/list" in data["app_server_methods"]


def test_codex_capture_attach_uses_cdp_binding_bridge(capsys, tmp_path: Path):
    seen = {}

    def fake_attach(cdp_port, trace_dir, config, *, capture_port, timeout_seconds, target_wait_seconds, once):
        seen["cdp_port"] = cdp_port
        seen["trace_dir"] = trace_dir
        seen["capture_port"] = capture_port
        seen["timeout_seconds"] = timeout_seconds
        seen["target_wait_seconds"] = target_wait_seconds
        seen["once"] = once
        return {
            "status": "attached",
            "bridge": "cdp_binding",
            "targets_attached": 2,
            "events_written": 1,
        }

    cli.attach_capture_bridge_via_cdp = fake_attach

    exit_code = main([
        "codex", "capture", "attach",
        "--cdp-port", "65031",
        "--trace-dir", str(tmp_path / "traces"),
        "--capture-port", "65030",
        "--timeout-seconds", "1.5",
        "--once",
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex capture attach"
    assert data["bridge"] == "cdp_binding"
    assert data["targets_attached"] == 2
    assert seen["cdp_port"] == 65031
    assert seen["trace_dir"] == tmp_path / "traces"
    assert seen["capture_port"] == 65030
    assert seen["timeout_seconds"] == 1.5
    assert seen["target_wait_seconds"] == 10
    assert seen["once"] is True


def test_codex_capture_report_flags_serialized_sensitive_content(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text('{"payload_shape":{"/Users/alice/secret.py":"str"}}\n', encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 1


def test_codex_capture_report_flags_bearer_and_cookie(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text(
        '{"headers":{"Authorization":"Bearer sk-test","Cookie":"abc"}}\n',
        encoding="utf-8",
    )

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 1


def test_codex_capture_report_generates_trace_links(capsys, tmp_path: Path):
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (trace_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert (trace_dir / "trace_link.jsonl").exists()
    assert "request-1" not in (trace_dir / "trace_link.jsonl").read_text(encoding="utf-8")


def test_codex_capture_report_accepts_separate_gateway_trace_dir(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_dir = tmp_path / "gateway"
    trace_dir.mkdir()
    gateway_dir.mkdir()
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(gateway_dir),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert (trace_dir / "trace_link.jsonl").exists()


def test_codex_capture_report_discovers_gateway_trace_files_recursively(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_dir = tmp_path / "gateway" / "2026-05-14" / "trace_1"
    trace_dir.mkdir()
    gateway_dir.mkdir(parents=True)
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(tmp_path / "gateway"),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1

def test_codex_capture_report_summarizes_deferred_tool_search_sequence(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    trace_dir.mkdir()
    (trace_dir / "deferred_tool_search.jsonl").write_text(
        json.dumps({
            "event_type": "tool_search_call",
            "call_id": "call_fixture",
            "capture_ts": "2026-06-03T11:43:33.493Z",
        }) + "\n" +
        json.dumps({
            "event_type": "tool_search_output",
            "call_id": "call_fixture",
            "capture_ts": "2026-06-03T11:43:33.599Z",
            "tools": [{
                "type": "namespace",
                "name": "multi_agent_v1",
                "tools": [{"name": "spawn_agent", "input_schema": {"type": "object"}}],
            }],
        }) + "\n",
        encoding="utf-8",
    )

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["deferred_tool_search"] == {
        "events": 2,
        "tool_search_call_count": 1,
        "tool_search_output_count": 1,
        "tool_search_call_followed_by_output": True,
        "spawn_agent_present": True,
        "discovered_namespaces": ["multi_agent_v1"],
        "discovered_tools": ["multi_agent_v1.spawn_agent"],
        "tool_family_matrix": {
            "multi_agent_v1": {"tool_count": 1, "tools": ["spawn_agent"]},
        },
    }
    assert "call_fixture" not in json.dumps(data)


def test_codex_capture_report_summarizes_deferred_tool_family_matrix(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    trace_dir.mkdir()
    (trace_dir / "deferred_tool_search.jsonl").write_text(
        json.dumps({
            "event_type": "tool_search_call",
            "capture_ts": "2026-06-03T11:43:33.493Z",
        }) + "\n" +
        json.dumps({
            "event_type": "tool_search_output",
            "capture_ts": "2026-06-03T11:43:33.599Z",
            "tools": [
                {"type": "namespace", "name": "multi_agent_v1", "tools": [{"name": "spawn_agent", "input_schema": {"type": "object"}}]},
                {"type": "namespace", "name": "browser", "tools": [{"name": "navigate", "input_schema": {"type": "object"}}]},
                {"type": "namespace", "name": "computer_use", "tools": [{"name": "list_apps", "input_schema": {"type": "object"}}]},
                {"type": "namespace", "name": "documents", "tools": [{"name": "redline", "input_schema": {"type": "object"}}]},
            ],
        }) + "\n",
        encoding="utf-8",
    )

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    deferred = data["deferred_tool_search"]
    assert deferred["discovered_namespaces"] == ["browser", "computer_use", "documents", "multi_agent_v1"]
    assert deferred["discovered_tools"] == [
        "browser.navigate",
        "computer_use.list_apps",
        "documents.redline",
        "multi_agent_v1.spawn_agent",
    ]
    assert deferred["tool_family_matrix"] == {
        "browser": {"tool_count": 1, "tools": ["navigate"]},
        "computer_use": {"tool_count": 1, "tools": ["list_apps"]},
        "documents": {"tool_count": 1, "tools": ["redline"]},
        "multi_agent_v1": {"tool_count": 1, "tools": ["spawn_agent"]},
    }
    assert deferred["spawn_agent_present"] is True

def test_codex_capture_report_reads_real_gateway_capture_artifacts(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_trace = tmp_path / "gateway" / "2026-06-03" / "trace_real"
    trace_dir.mkdir()
    gateway_trace.mkdir(parents=True)
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-06-03T11:48:43.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_trace / "summary.json").write_text(json.dumps({
        "trace_id": "trace_real",
        "finished_at": "2026-06-03T11:48:43.010Z",
        "model": "deepseek-v4-pro",
        "path": "/codex/v1/responses",
        "request_diagnostics": {
            "deepseek_cache": {
                "previous_response_id_present": True,
                "previous_response_replay_mode": "full_replay_messages",
                "state_lookup_status": "hit",
                "messages_full_hash": "sha256:messages",
                "message_prefix_hash": "sha256:prefix",
                "message_suffix_hash": "sha256:suffix",
                "tool_schema_hash": "sha256:tools",
                "request_shape_hash": "sha256:shape",
            },
        },
    }), encoding="utf-8")
    (gateway_trace / "client_request.diagnostics.json").write_text(json.dumps({
        "deepseek_tool_output_summary": {
            "classes": {"computer_screenshot": True, "accessibility_tree": True},
            "fallback_preview_only": False,
            "operable_line_count": 4,
            "original_chars": 109000,
            "sha256": "sha256:tool-output",
        },
    }), encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(tmp_path / "gateway"),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert data["deepseek_cache_replay_diagnostics"]["previous_response_replay_modes"] == ["full_replay_messages"]
    assert data["deepseek_cache_replay_diagnostics"]["state_lookup_statuses"] == ["hit"]
    assert data["computer_use_normalized_output"]["classes"] == ["accessibility_tree", "computer_screenshot"]
    assert data["computer_use_normalized_output"]["operable_line_count_max"] == 4
    assert "tool-output" not in json.dumps(data)


def test_codex_capture_report_summarizes_deepseek_gateway_diagnostics(capsys, tmp_path: Path):
    trace_dir = tmp_path / "desktop"
    gateway_dir = tmp_path / "gateway" / "2026-06-03" / "trace_1"
    trace_dir.mkdir()
    gateway_dir.mkdir(parents=True)
    shared = {"x_client_request_id_hash": "hmac-sha256:abc"}
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "desktop_trace_id": "cd_1",
        "ts": "2026-06-03T11:48:43.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
    }) + "\n", encoding="utf-8")
    (gateway_dir / "gateway_trace.jsonl").write_text(json.dumps({
        "gateway_trace_id": "trace_1",
        "ts": "2026-06-03T11:48:43.010Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": shared,
        "request_diagnostics": {
            "deepseek_cache": {
                "previous_response_id_present": True,
                "previous_response_replay_mode": "full_replay_messages",
                "state_lookup_status": "hit",
                "messages_full_hash": "sha256:messages",
                "message_prefix_hash": "sha256:prefix",
                "message_suffix_hash": "sha256:suffix",
                "tool_schema_hash": "sha256:tools",
                "request_shape_hash": "sha256:shape",
            },
            "deepseek_tool_output_summary": {
                "classes": ["computer_screenshot", "accessibility_tree"],
                "fallback_preview_only": False,
                "operable_line_count": 3,
                "original_chars": 108000,
                "sha256": "sha256:tool-output",
            },
        },
    }) + "\n", encoding="utf-8")

    exit_code = main([
        "codex", "capture", "report",
        "--trace-dir", str(trace_dir),
        "--gateway-trace-dir", str(tmp_path / "gateway"),
    ])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["gateway_trace_links"] == 1
    assert data["deepseek_cache_replay_diagnostics"] == {
        "events": 1,
        "previous_response_id_present": True,
        "previous_response_replay_modes": ["full_replay_messages"],
        "state_lookup_statuses": ["hit"],
        "messages_full_hash_present": True,
        "message_prefix_hash_present": True,
        "message_suffix_hash_present": True,
        "tool_schema_hash_present": True,
        "request_shape_hash_present": True,
    }
    assert data["computer_use_normalized_output"] == {
        "events": 1,
        "classes": ["accessibility_tree", "computer_screenshot"],
        "fallback_preview_only": False,
        "operable_line_count_max": 3,
        "original_chars_max": 108000,
        "sha256_present": True,
    }
    assert "tool-output" not in json.dumps(data)


def test_codex_capture_status_accepts_correlation_key_file(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.default_state_store = lambda: type("Store", (), {"read": lambda self: {"desktop_capture_enabled": True}})()
    key = tmp_path / "key"
    key.write_text("shared", encoding="utf-8")

    exit_code = main(["codex", "capture", "--correlation-hash-key-file", str(key), "status"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["config"]["correlation_hash_key_file"] == "set"


def test_capture_install_marks_desktop_capture_enabled(capsys, tmp_path: Path):
    updates = {}
    cli.default_state_store = lambda: type("Store", (), {"update": lambda self, patch: updates.update(patch) or patch, "read": lambda self: {}})()
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.install_capture_hook = lambda app_path, config: {"status": "installed", "app_asar_modified": False, "hook_mode": "renderer_readonly"}

    assert main(["codex", "capture", "install", "--app", str(tmp_path / "Codex.app")]) == 0
    install_data = parse_output(capsys)
    assert install_data["status"] == "installed"
    assert updates["desktop_capture_enabled"] is True


def test_capture_uninstall_marks_desktop_capture_disabled(capsys, tmp_path: Path):
    updates = {}
    cli.default_state_store = lambda: type("Store", (), {"update": lambda self, patch: updates.update(patch) or patch, "read": lambda self: {}})()
    cli.default_capture_config = lambda *args: __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=args[0] if args else None)
    cli.uninstall_capture_hook = lambda app_path, config: {"status": "uninstalled", "app_asar_modified": False}

    assert main(["codex", "capture", "uninstall", "--app", str(tmp_path / "Codex.app")]) == 0
    uninstall_data = parse_output(capsys)
    assert uninstall_data["status"] == "uninstalled"
    assert updates["desktop_capture_enabled"] is False


def test_codex_capture_report_uses_cli_correlation_key_file(capsys, tmp_path: Path):
    key = tmp_path / "key"
    key.write_text("shared", encoding="utf-8")
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()

    exit_code = main(["codex", "capture", "--correlation-hash-key-file", str(key), "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["correlation_hash_key_file"] == "set"


def test_codex_capture_report_does_not_flag_hash_fields_as_commit_hashes(capsys, tmp_path: Path):
    cli.default_capture_config = ORIGINAL_DEFAULT_CAPTURE_CONFIG
    cli.generate_capture_report = ORIGINAL_GENERATE_CAPTURE_REPORT
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "app_server_v2.jsonl").write_text(json.dumps({
        "payload_hash": "sha256:" + "a" * 64,
        "schema_hash": "hmac-sha256:" + "b" * 64,
        "result_hash": "sha256:" + "c" * 64,
    }) + "\n", encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["content_policy_violations"] == 0


def test_launch_codex_starts_cdp_binding_bridge_when_capture_is_installed(capsys, tmp_path: Path):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def build_model_catalog(self, payload):
            return payload

    class FakeClient:
        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "claude-opus-4-6", "display_name": "Claude Opus 4.6"}]}

    class FakeStore:
        def __init__(self):
            self.data = {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
                "gateway_base_url": "https://example.com",
                "access_token": "access-token",
                "managed_session_id": "managed-session",
                "device_id": 9,
            }

        def read(self):
            return dict(self.data)

        def update(self, payload):
            self.data.update(payload)

    installed = {}
    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.default_http_client = lambda server: FakeClient()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: tmp_path / "Codex.app"
    cli.patch_codex_enhancements = lambda app_path, item="all": {
        "status": "patched",
        "app_path": str(app_path),
        "restart_required": True,
        "items": {
            "model-picker": {"status": "patched", "app_path": str(app_path)},
            "plugin-auth-gate": {"status": "patched", "app_path": str(app_path)},
            "plugin-mention-marketplace": {"status": "patched", "app_path": str(app_path)},
        },
    }
    cli.select_cdp_port = lambda: 9333
    cli.launch_codex_process = lambda command: None
    cli.default_capture_config = lambda: __import__(
        "zhumeng_agent.adapters.codex.capture_config",
        fromlist=["CodexDesktopCaptureConfig"],
    ).CodexDesktopCaptureConfig.defaults(enabled=True, base_dir=tmp_path / "captures")
    cli.capture_installation_enabled = lambda app_path, config: True
    cli.ensure_capture_receiver_running = lambda config: (_ for _ in ()).throw(AssertionError("launch capture path must not use renderer network receiver"))
    cli.inject_capture_hook_via_cdp = lambda *args, **kwargs: (_ for _ in ()).throw(AssertionError("launch capture path must use cdp binding bridge"))
    cli.ensure_capture_bridge_running = lambda config, cdp_port: installed.setdefault("bridge", {
        "status": "running",
        "bridge": "cdp_binding",
        "cdp_port": cdp_port,
        "trace_dir_hash": "hmac-sha256:x",
    })

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["capture"]["status"] == "running"
    assert data["capture"]["bridge"] == "cdp_binding"
    assert data["model_picker"]["status"] == "patched"
    assert data["plugin_auth_gate"]["status"] == "patched"
    assert data["plugin_mention_marketplace"]["status"] == "patched"
    assert installed["bridge"]["cdp_port"] == 9333


def test_launch_codex_reports_installed_but_disabled_capture(capsys, tmp_path: Path):
    class FakeManager:
        def repair(self, *args, **kwargs):
            return None

        def build_model_catalog(self, payload):
            return payload

    class FakeClient:
        def list_codex_models(self, **kwargs):
            return {"models": [{"slug": "claude-opus-4-6", "display_name": "Claude Opus 4.6"}]}

    class FakeStore:
        def __init__(self):
            self.data = {
                "proxy_port": 18081,
                "config_profile": {"model_provider": "zhumeng-managed"},
                "loopback_secret": "loopback-secret",
                "gateway_base_url": "https://example.com",
                "access_token": "access-token",
                "managed_session_id": "managed-session",
                "device_id": 9,
            }

        def read(self):
            return dict(self.data)

        def update(self, payload):
            self.data.update(payload)

    capture_config = __import__(
        "zhumeng_agent.adapters.codex.capture_config",
        fromlist=["CodexDesktopCaptureConfig"],
    ).CodexDesktopCaptureConfig.defaults(enabled=False, base_dir=tmp_path / "captures")

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: FakeManager()
    cli.default_http_client = lambda server: FakeClient()
    cli.ensure_proxy_running = lambda store: 9999
    cli.detect_codex_app_path = lambda **kwargs: tmp_path / "Codex.app"
    cli.patch_codex_enhancements = lambda app_path, item="all": {
        "status": "patched",
        "app_path": str(app_path),
        "restart_required": True,
        "items": {
            "model-picker": {"status": "patched", "app_path": str(app_path)},
            "plugin-auth-gate": {"status": "patched", "app_path": str(app_path)},
            "plugin-mention-marketplace": {"status": "patched", "app_path": str(app_path)},
        },
    }
    cli.select_cdp_port = lambda: 9333
    cli.launch_codex_process = lambda command: None
    cli.default_capture_config = lambda: capture_config
    cli.capture_installation_enabled = lambda app_path, config: True
    cli.ensure_capture_bridge_running = lambda config, cdp_port: (_ for _ in ()).throw(
        AssertionError("disabled capture must not start bridge")
    )

    exit_code = main(["launch", "codex"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["capture"]["status"] == "installed_but_disabled"


def test_capture_receiver_process_receives_correlation_key_file(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    cli.ensure_capture_receiver_running = ORIGINAL_ENSURE_CAPTURE_RECEIVER_RUNNING
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(
        base_dir=tmp_path / "captures",
        correlation_hash_key_file=key,
    )
    captured = {}
    cli.select_cdp_port = lambda: 18765
    monkeypatch.setattr(cli.subprocess, "Popen", lambda command: captured.setdefault("command", command))

    result = cli.ensure_capture_receiver_running(config)

    assert result["port"] == 18765
    assert "--correlation-hash-key-file" in captured["command"]
    assert str(key) in captured["command"]


def test_capture_bridge_process_receives_correlation_key_and_cdp_port(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    cli.ensure_capture_bridge_running = ORIGINAL_ENSURE_CAPTURE_BRIDGE_RUNNING
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = __import__("zhumeng_agent.adapters.codex.capture_config", fromlist=["CodexDesktopCaptureConfig"]).CodexDesktopCaptureConfig.defaults(
        base_dir=tmp_path / "captures",
        correlation_hash_key_file=key,
    )
    captured = {}

    class FakeProcess:
        pid = 12345

    def fake_popen(command):
        captured["command"] = command
        return FakeProcess()

    monkeypatch.setattr(cli.subprocess, "Popen", fake_popen)

    result = cli.ensure_capture_bridge_running(config, 65031)

    assert result["status"] == "running"
    assert result["bridge"] == "cdp_binding"
    assert "--correlation-hash-key-file" in captured["command"]
    assert str(key) in captured["command"]
    assert "--cdp-port" in captured["command"]
    assert "65031" in captured["command"]
    assert "--target-wait-seconds" in captured["command"]


def test_codex_model_picker_patch_is_separate_from_capture(capsys, tmp_path: Path):
    cli.patch_model_picker_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "model-picker", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex model-picker patch"
    assert data["status"] == "patched"


def test_codex_plugin_auth_gate_patch_is_explicit(capsys, tmp_path: Path):
    cli.patch_plugin_auth_gate_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "plugin-auth-gate", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-auth-gate patch"
    assert data["status"] == "patched"


def test_codex_plugin_auth_gate_patch_failure_returns_json(capsys, tmp_path: Path):
    def fail_patch(app_path):
        raise ModelPickerPatchError("unsupported desktop build")

    cli.patch_plugin_auth_gate_app = fail_patch

    exit_code = main(["codex", "plugin-auth-gate", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-auth-gate patch"
    assert data["status"] == "failed"
    assert "unsupported desktop build" in data["message"]
    assert data["recovery_hint"]


def test_codex_plugin_mention_marketplace_patch_is_explicit(capsys, tmp_path: Path):
    cli.patch_plugin_mention_marketplace_app = lambda app_path: {"status": "patched", "app_path": str(app_path)}

    exit_code = main(["codex", "plugin-mention-marketplace", "patch", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-mention-marketplace patch"
    assert data["status"] == "patched"


def test_logout_local_only_removes_managed_auth_when_no_prior_auth(tmp_path: Path, capsys):
    codex_home = tmp_path / ".codex"
    codex_home.mkdir(parents=True, exist_ok=True)
    (codex_home / "auth.json").write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-loopback-secret"}', encoding="utf-8")
    (codex_home / "config.toml").write_text('model_provider = "zhumeng-managed"\n', encoding="utf-8")
    backup = codex_home / "backups" / "config.toml.1.bak"
    backup.parent.mkdir(parents=True, exist_ok=True)
    backup.write_text('model_provider = "legacy"\n', encoding="utf-8")

    class FakeStore:
        def read(self):
            return {"backup_paths": [str(backup)]}

        def delete(self):
            return None

    cli.default_state_store = lambda: FakeStore()
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(codex_home)

    exit_code = main(["logout", "--local-only"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "completed"
    assert not (codex_home / "auth.json").exists()
    assert (codex_home / "config.toml").read_text(encoding="utf-8") == 'model_provider = "legacy"\n'


def test_logout_revoke_device_calls_backend_and_cleans_local_state(tmp_path: Path, capsys):
    codex_home = tmp_path / ".codex"
    codex_home.mkdir(parents=True, exist_ok=True)
    (codex_home / "auth.json").write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-loopback-secret"}', encoding="utf-8")
    (codex_home / "config.toml").write_text('model_provider = "zhumeng-managed"\n', encoding="utf-8")

    class FakeStore:
        def __init__(self):
            self.state = {
                "device_id": 9,
                "server_base_url": "https://example.com",
                "managed_session_id": "sess-1",
                "access_token": "access-token",
                "refresh_token": "refresh-token",
            }

        def read(self):
            return self.state

        def update(self, patch):
            self.state.update(patch)
            return self.state

        def delete(self):
            return None

    class FakeClient:
        def revoke_managed_device(self, **kwargs):
            return {"device_id": 9, "revoked": True}

        def refresh_device_token(self, **kwargs):
            raise AssertionError("refresh should not be needed in this test")

    cli.default_state_store = lambda: FakeStore()
    cli.default_http_client = lambda server: FakeClient()
    cli.default_config_manager = lambda: __import__("zhumeng_agent.adapters.codex.config_manager", fromlist=["CodexConfigManager"]).CodexConfigManager(codex_home)

    exit_code = main(["logout", "--revoke-device"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "completed"
    assert not (codex_home / "auth.json").exists()


def test_plugin_mention_marketplace_restore_command(capsys, tmp_path: Path):
    cli.restore_latest_plugin_mention_marketplace_backup = lambda app_path: {"status": "restored", "app_path": str(app_path)}

    exit_code = main(["codex", "plugin-mention-marketplace", "restore", "--app", str(tmp_path / "Codex.app")])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["command"] == "codex plugin-mention-marketplace restore"
    assert data["status"] == "restored"


def test_logout_detects_config_restore_conflict(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True)
    manager.config_path.write_text('model_provider = "zhumeng-codex"\n# user changed after setup\n', encoding="utf-8")
    manager.auth_path.write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-secret"}', encoding="utf-8")
    catalog = manager.catalog_path_for_profile({"model_provider": "zhumeng-codex"})
    catalog.write_text('{"models":[]}', encoding="utf-8")

    class Store:
        def __init__(self):
            self.deleted = False
        def read(self):
            return {
                "backup_paths": [],
                "prior_auth_json": None,
                "config_hash_after": "not-current",
                "auth_hash_after": cli.file_sha256(manager.auth_path),
                "catalog_hash_after": cli.file_sha256(catalog),
                "catalog_path": str(catalog),
                "catalog_preexisting": False,
                "config_profile": {"model_provider": "zhumeng-codex"},
            }
        def delete(self):
            self.deleted = True

    store = Store()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager

    exit_code = main(["logout", "--local-only"])

    assert exit_code == 1
    data = parse_output(capsys)
    assert data["status"] == "restore_conflict"
    assert store.deleted is False
    assert "user changed after setup" in manager.config_path.read_text(encoding="utf-8")


def test_logout_restores_catalog_or_removes_managed_catalog(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    plan = manager.plan_configure({"model_provider": "zhumeng-codex"}, 18081, "secret", {"models": []})
    manager.apply_configure(plan)
    catalog = manager.catalog_path_for_profile({"model_provider": "zhumeng-codex"})

    class Store:
        def __init__(self):
            self.deleted = False
        def read(self):
            return {
                "backup_paths": [],
                "prior_auth_json": None,
                "config_hash_after": cli.file_sha256(manager.config_path),
                "auth_hash_after": cli.file_sha256(manager.auth_path),
                "catalog_hash_after": cli.file_sha256(catalog),
                "catalog_path": str(catalog),
                "catalog_preexisting": False,
                "config_profile": {"model_provider": "zhumeng-codex"},
            }
        def delete(self):
            self.deleted = True

    store = Store()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager

    exit_code = main(["logout", "--local-only"])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["status"] == "completed"
    assert store.deleted is True
    assert not catalog.exists()


def test_logout_treats_enhancement_running_as_restore_failed(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True)
    manager.config_path.write_text('model_provider = "zhumeng-codex"\n', encoding="utf-8")
    manager.auth_path.write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-secret"}', encoding="utf-8")

    class Store:
        def __init__(self):
            self.deleted = False
            self.patch = None
        def read(self):
            return {
                "config_hash_after": cli.file_sha256(manager.config_path),
                "auth_hash_after": cli.file_sha256(manager.auth_path),
                "prior_auth_json": None,
                "backup_paths": [],
                "codex_app_path": str(tmp_path / "Codex.app"),
            }
        def update(self, patch):
            self.patch = patch
        def delete(self):
            self.deleted = True

    store = Store()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager
    cli.restore_codex_enhancements = lambda app_path, item="all": {"status": "app_running_blocking_change"}

    exit_code = main(["logout", "--local-only"])

    data = parse_output(capsys)
    assert exit_code == 1
    assert data["status"] == "error_restore_failed"
    assert store.deleted is False
    assert store.patch == {"restore_status": "error_restore_failed"}


def test_logout_missing_config_backup_is_restore_failed(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True)
    manager.config_path.write_text('model_provider = "zhumeng-codex"\n', encoding="utf-8")
    manager.auth_path.write_text('{"OPENAI_API_KEY":"zhumeng-local-managed-secret"}', encoding="utf-8")

    class Store:
        def __init__(self):
            self.deleted = False
        def read(self):
            return {
                "config_hash_after": cli.file_sha256(manager.config_path),
                "auth_hash_after": cli.file_sha256(manager.auth_path),
                "backup_paths": [str(tmp_path / "config.toml.missing.bak")],
            }
        def update(self, patch):
            self.patch = patch
        def delete(self):
            self.deleted = True

    store = Store()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager

    exit_code = main(["logout", "--local-only"])

    data = parse_output(capsys)
    assert exit_code == 1
    assert data["status"] == "error_restore_failed"
    assert store.deleted is False
    assert manager.config_path.exists()


def test_logout_missing_managed_file_is_restore_conflict(capsys, tmp_path: Path):
    manager = cli.CodexConfigManager(tmp_path / ".codex")
    manager.config_path.parent.mkdir(parents=True)
    manager.config_path.write_text('model_provider = "zhumeng-codex"\n', encoding="utf-8")
    config_hash = cli.file_sha256(manager.config_path)
    manager.config_path.unlink()

    class Store:
        def __init__(self):
            self.deleted = False
        def read(self):
            return {"config_hash_after": config_hash, "backup_paths": []}
        def update(self, patch):
            self.patch = patch
        def delete(self):
            self.deleted = True

    store = Store()
    cli.default_state_store = lambda: store
    cli.default_config_manager = lambda: manager

    main(["logout", "--local-only"])

    data = parse_output(capsys)
    assert data["status"] == "restore_conflict"
    assert store.deleted is False


def test_codex_capture_report_flags_spawn_agent_model_override_mismatch(capsys, tmp_path: Path, monkeypatch):
    monkeypatch.setattr(cli, "default_capture_config", ORIGINAL_DEFAULT_CAPTURE_CONFIG)
    monkeypatch.setattr(cli, "generate_capture_report", ORIGINAL_GENERATE_CAPTURE_REPORT)
    codex_home = tmp_path / ".codex"
    catalog = codex_home / "zhumeng-codex-models.json"
    codex_home.mkdir(parents=True)
    catalog.write_text(json.dumps({
        "models": [
            {"slug": "deepseek-v4-pro"},
            {"slug": "deepseek-v4-flash"},
            {"slug": "claude-sonnet-4-6"},
        ]
    }), encoding="utf-8")
    (codex_home / "config.toml").write_text(f'model = "deepseek-v4-pro"\nmodel_catalog_json = "{catalog}"\n', encoding="utf-8")
    monkeypatch.setattr(cli, "default_config_manager", lambda: cli.CodexConfigManager(codex_home))
    trace_dir = tmp_path / "traces"
    trace_dir.mkdir()
    (trace_dir / "deferred_tool_search.jsonl").write_text(json.dumps({
        "event_type": "tool_search_output",
        "capture_ts": "2026-06-03T11:43:33.599Z",
        "tools": [{
            "type": "namespace",
            "name": "multi_agent_v1",
            "tools": [{
                "name": "spawn_agent",
                "input_schema": {
                    "type": "object",
                    "properties": {
                        "model": {"type": "string", "enum": ["claude-haiku-4-5", "claude-sonnet-4-6"]}
                    },
                },
            }],
        }],
    }) + "\n", encoding="utf-8")

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    mismatch = data["spawn_agent_model_override"]
    assert mismatch["spawn_agent_model_override_mismatch"] is True
    assert mismatch["catalog_has_deepseek"] is True
    assert mismatch["spawn_agent_has_deepseek"] is False
    assert mismatch["catalog_hash"]
    assert mismatch["catalog_mtime"]
    assert mismatch["capture_ts"] == "2026-06-03T11:43:33.599Z"
