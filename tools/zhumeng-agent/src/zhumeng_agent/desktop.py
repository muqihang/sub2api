from __future__ import annotations

import argparse
from pathlib import Path
from typing import Any, Callable

from .desktop_schema import emit_envelope, envelope, error_envelope, redact


class DesktopArgumentError(ValueError):
    pass


class DesktopArgumentParser(argparse.ArgumentParser):
    def error(self, message: str) -> None:
        raise DesktopArgumentError(message)

    def exit(self, status: int = 0, message: str | None = None) -> None:
        raise DesktopArgumentError(message or f"invalid arguments ({status})")


def run_desktop_command(argv: list[str], handlers: dict[str, Callable[..., Any]]) -> int:
    command_name = "desktop" if not argv else f"desktop {argv[0]}"
    try:
        return _run_desktop_command(argv, handlers)
    except (SystemExit, DesktopArgumentError):
        return emit_envelope(error_envelope(command_name, "invalid_arguments", "invalid arguments"))
    except Exception as err:  # Desktop sidecar must never leak tracebacks to the shell.
        return emit_envelope(error_envelope(command_name, "internal_error", str(err)))


def command_ok(status: str) -> bool:
    return status not in {
        "failed",
        "error",
        "app_bundle_not_writable",
        "app_running_blocking_change",
        "not_configured",
        "error_restore_failed",
        "restore_conflict",
    }


def _run_desktop_command(argv: list[str], handlers: dict[str, Callable[..., Any]]) -> int:
    if not argv:
        return emit_envelope(error_envelope("desktop", "missing_command", "missing desktop command"))
    subcommand = argv[0]
    if subcommand == "status":
        parser = DesktopArgumentParser(prog="zhumeng-agent desktop status")
        parser.add_argument("--json", action="store_true")
        parser.parse_args(argv[1:])
        data = handlers["status"]()
        return emit_envelope(envelope(command="desktop status", ok=True, status=str(data.get("status", "unknown")), data=data))
    if subcommand in {"setup", "reauth"}:
        parser = DesktopArgumentParser(prog=f"zhumeng-agent desktop {subcommand}")
        parser.add_argument("--client", required=True)
        parser.add_argument("--code", required=True)
        parser.add_argument("--server", required=True)
        parser.add_argument("--json", action="store_true")
        args = parser.parse_args(argv[1:])
        handler_name = "reauth" if subcommand == "reauth" else "setup"
        data = handlers[handler_name](args.client, args.code, args.server)
        status = "reauthorized" if subcommand == "reauth" else str(data.get("status", "configured"))
        if subcommand == "reauth":
            data["status"] = status
        return emit_envelope(envelope(command=f"desktop {subcommand}", ok=True, status=status, data=data))
    if subcommand == "open":
        parser = DesktopArgumentParser(prog="zhumeng-agent desktop open")
        parser.add_argument("--app", required=True)
        parser.add_argument("--json", action="store_true")
        args = parser.parse_args(argv[1:])
        data = handlers["open"](args.app)
        status = str(data.get("status", "opened"))
        return emit_envelope(envelope(command="desktop open", ok=command_ok(status), status=status, data=data))
    if subcommand == "repair":
        parser = DesktopArgumentParser(prog="zhumeng-agent desktop repair")
        parser.add_argument("--client", required=True)
        parser.add_argument("--json", action="store_true")
        args = parser.parse_args(argv[1:])
        data = handlers["repair"](args.client)
        status = str(data.get("status", "repaired"))
        return emit_envelope(envelope(command="desktop repair", ok=command_ok(status), status=status, data=data))
    if subcommand == "diagnose":
        parser = DesktopArgumentParser(prog="zhumeng-agent desktop diagnose")
        parser.add_argument("--redacted", action="store_true")
        parser.add_argument("--json", action="store_true")
        args = parser.parse_args(argv[1:])
        data = handlers["diagnose"]()
        if not args.redacted:
            data = redact(data)
        return emit_envelope(envelope(command="desktop diagnose", ok=True, status="reported", data=data))
    if subcommand == "codex-enhancements":
        return _run_codex_enhancements(argv[1:], handlers)
    return emit_envelope(error_envelope(f"desktop {subcommand}", "unknown_command", f"unknown desktop command: {subcommand}"))


def _run_codex_enhancements(argv: list[str], handlers: dict[str, Callable[..., Any]]) -> int:
    parser = DesktopArgumentParser(prog="zhumeng-agent desktop codex-enhancements")
    subparsers = parser.add_subparsers(dest="action", required=True)
    for action in ("status", "patch", "restore"):
        action_parser = subparsers.add_parser(action)
        action_parser.add_argument("--app", required=True)
        if action in {"patch", "restore"}:
            action_parser.add_argument("--item", default="all")
        action_parser.add_argument("--json", action="store_true")
    args = parser.parse_args(argv)
    app_path = Path(args.app)
    if args.action == "status":
        data = handlers["enhancements_status"](app_path)
    elif args.action == "patch":
        data = handlers["enhancements_patch"](app_path, args.item)
    else:
        data = handlers["enhancements_restore"](app_path, args.item)
    ok = command_ok(str(data.get("status")))
    return emit_envelope(envelope(
        command=f"desktop codex-enhancements {args.action}",
        ok=ok,
        status=str(data.get("status", "ok")),
        data=data,
        error=None if ok else {"code": str(data.get("status")), "message": str(data.get("message", data.get("status")))},
    ))
