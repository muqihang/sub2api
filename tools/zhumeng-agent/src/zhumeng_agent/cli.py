from __future__ import annotations

import argparse
import json
import os
import platform
import subprocess
import sys
from pathlib import Path
from typing import Sequence

from .adapters.codex.config_manager import CodexConfigManager, choose_local_proxy_port
from .adapters.codex.detect import resolve_codex_home
from .adapters.codex.launcher import build_codex_launch_command, detect_codex_app_path, select_cdp_port
from .adapters.base import BaseAdapter
from .doctor import codex_doctor_report
from .http_client import AgentHTTPClient
from .platform_paths import state_dir
from .proxy.server import ManagedProxyConfig, ManagedProxyServer
from .security import generate_loopback_secret
from .deeplink import parse_zhumeng_deeplink
from .state import JsonStateStore, ensure_revoke_device_ready, logout_local_state


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

    return parser


def normalize_passthrough(args: list[str]) -> list[str]:
    if args and args[0] == "--":
        return args[1:]
    return args


def emit(payload: dict[str, object]) -> int:
    print(json.dumps(payload))
    return 0


def default_state_store() -> JsonStateStore:
    return JsonStateStore(state_dir() / "state.json")


def default_http_client(server: str) -> AgentHTTPClient:
    return AgentHTTPClient(server)


def default_config_manager() -> CodexConfigManager:
    return CodexConfigManager()


def run_codex_process(args: list[str], env: dict[str, str]) -> int:
    return subprocess.call(["codex", *args], env=env)


def launch_codex_process(command: list[str]) -> None:
    subprocess.Popen(command)


def is_process_alive(pid: int | None) -> bool:
    if not pid or pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def ensure_proxy_running(store: JsonStateStore) -> int:
    state = store.read()
    required = ("gateway_base_url", "device_id", "managed_session_id", "access_token", "loopback_secret", "proxy_port")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"proxy state is incomplete: missing {', '.join(missing)}")

    pid = int(state.get("proxy_pid", 0) or 0)
    if is_process_alive(pid):
        return pid

    process = subprocess.Popen([
        sys.executable,
        "-m",
        "zhumeng_agent",
        "proxy-serve",
        "--state-file",
        str(store.path),
    ])
    store.update({"proxy_pid": process.pid})
    return int(process.pid)


def load_managed_state(store: JsonStateStore) -> dict[str, object]:
    state = store.read()
    required = ("config_profile", "proxy_port", "loopback_secret")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"managed setup is incomplete: missing {', '.join(missing)}")
    return state


def main(argv: Sequence[str] | None = None) -> int:
    argv_list = list(argv) if argv is not None else None
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
        plan = config_manager.plan_configure(
            exchanged["config_profile"],
            proxy_port,
            loopback_secret,
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
        config_manager.repair(
            state.get("config_profile", {
                "model_provider": "zhumeng-managed",
                "wire_api": "responses",
                "requires_openai_auth": True,
                "supports_websockets": True,
            }),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
        )
        ensure_proxy_running(store)
        search_roots = [Path("/Applications"), Path.home() / "Applications"]
        app_path = detect_codex_app_path(search_roots=search_roots, platform=platform.system().lower().replace("windows", "win32"))
        if app_path is not None:
            command = build_codex_launch_command(app_path, select_cdp_port())
            launch_codex_process(command)
            return emit({
                "command": "launch",
                "client": args.client,
                "status": "degraded",
                "launch_command": command,
                "injection": "not_implemented",
            })
        result = adapter.launch(dry_run=False)
        return emit({
            "command": "launch",
            **result.to_dict(),
        })

    if args.command == "codex":
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
        config_manager.repair(
            state.get("config_profile", {
                "model_provider": "zhumeng-managed",
                "wire_api": "responses",
                "requires_openai_auth": True,
                "supports_websockets": True,
            }),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
        )
        ensure_proxy_running(store)
        passthrough_args = normalize_passthrough(args.args)
        return_code = run_codex_process(passthrough_args, env)
        return emit({
            "command": "codex",
            "args": passthrough_args,
            "status": "executed",
            "returncode": return_code,
            "proxy_port": state.get("proxy_port"),
            "NO_PROXY": env.get("NO_PROXY"),
        })

    if args.command == "status":
        state = default_state_store().read()
        return emit({
            "command": "status",
            "format": "json" if args.json else "text",
            "status": state.get("status", "not_configured"),
        })

    if args.command == "doctor":
        report = codex_doctor_report(resolve_codex_home())
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
        config_manager.repair(
            state.get("config_profile", {
                "model_provider": "zhumeng-managed",
                "wire_api": "responses",
                "requires_openai_auth": True,
                "supports_websockets": True,
            }),
            int(state.get("proxy_port", choose_local_proxy_port())),
            str(state.get("loopback_secret", generate_loopback_secret())),
        )
        ensure_proxy_running(store)
        return emit({
            "command": "repair",
            "client": args.client,
            "status": "repaired",
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
            agent_version="0.1.0",
            config_hash=state.get("config_hash"),
            server_base_url=str(state.get("server_base_url", "")) or None,
            refresh_token=str(state.get("refresh_token", "")) or None,
            state_store=store,
        ))
        import asyncio
        asyncio.run(proxy.serve_forever(int(state["proxy_port"])))
        return 0

    parser.error("unknown command")
    return 2


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
