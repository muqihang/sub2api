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
from pathlib import Path
from typing import Sequence

from .adapters.codex.config_manager import CodexConfigManager, choose_local_proxy_port
from .adapters.codex.capture_baseline import generate_capture_baseline
from .adapters.codex.capture_config import CodexDesktopCaptureConfig
from .adapters.codex.capture_config import CorrelationHasher
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
from .adapters.codex.model_picker import restore_latest_plugin_auth_gate_backup
from .adapters.base import BaseAdapter
from .doctor import codex_doctor_report
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


def default_codex_app_path() -> Path | None:
    return detect_codex_app_path(
        search_roots=[Path("/Applications"), Path.home() / "Applications"],
        platform=platform.system().lower().replace("windows", "win32"),
    )


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
            "model_picker": {"status": "app_not_found"},
            "plugin_auth_gate": {"status": "app_not_found"},
            "plugin_mention_marketplace": {"status": "app_not_found"},
        }
    return {
        "app_path": str(app_path),
        "model_picker": run_desktop_patch(lambda: patch_model_picker_app(app_path)),
        "plugin_auth_gate": run_desktop_patch(lambda: patch_plugin_auth_gate_app(app_path)),
        "plugin_mention_marketplace": run_desktop_patch(lambda: patch_plugin_mention_marketplace_app(app_path)),
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


def ensure_proxy_running(store: JsonStateStore) -> int:
    state = store.read()
    required = ("gateway_base_url", "device_id", "managed_session_id", "access_token", "loopback_secret", "proxy_port")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"proxy state is incomplete: missing {', '.join(missing)}")

    pid = int(state.get("proxy_pid", 0) or 0)
    if is_process_alive(pid) and proxy_matches_current_runtime(proxy_port := int(state["proxy_port"])):
        return pid
    proxy_port = int(state["proxy_port"])
    if is_process_alive(pid):
        terminate_proxy_process(pid)
    if is_loopback_port_accepting_connections(proxy_port) and proxy_matches_current_runtime(proxy_port):
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


def main(argv: Sequence[str] | None = None) -> int:
    argv_list = list(argv) if argv is not None else list(sys.argv[1:])
    if argv_list and len(argv_list) == 1 and argv_list[0].startswith("zhumeng-agent://"):
        deeplink = parse_zhumeng_deeplink(argv_list[0])
        argv_list = [
            "setup",
            "--client", deeplink["client"],
            "--code", deeplink["code"],
            "--server", deeplink["server"],
        ]

    parser = build_parser()
    args = parser.parse_args(argv_list)

    if args.command == "setup":
        client = default_http_client(args.server)
        exchanged = client.exchange_setup_grant(code=args.code, server_origin=args.server, client=args.client)
        loopback_secret = generate_loopback_secret()
        proxy_port = choose_local_proxy_port()
        config_manager = default_config_manager()
        prior_auth_json = None
        if config_manager.auth_path.exists():
            prior_auth_json = config_manager.auth_path.read_text(encoding="utf-8")
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
        )
        config_manager.apply_configure(plan)
        store = default_state_store()
        store.write({
            "client": args.client,
            "server_base_url": exchanged["server_base_url"],
            "gateway_base_url": exchanged["gateway_base_url"],
            "device_id": exchanged["device_id"],
            "managed_session_id": exchanged["managed_session_id"],
            "access_token": exchanged["access_token"],
            "refresh_token": exchanged["refresh_token"],
            "config_profile": exchanged["config_profile"],
            "proxy_port": proxy_port,
            "loopback_secret": loopback_secret,
            "backup_paths": [str(path) for path in plan.backup_paths],
            "prior_auth_json": prior_auth_json,
            "desktop_capture_enabled": False,
            "model_catalog_meta": model_catalog_meta,
            "status": "configured",
        })
        ensure_proxy_running(store)
        return emit({
            "command": "setup",
            "client": args.client,
            "server": args.server,
            "code_redacted": True,
            "status": "configured",
            "proxy_port": proxy_port,
            "device_id": exchanged["device_id"],
            "model_catalog": model_catalog_meta,
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
        )
        proxy_pid = ensure_proxy_running(store)
        store.update({"status": "configured", "proxy_pid": proxy_pid})
        desktop_patches = patch_detected_codex_desktop()
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
            "status": state.get("status", "not_configured"),
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
        )
        proxy_pid = ensure_proxy_running(store)
        store.update({"status": "configured", "proxy_pid": proxy_pid})
        desktop_patches = patch_detected_codex_desktop()
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
            if not restore_local_managed_config(default_config_manager(), state):
                return emit({
                    "command": "logout",
                    "mode": "local_only",
                    "status": "manual_restore_required",
                })
            logout_local_state(store)
            return emit({
                "command": "logout",
                "mode": "local_only",
                "status": "completed",
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
        if not restore_local_managed_config(default_config_manager(), state):
            return emit({
                "command": "logout",
                "mode": "revoke_device",
                "status": "manual_restore_required",
            })
        logout_local_state(store)
        return emit({
            "command": "logout",
            "mode": "revoke_device",
            "status": "completed",
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
        ))
        asyncio.run(proxy.serve_forever(int(state["proxy_port"])))
        return 0

    if args.command == "capture-serve":
        config = default_capture_config(Path(args.correlation_hash_key_file).expanduser() if args.correlation_hash_key_file else None)
        asyncio.run(serve_capture_receiver(Path(args.trace_dir), int(args.port), config))
        return 0

    parser.error("unknown command")
    return 2


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
            return emit({"command": "codex model-picker restore", **restore_latest_model_picker_backup(app_path)})
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
            return emit({"command": "codex plugin-auth-gate restore", **restore_latest_plugin_auth_gate_backup(app_path)})
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
    link_path = trace_dir / "trace_link.jsonl"
    link_events = load_jsonl(link_path)
    if not link_events and gateway_events:
        link_events = link_traces(app_server_events, gateway_events)
        write_trace_links(link_path, link_events)
    methods = sorted({str(event.get("method")) for event in app_server_events if event.get("method")})
    policy_violations = detect_policy_violations([*app_server_events, *tool_events, *model_events])
    return {
        "status": "reported",
        "trace_dir_hash": hasher.hash_identifier(str(trace_dir)),
        "app_server_methods": methods,
        "model_picker_events": len(model_events),
        "tool_lifecycle_events": len(tool_events),
        "gateway_trace_links": len(link_events),
        "content_policy_violations": len(policy_violations),
        "low_confidence_links": sum(1 for event in link_events if event.get("confidence") == "low"),
        "hook_mode": "renderer_readonly",
        "app_asar_modified": False,
        "correlation_hash_key_file": "set" if config.correlation_hash_key_file else "unset",
    }


def load_gateway_trace_events(gateway_root: Path) -> list[dict[str, object]]:
    direct = gateway_root / "gateway_trace.jsonl"
    if direct.exists():
        return load_jsonl(direct)
    if gateway_root.is_file():
        return load_jsonl(gateway_root)
    events: list[dict[str, object]] = []
    if gateway_root.exists():
        for path in sorted(gateway_root.rglob("gateway_trace.jsonl")):
            events.extend(load_jsonl(path))
    return events


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


def restore_local_managed_config(config_manager: CodexConfigManager, state: dict[str, object]) -> bool:
    restored = False
    config_restored = False
    for backup_path in state.get("backup_paths", []):
        backup = Path(str(backup_path))
        if backup.exists():
            config_manager.restore_backup(backup)
            restored = True
            if backup.name.startswith("config.toml"):
                config_restored = True
    if not config_restored and config_manager.config_path.exists():
        config_text = config_manager.config_path.read_text(encoding="utf-8")
        if 'model_provider = "zhumeng-managed"' in config_text:
            config_manager.config_path.unlink()
            restored = True
    prior_auth_json = state.get("prior_auth_json")
    if prior_auth_json:
        config_manager._write_text_atomic(config_manager.auth_path, str(prior_auth_json))
        restored = True
    elif config_manager.auth_path.exists():
        auth_text = config_manager.auth_path.read_text(encoding="utf-8")
        if "zhumeng-local-managed-" in auth_text:
            config_manager.auth_path.unlink()
            restored = True
    return restored
