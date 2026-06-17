from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
import platform
import re
import signal
import socket
import urllib.request
import subprocess
import sys
from datetime import UTC, datetime
from pathlib import Path
from typing import Sequence

from .adapters.codex.config_manager import CodexConfigManager, choose_local_proxy_port, discover_git_project_path
from .adapters.codex.capture_baseline import generate_capture_baseline
from .adapters.codex.capture_config import CodexDesktopCaptureConfig
from .adapters.codex.capture_config import CorrelationHasher
from .adapters.codex.capture_shape import build_spawn_agent_model_override_report, build_subagent_registration_report
from .adapters.codex.capture_injector import install_capture_hook, uninstall_capture_hook
from .adapters.codex.capture_injector import capture_installation_enabled, inject_capture_hook_via_cdp
from .adapters.codex.capture_injector import attach_capture_bridge_via_cdp
from .adapters.codex.capture_injector import route_capture_event
from .adapters.codex.capture_linker import load_jsonl
from .adapters.codex.capture_linker import link_traces, write_trace_links
from .adapters.codex.detect import resolve_codex_home
from .adapters.codex.launcher import build_codex_launch_command, detect_codex_app_path, select_cdp_port
from .adapters.codex.model_picker import ModelPickerPatchError, patch_model_picker_app
from .adapters.codex.model_picker import inspect_model_picker_app, restore_latest_model_picker_backup
from .adapters.codex.model_picker import inspect_plugin_auth_gate_app, patch_plugin_auth_gate_app
from .adapters.codex.model_picker import inspect_plugin_mention_marketplace_app, patch_plugin_mention_marketplace_app
from .adapters.codex.model_picker import restore_latest_plugin_auth_gate_backup, restore_latest_plugin_mention_marketplace_backup
from .adapters.codex.model_picker import codex_app_is_running
from .adapters.base import BaseAdapter
from .adapters.claude_code.launcher import run_managed_claude_code
from .adapters.claude_code.runtime_installer import (
    RuntimeInstallerError,
    apply_shell_alias_plan,
    build_managed_runtime_install_plan,
    build_shell_alias_plan,
    disable_managed_runtime,
    read_managed_runtime_status,
    write_managed_runtime_artifacts,
)
from .adapters.claude_code.live_matrix import (
    CP8LiveMatrixError,
    collect_cp8_live_provider_provenance,
    verify_cp8_live_matrix,
)
from .adapters.claude_code.status import derive_claude_code_operator_status
from .doctor import codex_doctor_report
from .desktop import run_desktop_command
from .diagnostics import desktop_diagnostic_report, public_state
from .adapters.codex.enhancements import inspect_codex_enhancements, patch_codex_enhancements, restore_codex_enhancements
from .http_client import AgentHTTPClient
from .platform_paths import state_dir
from .proxy.server import ManagedProxyConfig, ManagedProxyServer
from .security import generate_loopback_secret
from .deeplink import parse_zhumeng_deeplink
from .state import JsonStateStore, ensure_revoke_device_ready, logout_local_state

DEFAULT_CODEX_CONFIG_PROFILE = {
    "model_provider": "zhumeng-codex",
    "wire_api": "responses",
    "requires_openai_auth": True,
    "supports_websockets": False,
}

AGENT_VERSION = "0.1.0"
AGENT_SOURCE_ROOT = str(Path(__file__).resolve().parents[2])


def compute_runtime_signature() -> str:
    digest = hashlib.sha256()
    for path in sorted((Path(__file__).resolve().parents[1]).rglob("*.py")):
        digest.update(str(path.relative_to(Path(__file__).resolve().parents[1])).encode("utf-8"))
        digest.update(path.read_bytes())
    return digest.hexdigest()[:16]


AGENT_RUNTIME_SIGNATURE = compute_runtime_signature()


class CodexAdapterPlaceholder(BaseAdapter):
    client_name = "codex"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="zhumeng-agent")
    subparsers = parser.add_subparsers(dest="command", required=True)

    setup_parser = subparsers.add_parser("setup")
    setup_parser.add_argument("--client", required=True)
    setup_parser.add_argument("--code", required=True)
    setup_parser.add_argument("--server", required=True)

    launch_parser = subparsers.add_parser("launch")
    launch_parser.add_argument("client")
    launch_parser.add_argument("--dry-run", action="store_true")

    codex_parser = subparsers.add_parser("codex")
    codex_parser.add_argument("args", nargs=argparse.REMAINDER)

    claude_code_parser = subparsers.add_parser("claude-code")
    claude_code_subparsers = claude_code_parser.add_subparsers(dest="claude_code_command", required=True)
    claude_code_start = claude_code_subparsers.add_parser("start")
    claude_code_start.add_argument("--executable", default="claude")
    claude_code_start.add_argument("--state-root", type=Path, default=state_dir())
    claude_code_start.add_argument("--project-cwd", type=Path, default=Path.cwd())
    claude_code_start.add_argument("--guard-port", type=int)
    claude_code_start.add_argument("args", nargs=argparse.REMAINDER)
    claude_code_install = claude_code_subparsers.add_parser("install")
    claude_code_install.add_argument("--executable", default="claude")
    claude_code_install.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_status = claude_code_subparsers.add_parser("status")
    claude_code_status.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_doctor = claude_code_subparsers.add_parser("doctor")
    claude_code_doctor.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_restart = claude_code_subparsers.add_parser("restart")
    claude_code_restart.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_rollback = claude_code_subparsers.add_parser("rollback")
    claude_code_rollback.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_uninstall = claude_code_subparsers.add_parser("uninstall")
    claude_code_uninstall.add_argument("--runtime-root", type=Path, default=state_dir() / "runtimes")
    claude_code_alias = claude_code_subparsers.add_parser("alias")
    claude_code_alias_subparsers = claude_code_alias.add_subparsers(dest="alias_action", required=True)
    for action in ("enable", "disable", "status"):
        alias_parser = claude_code_alias_subparsers.add_parser(action)
        alias_parser.add_argument("--shell-rc", required=True, type=Path)
    claude_code_live_matrix = claude_code_subparsers.add_parser("live-matrix")
    claude_code_live_matrix.add_argument("--evidence", type=Path)
    claude_code_live_matrix.add_argument("--strict-live", action="store_true")
    claude_code_live_matrix.add_argument("--collect-provider-provenance", action="store_true")
    claude_code_live_matrix.add_argument("--run-id")
    claude_code_live_matrix.add_argument(
        "--output-root",
        type=Path,
        help="Dedicated CP8 evidence output directory; do not point this at a source worktree.",
    )

    status_parser = subparsers.add_parser("status")
    status_parser.add_argument("--json", action="store_true")

    doctor_parser = subparsers.add_parser("doctor")
    doctor_parser.add_argument("--json", action="store_true")

    repair_parser = subparsers.add_parser("repair")
    repair_parser.add_argument("client")

    logout_parser = subparsers.add_parser("logout")
    group = logout_parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--local-only", action="store_true")
    group.add_argument("--revoke-device", action="store_true")

    proxy_parser = subparsers.add_parser("proxy-serve")
    proxy_parser.add_argument("--state-file", required=True)

    capture_serve_parser = subparsers.add_parser("capture-serve")
    capture_serve_parser.add_argument("--trace-dir", required=True)
    capture_serve_parser.add_argument("--port", required=True, type=int)
    capture_serve_parser.add_argument("--correlation-hash-key-file")

    desktop_parser = subparsers.add_parser("desktop")
    desktop_parser.add_argument("args", nargs=argparse.REMAINDER)

    return parser


def normalize_passthrough(args: list[str]) -> list[str]:
    if args and args[0] == "--":
        return args[1:]
    return args


def emit(payload: dict[str, object]) -> int:
    print(json.dumps(payload))
    return 0


def emit_failed(payload: dict[str, object]) -> int:
    print(json.dumps(payload))
    return 1


def default_state_store() -> JsonStateStore:
    return JsonStateStore(state_dir() / "state.json")


def default_http_client(server: str) -> AgentHTTPClient:
    return AgentHTTPClient(server)


def default_config_manager() -> CodexConfigManager:
    return CodexConfigManager()


def current_trusted_project_paths() -> list[Path]:
    project = discover_git_project_path()
    return [project] if project is not None else []


def default_capture_config(correlation_hash_key_file: Path | None = None) -> CodexDesktopCaptureConfig:
    env_key = os.environ.get("ZHUMENG_CODEX_DESKTOP_CAPTURE_CORRELATION_HASH_KEY_FILE")
    env_enabled = os.environ.get("ZHUMENG_CODEX_DESKTOP_CAPTURE_ENABLED")
    state = default_state_store().read()
    enabled = bool(state.get("desktop_capture_enabled", False))
    if env_enabled is not None:
        enabled = env_enabled.strip().lower() in {"1", "true", "yes", "on"}
    state_key = state.get("desktop_capture_correlation_hash_key_file")
    configured_key = correlation_hash_key_file or (Path(str(state_key)).expanduser() if state_key else None)
    return CodexDesktopCaptureConfig.defaults(
        enabled=enabled,
        correlation_hash_key_file=configured_key or (Path(env_key).expanduser() if env_key else None),
    )


def run_codex_process(args: list[str], env: dict[str, str]) -> int:
    return subprocess.call(["codex", *args], env=env)


def launch_codex_process(command: list[str]) -> None:
    subprocess.Popen(command)


def launch_claude_code_process(
    command: list[str],
    env: dict[str, str] | None = None,
    cwd: Path | None = None,
    *,
    detach_stdio: bool = False,
):
    if detach_stdio:
        return subprocess.Popen(
            command,
            env=env,
            cwd=str(cwd) if cwd is not None else None,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
    return subprocess.Popen(command, env=env, cwd=str(cwd) if cwd is not None else None)


def default_codex_app_path() -> Path | None:
    return detect_codex_app_path(
        search_roots=[Path("/Applications"), Path.home() / "Applications"],
        platform=platform.system().lower().replace("windows", "win32"),
    )



def remember_desktop_enhancement_state(store: JsonStateStore, desktop_patches: dict[str, object]) -> None:
    app_path = desktop_patches.get("app_path")
    if not app_path:
        return
    patch: dict[str, object] = {"codex_app_path": str(app_path), "desktop_enhancements": desktop_patches}
    if desktop_patches.get("restart_required"):
        patch["restart_required"] = True
    if hasattr(store, "update"):
        store.update(patch)


def refresh_desktop_enhancement_state(state: dict[str, object], store: JsonStateStore | None = None) -> dict[str, object]:
    if state.get("client") != "codex":
        return state
    app_path_value = state.get("codex_app_path")
    app_path = Path(str(app_path_value)).expanduser() if app_path_value else default_codex_app_path()
    if app_path is None:
        return state
    try:
        enhancements = inspect_codex_enhancements(app_path)
    except Exception:
        return state
    if not isinstance(enhancements, dict) or enhancements.get("status") != "ok":
        return state
    state["codex_app_path"] = str(app_path)
    state["desktop_enhancements"] = enhancements
    if store is not None and hasattr(store, "update"):
        store.update({"codex_app_path": str(app_path), "desktop_enhancements": enhancements})
    return state


def patch_detected_codex_model_picker() -> dict[str, object]:
    app_path = default_codex_app_path()
    if app_path is None:
        return {"status": "app_not_found"}
    return patch_model_picker_app(app_path)


def patch_detected_codex_desktop() -> dict[str, object]:
    app_path = default_codex_app_path()
    if app_path is None:
        return {
            "app_path": None,
            "status": "app_not_found",
            "restart_required": False,
            "model_picker": {"status": "app_not_found"},
            "plugin_auth_gate": {"status": "app_not_found"},
            "plugin_mention_marketplace": {"status": "app_not_found"},
        }
    aggregate = patch_codex_enhancements(app_path, item="all")
    items = aggregate.get("items", {}) if isinstance(aggregate.get("items"), dict) else {}
    return {
        **aggregate,
        "app_path": str(app_path),
        "model_picker": items.get("model-picker", {"status": aggregate.get("status", "failed")}),
        "plugin_auth_gate": items.get("plugin-auth-gate", {"status": aggregate.get("status", "failed")}),
        "plugin_mention_marketplace": items.get("plugin-mention-marketplace", {"status": aggregate.get("status", "failed")}),
    }


def run_desktop_patch(operation) -> dict[str, object]:
    try:
        return operation()
    except ModelPickerPatchError as err:
        return {
            "status": "failed",
            "message": str(err),
        }


def is_process_alive(pid: int | None) -> bool:
    if not pid or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def is_loopback_port_accepting_connections(port: int) -> bool:
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=0.25):
            return True
    except OSError:
        return False


def terminate_proxy_process(pid: int | None) -> None:
    if not pid or pid <= 0:
        return
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        return


def proxy_process_pids_for_state_file(state_file: Path) -> list[int]:
    try:
        output = subprocess.check_output(["ps", "-axo", "pid=,command="], text=True)
    except (OSError, subprocess.SubprocessError):
        return []
    state_arg = str(state_file)
    pids: list[int] = []
    for line in output.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        pid_text, _, command = stripped.partition(" ")
        try:
            pid = int(pid_text)
        except ValueError:
            continue
        if (
            "zhumeng_agent" in command
            and "proxy-serve" in command
            and "--state-file" in command
            and state_arg in command
        ):
            pids.append(pid)
    return pids


def terminate_duplicate_proxy_processes(state_file: Path, keep_pid: int | None) -> None:
    if keep_pid is None:
        return
    for pid in proxy_process_pids_for_state_file(state_file):
        if pid == keep_pid:
            continue
        if is_process_alive(pid):
            terminate_proxy_process(pid)


def ensure_proxy_running(store: JsonStateStore) -> int:
    state = store.read()
    required = ("gateway_base_url", "device_id", "managed_session_id", "access_token", "loopback_secret", "proxy_port")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"proxy state is incomplete: missing {', '.join(missing)}")

    pid = int(state.get("proxy_pid", 0) or 0)
    if is_process_alive(pid) and proxy_matches_current_runtime(proxy_port := int(state["proxy_port"])):
        terminate_duplicate_proxy_processes(store.path, keep_pid=pid)
        return pid
    proxy_port = int(state["proxy_port"])
    if is_process_alive(pid):
        terminate_proxy_process(pid)
    if is_loopback_port_accepting_connections(proxy_port) and proxy_matches_current_runtime(proxy_port):
        terminate_duplicate_proxy_processes(store.path, keep_pid=pid if is_process_alive(pid) else None)
        return 0

    process = subprocess.Popen([
        sys.executable,
        "-m",
        "zhumeng_agent",
        "proxy-serve",
        "--state-file",
        str(store.path),
    ], start_new_session=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    store.update({"proxy_pid": process.pid})
    terminate_duplicate_proxy_processes(store.path, keep_pid=int(process.pid))
    return int(process.pid)


def proxy_matches_current_runtime(port: int) -> bool:
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/__zhumeng/health", timeout=0.5) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except Exception:
        return False
    return (
        payload.get("agent_version") == AGENT_VERSION
        and payload.get("runtime_signature") == AGENT_RUNTIME_SIGNATURE
        and str(payload.get("source_root", "")) == AGENT_SOURCE_ROOT
    )


def load_managed_state(store: JsonStateStore) -> dict[str, object]:
    state = store.read()
    required = ("config_profile", "proxy_port", "loopback_secret")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"managed setup is incomplete: missing {', '.join(missing)}")
    return state


def fetch_codex_model_catalog(client, config_manager: CodexConfigManager, state: dict[str, object], store=None) -> tuple[dict[str, object], dict[str, object]]:
    def existing_catalog() -> dict[str, object]:
        read_existing = getattr(config_manager, "read_existing_model_catalog", None)
        if read_existing is None:
            return {"models": []}
        return read_existing(state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE))

    def persist_refreshed_state(patch: dict[str, object]) -> None:
        if store is None:
            return
        update = getattr(store, "update", None)
        if update is not None:
            update(patch)
            return
        write = getattr(store, "write", None)
        read = getattr(store, "read", None)
        if write is not None and read is not None:
            current = dict(read() or {})
            current.update(patch)
            write(current)

    def try_list_models() -> dict[str, object]:
        return list_models(
            gateway_base_url=str(state["gateway_base_url"]),
            access_token=str(state["access_token"]),
            managed_session_id=str(state["managed_session_id"]),
            device_id=int(state["device_id"]),
            catalog_format="codex_cli",
        )

    list_models = getattr(client, "list_codex_models", None)
    if list_models is None:
        return existing_catalog(), {"source": "existing", "reason": "list_models_unavailable"}
    required = ("gateway_base_url", "access_token", "managed_session_id", "device_id")
    if any(not state.get(key) for key in required):
        return existing_catalog(), {"source": "existing", "reason": "managed_state_incomplete"}
    try:
        payload = try_list_models()
    except Exception as err:
        response = getattr(err, "response", None)
        if getattr(response, "status_code", None) in {401, 403}:
            refresh_device_token = getattr(client, "refresh_device_token", None)
            refresh_token = str(state.get("refresh_token", "") or "")
            if refresh_device_token is not None and refresh_token:
                try:
                    refreshed = refresh_device_token(
                        device_id=int(state["device_id"]),
                        refresh_token=refresh_token,
                    )
                    patch = {
                        "access_token": refreshed["access_token"],
                        "refresh_token": refreshed["refresh_token"],
                        "managed_session_id": refreshed["managed_session_id"],
                        "status": "configured",
                    }
                    state.update(patch)
                    persist_refreshed_state(patch)
                    payload = try_list_models()
                    return config_manager.build_model_catalog(payload), {
                        "source": "gateway",
                        "refreshed": True,
                    }
                except Exception as refresh_err:
                    refresh_response = getattr(refresh_err, "response", None)
                    return existing_catalog(), {
                        "source": "existing",
                        "reason": "upstream_unauthorized",
                        "status_code": getattr(response, "status_code", None),
                        "refresh_status_code": getattr(refresh_response, "status_code", None),
                        "refresh_error": str(refresh_err),
                    }
            return existing_catalog(), {"source": "existing", "reason": "upstream_unauthorized", "status_code": getattr(response, "status_code", None)}
        return existing_catalog(), {
            "source": "existing",
            "reason": "upstream_fetch_failed",
            "status_code": getattr(response, "status_code", None),
            "error": str(err),
        }
    return config_manager.build_model_catalog(payload), {"source": "gateway"}



def file_sha256(path: Path) -> str | None:
    if not path.exists():
        return None
    return hashlib.sha256(path.read_bytes()).hexdigest()


def codex_restart_required_after_config_change(config_hash_before: str | None, config_hash_after: str | None) -> bool:
    if not config_hash_before or not config_hash_after or config_hash_before == config_hash_after:
        return False
    app_path = default_codex_app_path()
    return bool(app_path is not None and codex_app_is_running(app_path))


def server_native_attestation_secret(exchanged: dict[str, object], *, existing_state: dict[str, object] | None = None) -> str:
    secret = str(exchanged.get("claude_code_native_attestation_secret") or "").strip()
    if secret:
        return secret
    current = existing_state or {}
    if (
        str(current.get("claude_code_native_attestation_secret_source") or "").strip().lower() == "server"
        and str(current.get("claude_code_native_attestation_secret") or "").strip()
    ):
        return str(current["claude_code_native_attestation_secret"])
    raise ValueError("managed setup is incomplete: missing server-provisioned claude_code_native_attestation_secret")


def require_server_native_attestation_secret(state: dict[str, object]) -> str:
    secret = str(state.get("claude_code_native_attestation_secret") or "").strip()
    source = str(state.get("claude_code_native_attestation_secret_source") or "").strip().lower()
    if secret and source == "server":
        return secret
    raise ValueError("managed setup is incomplete: missing server-provisioned claude_code_native_attestation_secret")


def server_route_hint_secret(state: dict[str, object]) -> str | None:
    secret = str(state.get("claude_code_route_hint_secret") or "").strip()
    source = str(state.get("claude_code_route_hint_secret_source") or "").strip().lower()
    if secret and source == "server":
        return secret
    return None


def require_server_route_hint_secret(state: dict[str, object]) -> str:
    secret = server_route_hint_secret(state)
    if secret:
        return secret
    raise ValueError("managed setup is incomplete: missing server-provisioned claude_code_route_hint_secret")


def setup_managed_client(client_name: str, code: str, server: str) -> dict[str, object]:
    if client_name != "codex":
        raise ValueError(f"unsupported client: {client_name}")
    client = default_http_client(server)
    exchanged = client.exchange_setup_grant(code=code, server_origin=server, client=client_name)
    loopback_secret = generate_loopback_secret()
    claude_code_native_attestation_secret = server_native_attestation_secret(exchanged)
    proxy_port = choose_local_proxy_port()
    config_manager = default_config_manager()
    prior_auth_json = config_manager.auth_path.read_text(encoding="utf-8") if config_manager.auth_path.exists() else None
    catalog_path = config_manager.catalog_path_for_profile(exchanged.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE))
    prior_catalog_json = catalog_path.read_text(encoding="utf-8") if catalog_path.exists() else None
    setup_state = {
        "gateway_base_url": exchanged["gateway_base_url"],
        "device_id": exchanged["device_id"],
        "managed_session_id": exchanged["managed_session_id"],
        "access_token": exchanged["access_token"],
    }
    model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, setup_state)
    plan = config_manager.plan_configure(
        exchanged["config_profile"],
        proxy_port,
        loopback_secret,
        model_catalog,
        trusted_project_paths=current_trusted_project_paths(),
    )
    config_hash_before = file_sha256(config_manager.config_path)
    auth_hash_before = file_sha256(config_manager.auth_path)
    catalog_hash_before = file_sha256(plan.model_catalog_path)
    config_manager.apply_configure(plan)
    config_hash_after = file_sha256(config_manager.config_path)
    auth_hash_after = file_sha256(config_manager.auth_path)
    catalog_hash_after = file_sha256(plan.model_catalog_path)
    restart_required = codex_restart_required_after_config_change(config_hash_before, config_hash_after)
    store = default_state_store()
    state_payload = {
        "client": client_name,
        "server_base_url": exchanged["server_base_url"],
        "gateway_base_url": exchanged["gateway_base_url"],
        "device_id": exchanged["device_id"],
        "managed_session_id": exchanged["managed_session_id"],
        "access_token": exchanged["access_token"],
        "refresh_token": exchanged["refresh_token"],
        "config_profile": exchanged["config_profile"],
        "proxy_port": proxy_port,
        "loopback_secret": loopback_secret,
        "claude_code_native_attestation_secret": claude_code_native_attestation_secret,
        "claude_code_native_attestation_secret_source": "server",
        "claude_code_route_hint_secret": str(exchanged.get("claude_code_route_hint_secret") or "").strip(),
        "claude_code_route_hint_secret_source": "server" if str(exchanged.get("claude_code_route_hint_secret") or "").strip() else "",
        "backup_paths": [str(path) for path in plan.backup_paths],
        "prior_auth_json": prior_auth_json,
        "prior_catalog_json": prior_catalog_json,
        "catalog_path": str(plan.model_catalog_path),
        "catalog_preexisting": prior_catalog_json is not None,
        "config_hash_before": config_hash_before,
        "auth_hash_before": auth_hash_before,
        "catalog_hash_before": catalog_hash_before,
        "config_hash_after": config_hash_after,
        "auth_hash_after": auth_hash_after,
        "catalog_hash_after": catalog_hash_after,
        "desktop_capture_enabled": False,
        "model_catalog_meta": model_catalog_meta,
        "restart_required": restart_required,
        "status": "configured",
    }
    store.write(state_payload)
    proxy_pid = ensure_proxy_running(store)
    state_payload["proxy_pid"] = proxy_pid
    return {
        "client": client_name,
        "server": server,
        "code_redacted": True,
        "status": "configured",
        "proxy_port": proxy_port,
        "proxy_pid": proxy_pid,
        "device_id": exchanged["device_id"],
        "model_catalog": model_catalog_meta,
        **build_desktop_status(state_payload),
    }



def build_desktop_status(state: dict[str, object]) -> dict[str, object]:
    status = str(state.get("status", "not_configured"))
    enhancements = state.get("desktop_enhancements", {})
    if not isinstance(enhancements, dict):
        enhancements = {}
    model_picker = desktop_enhancement_item(enhancements, "model_picker", "model-picker")
    plugin_auth_gate = desktop_enhancement_item(enhancements, "plugin_auth_gate", "plugin-auth-gate")
    plugin_mention_marketplace = desktop_enhancement_item(
        enhancements,
        "plugin_mention_marketplace",
        "plugin-mention-marketplace",
    )
    claude_code_status = derive_claude_code_operator_status(state, process_alive=is_process_alive).to_safe_dict()
    adapters = {
        "codex": {
            "status": status if state.get("client") == "codex" else "not_configured",
            "enhancements": enhancements,
            "restart_required": bool(state.get("restart_required")),
        },
        "claude_code": claude_code_status,
    }
    return {
        "status": status,
        "global_status": status,
        "proxy": {
            "status": "configured" if state.get("proxy_port") else "not_configured",
            "port": state.get("proxy_port"),
            "pid": state.get("proxy_pid"),
        },
        "backend": {
            "server_base_url": state.get("server_base_url"),
            "gateway_base_url": state.get("gateway_base_url"),
        },
        "authorization": {
            "status": status,
            "device_id": state.get("device_id"),
            "managed_session_id_redacted": public_state(state).get("managed_session_id_redacted"),
        },
        "adapters": adapters,
        "claude_code": claude_code_status,
        "model_picker": model_picker,
        "plugin_auth_gate": plugin_auth_gate,
        "plugin_mention_marketplace": plugin_mention_marketplace,
        "model_catalog": state.get("model_catalog_meta", {}),
        "state": public_state(state),
    }


def desktop_enhancement_item(enhancements: dict[str, object], key: str, item_key: str) -> dict[str, object]:
    direct = enhancements.get(key)
    if isinstance(direct, dict):
        return direct
    items = enhancements.get("items")
    if isinstance(items, dict):
        item = items.get(item_key)
        if isinstance(item, dict):
            return item
    return {}


def reauth_managed_client(client_name: str, code: str, server: str) -> dict[str, object]:
    if client_name != "codex":
        raise ValueError(f"unsupported client: {client_name}")
    store = default_state_store()
    current = store.read()
    if not current:
        raise ValueError("managed setup is incomplete: missing existing state")
    client = default_http_client(server)
    exchanged = client.exchange_setup_grant(code=code, server_origin=server, client=client_name)
    config_manager = default_config_manager()
    profile = exchanged.get("config_profile") or current.get("config_profile") or DEFAULT_CODEX_CONFIG_PROFILE
    setup_state = {
        "gateway_base_url": exchanged["gateway_base_url"],
        "device_id": exchanged["device_id"],
        "managed_session_id": exchanged["managed_session_id"],
        "access_token": exchanged["access_token"],
    }
    model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, setup_state)
    proxy_port = int(current.get("proxy_port", choose_local_proxy_port()))
    loopback_secret = str(current.get("loopback_secret") or generate_loopback_secret())
    claude_code_native_attestation_secret = server_native_attestation_secret(exchanged, existing_state=current)
    config_hash_before = file_sha256(config_manager.config_path)
    config_manager.repair(profile, proxy_port, loopback_secret, model_catalog, trusted_project_paths=current_trusted_project_paths())
    config_hash_after = file_sha256(config_manager.config_path)
    route_hint_secret = str(exchanged.get("claude_code_route_hint_secret") or "").strip()
    if not route_hint_secret:
        route_hint_secret = server_route_hint_secret(current) or ""
    route_hint_secret_source = "server" if route_hint_secret else ""
    patch = {
        "client": client_name,
        "server_base_url": exchanged["server_base_url"],
        "gateway_base_url": exchanged["gateway_base_url"],
        "device_id": exchanged["device_id"],
        "managed_session_id": exchanged["managed_session_id"],
        "access_token": exchanged["access_token"],
        "refresh_token": exchanged["refresh_token"],
        "config_profile": profile,
        "proxy_port": proxy_port,
        "loopback_secret": loopback_secret,
        "claude_code_native_attestation_secret": claude_code_native_attestation_secret,
        "claude_code_native_attestation_secret_source": "server",
        "claude_code_route_hint_secret": route_hint_secret,
        "claude_code_route_hint_secret_source": route_hint_secret_source,
        "model_catalog_meta": model_catalog_meta,
        "config_hash_after": config_hash_after,
        "auth_hash_after": file_sha256(config_manager.auth_path),
        "catalog_hash_after": file_sha256(config_manager.catalog_path_for_profile(profile)),
        "restart_required": codex_restart_required_after_config_change(config_hash_before, config_hash_after),
        "status": "configured",
    }
    updated = store.update(patch)
    proxy_pid = ensure_proxy_running(store)
    updated.update({"proxy_pid": proxy_pid})
    return {"status": "reauthorized", **build_desktop_status(updated)}

def desktop_status_data() -> dict[str, object]:
    store = default_state_store()
    state = store.read()
    state = refresh_desktop_enhancement_state(state, store)
    if hasattr(store, "path"):
        state["state_file"] = str(store.path)
    return build_desktop_status(state)


def desktop_diagnose_data() -> dict[str, object]:
    store = default_state_store()
    state = store.read()
    if hasattr(store, "path"):
        state["state_file"] = str(store.path)
    codex_home = resolve_codex_home()
    return desktop_diagnostic_report(
        state=state,
        doctor=codex_doctor_report(codex_home, codex_app_path=default_codex_app_path(), state=state),
        codex_home=codex_home,
    )


def desktop_open_app(app: str) -> dict[str, object]:
    if app == "zhumeng-claude":
        command = [sys.executable, "-m", "zhumeng_agent", "claude-code", "start"]
        process = launch_claude_code_process(
            command,
            env=merge_env_no_proxy(dict(os.environ)),
            cwd=Path.cwd(),
            detach_stdio=True,
        )
        return {
            "status": "started",
            "app": app,
            "pid": int(getattr(process, "pid", 0) or 0),
            "launch_command": command,
            "nonblocking": True,
            "stdio_detached": True,
            "official_claude_unaffected": True,
        }
    if app == "claude-code":
        return build_claude_code_start_payload(
            executable="claude",
            state_root=state_dir(),
            project_cwd=Path.cwd(),
            guard_port=None,
            argv=[],
        )
    if app != "codex":
        raise ValueError(f"unsupported app: {app}")
    app_path = default_codex_app_path()
    if app_path is None:
        return {"status": "app_not_found", "app": app}
    command = build_codex_launch_command(app_path, select_cdp_port())
    launch_codex_process(command)
    return {"status": "launched", "app": app, "app_path": str(app_path), "launch_command": command}



def desktop_patch_enhancements(app_path: Path, item: str) -> dict[str, object]:
    result = patch_codex_enhancements(app_path, item=item)
    remember_desktop_enhancement_state(default_state_store(), result)
    return result


def desktop_restore_enhancements(app_path: Path, item: str) -> dict[str, object]:
    result = restore_codex_enhancements(app_path, item=item)
    if result.get("status") == "restored" and hasattr(default_state_store(), "update"):
        default_state_store().update({"desktop_enhancements_restored": True, "restart_required": bool(result.get("restart_required"))})
    return result


def codex_model_catalog_summary(
    catalog: dict[str, object],
    *,
    catalog_path: Path,
    last_synced_at: object = None,
    source: str | None = None,
    include_models: bool = False,
) -> dict[str, object]:
    models = catalog.get("models", [])
    if not isinstance(models, list):
        models = []
    model_count = 0
    main_list_count = 0
    restricted_count = 0
    incompatible_count = 0
    missing_pricing_count = 0
    for model in models:
        if not isinstance(model, dict):
            continue
        model_count += 1
        compatible = codex_model_is_compatible(model)
        if not compatible:
            incompatible_count += 1
        if codex_model_in_main_list(model) and compatible:
            main_list_count += 1
        elif compatible:
            restricted_count += 1
        if codex_model_pricing_missing(model.get("pricing")):
            missing_pricing_count += 1
    summary: dict[str, object] = {
        "status": "synced" if model_count else "empty",
        "model_count": model_count,
        "main_list_count": main_list_count,
        "restricted_count": restricted_count,
        "incompatible_count": incompatible_count,
        "missing_pricing_count": missing_pricing_count,
        "last_synced_at": last_synced_at,
        "catalog_path": str(catalog_path),
        "source": source,
    }
    if include_models:
        summary["models"] = [model for model in models if isinstance(model, dict)]
    return summary


def codex_model_in_main_list(model: dict[str, object]) -> bool:
    visibility = str(model.get("visibility", "list") or "list").strip().lower()
    return bool(model.get("supported_in_api", True)) and visibility in {"list", "visible"}


def codex_model_is_compatible(model: dict[str, object]) -> bool:
    capabilities = model.get("capabilities")
    if not isinstance(capabilities, dict):
        return False
    return all(bool(capabilities.get(key)) for key in ("responses", "streaming", "tool_calls", "context_continuation"))


def codex_model_pricing_missing(pricing: object) -> bool:
    if not isinstance(pricing, dict):
        return True
    return not any(pricing.get(key) for key in ("input_price", "output_price", "cached_input_price", "cache_write_price"))


def desktop_models_status(client_name: str) -> dict[str, object]:
    if client_name != "codex":
        raise ValueError(f"unsupported client: {client_name}")
    store = default_state_store()
    state = store.read()
    config_manager = default_config_manager()
    profile = state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE)
    catalog = config_manager.read_existing_model_catalog(profile)
    catalog_path = config_manager.catalog_path_for_profile(profile)
    return codex_model_catalog_summary(
        catalog,
        catalog_path=catalog_path,
        last_synced_at=state.get("model_catalog_synced_at"),
        source=str(state.get("model_catalog_meta", {}).get("source", "local")) if isinstance(state.get("model_catalog_meta"), dict) else "local",
        include_models=True,
    )


def desktop_models_sync(client_name: str) -> dict[str, object]:
    if client_name != "codex":
        raise ValueError(f"unsupported client: {client_name}")
    store = default_state_store()
    state = store.read()
    required = ("gateway_base_url", "access_token", "managed_session_id", "device_id")
    missing = [key for key in required if not state.get(key)]
    if missing:
        return {"status": "not_configured", "message": f"managed model sync is incomplete: missing {', '.join(missing)}"}
    config_manager = default_config_manager()
    client = default_http_client(str(state.get("server_base_url", "")))
    catalog, meta = fetch_codex_model_catalog(client, config_manager, state, store)
    profile = state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE)
    saved = config_manager.write_model_catalog(profile, catalog)
    catalog_path = config_manager.catalog_path_for_profile(profile)
    synced_at = datetime.now(UTC).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    patch = {
        "model_catalog_meta": meta,
        "model_catalog_synced_at": synced_at,
        "catalog_path": str(catalog_path),
        "catalog_hash_after": file_sha256(catalog_path),
    }
    updated = store.update(patch) if hasattr(store, "update") else {**state, **patch}
    return codex_model_catalog_summary(
        saved,
        catalog_path=catalog_path,
        last_synced_at=updated.get("model_catalog_synced_at"),
        source=str(meta.get("source", "gateway")),
    )

def desktop_repair_client(client_name: str) -> dict[str, object]:
    if client_name != "codex":
        raise ValueError(f"unsupported client: {client_name}")
    store = default_state_store()
    state = load_managed_state(store)
    config_manager = default_config_manager()
    client = default_http_client(str(state.get("server_base_url", "")))
    model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, state, store)
    config_manager.repair(
        state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE),
        int(state.get("proxy_port", choose_local_proxy_port())),
        str(state.get("loopback_secret", generate_loopback_secret())),
        model_catalog,
        trusted_project_paths=current_trusted_project_paths(),
    )
    proxy_pid = ensure_proxy_running(store)
    patch = {"status": "configured", "proxy_pid": proxy_pid}
    app_path = default_codex_app_path()
    enhancements = None
    if app_path is not None:
        enhancements = patch_codex_enhancements(app_path, item="all")
        patch["desktop_enhancements"] = enhancements
        patch["restart_required"] = bool(enhancements.get("restart_required"))
    updated = store.update(patch)
    status = "repaired"
    if enhancements and enhancements.get("status") in {"app_running_blocking_change", "app_bundle_not_writable", "failed"}:
        status = str(enhancements.get("status"))
    status_data = build_desktop_status(updated)
    status_data["status"] = status
    return {**status_data, "client": client_name, "proxy_pid": proxy_pid, "model_catalog": model_catalog_meta, "enhancements": enhancements}

def main(argv: Sequence[str] | None = None) -> int:
    argv_list = list(argv) if argv is not None else list(sys.argv[1:])
    if argv_list and len(argv_list) == 1 and argv_list[0].startswith("zhumeng-agent://"):
        deeplink = parse_zhumeng_deeplink(argv_list[0])
        action = deeplink.get("action", "setup")
        if action == "setup":
            argv_list = [
                "setup",
                "--client", deeplink["client"],
                "--code", deeplink["code"],
                "--server", deeplink["server"],
            ]
        elif action == "reauth":
            argv_list = [
                "desktop", "reauth",
                "--client", deeplink["client"],
                "--code", deeplink["code"],
                "--server", deeplink["server"],
                "--json",
            ]
        elif action == "open":
            argv_list = ["desktop", "open", "--app", deeplink["app"], "--json"]

    parser = build_parser()
    args = parser.parse_args(argv_list)

    if args.command == "desktop":
        return run_desktop_command(args.args, {
            "status": desktop_status_data,
            "setup": setup_managed_client,
            "reauth": reauth_managed_client,
            "open": desktop_open_app,
            "repair": desktop_repair_client,
            "diagnose": desktop_diagnose_data,
            "enhancements_status": inspect_codex_enhancements,
            "enhancements_patch": desktop_patch_enhancements,
            "enhancements_restore": desktop_restore_enhancements,
            "models_status": desktop_models_status,
            "models_sync": desktop_models_sync,
        })

    if args.command == "setup":
        try:
            result = setup_managed_client(args.client, args.code, args.server)
        except ValueError as err:
            return emit_failed({
                "command": "setup",
                "client": args.client,
                "server": args.server,
                "status": "not_configured",
                "message": str(err),
            })
        return emit({
            "command": "setup",
            **result,
        })

    if args.command == "launch":
        adapter = CodexAdapterPlaceholder()
        store = default_state_store()
        try:
            state = load_managed_state(store)
        except ValueError as err:
            return emit({
                "command": "launch",
                "client": args.client,
                "status": "not_configured",
                "message": str(err),
            })
        if args.dry_run:
            return emit({
                "command": "launch",
                "client": args.client,
                "dry_run": True,
                "status": "planned",
                "proxy_port": state.get("proxy_port"),
                "repair": True,
                "launch": True,
                "injection": True,
            })
        config_manager = default_config_manager()
        client = default_http_client(str(state.get("server_base_url", "")))
        model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, state, store)
        config_manager.repair(
            state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
            model_catalog,
            trusted_project_paths=current_trusted_project_paths(),
        )
        proxy_pid = ensure_proxy_running(store)
        store.update({"status": "configured", "proxy_pid": proxy_pid})
        desktop_patches = patch_detected_codex_desktop()
        remember_desktop_enhancement_state(store, desktop_patches)
        search_roots = [Path("/Applications"), Path.home() / "Applications"]
        app_path = detect_codex_app_path(search_roots=search_roots, platform=platform.system().lower().replace("windows", "win32"))
        if app_path is not None:
            cdp_port = select_cdp_port()
            command = build_codex_launch_command(app_path, cdp_port)
            launch_codex_process(command)
            capture_status: dict[str, object] = {"status": "not_installed"}
            capture_config = default_capture_config()
            capture_installed = capture_installation_enabled(app_path, capture_config)
            if not capture_config.enabled and capture_installed:
                capture_status = {"status": "installed_but_disabled"}
            elif not capture_config.enabled:
                capture_status = {"status": "disabled"}
            elif capture_config.enabled and capture_installed:
                capture_status = ensure_capture_bridge_running(capture_config, cdp_port)
            return emit({
                "command": "launch",
                "client": args.client,
                "status": "launched",
                "launch_command": command,
                "injection": "enabled",
                "desktop_patches": desktop_patches,
                "model_picker": desktop_patches["model_picker"],
                "plugin_auth_gate": desktop_patches["plugin_auth_gate"],
                "plugin_mention_marketplace": desktop_patches["plugin_mention_marketplace"],
                "capture": capture_status,
                "model_catalog": model_catalog_meta,
            })
        result = adapter.launch(dry_run=False)
        return emit({
            "command": "launch",
            **result.to_dict(),
        })

    if args.command == "claude-code":
        if args.claude_code_command == "start":
            return handle_claude_code_start(args)
        if args.claude_code_command in {"install", "status", "doctor", "restart", "rollback", "uninstall", "alias", "live-matrix"}:
            return handle_claude_code_runtime_command(args)
        parser.error("unknown claude-code command")

    if args.command == "codex":
        if args.args and args.args[0] == "model-picker":
            return handle_codex_model_picker(args.args[1:])
        if args.args and args.args[0] == "plugin-auth-gate":
            return handle_codex_plugin_auth_gate(args.args[1:])
        if args.args and args.args[0] == "plugin-mention-marketplace":
            return handle_codex_plugin_mention_marketplace(args.args[1:])
        if args.args and args.args[0] == "capture":
            return handle_codex_capture(args.args[1:])
        store = default_state_store()
        try:
            state = load_managed_state(store)
        except ValueError as err:
            return emit({
                "command": "codex",
                "args": normalize_passthrough(args.args),
                "status": "not_configured",
                "message": str(err),
            })
        env = merge_env_no_proxy(dict(os.environ))
        config_manager = default_config_manager()
        client = default_http_client(str(state.get("server_base_url", "")))
        model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, state, store)
        config_manager.repair(
            state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
            model_catalog,
            trusted_project_paths=current_trusted_project_paths(),
        )
        proxy_pid = ensure_proxy_running(store)
        store.update({"status": "configured", "proxy_pid": proxy_pid})
        passthrough_args = normalize_passthrough(args.args)
        return_code = run_codex_process(passthrough_args, env)
        return emit({
            "command": "codex",
            "args": passthrough_args,
            "status": "executed",
            "returncode": return_code,
            "proxy_port": state.get("proxy_port"),
            "NO_PROXY": env.get("NO_PROXY"),
            "model_catalog": model_catalog_meta,
        })

    if args.command == "status":
        state = default_state_store().read()
        return emit({
            "command": "status",
            "format": "json" if args.json else "text",
            **build_desktop_status(state),
        })

    if args.command == "doctor":
        report = codex_doctor_report(resolve_codex_home(), codex_app_path=default_codex_app_path())
        report["command"] = "doctor"
        report["format"] = "json" if args.json else "text"
        return emit(report)

    if args.command == "repair":
        store = default_state_store()
        try:
            state = load_managed_state(store)
        except ValueError as err:
            return emit({
                "command": "repair",
                "client": args.client,
                "status": "not_configured",
                "message": str(err),
            })
        config_manager = default_config_manager()
        client = default_http_client(str(state.get("server_base_url", "")))
        model_catalog, model_catalog_meta = fetch_codex_model_catalog(client, config_manager, state, store)
        config_manager.repair(
            state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
            model_catalog,
            trusted_project_paths=current_trusted_project_paths(),
        )
        proxy_pid = ensure_proxy_running(store)
        store.update({"status": "configured", "proxy_pid": proxy_pid})
        desktop_patches = patch_detected_codex_desktop()
        remember_desktop_enhancement_state(store, desktop_patches)
        return emit({
            "command": "repair",
            "client": args.client,
            "status": "repaired",
            "desktop_patches": desktop_patches,
            "model_picker": desktop_patches["model_picker"],
            "plugin_auth_gate": desktop_patches["plugin_auth_gate"],
            "plugin_mention_marketplace": desktop_patches["plugin_mention_marketplace"],
            "model_catalog": model_catalog_meta,
        })

    if args.command == "logout":
        store = default_state_store()
        state = store.read()
        if args.local_only:
            restore_result = restore_local_managed_config(default_config_manager(), state)
            if not restore_result.get("ok"):
                store.update({"restore_status": restore_result.get("status")}) if hasattr(store, "update") else None
                return emit_failed({
                    "command": "logout",
                    "mode": "local_only",
                    **restore_result,
                })
            logout_local_state(store)
            return emit({
                "command": "logout",
                "mode": "local_only",
                **restore_result,
            })
        ensure_revoke_device_ready(state)
        client = default_http_client(str(state["server_base_url"]))
        access_token = str(state.get("access_token", ""))
        managed_session_id = str(state.get("managed_session_id", ""))
        try:
            client.revoke_managed_device(
                device_id=int(state["device_id"]),
                access_token=f"Bearer {access_token}",
                managed_session_id=managed_session_id,
            )
        except Exception:
            refreshed = client.refresh_device_token(
                device_id=int(state["device_id"]),
                refresh_token=str(state["refresh_token"]),
            )
            store.update({
                "access_token": refreshed["access_token"],
                "refresh_token": refreshed["refresh_token"],
                "managed_session_id": refreshed["managed_session_id"],
            })
            client.revoke_managed_device(
                device_id=int(state["device_id"]),
                access_token=f'Bearer {refreshed["access_token"]}',
                managed_session_id=str(refreshed["managed_session_id"]),
            )
        restore_result = restore_local_managed_config(default_config_manager(), state)
        if not restore_result.get("ok"):
            store.update({"restore_status": restore_result.get("status")}) if hasattr(store, "update") else None
            return emit_failed({
                "command": "logout",
                "mode": "revoke_device",
                **restore_result,
            })
        logout_local_state(store)
        return emit({
            "command": "logout",
            "mode": "revoke_device",
            **restore_result,
        })

    if args.command == "proxy-serve":
        store = JsonStateStore(Path(args.state_file))
        state = store.read()
        proxy = ManagedProxyServer(ManagedProxyConfig(
            upstream_base_url=str(state["gateway_base_url"]),
            device_id=int(state["device_id"]),
            managed_session_id=str(state["managed_session_id"]),
            access_token=str(state["access_token"]),
            loopback_secret=str(state["loopback_secret"]),
            agent_version=AGENT_VERSION,
            runtime_signature=AGENT_RUNTIME_SIGNATURE,
            config_hash=state.get("config_hash"),
            server_base_url=str(state.get("server_base_url", "")) or None,
            refresh_token=str(state.get("refresh_token", "")) or None,
            source_root=AGENT_SOURCE_ROOT,
            state_store=store,
            codex_home=default_config_manager().codex_home,
        ))
        asyncio.run(proxy.serve_forever(int(state["proxy_port"])))
        return 0

    if args.command == "capture-serve":
        config = default_capture_config(Path(args.correlation_hash_key_file).expanduser() if args.correlation_hash_key_file else None)
        asyncio.run(serve_capture_receiver(Path(args.trace_dir), int(args.port), config))
        return 0

    parser.error("unknown command")
    return 2


def handle_claude_code_start(args: argparse.Namespace) -> int:
    try:
        payload = build_claude_code_start_payload(
            executable=args.executable,
            state_root=args.state_root,
            project_cwd=args.project_cwd,
            guard_port=args.guard_port,
            argv=normalize_passthrough(args.args),
        )
    except ValueError as err:
        return emit_failed({
            "command": "claude-code start",
            "status": "not_configured",
            "message": str(err),
        })
    return emit(payload)


def handle_claude_code_runtime_command(args: argparse.Namespace) -> int:
    try:
        if args.claude_code_command == "install":
            plan = build_managed_runtime_install_plan(
                executable=args.executable,
                runtime_root=args.runtime_root,
                runner=subprocess.run,
            )
            write_managed_runtime_artifacts(plan)
            status = read_managed_runtime_status(args.runtime_root)
            return emit({
                "command": "claude-code install",
                "status": "installed",
                "runtime": "claude-code",
                "runtime_root": str(args.runtime_root),
                "active_version": status.get("active_version"),
                "manifest_path": status.get("manifest_path"),
                "official_claude_unaffected": True,
            })
        if args.claude_code_command == "status":
            return emit({"command": "claude-code status", **read_managed_runtime_status(args.runtime_root)})
        if args.claude_code_command == "doctor":
            status = read_managed_runtime_status(args.runtime_root)
            return emit({
                "command": "claude-code doctor",
                **status,
                "destructive_cleanup_requires_confirmation": True,
            })
        if args.claude_code_command == "restart":
            status = read_managed_runtime_status(args.runtime_root)
            if status.get("status") not in {"enabled", "ready"}:
                return emit_failed({
                    "command": "claude-code restart",
                    **status,
                    "message": "managed Claude Code runtime is not enabled",
                    "nonblocking": False,
                })
            return emit_failed({
                "command": "claude-code restart",
                **status,
                "status": "restart_unavailable",
                "message": "no running managed Claude Code process state is available; use zhumeng-claude start to launch a new session",
                "nonblocking": False,
                "official_claude_unaffected": True,
                "destructive_cleanup_requires_confirmation": True,
            })
        if args.claude_code_command in {"rollback", "uninstall"}:
            disabled = disable_managed_runtime(args.runtime_root)
            return emit({
                "command": f"claude-code {args.claude_code_command}",
                **disabled,
            })
        if args.claude_code_command == "alias":
            plan = build_shell_alias_plan(action=args.alias_action, shell_rc=args.shell_rc)
            result = apply_shell_alias_plan(plan)
            return emit({
                "command": f"claude-code alias {args.alias_action}",
                **result,
            })
        if args.claude_code_command == "live-matrix":
            if args.collect_provider_provenance:
                if not args.run_id or args.output_root is None:
                    return emit_failed({
                        "command": "claude-code live-matrix collect-provider-provenance",
                        "status": "not_configured",
                        "message": "provider provenance collection requires --run-id and --output-root",
                    })
                provenance = collect_cp8_live_provider_provenance(run_id=args.run_id, output_root=args.output_root)
                return emit({
                    "command": "claude-code live-matrix collect-provider-provenance",
                    "status": "collected",
                    "live_provenance": provenance,
                })
            if args.evidence is None:
                return emit_failed({
                    "command": "claude-code live-matrix",
                    "status": "not_configured",
                    "message": "live matrix verification requires --evidence",
                })
            payload = json.loads(args.evidence.read_text(encoding="utf-8"))
            result = verify_cp8_live_matrix(payload, strict_live=args.strict_live, evidence_root=args.evidence.parent)
            body = {"command": "claude-code live-matrix", **result.to_dict()}
            return emit(body) if result.status == "pass" else emit_failed(body)
    except (RuntimeInstallerError, CP8LiveMatrixError, OSError, json.JSONDecodeError) as err:
        return emit_failed({
            "command": f"claude-code {args.claude_code_command}",
            "status": "not_configured",
            "message": str(err),
        })
    return emit_failed({
        "command": f"claude-code {args.claude_code_command}",
        "status": "unknown_command",
    })


def build_claude_code_start_payload(
    *,
    executable: Path | str,
    state_root: Path,
    project_cwd: Path,
    guard_port: int | None,
    argv: list[str],
) -> dict[str, object]:
    store = default_state_store()
    state = store.read()
    missing = [key for key in ("gateway_base_url", "access_token", "managed_session_id", "device_id") if not state.get(key)]
    if missing:
        raise ValueError(f"managed setup is incomplete: missing {', '.join(missing)}")
    selected_guard_port = int(guard_port or choose_local_proxy_port())
    attestation_secret = require_server_native_attestation_secret(state)
    route_hint_secret = require_server_route_hint_secret(state)
    result = run_managed_claude_code(
        executable=executable,
        repo_root=Path(__file__).resolve().parents[4],
        upstream_base=str(state["gateway_base_url"]),
        sub2api_auth=str(state["access_token"]),
        attestation_secret=attestation_secret,
        route_hint_secret=route_hint_secret,
        managed_session_id=str(state.get("managed_session_id") or "") or None,
        device_id=int(state["device_id"]) if state.get("device_id") is not None else None,
        config_root=state_root,
        project_cwd=project_cwd,
        guard_listen_port=selected_guard_port,
        argv=argv,
        inherited_env=dict(os.environ),
    )
    guard_listen = str(result.guard_ready.get("listen", ""))
    return {
        "command": "claude-code start",
        "status": "exited",
        "returncode": result.returncode,
        "guard": {
            "listen": guard_listen,
            "attested": "--native-attestation" in result.guard_plan.command,
            "route_hint_contract": "--route-hint-secret-env" in result.guard_plan.command,
            "summary_path": str(result.guard_plan.config.summary_path),
        },
        "claude_base_url": result.launch_plan.env.get("ANTHROPIC_BASE_URL"),
        "claude_code_api_base_url": result.launch_plan.env.get("CLAUDE_CODE_API_BASE_URL"),
    }


def zhumeng_claude_main(argv: Sequence[str] | None = None) -> int:
    passthrough = list(argv) if argv is not None else list(sys.argv[1:])
    claude_runtime_commands = {"install", "status", "doctor", "restart", "rollback", "uninstall", "alias", "live-matrix"}
    if passthrough and passthrough[0] == "start":
        return main(["claude-code", "start", *passthrough[1:]])
    if passthrough and passthrough[0] in claude_runtime_commands:
        return main(["claude-code", *passthrough])
    return main(["claude-code", "start", "--", *passthrough])

def handle_codex_capture(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="zhumeng-agent codex capture")
    parser.add_argument("--correlation-hash-key-file")
    subparsers = parser.add_subparsers(dest="capture_command", required=True)
    subparsers.add_parser("status")

    baseline_parser = subparsers.add_parser("baseline")
    baseline_parser.add_argument("--out", required=True)
    baseline_parser.add_argument("--app")

    install_parser = subparsers.add_parser("install")
    install_parser.add_argument("--app", required=True)

    uninstall_parser = subparsers.add_parser("uninstall")
    uninstall_parser.add_argument("--app", required=True)

    report_parser = subparsers.add_parser("report")
    report_parser.add_argument("--trace-dir", required=True)
    report_parser.add_argument("--gateway-trace-dir")

    attach_parser = subparsers.add_parser("attach")
    attach_parser.add_argument("--cdp-port", required=True, type=int)
    attach_parser.add_argument("--trace-dir", required=True)
    attach_parser.add_argument("--capture-port", type=int, default=0)
    attach_parser.add_argument("--timeout-seconds", type=float, default=600)
    attach_parser.add_argument("--target-wait-seconds", type=float, default=10)
    attach_parser.add_argument("--once", action="store_true")

    parsed = parser.parse_args(argv)
    config = default_capture_config(Path(parsed.correlation_hash_key_file).expanduser() if parsed.correlation_hash_key_file else None)
    if parsed.capture_command == "status":
        return emit({
            "command": "codex capture status",
            "config": config.public_dict(),
            "installation": __import__("zhumeng_agent.doctor", fromlist=["capture_install_manifest_state"]).capture_install_manifest_state(config),
        })
    if parsed.capture_command == "baseline":
        app_path = Path(parsed.app) if parsed.app else default_codex_app_path()
        if app_path is None:
            return emit({
                "command": "codex capture baseline",
                "status": "app_not_found",
            })
        result = generate_capture_baseline(Path(parsed.out), app_path, config)
        return emit({
            "command": "codex capture baseline",
            **result,
        })
    if parsed.capture_command == "install":
        result = install_capture_hook(Path(parsed.app), config)
        default_state_store().update({
            "desktop_capture_enabled": True,
            "desktop_capture_correlation_hash_key_file": str(config.correlation_hash_key_file) if config.correlation_hash_key_file else "",
        })
        return emit({
            "command": "codex capture install",
            **result,
        })
    if parsed.capture_command == "uninstall":
        result = uninstall_capture_hook(Path(parsed.app), config)
        default_state_store().update({
            "desktop_capture_enabled": False,
            "desktop_capture_correlation_hash_key_file": str(config.correlation_hash_key_file) if config.correlation_hash_key_file else "",
        })
        return emit({
            "command": "codex capture uninstall",
            **result,
        })
    if parsed.capture_command == "report":
        result = generate_capture_report(
            Path(parsed.trace_dir),
            config,
            gateway_trace_dir=Path(parsed.gateway_trace_dir) if parsed.gateway_trace_dir else None,
        )
        return emit({
            "command": "codex capture report",
            **result,
        })
    if parsed.capture_command == "attach":
        result = attach_capture_bridge_via_cdp(
            int(parsed.cdp_port),
            Path(parsed.trace_dir),
            config,
            capture_port=int(parsed.capture_port),
            timeout_seconds=float(parsed.timeout_seconds),
            target_wait_seconds=float(parsed.target_wait_seconds),
            once=bool(parsed.once),
        )
        return emit({
            "command": "codex capture attach",
            **result,
        })
    parser.error("unknown capture command")
    return 2


def ensure_capture_receiver_running(config: CodexDesktopCaptureConfig) -> dict[str, object]:
    trace_dir = config.base_dir / "runtime"
    port = select_cdp_port()
    subprocess.Popen([
        sys.executable,
        "-m",
        "zhumeng_agent",
        "capture-serve",
        "--trace-dir",
        str(trace_dir),
        "--port",
        str(port),
        *(
            [
                "--correlation-hash-key-file",
                str(config.correlation_hash_key_file),
            ]
            if config.correlation_hash_key_file
            else []
        ),
    ])
    return {
        "status": "running",
        "port": port,
        "trace_dir_hash": CorrelationHasher.from_key_file(config.correlation_hash_key_file).hash_identifier(str(trace_dir)),
    }


def ensure_capture_bridge_running(config: CodexDesktopCaptureConfig, cdp_port: int) -> dict[str, object]:
    trace_dir = config.base_dir / "runtime"
    command = [
        sys.executable,
        "-m",
        "zhumeng_agent",
        "codex",
        "capture",
        *(
            [
                "--correlation-hash-key-file",
                str(config.correlation_hash_key_file),
            ]
            if config.correlation_hash_key_file
            else []
        ),
        "attach",
        "--cdp-port",
        str(cdp_port),
        "--trace-dir",
        str(trace_dir),
        "--timeout-seconds",
        "21600",
        "--target-wait-seconds",
        "30",
    ]
    process = subprocess.Popen(command)
    return {
        "status": "running",
        "bridge": "cdp_binding",
        "pid": process.pid,
        "cdp_port": cdp_port,
        "trace_dir_hash": CorrelationHasher.from_key_file(config.correlation_hash_key_file).hash_identifier(str(trace_dir)),
    }


def capture_receiver_cors_headers(origin: str | None) -> dict[str, str]:
    allowed_origins = {
        "app://-",
        "null",
        "http://127.0.0.1",
        "https://127.0.0.1",
        "http://localhost",
        "https://localhost",
    }
    allow_origin = origin if origin in allowed_origins else "null"
    return {
        "Access-Control-Allow-Origin": allow_origin,
        "Access-Control-Allow-Methods": "POST, OPTIONS",
        "Access-Control-Allow-Headers": "content-type",
        "Access-Control-Max-Age": "600",
    }


def create_capture_receiver_app(trace_dir: Path, config: CodexDesktopCaptureConfig | None = None):
    from aiohttp import WSMsgType, web
    config = config or default_capture_config()

    def route_json_payload(text: str) -> None:
        try:
            payload = json.loads(text)
            if isinstance(payload, dict):
                route_capture_event(payload, trace_dir, config)
        except Exception:
            pass

    async def handle_options(request):
        return web.Response(status=204, headers=capture_receiver_cors_headers(request.headers.get("Origin")))

    async def handle(request):
        try:
            route_json_payload((await request.read()).decode("utf-8", errors="replace"))
        except Exception:
            pass
        return web.json_response({"ok": True}, headers=capture_receiver_cors_headers(request.headers.get("Origin")))

    async def handle_websocket(request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        async for msg in ws:
            if msg.type == WSMsgType.TEXT:
                route_json_payload(msg.data)
        return ws

    app = web.Application()
    app.router.add_options("/codex-desktop-capture-v2", handle_options)
    app.router.add_post("/codex-desktop-capture-v2", handle)
    app.router.add_get("/codex-desktop-capture-v2/ws", handle_websocket)
    return app


async def serve_capture_receiver(trace_dir: Path, port: int, config: CodexDesktopCaptureConfig | None = None) -> None:
    from aiohttp import web
    config = config or default_capture_config()
    app = create_capture_receiver_app(trace_dir, config)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, "127.0.0.1", port)
    await site.start()
    while True:
        await asyncio.sleep(3600)


def handle_codex_model_picker(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="zhumeng-agent codex model-picker")
    subparsers = parser.add_subparsers(dest="model_picker_command", required=True)
    status_parser = subparsers.add_parser("status")
    status_parser.add_argument("--app", required=True)
    patch_parser = subparsers.add_parser("patch")
    patch_parser.add_argument("--app", required=True)
    restore_parser = subparsers.add_parser("restore")
    restore_parser.add_argument("--app", required=True)
    parsed = parser.parse_args(argv)
    app_path = Path(parsed.app)
    try:
        if parsed.model_picker_command == "status":
            return emit({"command": "codex model-picker status", **inspect_model_picker_app(app_path)})
        if parsed.model_picker_command == "patch":
            return emit({"command": "codex model-picker patch", **patch_model_picker_app(app_path)})
        if parsed.model_picker_command == "restore":
            if codex_app_is_running(app_path):
                return emit_failed({"command": "codex model-picker restore", "status": "app_running_blocking_change", "message": "Codex App is running; quit it before restoring model picker."})
            return emit({"command": "codex model-picker restore", **restore_latest_model_picker_backup(app_path)})
    except PermissionError as err:
        return emit_failed({"command": f"codex model-picker {parsed.model_picker_command}", "status": "app_bundle_not_writable", "message": str(err)})
    except OSError as err:
        return emit_failed({"command": f"codex model-picker {parsed.model_picker_command}", "status": "app_bundle_not_writable" if getattr(err, "errno", None) in {13, 30} else "failed", "message": str(err)})
    except ModelPickerPatchError as err:
        return emit_failed({
            "command": f"codex model-picker {parsed.model_picker_command}",
            "status": "failed",
            "message": str(err),
            "recovery_hint": "Run status to inspect the app. If a patch write failed, restore from the reported backup before retrying.",
        })
    parser.error("unknown model-picker command")
    return 2


def handle_codex_plugin_auth_gate(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="zhumeng-agent codex plugin-auth-gate")
    subparsers = parser.add_subparsers(dest="plugin_auth_gate_command", required=True)
    status_parser = subparsers.add_parser("status")
    status_parser.add_argument("--app", required=True)
    patch_parser = subparsers.add_parser("patch")
    patch_parser.add_argument("--app", required=True)
    restore_parser = subparsers.add_parser("restore")
    restore_parser.add_argument("--app", required=True)
    parsed = parser.parse_args(argv)
    app_path = Path(parsed.app)
    try:
        if parsed.plugin_auth_gate_command == "status":
            return emit({"command": "codex plugin-auth-gate status", **inspect_plugin_auth_gate_app(app_path)})
        if parsed.plugin_auth_gate_command == "patch":
            return emit({"command": "codex plugin-auth-gate patch", **patch_plugin_auth_gate_app(app_path)})
        if parsed.plugin_auth_gate_command == "restore":
            if codex_app_is_running(app_path):
                return emit_failed({"command": "codex plugin-auth-gate restore", "status": "app_running_blocking_change", "message": "Codex App is running; quit it before restoring plugin auth gate."})
            return emit({"command": "codex plugin-auth-gate restore", **restore_latest_plugin_auth_gate_backup(app_path)})
    except PermissionError as err:
        return emit_failed({"command": f"codex plugin-auth-gate {parsed.plugin_auth_gate_command}", "status": "app_bundle_not_writable", "message": str(err)})
    except OSError as err:
        return emit_failed({"command": f"codex plugin-auth-gate {parsed.plugin_auth_gate_command}", "status": "app_bundle_not_writable" if getattr(err, "errno", None) in {13, 30} else "failed", "message": str(err)})
    except ModelPickerPatchError as err:
        return emit_failed({
            "command": f"codex plugin-auth-gate {parsed.plugin_auth_gate_command}",
            "status": "failed",
            "message": str(err),
            "recovery_hint": "Quit Codex Desktop, run status, and retry only if the app still has a supported patch point.",
        })
    parser.error("unknown plugin-auth-gate command")
    return 2


def handle_codex_plugin_mention_marketplace(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="zhumeng-agent codex plugin-mention-marketplace")
    subparsers = parser.add_subparsers(dest="plugin_mention_marketplace_command", required=True)
    status_parser = subparsers.add_parser("status")
    status_parser.add_argument("--app", required=True)
    patch_parser = subparsers.add_parser("patch")
    patch_parser.add_argument("--app", required=True)
    restore_parser = subparsers.add_parser("restore")
    restore_parser.add_argument("--app", required=True)
    parsed = parser.parse_args(argv)
    app_path = Path(parsed.app)
    try:
        if parsed.plugin_mention_marketplace_command == "status":
            return emit({
                "command": "codex plugin-mention-marketplace status",
                **inspect_plugin_mention_marketplace_app(app_path),
            })
        if parsed.plugin_mention_marketplace_command == "patch":
            return emit({
                "command": "codex plugin-mention-marketplace patch",
                **patch_plugin_mention_marketplace_app(app_path),
            })
        if parsed.plugin_mention_marketplace_command == "restore":
            if codex_app_is_running(app_path):
                return emit_failed({
                    "command": "codex plugin-mention-marketplace restore",
                    "status": "app_running_blocking_change",
                    "message": "Codex App is running; quit it before restoring plugin mention marketplace.",
                })
            return emit({
                "command": "codex plugin-mention-marketplace restore",
                **restore_latest_plugin_mention_marketplace_backup(app_path),
            })
    except PermissionError as err:
        return emit_failed({"command": f"codex plugin-mention-marketplace {parsed.plugin_mention_marketplace_command}", "status": "app_bundle_not_writable", "message": str(err)})
    except OSError as err:
        return emit_failed({"command": f"codex plugin-mention-marketplace {parsed.plugin_mention_marketplace_command}", "status": "app_bundle_not_writable" if getattr(err, "errno", None) in {13, 30} else "failed", "message": str(err)})
    except ModelPickerPatchError as err:
        return emit_failed({
            "command": f"codex plugin-mention-marketplace {parsed.plugin_mention_marketplace_command}",
            "status": "failed",
            "message": str(err),
            "recovery_hint": "Quit Codex Desktop, run status, and retry only if the app still has a supported @ menu patch point.",
        })
    parser.error("unknown plugin-mention-marketplace command")
    return 2


def generate_capture_report(
    trace_dir: Path,
    config: CodexDesktopCaptureConfig | None = None,
    *,
    gateway_trace_dir: Path | None = None,
) -> dict[str, object]:
    config = config or default_capture_config()
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    app_server_events = load_jsonl(trace_dir / "app_server_v2.jsonl")
    gateway_root = gateway_trace_dir or trace_dir
    gateway_events = load_gateway_trace_events(gateway_root)
    tool_events = load_jsonl(trace_dir / "tool_lifecycle.jsonl")
    model_events = load_jsonl(trace_dir / "model_picker.jsonl")
    subagent_events = load_jsonl(trace_dir / "subagent_registration.jsonl")
    deferred_tool_events = load_jsonl(trace_dir / "deferred_tool_search.jsonl")
    link_path = trace_dir / "trace_link.jsonl"
    link_events = load_jsonl(link_path)
    if not link_events and gateway_events:
        link_events = link_traces(app_server_events, gateway_events)
        write_trace_links(link_path, link_events)
    methods = sorted({str(event.get("method")) for event in app_server_events if event.get("method")})
    policy_violations = detect_policy_violations([*app_server_events, *tool_events, *model_events])
    subagent_report = build_subagent_registration_report(subagent_events) if subagent_events else {}
    spawn_agent_report = build_spawn_agent_override_capture_report(deferred_tool_events) if deferred_tool_events else {}
    deferred_report = build_deferred_tool_search_report(deferred_tool_events) if deferred_tool_events else {}
    deepseek_cache_report = build_deepseek_cache_replay_report(gateway_events)
    computer_use_report = build_computer_use_normalized_output_report(gateway_events)
    return {
        "status": "reported",
        "trace_dir_hash": hasher.hash_identifier(str(trace_dir)),
        "app_server_methods": methods,
        "model_picker_events": len(model_events),
        "tool_lifecycle_events": len(tool_events),
        "subagent_registration_events": len(subagent_events),
        "gateway_trace_links": len(link_events),
        "content_policy_violations": len(policy_violations),
        "low_confidence_links": sum(1 for event in link_events if event.get("confidence") == "low"),
        "hook_mode": "renderer_readonly",
        "app_asar_modified": False,
        "correlation_hash_key_file": "set" if config.correlation_hash_key_file else "unset",
        **subagent_report,
        **({"spawn_agent_model_override": spawn_agent_report} if spawn_agent_report else {}),
        **({"deferred_tool_search": deferred_report} if deferred_report else {}),
        **({"deepseek_cache_replay_diagnostics": deepseek_cache_report} if deepseek_cache_report else {}),
        **({"computer_use_normalized_output": computer_use_report} if computer_use_report else {}),
    }



def build_deferred_tool_search_report(events: list[dict[str, object]]) -> dict[str, object]:
    ordered = sorted(events, key=lambda event: str(event.get("capture_ts") or event.get("ts") or ""))
    call_count = sum(1 for event in ordered if event.get("event_type") == "tool_search_call")
    output_count = sum(1 for event in ordered if event.get("event_type") == "tool_search_output")
    seen_call = False
    followed_by_output = False
    for event in ordered:
        event_type = str(event.get("event_type") or "")
        if event_type == "tool_search_call":
            seen_call = True
        elif event_type == "tool_search_output" and seen_call:
            followed_by_output = True
            break
    namespaces, tool_paths, matrix = summarize_deferred_tool_families(event.get("tools") for event in ordered)
    return {
        "events": len(ordered),
        "tool_search_call_count": call_count,
        "tool_search_output_count": output_count,
        "tool_search_call_followed_by_output": followed_by_output,
        "spawn_agent_present": any(capture_shape_contains_spawn_agent(event.get("tools")) for event in ordered),
        "discovered_namespaces": namespaces,
        "discovered_tools": tool_paths,
        "tool_family_matrix": matrix,
    }


def summarize_deferred_tool_families(values: object) -> tuple[list[str], list[str], dict[str, dict[str, object]]]:
    namespace_tools: dict[str, set[str]] = {}
    discovered_tools: set[str] = set()
    for value in values if isinstance(values, list) else list(values):
        collect_deferred_tool_family(value, [], namespace_tools, discovered_tools)
    namespaces = sorted(namespace_tools)
    matrix = {
        namespace: {
            "tool_count": len(namespace_tools[namespace]),
            "tools": sorted(namespace_tools[namespace]),
        }
        for namespace in namespaces
    }
    return namespaces, sorted(discovered_tools), matrix


def collect_deferred_tool_family(
    value: object,
    namespace_path: list[str],
    namespace_tools: dict[str, set[str]],
    discovered_tools: set[str],
) -> None:
    if isinstance(value, list):
        for child in value:
            collect_deferred_tool_family(child, namespace_path, namespace_tools, discovered_tools)
        return
    if not isinstance(value, dict):
        return
    name = str(value.get("name") or "").strip()
    children = value.get("tools")
    if isinstance(children, list) and name:
        next_path = [*namespace_path, name]
        namespace = ".".join(next_path)
        namespace_tools.setdefault(namespace, set())
        collect_deferred_tool_family(children, next_path, namespace_tools, discovered_tools)
        return
    if name and namespace_path:
        namespace = ".".join(namespace_path)
        namespace_tools.setdefault(namespace, set()).add(name)
        discovered_tools.add(namespace + "." + name)


def capture_shape_contains_spawn_agent(value: object) -> bool:
    if isinstance(value, dict):
        if value.get("name") == "spawn_agent":
            return True
        return any(capture_shape_contains_spawn_agent(child) for child in value.values())
    if isinstance(value, list):
        return any(capture_shape_contains_spawn_agent(child) for child in value)
    return False


def gateway_request_diagnostics(event: dict[str, object]) -> dict[str, object]:
    diagnostics = event.get("request_diagnostics")
    return diagnostics if isinstance(diagnostics, dict) else {}


def build_deepseek_cache_replay_report(events: list[dict[str, object]]) -> dict[str, object]:
    caches = [
        diag["deepseek_cache"]
        for diag in (gateway_request_diagnostics(event) for event in events)
        if isinstance(diag.get("deepseek_cache"), dict)
    ]
    if not caches:
        return {}

    def present(key: str) -> bool:
        return any(bool(cache.get(key)) for cache in caches)

    return {
        "events": len(caches),
        "previous_response_id_present": any(bool(cache.get("previous_response_id_present")) for cache in caches),
        "previous_response_replay_modes": sorted({str(cache.get("previous_response_replay_mode")) for cache in caches if cache.get("previous_response_replay_mode")}),
        "state_lookup_statuses": sorted({str(cache.get("state_lookup_status")) for cache in caches if cache.get("state_lookup_status")}),
        "messages_full_hash_present": present("messages_full_hash"),
        "message_prefix_hash_present": present("message_prefix_hash"),
        "message_suffix_hash_present": present("message_suffix_hash"),
        "tool_schema_hash_present": present("tool_schema_hash"),
        "request_shape_hash_present": present("request_shape_hash"),
    }


def build_computer_use_normalized_output_report(events: list[dict[str, object]]) -> dict[str, object]:
    summaries = [
        diag["deepseek_tool_output_summary"]
        for diag in (gateway_request_diagnostics(event) for event in events)
        if isinstance(diag.get("deepseek_tool_output_summary"), dict)
    ]
    computer_summaries = [summary for summary in summaries if deepseek_tool_summary_is_computer_use(summary)]
    if not computer_summaries:
        return {}
    classes = sorted({item for summary in computer_summaries for item in deepseek_tool_summary_classes(summary)})
    return {
        "events": len(computer_summaries),
        "classes": classes,
        "fallback_preview_only": all(bool(summary.get("fallback_preview_only")) for summary in computer_summaries),
        "operable_line_count_max": max((safe_int(summary.get("operable_line_count")) for summary in computer_summaries), default=0),
        "original_chars_max": max((safe_int(summary.get("original_chars")) for summary in computer_summaries), default=0),
        "sha256_present": any(bool(summary.get("sha256")) for summary in computer_summaries),
    }


def deepseek_tool_summary_classes(summary: dict[str, object]) -> list[str]:
    classes = summary.get("classes")
    if isinstance(classes, list):
        return sorted({str(item) for item in classes if is_safe_capture_report_label(str(item))})
    if isinstance(classes, dict):
        return sorted({str(key) for key, value in classes.items() if value and is_safe_capture_report_label(str(key))})
    return []


def deepseek_tool_summary_is_computer_use(summary: dict[str, object]) -> bool:
    classes = set(deepseek_tool_summary_classes(summary))
    return bool(classes.intersection({"computer_screenshot", "accessibility_tree", "visual_tree", "computer_use"})) or safe_int(summary.get("operable_line_count")) > 0


def safe_int(value: object) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    return 0


def is_safe_capture_report_label(value: str) -> bool:
    return bool(re.match(r"^[A-Za-z0-9_.:-]{1,80}$", value))


def build_spawn_agent_override_capture_report(events: list[dict[str, object]]) -> dict[str, object]:
    manager = default_config_manager()
    parsed = manager._parsed_config() or {}
    catalog_path = Path(str(parsed.get("model_catalog_json") or manager.catalog_path_for_profile(None))).expanduser()
    if not catalog_path.is_absolute():
        catalog_path = manager.codex_home / catalog_path
    try:
        payload = json.loads(catalog_path.read_text(encoding="utf-8")) if catalog_path.exists() else {"models": []}
    except (OSError, json.JSONDecodeError):
        payload = {"models": []}
    models = payload.get("models") if isinstance(payload, dict) else []
    catalog_models: list[str] = []
    if isinstance(models, list):
        for model in models:
            if isinstance(model, dict):
                slug = str(model.get("slug") or model.get("model") or model.get("id") or "").strip()
                if slug:
                    catalog_models.append(slug)
    catalog_mtime = None
    if catalog_path.exists():
        try:
            catalog_mtime = datetime.fromtimestamp(catalog_path.stat().st_mtime, UTC).isoformat().replace("+00:00", "Z")
        except OSError:
            catalog_mtime = None
    return build_spawn_agent_model_override_report(
        events=events,
        catalog_models=sorted(catalog_models),
        catalog_hash=file_sha256(catalog_path),
        catalog_mtime=catalog_mtime,
    )

def load_gateway_trace_events(gateway_root: Path) -> list[dict[str, object]]:
    direct = gateway_root / "gateway_trace.jsonl"
    if direct.exists():
        return load_jsonl(direct)
    if gateway_root.is_file():
        if gateway_root.name.endswith(".jsonl"):
            return load_jsonl(gateway_root)
        event = load_gateway_capture_json_event(gateway_root)
        return [event] if event else []
    events: list[dict[str, object]] = []
    if gateway_root.exists():
        for path in sorted(gateway_root.rglob("gateway_trace.jsonl")):
            events.extend(load_jsonl(path))
        for path in sorted(gateway_root.rglob("trace_report.json")):
            event = load_gateway_capture_trace_dir_event(path.parent)
            if event:
                events.append(event)
        for path in sorted(gateway_root.rglob("summary.json")):
            if (path.parent / "trace_report.json").exists():
                continue
            event = load_gateway_capture_trace_dir_event(path.parent)
            if event:
                events.append(event)
        for path in sorted(gateway_root.rglob("session_report.jsonl")):
            events.extend(load_gateway_session_report_events(path))
    return events


def load_gateway_capture_trace_dir_event(trace_dir: Path) -> dict[str, object] | None:
    report = load_gateway_capture_json_event(trace_dir / "trace_report.json") or {}
    summary = load_gateway_capture_json_event(trace_dir / "summary.json") or {}
    diagnostics = load_gateway_capture_json_event(trace_dir / "client_request.diagnostics.json") or {}
    event: dict[str, object] = {}
    for source in (summary, report):
        if not isinstance(source, dict):
            continue
        for src_key, dst_key in (
            ("trace_id", "gateway_trace_id"),
            ("finished_at", "ts"),
            ("ts", "ts"),
            ("model", "model"),
            ("path", "request_path"),
            ("request_path", "request_path"),
            ("correlation_hashes", "correlation_hashes"),
        ):
            if source.get(src_key) and not event.get(dst_key):
                event[dst_key] = source[src_key]
    if not event.get("gateway_trace_id"):
        event["gateway_trace_id"] = trace_dir.name
    merged_diagnostics = merge_request_diagnostics(
        summary.get("request_diagnostics") if isinstance(summary, dict) else None,
        report.get("request_diagnostics") if isinstance(report, dict) else None,
        diagnostics,
    )
    if merged_diagnostics:
        event["request_diagnostics"] = merged_diagnostics
    return event if event else None


def load_gateway_capture_json_event(path: Path) -> dict[str, object] | None:
    if not path.exists():
        return None
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return payload if isinstance(payload, dict) else None


def load_gateway_session_report_events(path: Path) -> list[dict[str, object]]:
    events: list[dict[str, object]] = []
    for row in load_jsonl(path):
        event: dict[str, object] = {
            "gateway_trace_id": row.get("trace_id") or path.stem,
            "ts": row.get("ts"),
            "model": row.get("model"),
            "request_path": row.get("request_path") or row.get("path") or "/codex/v1/responses",
        }
        diagnostics = merge_request_diagnostics({"deepseek_cache": row.get("deepseek_cache")} if isinstance(row.get("deepseek_cache"), dict) else None)
        if diagnostics:
            event["request_diagnostics"] = diagnostics
        events.append({key: value for key, value in event.items() if value})
    return events


def merge_request_diagnostics(*sources: object) -> dict[str, object]:
    merged: dict[str, object] = {}
    for source in sources:
        if not isinstance(source, dict):
            continue
        for key in ("deepseek_cache", "deepseek_tool_output_summary"):
            value = source.get(key)
            if isinstance(value, dict):
                current = merged.get(key)
                if isinstance(current, dict):
                    current.update(value)
                else:
                    merged[key] = dict(value)
    return merged


def detect_policy_violations(events: list[dict[str, object]]) -> list[dict[str, object]]:
    violations: list[dict[str, object]] = []
    for event in events:
        if has_sensitive_capture_content(event):
            violations.append(event)
    return violations


HASH_VALUE_RE = re.compile(r"^(hmac-sha256|sha256):[a-f0-9]{64}$", re.IGNORECASE)
SENSITIVE_VALUE_RE = re.compile(
    r"(/Users/|/Applications/|https?://|git@|Cookie\s*=|Bearer\s+|api[_-]?key|refs/heads/|feature/[A-Za-z0-9_./-]+|(?<!sha256:)[A-Fa-f0-9]{40}(?![A-Fa-f0-9]))",
    re.IGNORECASE,
)
SENSITIVE_FIELD_RE = re.compile(r"(authorization|cookie|api[_-]?key|token|secret|remote_?url|repo_?url|branch|commit|revision)", re.IGNORECASE)


def has_sensitive_capture_content(value: object, field_name: str = "") -> bool:
    if field_name.endswith("_hash") or field_name == "hash":
        return False
    if isinstance(value, dict):
        if value.get("raw_payload") or value.get("raw_content"):
            return True
        for key, child in value.items():
            key_text = str(key)
            if SENSITIVE_FIELD_RE.search(key_text) and not key_text.endswith("_hash"):
                return True
            if has_sensitive_capture_content(child, key_text):
                return True
        return False
    if isinstance(value, list):
        return any(has_sensitive_capture_content(item, field_name) for item in value)
    if isinstance(value, str):
        if HASH_VALUE_RE.match(value):
            return False
        return bool(SENSITIVE_VALUE_RE.search(value))
    return False


def merge_env_no_proxy(env: dict[str, str]) -> dict[str, str]:
    from .proxy.server import merge_no_proxy

    return merge_no_proxy(env)


def restore_local_managed_config(config_manager: CodexConfigManager, state: dict[str, object]) -> dict[str, object]:
    try:
        conflict = managed_restore_conflict(config_manager, state)
        if conflict:
            return {"ok": False, "status": "restore_conflict", "conflicts": conflict}

        restored: list[str] = []
        declared_config_backups = [Path(str(path)) for path in state.get("backup_paths", []) if Path(str(path)).name.startswith("config.toml")]
        for backup in declared_config_backups:
            if not backup.exists():
                return {"ok": False, "status": "error_restore_failed", "message": f"missing config backup: {backup}"}

        app_path_value = state.get("codex_app_path") or state.get("app_path")
        enhancements: dict[str, object] | None = None
        # Restore app bundle patches before local config, after all local restore preflight checks pass.
        if app_path_value:
            enhancements = restore_codex_enhancements(Path(str(app_path_value)), item="all")
            if enhancements.get("status") != "restored":
                return {"ok": False, "status": "error_restore_failed", "restored": restored, "enhancements": enhancements}

        config_restored = False
        for backup in declared_config_backups:
            config_manager.restore_backup(backup)
            restored.append(str(backup))
            config_restored = True
        for backup_path in state.get("backup_paths", []):
            backup = Path(str(backup_path))
            if backup in declared_config_backups:
                continue
            if backup.exists():
                config_manager.restore_backup(backup)
                restored.append(str(backup))
        if not config_restored and config_manager.config_path.exists():
            config_text = config_manager.config_path.read_text(encoding="utf-8")
            if 'model_provider = "zhumeng-managed"' in config_text or 'model_provider = "zhumeng-codex"' in config_text:
                config_manager.config_path.unlink()
                restored.append(str(config_manager.config_path))
        prior_auth_json = state.get("prior_auth_json")
        if prior_auth_json is not None:
            config_manager._write_text_atomic(config_manager.auth_path, str(prior_auth_json))
            restored.append(str(config_manager.auth_path))
        elif config_manager.auth_path.exists():
            auth_text = config_manager.auth_path.read_text(encoding="utf-8")
            if "zhumeng-local-managed-" in auth_text:
                config_manager.auth_path.unlink()
                restored.append(str(config_manager.auth_path))
        catalog_path = Path(str(state.get("catalog_path") or config_manager.catalog_path_for_profile(state.get("config_profile", DEFAULT_CODEX_CONFIG_PROFILE))))
        prior_catalog_json = state.get("prior_catalog_json")
        if prior_catalog_json is not None:
            config_manager._write_text_atomic(catalog_path, str(prior_catalog_json))
            restored.append(str(catalog_path))
        elif catalog_path.exists() and state.get("catalog_preexisting") is False:
            catalog_path.unlink()
            restored.append(str(catalog_path))
        return {"ok": True, "status": "completed", "restored": restored, "enhancements": enhancements}
    except Exception as err:
        return {"ok": False, "status": "error_restore_failed", "message": str(err)}

def managed_restore_conflict(config_manager: CodexConfigManager, state: dict[str, object]) -> list[dict[str, object]]:
    checks = [
        ("config", config_manager.config_path, state.get("config_hash_after")),
        ("auth", config_manager.auth_path, state.get("auth_hash_after")),
    ]
    catalog_path = state.get("catalog_path")
    if catalog_path:
        checks.append(("catalog", Path(str(catalog_path)), state.get("catalog_hash_after")))
    conflicts: list[dict[str, object]] = []
    for name, path, expected_hash in checks:
        if not expected_hash:
            continue
        current_hash = file_sha256(path) if path.exists() else None
        if current_hash != expected_hash:
            conflicts.append({"target": name, "path": str(path), "expected_hash": expected_hash, "current_hash": current_hash})
    return conflicts
