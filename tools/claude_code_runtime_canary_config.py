#!/usr/bin/env python3
"""Plan Claude Code Runtime canary placeholder groups safely.

This helper is intentionally dry-run only until the Claude Code bridge routing
trust contract is fully green and the operator explicitly approves database
writes. It emits the disabled bridge-pool group shape needed for review without
binding live upstream accounts or touching the shared 3012/3017 database.
"""
from __future__ import annotations

import argparse
import json
import sys
from typing import Any
from urllib.parse import SplitResult, urlsplit, urlunsplit

NATIVE_GROUP_ID = 8

_PLACEHOLDERS: tuple[dict[str, str], ...] = (
    {
        "name": "zhumeng-claude-code-bridge-openai",
        "provider": "openai",
        "client_type": "claude_code_bridge_openai",
        "route": "openai_bridge",
    },
    {
        "name": "zhumeng-claude-code-bridge-deepseek",
        "provider": "deepseek",
        "client_type": "claude_code_bridge_deepseek",
        "route": "deepseek_bridge",
    },
    {
        "name": "zhumeng-claude-code-bridge-agnes",
        "provider": "agnes",
        "client_type": "claude_code_bridge_agnes",
        "route": "agnes_bridge",
    },
    {
        "name": "zhumeng-claude-code-bridge-anthropic-compat",
        "provider": "anthropic_compat",
        "client_type": "claude_code_bridge_anthropic_compat",
        "route": "anthropic_compat_bridge",
    },
    {
        "name": "zhumeng-claude-code-bridge-glm",
        "provider": "zai_glm",
        "client_type": "claude_code_bridge_zai_glm",
        "route": "zai_glm_bridge",
    },
    {
        "name": "zhumeng-claude-code-bridge-kimi",
        "provider": "kimi",
        "client_type": "claude_code_bridge_kimi",
        "route": "kimi_bridge",
    },
)


def redact_target(target: str) -> str:
    """Return a target URL safe for logs/reports."""
    if not target:
        return ""
    try:
        parsed = urlsplit(target)
        if not parsed.scheme or not parsed.netloc:
            return "<redacted_target>"
        port = parsed.port
    except ValueError:
        return "<redacted_target>"

    hostname = parsed.hostname or ""
    if ":" in hostname and not hostname.startswith("["):
        hostname = f"[{hostname}]"
    host = hostname
    if port is not None:
        host = f"{host}:{port}"
    if parsed.username is not None or parsed.password is not None:
        host = f"***:***@{host}"
    safe = SplitResult(parsed.scheme, host, "", "", "")
    return urlunsplit(safe)


def _placeholder_group(spec: dict[str, str]) -> dict[str, Any]:
    return {
        "name": spec["name"],
        "platform": "claude_code_bridge",
        "status": "disabled",
        "provider": spec["provider"],
        "route": spec["route"],
        "client_type": spec["client_type"],
        "claude_code_only": True,
        "codex_gateway_entitled": False,
        "augment_gateway_entitled": False,
        "formal_pool_allowed": False,
        "native_attestation_allowed": False,
        "native_group_membership": False,
        "excluded_group_ids": [NATIVE_GROUP_ID],
        "upstream_account_bindings": [],
        "no_live_upstream_account_binding": True,
        "models_list_config": {
            "enabled": False,
            "live_bridge_enabled": False,
            "models": [],
        },
        "notes": [
            "dry-run placeholder only",
            "no live upstream account binding before route contract is green",
            "must not be added to Claude Code native formal-pool group",
        ],
    }


def build_bridge_placeholder_plan(target: str = "http://127.0.0.1:3017") -> dict[str, Any]:
    safe_target = redact_target(target)
    return {
        "schema_version": "claude-code-runtime-canary-config.v1",
        "mode": "dry-run",
        "writes_enabled": False,
        "target": safe_target,
        "safety_invariants": {
            "dry_run_default": True,
            "no_secrets_printed": True,
            "no_db_writes": True,
            "no_codex_gateway_group_mutation": True,
            "no_augment_gateway_group_mutation": True,
            "no_native_formal_pool_membership": True,
            "no_bridge_live_models_before_route_contract_green": True,
        },
        "actions": [
            {
                "action": "ensure_disabled_placeholder_group",
                "write_mode": "plan_only",
                "group": _placeholder_group(spec),
            }
            for spec in _PLACEHOLDERS
        ],
    }


def _render_text(plan: dict[str, Any]) -> str:
    lines = [
        "Claude Code Runtime canary config plan (dry-run only)",
        f"target: {plan['target']}",
        "writes_enabled: false",
        "actions:",
    ]
    for action in plan["actions"]:
        group = action["group"]
        lines.append(
            "- {name}: status=disabled formal_pool_allowed=false "
            "models_list_config.enabled=false upstream_bindings=0 native_group_membership=false".format(
                name=group["name"]
            )
        )
    return "\n".join(lines) + "\n"


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Plan disabled Claude Code bridge placeholder groups without DB writes.",
    )
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true", help="Print the plan without writing anything (default).")
    mode.add_argument("--apply", action="store_true", help="Fail closed until an approved DB-write workflow exists.")
    parser.add_argument("--target", default="http://127.0.0.1:3017", help="Local Sub2API target used only for redacted plan labeling.")
    parser.add_argument("--format", choices=("text", "json"), default="text")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_arg_parser()
    args = parser.parse_args(argv)
    if args.apply:
        print(
            "Refusing to apply: this helper is dry-run only until a user-approved DB-write workflow exists.",
            file=sys.stderr,
        )
        return 2

    plan = build_bridge_placeholder_plan(args.target)
    if args.format == "json":
        print(json.dumps(plan, ensure_ascii=True, indent=2, sort_keys=True))
    else:
        sys.stdout.write(_render_text(plan))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
