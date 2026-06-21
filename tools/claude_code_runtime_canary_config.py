#!/usr/bin/env python3
"""Configure Claude Code Runtime canary metadata without leaking secrets.

Default mode is dry-run. The apply path is deliberately explicit and only
creates Claude Code dedicated bridge groups plus a redacted env file for the
3017 canary. It does not mutate existing Codex Gateway groups, the native group,
or account credentials.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
import subprocess
import sys
import secrets
import tempfile
from pathlib import Path
from typing import Any, Iterable
from urllib.parse import SplitResult, urlsplit, urlunsplit

NATIVE_GROUP_ID = 8
_DEFAULT_RUNTIME_HASH = "sha256:" + ("1" * 64)
_DEFAULT_OVERLAY_HASH = "sha256:" + ("2" * 64)
_CATALOG_VERSION = "cp4-cli-fixture-v1"
_REPO_ROOT = Path(__file__).resolve().parents[1]
_ARTIFACT_DIR = _REPO_ROOT / "artifacts" / "claude-code-runtime"
_DEFAULT_ENV_OUT = _ARTIFACT_DIR / "3017-claude-code-runtime.env"

_BRIDGE_MODELS_BY_PROVIDER: dict[str, tuple[dict[str, str], ...]] = {
    "openai": (
        {"model_id": "claude-code-bridge-gpt-5.5", "upstream_model": "gpt-5.5", "capability_tier": "opus_equivalent"},
        {"model_id": "claude-code-bridge-gpt-5.4", "upstream_model": "gpt-5.4", "capability_tier": "sonnet_equivalent"},
        {"model_id": "claude-code-bridge-gpt-5.4-mini", "upstream_model": "gpt-5.4-mini", "capability_tier": "fast"},
    ),
    "deepseek": (
        {"model_id": "claude-code-bridge-deepseek-v4-pro", "upstream_model": "deepseek-v4-pro", "capability_tier": "strong"},
        {"model_id": "claude-code-bridge-deepseek-v4-flash", "upstream_model": "deepseek-v4-flash", "capability_tier": "fast"},
    ),
    "agnes": (
        {"model_id": "claude-code-bridge-agnes-2.0-flash", "upstream_model": "agnes-2.0-flash", "capability_tier": "fast_multimodal_helper"},
    ),
    "zai_glm": (
        {"model_id": "claude-code-bridge-glm-5.2-1m", "upstream_model": "glm-5.2[1m]", "capability_tier": "strong_long_context"},
    ),
    "kimi": (
        {"model_id": "claude-code-bridge-kimi-k2.7-code", "upstream_model": "kimi-k2.7-code", "capability_tier": "strong_coding_agent"},
    ),
}

_PLACEHOLDERS: tuple[dict[str, str], ...] = (
    {"name": "zhumeng-claude-code-bridge-openai", "provider": "openai", "client_type": "claude_code_bridge_openai", "route": "openai_bridge"},
    {"name": "zhumeng-claude-code-bridge-deepseek", "provider": "deepseek", "client_type": "claude_code_bridge_deepseek", "route": "deepseek_bridge"},
    {"name": "zhumeng-claude-code-bridge-agnes", "provider": "agnes", "client_type": "claude_code_bridge_agnes", "route": "agnes_bridge"},
    {"name": "zhumeng-claude-code-bridge-glm", "provider": "zai_glm", "client_type": "claude_code_bridge_zai_glm", "route": "zai_glm_bridge"},
    {"name": "zhumeng-claude-code-bridge-kimi", "provider": "kimi", "client_type": "claude_code_bridge_kimi", "route": "kimi_bridge"},
)

_NATIVE_REMOTE_SUB2API_ACCOUNT_NAMES = "zhumeng-claude-code-native-upstream"
_RUNTIME_DISPATCH_GROUPS: tuple[dict[str, Any], ...] = (
    {
        "name": "zhumeng-claude-code-bridge-runtime-openai",
        "provider": "openai",
        "platform": "openai",
        "account_names": ("zhumeng-claude-code-bridge-openai-runtime",),
        "source_account_name": "codex-upstream-openai-compatible",
        "runtime_account_type": "upstream",
        "api_key_name": "zhumeng-claude-code-bridge-openai-runtime-key",
        "api_key_env": "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY",
        "models": _BRIDGE_MODELS_BY_PROVIDER["openai"],
    },
    {
        "name": "zhumeng-claude-code-bridge-runtime-deepseek",
        "provider": "deepseek",
        "platform": "anthropic",
        "source_platform": "openai",
        "account_names": ("zhumeng-claude-code-bridge-deepseek-anthropic",),
        "source_account_name": "codex-upstream-deepseek-v4",
        "api_key_name": "zhumeng-claude-code-bridge-deepseek-runtime-key",
        "api_key_env": "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY",
        "models": _BRIDGE_MODELS_BY_PROVIDER["deepseek"],
        "anthropic_base_url": "https://api.deepseek.com/anthropic",
    },
    {
        "name": "zhumeng-claude-code-bridge-runtime-agnes",
        "provider": "agnes",
        "platform": "openai",
        "account_names": ("zhumeng-claude-code-bridge-agnes-runtime",),
        "source_account_name": "codex-upstream-agnes-apihub",
        "runtime_account_type": "upstream",
        "api_key_name": "zhumeng-claude-code-bridge-agnes-runtime-key",
        "api_key_env": "SUB2API_CLAUDE_CODE_BRIDGE_AGNES_API_KEY",
        "models": _BRIDGE_MODELS_BY_PROVIDER["agnes"],
    },
)

_NATIVE_MODELS = (
    "claude-opus-4-8",
    "claude-sonnet-4-6",
    "claude-haiku-4-5-20251001",
)
def _native_display_models() -> list[str]:
    return list(_NATIVE_MODELS) + [
        model["model_id"]
        for provider_models in _BRIDGE_MODELS_BY_PROVIDER.values()
        for model in provider_models
    ]


def _native_models_list_config() -> dict[str, Any]:
    return {
        "enabled": True,
        "models": _native_display_models(),
    }


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


def _loopback_origin(target: str) -> str:
    parsed = urlsplit(target)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise ValueError("target must be an http(s) loopback origin")
    if parsed.path not in {"", "/"} or parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError("target must not contain path, query, fragment, or credentials")
    host = parsed.hostname.lower()
    if host not in {"127.0.0.1", "localhost", "::1"}:
        raise ValueError("target must be loopback")
    return urlunsplit(SplitResult(parsed.scheme, parsed.netloc, "", "", ""))



def _models_list_config_for_provider(provider: str, *, enabled: bool) -> dict[str, Any]:
    # Current backend domain.GroupModelsListConfig requires models to be []string.
    # Rich bridge metadata belongs in the provider catalog env, not this DB column.
    return {
        "enabled": bool(enabled),
        "models": [model["model_id"] for model in _BRIDGE_MODELS_BY_PROVIDER.get(provider, ())],
    }


def placeholder_models_list_config_for_test() -> list[dict[str, Any]]:
    return [_models_list_config_for_provider(spec["provider"], enabled=False) for spec in _PLACEHOLDERS]


def runtime_models_list_config_for_test() -> list[dict[str, Any]]:
    return [_runtime_models_list_config(spec) for spec in _RUNTIME_DISPATCH_GROUPS]


def _placeholder_group(spec: dict[str, str], *, enabled: bool = False) -> dict[str, Any]:
    return {
        "name": spec["name"],
        "platform": "claude_code_bridge",
        "status": "active" if enabled else "disabled",
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
        "models_list_config": _models_list_config_for_provider(spec["provider"], enabled=bool(enabled)),
        "notes": [
            "Claude Code dedicated bridge pool",
            "no live upstream account binding in this metadata row",
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


def _load_route_trust_module():
    module_path = _REPO_ROOT / "tools" / "claude_code_route_trust.py"
    spec = importlib.util.spec_from_file_location("zhumeng_canary_route_trust", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load Claude Code route trust module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module




def _provider_for_model_id(model_id: str) -> str | None:
    for provider, models in _BRIDGE_MODELS_BY_PROVIDER.items():
        if any(str(model["model_id"]) == str(model_id).strip() for model in models):
            return provider
    return None


def validate_live_bridge_models_supported(
    live_bridge_models: tuple[str, ...],
    *,
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> None:
    runtime_providers = {str(spec["provider"]) for spec in _RUNTIME_DISPATCH_GROUPS}
    statuses = {str(provider): str(status) for provider, status in (provider_release_statuses or {}).items()}
    expanded = {str(provider).strip() for provider in expanded_live_providers if str(provider).strip()}
    for model_id in live_bridge_models:
        provider = _provider_for_model_id(model_id)
        if provider is None:
            raise ValueError(f"unknown live bridge model: {model_id}")
        if provider not in {"openai", "deepseek", "agnes", "zai_glm", "kimi"}:
            raise ValueError(f"unsupported live bridge provider: {provider}")
        if provider == "agnes" and statuses.get("agnes") != "strict-live-pass":
            raise ValueError("AGNES live bridge requires strict-live provider evidence before enabling")
        if provider == "zai_glm" and provider not in expanded:
            raise ValueError("GLM live bridge requires explicit expanded live scope and strict-live provider evidence")
        if provider == "kimi" and provider not in expanded:
            raise ValueError("Kimi live bridge requires explicit expanded live scope and strict-live provider evidence")
        if provider in {"zai_glm", "kimi"} and statuses.get(provider) != "strict-live-pass":
            label = "GLM" if provider == "zai_glm" else "Kimi"
            raise ValueError(f"{label} live bridge requires strict-live provider evidence before enabling")
        if provider not in runtime_providers:
            raise ValueError(f"unsupported live bridge provider without runtime account: {provider}")

def _route_catalog_hash(
    *,
    runtime_hash: str,
    overlay_hash: str,
    catalog_version: str,
    bridge_live_models: Iterable[str],
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> str:
    route_trust = _load_route_trust_module()
    provisional = route_trust.cp4_fixture_route_catalog(
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        catalog_hash="sha256:" + ("3" * 64),
        catalog_version=catalog_version,
        bridge_live_models=tuple(bridge_live_models),
        bridge_live_provider_statuses=provider_release_statuses,
        bridge_live_expanded_providers=expanded_live_providers,
    )
    return route_trust.route_catalog_content_hash(provisional)


def _route_catalog_entries(
    *,
    runtime_hash: str,
    overlay_hash: str,
    catalog_hash: str,
    catalog_version: str,
    bridge_live_models: Iterable[str],
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> dict[str, Any]:
    route_trust = _load_route_trust_module()
    catalog = route_trust.cp4_fixture_route_catalog(
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        catalog_hash=catalog_hash,
        catalog_version=catalog_version,
        bridge_live_models=tuple(bridge_live_models),
        bridge_live_provider_statuses=provider_release_statuses,
        bridge_live_expanded_providers=expanded_live_providers,
    )
    return catalog.entries


def _bridge_catalog_entry(entry: Any, model_meta: dict[str, str], runtime_origin: str, *, deepseek_anthropic_fixture_green: bool) -> dict[str, Any]:
    provider = entry.provider
    preferred_protocol = "anthropic_messages" if provider in {"deepseek", "zai_glm", "kimi"} else "responses"
    result: dict[str, Any] = {
        "model_id": entry.model_id,
        "upstream_model": model_meta["upstream_model"],
        "provider": provider,
        "route": entry.route,
        "client_type": entry.client_type,
        "provider_owner": entry.provider_owner,
        "credential_scope": entry.credential_scope,
        "gateway_location": entry.gateway_location,
        "catalog_fresh": True,
        "formal_pool_allowed": False,
        "native_attestation_allowed": False,
        "preferred_protocol": preferred_protocol,
        "capabilities_verified": True,
        "supports_text": True,
        "supports_tools": True,
        "supports_streaming": True,
        "supports_usage": True,
        "supports_cache_audit": True,
        "supports_reasoning_mapping": provider in {"deepseek", "openai", "zai_glm"},
        "supports_error_passthrough": True,
        "cache_policy": "provider_cache_audit_required",
        "capability_tier": model_meta.get("capability_tier", "standard"),
        "live_enabled": bool(entry.live_enabled),
    }
    if provider == "deepseek":
        result["reasoning_effort_levels"] = ["high", "max"]
        if deepseek_anthropic_fixture_green:
            result["anthropic_base_url"] = runtime_origin
        else:
            result["preferred_protocol"] = "openai_chat_completions"
            result["openai_base_url"] = runtime_origin
            result["fallback_protocol"] = "openai_chat_completions"
            result["fallback_reason"] = "anthropic_cache_fixture_failed"
    elif provider == "zai_glm":
        result["reasoning_effort_levels"] = ["high", "max"]
        result["anthropic_base_url"] = runtime_origin
    elif provider == "kimi":
        # Kimi K2.7 Code documents Claude Code "Thinking on", not a
        # multi-level effort enum. Do not invent UI levels here.
        result["anthropic_base_url"] = runtime_origin
    elif provider == "openai":
        result["reasoning_effort_levels"] = ["low", "medium", "high", "xhigh"]
        result["cache_policy"] = "responses_prompt_cache_key_exact_prefix"
        result["openai_base_url"] = runtime_origin
    else:
        result["supports_reasoning_mapping"] = False
        result["openai_base_url"] = runtime_origin
    return result


def build_provider_catalog_env(
    target: str = "http://127.0.0.1:3017",
    *,
    runtime_target: str | None = None,
    runtime_hash: str = _DEFAULT_RUNTIME_HASH,
    overlay_hash: str = _DEFAULT_OVERLAY_HASH,
    catalog_version: str = _CATALOG_VERSION,
    live_bridge_models: tuple[str, ...] = (),
    bridge_api_keys: dict[str, str] | None = None,
    deepseek_anthropic_fixture_green: bool = True,
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> dict[str, str]:
    target_origin = _loopback_origin(target)
    runtime_origin = _loopback_origin(runtime_target or target_origin)
    live_set = tuple(str(model).strip() for model in live_bridge_models if str(model).strip())
    validate_live_bridge_models_supported(
        live_set,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    catalog_hash = _route_catalog_hash(
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        catalog_version=catalog_version,
        bridge_live_models=live_set,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    route_entries = _route_catalog_entries(
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        catalog_hash=catalog_hash,
        catalog_version=catalog_version,
        bridge_live_models=live_set,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    meta_by_model = {model["model_id"]: model for models in _BRIDGE_MODELS_BY_PROVIDER.values() for model in models}
    catalog_models: list[dict[str, Any]] = []
    for model_id in _NATIVE_MODELS:
        entry = route_entries.get(model_id)
        if entry is None:
            continue
        catalog_models.append({
            "model_id": model_id,
            "provider": "claude",
            "route": entry.route,
            "client_type": entry.client_type,
            "provider_owner": entry.provider_owner,
            "credential_scope": entry.credential_scope,
            "gateway_location": entry.gateway_location,
            "catalog_fresh": True,
            "formal_pool_allowed": True,
            "native_attestation_allowed": True,
        })
    for model_id, model_meta in sorted(meta_by_model.items()):
        entry = route_entries[model_id]
        catalog_models.append(_bridge_catalog_entry(entry, model_meta, runtime_origin, deepseek_anthropic_fixture_green=deepseek_anthropic_fixture_green))
    catalog = {
        "catalog_version": catalog_version,
        "runtime_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "catalog_hash": catalog_hash,
        "models": catalog_models,
    }
    openai_bridge_live = any(model.startswith("claude-code-bridge-gpt-") for model in live_set)
    deepseek_bridge_live = any("deepseek" in model for model in live_set)
    env = {
        "SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON": json.dumps(catalog, ensure_ascii=True, sort_keys=True, separators=(",", ":")),
        "SUB2API_CLAUDE_CODE_ROUTE_HINT_CATALOG_VERSION": catalog_version,
        "SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES": runtime_hash,
        "SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES": overlay_hash,
        "SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES": catalog_hash,
        "SUB2API_CLAUDE_CODE_PUBLIC_TARGET_ORIGIN": target_origin,
        "SUB2API_CLAUDE_CODE_RUNTIME_TARGET_ORIGIN": runtime_origin,
        "SUB2API_CLAUDE_CODE_DEEPSEEK_ANTHROPIC_FIXTURE_GREEN": "true" if deepseek_anthropic_fixture_green else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED": "true" if live_set else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_LIVE_UNSAFE_BILLING_BYPASS_FOR_LAB": "true" if live_set else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED": "true" if openai_bridge_live else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_CHAT_COMPLETIONS_FALLBACK_ENABLED": "true" if openai_bridge_live else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED": "true" if any("deepseek" in model or "glm" in model or "kimi" in model for model in live_set) else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED": "true" if deepseek_bridge_live else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED": "true" if deepseek_bridge_live and not deepseek_anthropic_fixture_green else "false",
        "SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED": "true" if any("agnes" in model for model in live_set) else "false",
        "SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS": ",".join(_NATIVE_MODELS),
        "SUB2API_CLAUDE_CODE_NATIVE_REMOTE_SUB2API_ACCOUNT_NAMES": _NATIVE_REMOTE_SUB2API_ACCOUNT_NAMES,
    }
    if live_set:
        env["SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY"] = secrets.token_urlsafe(32)
        env["SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID"] = "claude-code-cache-audit-v1"
    for provider, api_key in (bridge_api_keys or {}).items():
        provider_key = str(provider).strip().upper().replace("-", "_")
        if provider_key and str(api_key).strip():
            env[f"SUB2API_CLAUDE_CODE_BRIDGE_{provider_key}_API_KEY"] = str(api_key).strip()
    return env


def _group_apply_sql_payload() -> list[dict[str, Any]]:
    return [_placeholder_group(spec, enabled=True) for spec in _PLACEHOLDERS]


def _runtime_models_list_config(spec: dict[str, Any]) -> dict[str, Any]:
    return _models_list_config_for_provider(str(spec["provider"]), enabled=True)


def _runtime_group_description(spec: dict[str, Any]) -> str:
    desc = {
        "provider": spec["provider"],
        "purpose": "Claude Code bridge runtime dispatch pool",
        "claude_code_only": True,
        "codex_gateway_entitled": False,
        "formal_pool_allowed": False,
        "native_attestation_allowed": False,
        "native_group_membership": False,
        "source_accounts": [str(spec.get("source_account_name", account)) for account in spec["account_names"]],
        "runtime_accounts": list(spec["account_names"]),
        "excluded_group_ids": [NATIVE_GROUP_ID],
        "notes": [
            "Canary runtime group for Claude Code bridge calls only",
            "Dedicated API key authenticates back into local Sub2API",
            "Do not add native Claude formal-pool accounts to this group",
        ],
    }
    return json.dumps(desc, ensure_ascii=True, sort_keys=True)


def _random_sub2api_key() -> str:
    return "sk-" + secrets.token_hex(32)


def _runtime_key_literals(bridge_api_keys: dict[str, str] | None, existing_env: dict[str, str] | None = None) -> dict[str, str]:
    provided = bridge_api_keys or {}
    existing = existing_env or {}
    out: dict[str, str] = {}
    for spec in _RUNTIME_DISPATCH_GROUPS:
        provider = str(spec["provider"])
        env_name = str(spec["api_key_env"])
        out[provider] = str(provided.get(provider) or existing.get(env_name) or _random_sub2api_key()).strip()
    return out


def build_runtime_bridge_api_keys(bridge_api_keys: dict[str, str] | None = None, existing_env: dict[str, str] | None = None) -> dict[str, str]:
    """Return provider->Sub2API API key values for env-file use only."""
    return _runtime_key_literals(bridge_api_keys, existing_env=existing_env)


def build_apply_sql(bridge_api_keys: dict[str, str] | None = None) -> str:
    groups = _group_apply_sql_payload()
    native_models_config = _native_models_list_config()
    values = []
    for group in groups:
        desc = {
            "provider": group["provider"],
            "route": group["route"],
            "client_type": group["client_type"],
            "formal_pool_allowed": False,
            "native_attestation_allowed": False,
            "native_group_membership": False,
            "notes": group["notes"],
        }
        values.append(
            "({name},{description},'claude_code_bridge','subscription','active',true,true,false,false,{models},'[]'::jsonb)".format(
                name=_sql_literal(group["name"]),
                description=_sql_literal(json.dumps(desc, ensure_ascii=True, sort_keys=True)),
                models=_sql_literal(json.dumps(group["models_list_config"], ensure_ascii=True, sort_keys=True)) + "::jsonb",
            )
        )

    runtime_group_values = []
    for spec in _RUNTIME_DISPATCH_GROUPS:
        runtime_group_values.append(
            "({name},{description},{platform},'subscription','active',true,true,false,false,{models},'[]'::jsonb)".format(
                name=_sql_literal(str(spec["name"])),
                description=_sql_literal(_runtime_group_description(spec)),
                platform=_sql_literal(str(spec["platform"])),
                models=_sql_literal(json.dumps(_runtime_models_list_config(spec), ensure_ascii=True, sort_keys=True)) + "::jsonb",
            )
        )

    account_binding_values = []
    for spec in _RUNTIME_DISPATCH_GROUPS:
        for account_name in spec["account_names"]:
            account_binding_values.append(
                "({account_name},{group_name},1)".format(
                    account_name=_sql_literal(str(account_name)),
                    group_name=_sql_literal(str(spec["name"])),
                )
            )

    runtime_account_values = []
    for spec in _RUNTIME_DISPATCH_GROUPS:
        source_name = str(spec.get("source_account_name", ""))
        if not source_name:
            continue
        source_platform = str(spec.get("source_platform", spec["platform"]))
        mapping = {str(model["model_id"]): str(model["upstream_model"]) for model in spec["models"]}
        # Also allow already-rewritten upstream IDs for direct diagnostics.
        mapping.update({str(model["upstream_model"]): str(model["upstream_model"]) for model in spec["models"]})
        credentials: dict[str, Any] = {
            "api_key": "__SOURCE_ACCOUNT_API_KEY__",
            "base_url": "__SOURCE_ACCOUNT_BASE_URL__",
            "model_mapping": mapping,
        }
        if str(spec.get("provider")) == "openai":
            # Preserve the upstream persona/profile expected by OpenAI-compatible Codex pools.
            # The source account owns the actual UA; SQL clones it without printing the value.
            credentials["user_agent"] = "__SOURCE_ACCOUNT_USER_AGENT__"
        if str(spec.get("platform")) == "anthropic" and spec.get("anthropic_base_url"):
            credentials["base_url"] = str(spec["anthropic_base_url"])
        extra = {
            "provider_role": str(spec["provider"]),
            "claude_code_bridge_runtime": True,
            "source_account_name": source_name,
        }
        if str(spec.get("platform")) == "anthropic":
            extra["anthropic_passthrough"] = True
        runtime_account_values.append(
            "({account_name},{source_name},{source_platform},{platform},{account_type},'active',true,{credentials},{extra})".format(
                account_name=_sql_literal(str(spec["account_names"][0])),
                source_name=_sql_literal(source_name),
                source_platform=_sql_literal(source_platform),
                platform=_sql_literal(str(spec["platform"])),
                account_type=_sql_literal(str(spec.get("runtime_account_type", "apikey"))),
                credentials=_sql_literal(json.dumps(credentials, ensure_ascii=True, sort_keys=True)) + "::jsonb",
                extra=_sql_literal(json.dumps(extra, ensure_ascii=True, sort_keys=True)) + "::jsonb",
            )
        )

    api_key_values = []
    key_values = _runtime_key_literals(bridge_api_keys)
    for spec in _RUNTIME_DISPATCH_GROUPS:
        provider = str(spec["provider"])
        api_key_values.append(
            "(1,{key},{name},{group_name},'active','claude_code_runtime')".format(
                key=_sql_literal(key_values[provider]),
                name=_sql_literal(str(spec["api_key_name"])),
                group_name=_sql_literal(str(spec["name"])),
            )
        )

    return """
BEGIN;
UPDATE groups
SET models_list_config = {native_models_config},
    claude_code_only = true,
    codex_gateway_entitled = false,
    augment_gateway_entitled = false,
    updated_at = now()
WHERE id = {native_group_id}
  AND name = 'zhumeng-claude-code-native'
  AND deleted_at IS NULL;

WITH desired(name, description, platform, subscription_type, status, is_exclusive, claude_code_only, codex_gateway_entitled, augment_gateway_entitled, models_list_config, supported_model_scopes) AS (
  VALUES
  {values}
)
INSERT INTO groups (
  name, description, platform, subscription_type, status, is_exclusive,
  claude_code_only, codex_gateway_entitled, augment_gateway_entitled,
  models_list_config, supported_model_scopes, rate_multiplier, default_validity_days,
  allow_messages_dispatch, model_routing_enabled, mcp_xml_inject, sort_order,
  allow_image_generation, image_rate_independent, image_rate_multiplier
)
SELECT name, description, platform, subscription_type, status, is_exclusive,
       claude_code_only, codex_gateway_entitled, augment_gateway_entitled,
       models_list_config, supported_model_scopes, 1.0, 30,
       false, false, false, 4700,
       false, false, 1.0
FROM desired
ON CONFLICT (name) WHERE deleted_at IS NULL DO UPDATE SET
  description = EXCLUDED.description,
  platform = EXCLUDED.platform,
  subscription_type = EXCLUDED.subscription_type,
  status = EXCLUDED.status,
  is_exclusive = EXCLUDED.is_exclusive,
  claude_code_only = EXCLUDED.claude_code_only,
  codex_gateway_entitled = false,
  augment_gateway_entitled = false,
  models_list_config = EXCLUDED.models_list_config,
  supported_model_scopes = EXCLUDED.supported_model_scopes,
  allow_messages_dispatch = false,
  model_routing_enabled = false,
  mcp_xml_inject = false,
  updated_at = now();

WITH desired_runtime(name, description, platform, subscription_type, status, is_exclusive, claude_code_only, codex_gateway_entitled, augment_gateway_entitled, models_list_config, supported_model_scopes) AS (
  VALUES
  {runtime_group_values}
)
INSERT INTO groups (
  name, description, platform, subscription_type, status, is_exclusive,
  claude_code_only, codex_gateway_entitled, augment_gateway_entitled,
  models_list_config, supported_model_scopes, rate_multiplier, default_validity_days,
  allow_messages_dispatch, model_routing_enabled, mcp_xml_inject, sort_order,
  allow_image_generation, image_rate_independent, image_rate_multiplier
)
SELECT name, description, platform, subscription_type, status, is_exclusive,
       claude_code_only, codex_gateway_entitled, augment_gateway_entitled,
       models_list_config, supported_model_scopes, 1.0, 30,
       true, false, false, 4710,
       false, false, 1.0
FROM desired_runtime
ON CONFLICT (name) WHERE deleted_at IS NULL DO UPDATE SET
  description = EXCLUDED.description,
  platform = EXCLUDED.platform,
  subscription_type = EXCLUDED.subscription_type,
  status = EXCLUDED.status,
  is_exclusive = EXCLUDED.is_exclusive,
  claude_code_only = EXCLUDED.claude_code_only,
  codex_gateway_entitled = false,
  augment_gateway_entitled = false,
  models_list_config = EXCLUDED.models_list_config,
  supported_model_scopes = EXCLUDED.supported_model_scopes,
  allow_messages_dispatch = true,
  model_routing_enabled = false,
  mcp_xml_inject = false,
  updated_at = now();

WITH desired_runtime_accounts(account_name, source_account_name, source_platform, platform, type, status, schedulable, credentials_template, extra) AS (
  VALUES
  {runtime_account_values}
), source_runtime_accounts AS (
  SELECT DISTINCT ON (accounts.name) accounts.name, accounts.credentials, accounts.extra, accounts.concurrency
  FROM accounts
  JOIN desired_runtime_accounts ON desired_runtime_accounts.source_account_name = accounts.name
  WHERE accounts.deleted_at IS NULL
    AND accounts.status = 'active'
    AND accounts.schedulable = true
    AND accounts.platform = desired_runtime_accounts.source_platform
    AND accounts.credentials ? 'api_key'
  ORDER BY accounts.name, accounts.id
), validate_runtime_sources AS (
  SELECT CASE
    WHEN COUNT(source_runtime_accounts.name) <> COUNT(desired_runtime_accounts.source_account_name) THEN
      CAST((ARRAY['1','missing Claude Code bridge runtime source accounts'])[1 + LEAST(GREATEST((COUNT(desired_runtime_accounts.source_account_name) - COUNT(source_runtime_accounts.name))::integer, 0), 1)] AS integer)
    ELSE 1
  END AS all_sources_present
  FROM desired_runtime_accounts
  LEFT JOIN source_runtime_accounts ON source_runtime_accounts.name = desired_runtime_accounts.source_account_name
), resolved_runtime_accounts AS (
  SELECT desired_runtime_accounts.account_name, desired_runtime_accounts.platform, desired_runtime_accounts.type,
         desired_runtime_accounts.status, desired_runtime_accounts.schedulable,
         CASE
           WHEN desired_runtime_accounts.credentials_template->>'user_agent' = '__SOURCE_ACCOUNT_USER_AGENT__' THEN
             jsonb_set(
               jsonb_set(
                 jsonb_set(
                   desired_runtime_accounts.credentials_template,
                   '{{api_key}}',
                   to_jsonb(source_runtime_accounts.credentials->>'api_key'),
                   true
                 ),
                 '{{base_url}}',
                 to_jsonb(COALESCE(NULLIF(desired_runtime_accounts.credentials_template->>'base_url', '__SOURCE_ACCOUNT_BASE_URL__'), source_runtime_accounts.credentials->>'base_url')),
                 true
               ),
               '{{user_agent}}',
               to_jsonb(COALESCE(source_runtime_accounts.credentials->>'user_agent', '')),
               true
             )
           ELSE
             jsonb_set(
               jsonb_set(
                 desired_runtime_accounts.credentials_template,
                 '{{api_key}}',
                 to_jsonb(source_runtime_accounts.credentials->>'api_key'),
                 true
               ),
               '{{base_url}}',
               to_jsonb(COALESCE(NULLIF(desired_runtime_accounts.credentials_template->>'base_url', '__SOURCE_ACCOUNT_BASE_URL__'), source_runtime_accounts.credentials->>'base_url')),
               true
             )
         END AS credentials,
         (safe_source_extra || desired_runtime_accounts.extra) AS extra,
         GREATEST(COALESCE(source_runtime_accounts.concurrency, 3), 3) + (validate_runtime_sources.all_sources_present - validate_runtime_sources.all_sources_present) AS concurrency
  FROM desired_runtime_accounts
  JOIN source_runtime_accounts ON source_runtime_accounts.name = desired_runtime_accounts.source_account_name
  CROSS JOIN validate_runtime_sources
  CROSS JOIN LATERAL (
    SELECT jsonb_strip_nulls(jsonb_build_object('codex_gateway_local_test', source_runtime_accounts.extra->'codex_gateway_local_test')) AS safe_source_extra
  ) safe_extra
), existing_runtime_accounts AS (
  SELECT DISTINCT ON (accounts.name) accounts.id, accounts.name
  FROM accounts
  JOIN resolved_runtime_accounts ON resolved_runtime_accounts.account_name = accounts.name
  WHERE accounts.deleted_at IS NULL
  ORDER BY accounts.name, accounts.id
), updated_runtime_accounts AS (
  UPDATE accounts
  SET platform = resolved_runtime_accounts.platform,
      type = resolved_runtime_accounts.type,
      status = resolved_runtime_accounts.status,
      schedulable = resolved_runtime_accounts.schedulable,
      credentials = resolved_runtime_accounts.credentials,
      extra = resolved_runtime_accounts.extra,
      concurrency = resolved_runtime_accounts.concurrency,
      proxy_id = NULL,
      proxy_fallback_origin_id = NULL,
      temp_unschedulable_until = NULL,
      temp_unschedulable_reason = NULL,
      overload_until = NULL,
      error_message = NULL,
      updated_at = now()
  FROM resolved_runtime_accounts
  JOIN existing_runtime_accounts ON existing_runtime_accounts.name = resolved_runtime_accounts.account_name
  WHERE accounts.id = existing_runtime_accounts.id
  RETURNING accounts.name
)
INSERT INTO accounts (
  name, platform, type, status, schedulable, credentials, extra,
  concurrency, priority, rate_multiplier, created_at, updated_at
)
SELECT account_name, platform, type, status, schedulable, credentials, extra,
       concurrency, 50, 1.0, now(), now()
FROM resolved_runtime_accounts
WHERE NOT EXISTS (
  SELECT 1 FROM updated_runtime_accounts WHERE updated_runtime_accounts.name = resolved_runtime_accounts.account_name
);

WITH desired_bindings(account_name, group_name, priority) AS (
  VALUES
  {account_binding_values}
), runtime_groups AS (
  SELECT groups.id AS group_id, array_agg(accounts.id) AS desired_account_ids
  FROM desired_bindings
  JOIN accounts ON accounts.name = desired_bindings.account_name AND accounts.deleted_at IS NULL
  JOIN groups ON groups.name = desired_bindings.group_name AND groups.deleted_at IS NULL
  WHERE groups.id <> {native_group_id}
  GROUP BY groups.id
), pruned_runtime_bindings AS (
  DELETE FROM account_groups
  USING runtime_groups
  WHERE account_groups.group_id = runtime_groups.group_id
    AND account_groups.account_id <> ALL(runtime_groups.desired_account_ids)
  RETURNING account_groups.group_id
)
INSERT INTO account_groups (account_id, group_id, priority)
SELECT accounts.id, groups.id, desired_bindings.priority
FROM desired_bindings
JOIN accounts ON accounts.name = desired_bindings.account_name AND accounts.deleted_at IS NULL
JOIN groups ON groups.name = desired_bindings.group_name AND groups.deleted_at IS NULL
WHERE groups.id <> {native_group_id}
ON CONFLICT (account_id, group_id) DO UPDATE SET priority = EXCLUDED.priority;

WITH desired_keys(user_id, key, name, group_name, status, restricted_client_product) AS (
  VALUES
  {api_key_values}
), resolved_runtime_keys AS (
  SELECT desired_keys.user_id, desired_keys.key, desired_keys.name, groups.id AS group_id, desired_keys.status, desired_keys.restricted_client_product
  FROM desired_keys
  JOIN groups ON groups.name = desired_keys.group_name AND groups.deleted_at IS NULL
  WHERE groups.id <> {native_group_id}
), existing_runtime_keys AS (
  SELECT DISTINCT ON (api_keys.name) api_keys.id, api_keys.name
  FROM api_keys
  JOIN resolved_runtime_keys ON resolved_runtime_keys.name = api_keys.name
  WHERE api_keys.deleted_at IS NULL
  ORDER BY api_keys.name, api_keys.id
), updated_runtime_keys AS (
  UPDATE api_keys
  SET key = resolved_runtime_keys.key,
      user_id = resolved_runtime_keys.user_id,
      group_id = resolved_runtime_keys.group_id,
      status = resolved_runtime_keys.status,
      restricted_client_product = resolved_runtime_keys.restricted_client_product,
      updated_at = now()
  FROM resolved_runtime_keys
  JOIN existing_runtime_keys ON existing_runtime_keys.name = resolved_runtime_keys.name
  WHERE api_keys.id = existing_runtime_keys.id
  RETURNING api_keys.name
)
INSERT INTO api_keys (user_id, key, name, group_id, status, restricted_client_product)
SELECT user_id, key, name, group_id, status, restricted_client_product
FROM resolved_runtime_keys
WHERE NOT EXISTS (
  SELECT 1 FROM updated_runtime_keys WHERE updated_runtime_keys.name = resolved_runtime_keys.name
);
COMMIT;
""".format(
        native_models_config=_sql_literal(json.dumps(native_models_config, ensure_ascii=True, sort_keys=True)) + "::jsonb",
        values=",\n  ".join(values),
        runtime_group_values=",\n  ".join(runtime_group_values),
        runtime_account_values=",\n  ".join(runtime_account_values) if runtime_account_values else "('__none__','__none__','anthropic','anthropic','apikey','disabled',false,'{{}}'::jsonb,'{{}}'::jsonb)",
        account_binding_values=",\n  ".join(account_binding_values),
        api_key_values=",\n  ".join(api_key_values),
        native_group_id=NATIVE_GROUP_ID,
    )


def _sql_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"



def _read_env_file(path: Path) -> dict[str, str]:
    if not path.exists():
        return {}
    env: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        env[key.strip()] = value.strip()
    return env


def _env_flag(env: dict[str, str], key: str) -> bool:
    return str(env.get(key, "")).strip().lower() in {"1", "true", "yes", "on"}


def _safe_sha256_hash(value: object) -> str:
    text = str(value or "").strip()
    if len(text) == 71 and text.startswith("sha256:") and all(ch in "0123456789abcdef" for ch in text[7:]):
        return text
    return ""


def _safe_hash_list(value: object) -> list[str]:
    out: list[str] = []
    for part in str(value or "").replace(",", " ").split():
        safe = _safe_sha256_hash(part)
        if safe and safe not in out:
            out.append(safe)
    return out


def build_canary_env_readiness_metadata(
    env: dict[str, str],
    *,
    approved_expanded_live_providers: tuple[str, ...] = (),
    expected_runtime_hash: str | None = None,
) -> dict[str, Any]:
    """Return secret-free readiness evidence for the 3017 canary env/catalog."""
    issues: list[str] = []
    catalog_status = "missing"
    catalog: dict[str, Any] = {}
    raw_catalog = env.get("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", "")
    if raw_catalog:
        try:
            parsed = json.loads(raw_catalog)
            if isinstance(parsed, dict):
                catalog = parsed
                catalog_status = "ok"
            else:
                catalog_status = "invalid_shape"
                issues.append("provider catalog JSON must be an object")
        except json.JSONDecodeError:
            catalog_status = "invalid_json"
            issues.append("provider catalog JSON is invalid")
    else:
        issues.append("missing provider catalog JSON")

    expected_runtime_hash_raw = str(expected_runtime_hash or "").strip()
    expected_runtime_hash_safe = _safe_sha256_hash(expected_runtime_hash_raw)
    env_runtime_hashes = _safe_hash_list(env.get("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES", ""))
    catalog_runtime_hash = _safe_sha256_hash(catalog.get("runtime_hash")) if catalog_status == "ok" else ""
    env_matches_catalog_runtime_hash = bool(catalog_runtime_hash and catalog_runtime_hash in env_runtime_hashes)
    env_matches_requested_runtime_hash = (
        bool(expected_runtime_hash_safe and expected_runtime_hash_safe in env_runtime_hashes and catalog_runtime_hash == expected_runtime_hash_safe)
        if expected_runtime_hash_safe
        else env_matches_catalog_runtime_hash
    )
    required_singleton_runtime_hash = expected_runtime_hash_safe or catalog_runtime_hash
    env_native_runtime_hashes_exact = bool(required_singleton_runtime_hash and env_runtime_hashes == [required_singleton_runtime_hash])
    if catalog_status == "ok":
        if expected_runtime_hash_raw and not expected_runtime_hash_safe:
            issues.append("provided Claude Code runtime hash is not a safe sha256 value")
        if not env_runtime_hashes:
            issues.append("missing SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES")
        if not catalog_runtime_hash:
            issues.append("provider catalog JSON missing safe runtime_hash")
        elif not env_matches_catalog_runtime_hash:
            issues.append("Claude Code runtime hash drift between env and provider catalog")
        elif not env_native_runtime_hashes_exact:
            issues.append("Claude Code native runtime hash allowlist must contain only the active managed runtime hash")
        if expected_runtime_hash_safe and not env_matches_requested_runtime_hash:
            issues.append("Claude Code runtime hash drift from active managed runtime")

    raw_models = catalog.get("models", [])
    models = [model for model in raw_models if isinstance(model, dict)]
    bridge_models = [model for model in models if str(model.get("model_id", "")).startswith("claude-code-bridge-")]
    native_models = [model for model in models if str(model.get("provider", "")) == "claude"]
    deepseek_models = [model for model in bridge_models if str(model.get("provider", "")) == "deepseek" or "deepseek" in str(model.get("model_id", ""))]
    deepseek_live_models = [model for model in deepseek_models if bool(model.get("live_enabled"))]

    fallback_key = "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED"
    fallback_gate_present = fallback_key in env
    fallback_enabled = _env_flag(env, fallback_key)
    deepseek_live_enabled = _env_flag(env, "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED")
    anthropic_live_enabled = _env_flag(env, "SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED")
    allowed_protocols = {"anthropic_messages", "openai_chat_completions", "responses"}
    raw_deepseek_protocols = [str(model.get("preferred_protocol", "")) for model in deepseek_live_models if model.get("preferred_protocol")]
    unknown_protocol_count = sum(1 for protocol in raw_deepseek_protocols if protocol not in allowed_protocols)
    deepseek_selected_protocols = sorted(
        {protocol if protocol in allowed_protocols else "unknown" for protocol in raw_deepseek_protocols}
    )
    all_live_deepseek_anthropic = bool(deepseek_live_models) and all(
        str(model.get("preferred_protocol", "")) == "anthropic_messages"
        for model in deepseek_live_models
    )
    all_live_deepseek_openai_fallback = bool(deepseek_live_models) and all(
        str(model.get("preferred_protocol", "")) == "openai_chat_completions"
        for model in deepseek_live_models
    )
    cache_evidence_eligible = bool(deepseek_live_models) and all_live_deepseek_anthropic and not fallback_enabled
    cache_audit_hmac_key_present = bool(str(env.get("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY", "")).strip())
    cache_audit_hmac_key_id = str(env.get("SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID", "")).strip() or "missing"

    if not fallback_gate_present:
        issues.append("missing SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED gate")
    if fallback_enabled and not deepseek_live_models:
        issues.append("DeepSeek OpenAI fallback gate is enabled without a live DeepSeek bridge model")
    if fallback_enabled and deepseek_live_models and not all_live_deepseek_openai_fallback:
        issues.append("DeepSeek OpenAI fallback gate requires live catalog preferred_protocol=openai_chat_completions")
    if unknown_protocol_count:
        issues.append("DeepSeek live catalog contains unknown preferred_protocol labels")
    if deepseek_live_enabled and not deepseek_live_models:
        issues.append("DeepSeek live env flag is true but catalog has no live DeepSeek bridge model")
    if deepseek_live_models and not deepseek_live_enabled:
        issues.append("DeepSeek catalog has live models but DeepSeek live env flag is false")
    if deepseek_live_models and not fallback_enabled:
        if not anthropic_live_enabled:
            issues.append("DeepSeek Anthropic-compatible live env flag is false")
        if not all_live_deepseek_anthropic:
            issues.append("DeepSeek live catalog must prefer anthropic_messages unless explicit fallback is enabled")
    if cache_evidence_eligible and not cache_audit_hmac_key_present:
        issues.append("DeepSeek cache audit requires SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY")

    bridge_isolated = True
    for model in bridge_models:
        if bool(model.get("formal_pool_allowed")) or bool(model.get("native_attestation_allowed")):
            bridge_isolated = False
            break
    native_formal_pool_models = {
        model.strip()
        for model in str(env.get("SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS", "")).split(",")
        if model.strip()
    }
    native_formal_pool_has_bridge_model = any(model.startswith("claude-code-bridge-") for model in native_formal_pool_models)
    if native_formal_pool_has_bridge_model:
        bridge_isolated = False
        issues.append("native formal-pool env list contains bridge model ids")
    if not bridge_isolated:
        issues.append("bridge models must not be allowed into the native Claude formal pool")

    agnes_live = any(str(model.get("provider", "")) == "agnes" and bool(model.get("live_enabled")) for model in bridge_models) or _env_flag(env, "SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED")
    glm_live = any(str(model.get("provider", "")) == "zai_glm" and bool(model.get("live_enabled")) for model in bridge_models) or _env_flag(env, "SUB2API_CLAUDE_CODE_BRIDGE_ZAI_GLM_LIVE_ENABLED")
    kimi_live = any(str(model.get("provider", "")) == "kimi" and bool(model.get("live_enabled")) for model in bridge_models) or _env_flag(env, "SUB2API_CLAUDE_CODE_BRIDGE_KIMI_LIVE_ENABLED")
    approved_expanded, unknown_approval_count = _approved_expanded_provider_set(approved_expanded_live_providers)
    if agnes_live and "agnes" not in approved_expanded:
        issues.append("AGNES live scope requires explicit readiness approval")
    if glm_live and "zai_glm" not in approved_expanded:
        issues.append("GLM live scope requires explicit readiness approval")
    if kimi_live and "kimi" not in approved_expanded:
        issues.append("Kimi live scope requires explicit readiness approval")

    bridge_api_key_present = {}
    for spec in _RUNTIME_DISPATCH_GROUPS:
        bridge_api_key_present[str(spec["provider"])] = bool(str(env.get(str(spec["api_key_env"]), "")).strip())

    return {
        "schema_version": "claude-code-runtime-canary-readiness.v1",
        "ready": not issues,
        "issues": issues,
        "catalog_parse_status": catalog_status,
        "runtime_hash_binding": {
            "env_native_runtime_hash": env_runtime_hashes[0] if len(env_runtime_hashes) == 1 else "",
            "env_native_runtime_hashes": env_runtime_hashes,
            "catalog_runtime_hash": catalog_runtime_hash,
            "expected_runtime_hash": expected_runtime_hash_safe,
            "env_matches_catalog_runtime_hash": env_matches_catalog_runtime_hash,
            "env_matches_requested_runtime_hash": env_matches_requested_runtime_hash,
            "env_native_runtime_hashes_exact": env_native_runtime_hashes_exact,
        },
        "catalog_summary": {
            "model_count": len(models),
            "native_model_count": len(native_models),
            "bridge_model_count": len(bridge_models),
            "live_bridge_model_count": sum(1 for model in bridge_models if bool(model.get("live_enabled"))),
        },
        "deepseek": {
            "live_enabled": bool(deepseek_live_models),
            "env_live_enabled": deepseek_live_enabled,
            "anthropic_live_enabled": anthropic_live_enabled,
            "openai_fallback_gate_present": fallback_gate_present,
            "openai_fallback_enabled": fallback_enabled,
            "live_model_count": len(deepseek_live_models),
            "live_selected_protocols": deepseek_selected_protocols,
            "unknown_protocol_count": unknown_protocol_count,
            "all_live_models_prefer_anthropic_messages": all_live_deepseek_anthropic,
            "cache_evidence_eligible": cache_evidence_eligible,
            "cache_audit_hmac_key_present": cache_audit_hmac_key_present,
            "cache_audit_hmac_key_id": cache_audit_hmac_key_id,
        },
        "formal_pool_isolation": {
            "native_model_count": len(native_models),
            "bridge_models_isolated_from_native_formal_pool": bridge_isolated,
            "native_formal_pool_env_model_count": len(native_formal_pool_models),
            "native_formal_pool_env_has_bridge_model": native_formal_pool_has_bridge_model,
        },
        "expanded_provider_scope": {
            "agnes_live_enabled": agnes_live,
            "glm_live_enabled": glm_live,
            "kimi_live_enabled": kimi_live,
            "approved_expanded_live_providers": sorted(approved_expanded),
            "unknown_approval_count": unknown_approval_count,
        },
        "bridge_api_key_presence": bridge_api_key_present,
    }


def _approved_expanded_provider_set(values: tuple[str, ...]) -> tuple[set[str], int]:
    aliases = {
        "agnes": "agnes",
        "glm": "zai_glm",
        "zai_glm": "zai_glm",
        "zai-glm": "zai_glm",
        "kimi": "kimi",
    }
    approved: set[str] = set()
    unknown = 0
    for raw in values:
        normalized = str(raw).strip().lower().replace(" ", "_")
        if not normalized:
            continue
        provider = aliases.get(normalized)
        if provider is None:
            unknown += 1
            continue
        approved.add(provider)
    return approved, unknown


def _catalog_provider_live_enabled(env: dict[str, str], provider: str) -> bool:
    try:
        catalog = json.loads(env.get("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", "") or "{}")
    except json.JSONDecodeError:
        catalog = {}
    if not isinstance(catalog, dict):
        return False
    models = catalog.get("models", [])
    if not isinstance(models, list):
        return False
    return any(
        isinstance(model, dict)
        and str(model.get("provider", "")) == provider
        and bool(model.get("live_enabled"))
        for model in models
    )


def _env_or_catalog_provider_live_enabled(env: dict[str, str], provider: str) -> bool:
    flag_by_provider = {
        "agnes": "SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED",
        "deepseek": "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED",
        "openai": "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_LIVE_ENABLED",
        "zai_glm": "SUB2API_CLAUDE_CODE_BRIDGE_ZAI_GLM_LIVE_ENABLED",
        "kimi": "SUB2API_CLAUDE_CODE_BRIDGE_KIMI_LIVE_ENABLED",
    }
    flag = flag_by_provider.get(provider)
    return bool(flag and _env_flag(env, flag)) or _catalog_provider_live_enabled(env, provider)


def _safe_env_flag_value(env: dict[str, str], key: str) -> str:
    if key not in env:
        return "missing"
    return "true" if _env_flag(env, key) else "false"


def _safe_cli_error(exc: Exception) -> str:
    message = str(exc)
    safe_messages = (
        "AGNES live bridge requires strict-live provider evidence before enabling",
        "GLM live bridge requires explicit expanded live scope and strict-live provider evidence",
        "Kimi live bridge requires explicit expanded live scope and strict-live provider evidence",
        "GLM live bridge requires strict-live provider evidence before enabling",
        "Kimi live bridge requires strict-live provider evidence before enabling",
        "provider release statuses must be a JSON object",
        "target must be an http(s) loopback origin",
        "target must not contain path, query, fragment, or credentials",
        "target must be loopback",
    )
    if message in safe_messages:
        return message
    if message.startswith("unsupported live bridge provider without runtime account: "):
        provider = message.rsplit(": ", 1)[-1]
        if provider in {"zai_glm", "kimi", "agnes", "deepseek", "openai"}:
            return message
    if message.startswith("unsupported live bridge provider: "):
        provider = message.rsplit(": ", 1)[-1]
        if provider in {"zai_glm", "kimi", "agnes", "deepseek", "openai"}:
            return message
    if message.startswith("unknown live bridge model: "):
        return "unknown live bridge model"
    return exc.__class__.__name__ or "error"


def build_preview_env_metadata(
    existing_env: dict[str, str],
    *,
    target: str = "http://127.0.0.1:3017",
    runtime_target: str | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    live_bridge_models: tuple[str, ...] = (),
    deepseek_anthropic_fixture_green: bool = True,
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
    approved_expanded_live_providers: tuple[str, ...] = (),
) -> dict[str, Any]:
    """Build a secret-free candidate env report without writing files or touching containers."""
    existing_readiness = build_canary_env_readiness_metadata(
        existing_env,
        approved_expanded_live_providers=approved_expanded_live_providers,
        expected_runtime_hash=runtime_hash,
    )
    bridge_api_keys = build_runtime_bridge_api_keys(existing_env=existing_env)
    candidate_env = merge_provider_catalog_env(
        existing_env,
        target=target,
        runtime_target=runtime_target,
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        live_bridge_models=live_bridge_models,
        bridge_api_keys=bridge_api_keys,
        deepseek_anthropic_fixture_green=deepseek_anthropic_fixture_green,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    candidate_readiness = build_canary_env_readiness_metadata(
        candidate_env,
        approved_expanded_live_providers=approved_expanded_live_providers,
        expected_runtime_hash=runtime_hash,
    )
    fallback_key = "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED"
    candidate_changes = {
        "deepseek_fallback_gate_will_be_added": fallback_key not in existing_env and fallback_key in candidate_env,
        "deepseek_fallback_gate_candidate_value": _safe_env_flag_value(candidate_env, fallback_key),
        "agnes_live_will_be_disabled": (
            _env_or_catalog_provider_live_enabled(existing_env, "agnes")
            and not _env_or_catalog_provider_live_enabled(candidate_env, "agnes")
        ),
        "runtime_hash_will_change": bool(runtime_hash and existing_readiness["runtime_hash_binding"]["env_native_runtime_hash"] != runtime_hash),
    }
    return {
        "schema_version": "claude-code-runtime-canary-env-preview.v1",
        "mode": "env-preview",
        "writes_enabled": False,
        "would_touch_3017": False,
        "would_touch_3012": False,
        "target": redact_target(target),
        "runtime_target": redact_target(runtime_target or target),
        "existing_readiness": existing_readiness,
        "candidate_readiness": candidate_readiness,
        "candidate_changes": candidate_changes,
        "live_bridge_models": sorted(str(model).strip() for model in live_bridge_models if str(model).strip()),
    }

def merge_provider_catalog_env(
    existing_env: dict[str, str] | None,
    *,
    target: str = "http://127.0.0.1:3017",
    runtime_target: str | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    catalog_version: str = _CATALOG_VERSION,
    live_bridge_models: tuple[str, ...] = (),
    bridge_api_keys: dict[str, str] | None = None,
    deepseek_anthropic_fixture_green: bool = True,
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> dict[str, str]:
    existing = existing_env or {}
    resolved_runtime_hash = (str(runtime_hash).strip() if runtime_hash else "") or existing.get("SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES", _DEFAULT_RUNTIME_HASH)
    resolved_overlay_hash = (str(overlay_hash).strip() if overlay_hash else "") or existing.get("SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES", _DEFAULT_OVERLAY_HASH)
    env = dict(existing)
    env.update(build_provider_catalog_env(
        target,
        runtime_target=runtime_target,
        runtime_hash=resolved_runtime_hash,
        overlay_hash=resolved_overlay_hash,
        catalog_version=catalog_version,
        live_bridge_models=live_bridge_models,
        bridge_api_keys=bridge_api_keys,
        deepseek_anthropic_fixture_green=deepseek_anthropic_fixture_green,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    ))
    for key in (
        "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID",
        "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET",
        "SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY",
        "SUB2API_CLAUDE_CODE_CACHE_AUDIT_HMAC_KEY_ID",
    ):
        if existing.get(key):
            env[key] = existing[key]
    return env

def _write_env_file(path: Path, env: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    lines = [f"{key}={str(value).replace(chr(10), '')}" for key, value in sorted(env.items())]
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def apply_bridge_groups(
    *,
    postgres_container: str,
    env_out: Path,
    target: str,
    live_bridge_models: tuple[str, ...],
    runtime_target: str | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    deepseek_anthropic_fixture_green: bool = True,
    provider_release_statuses: dict[str, str] | None = None,
    expanded_live_providers: tuple[str, ...] = (),
) -> dict[str, Any]:
    validate_live_bridge_models_supported(
        live_bridge_models,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    existing_env = _read_env_file(env_out)
    bridge_api_keys = build_runtime_bridge_api_keys(existing_env=existing_env)
    env = merge_provider_catalog_env(
        existing_env,
        target=target,
        runtime_target=runtime_target,
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        live_bridge_models=live_bridge_models,
        bridge_api_keys=bridge_api_keys,
        deepseek_anthropic_fixture_green=deepseek_anthropic_fixture_green,
        provider_release_statuses=provider_release_statuses,
        expanded_live_providers=expanded_live_providers,
    )
    sql = build_apply_sql(bridge_api_keys=bridge_api_keys)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as handle:
        handle.write(sql)
        sql_path = Path(handle.name)
    try:
        with sql_path.open("rb") as stdin:
            completed = subprocess.run(
                ["docker", "exec", "-i", postgres_container, "psql", "-U", "sub2api", "-d", "sub2api", "-X", "-v", "ON_ERROR_STOP=1"],
                stdin=stdin,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=False,
                check=False,
            )
        if completed.returncode != 0:
            raise RuntimeError("failed to apply Claude Code bridge groups")
    finally:
        try:
            sql_path.unlink()
        except OSError:
            pass
    _write_env_file(env_out, env)
    return build_apply_result_metadata(
        postgres_container=postgres_container,
        env_out=str(env_out),
        target=target,
        runtime_target=runtime_target or target,
        live_bridge_models=live_bridge_models,
        env=env,
    )


def build_apply_result_metadata(*, postgres_container: str, env_out: str, target: str, live_bridge_models: tuple[str, ...], env: dict[str, str], runtime_target: str | None = None) -> dict[str, Any]:
    bridge_env_names = {str(spec["provider"]): str(spec["api_key_env"]) for spec in _RUNTIME_DISPATCH_GROUPS}
    return {
        "mode": "applied",
        "writes_enabled": True,
        "target": redact_target(target),
        "runtime_target": redact_target(runtime_target or target),
        "postgres_container": postgres_container,
        "groups": [spec["name"] for spec in _PLACEHOLDERS],
        "runtime_dispatch_groups": [str(spec["name"]) for spec in _RUNTIME_DISPATCH_GROUPS],
        "runtime_dispatch_account_bindings": {str(spec["provider"]): list(spec["account_names"]) for spec in _RUNTIME_DISPATCH_GROUPS},
        "env_out": str(env_out),
        "env_keys": sorted(env),
        "bridge_api_key_env_names": bridge_env_names,
        "bridge_api_key_values": {provider: "<redacted>" for provider in bridge_env_names},
        "live_bridge_models": list(live_bridge_models),
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
            "- {name}: status={status} formal_pool_allowed=false models_list_config.enabled={enabled} "
            "upstream_bindings=0 native_group_membership=false".format(
                name=group["name"],
                status=group["status"],
                enabled=str(group["models_list_config"]["enabled"]).lower(),
            )
        )
    return "\n".join(lines) + "\n"


def _parse_models(raw: str) -> tuple[str, ...]:
    return tuple(part.strip() for part in raw.split(",") if part.strip())


def _parse_provider_release_statuses(raw: str) -> dict[str, str]:
    raw = str(raw or "").strip()
    if not raw:
        return {}
    payload = json.loads(raw)
    if not isinstance(payload, dict):
        raise ValueError("provider release statuses must be a JSON object")
    return {str(key): str(value) for key, value in payload.items()}


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Plan/apply Claude Code bridge canary groups without printing secrets.")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true", help="Print the plan without writing anything (default).")
    mode.add_argument("--apply", action="store_true", help="Apply only with --user-approved-db-write.")
    mode.add_argument("--verify-env", action="store_true", help="Read --env-out and print secret-free canary readiness metadata.")
    mode.add_argument("--preview-env", action="store_true", help="Preview the regenerated env and readiness without writing files or touching containers.")
    parser.add_argument("--user-approved-db-write", action="store_true", help="Required explicit confirmation for --apply.")
    parser.add_argument("--postgres-container", default="", help="Postgres container for approved apply; secrets are not printed.")
    parser.add_argument("--target", default="http://127.0.0.1:3017", help="Local Sub2API canary origin exposed to host tooling.")
    parser.add_argument("--runtime-target", default="", help="Loopback origin reachable from inside the canary runtime container; defaults to --target.")
    parser.add_argument("--runtime-hash", default="", help="Current managed Claude Code runtime hash; overrides stale env-file values during apply.")
    parser.add_argument("--overlay-hash", default="", help="Current managed Claude Code overlay hash; overrides stale env-file values during apply.")
    parser.add_argument("--deepseek-openai-fallback", action="store_true", help="Explicitly force DeepSeek OpenAI-compatible /chat/completions fallback when Anthropic-compatible fixture parity is not green.")
    parser.add_argument("--env-out", type=Path, default=_DEFAULT_ENV_OUT, help="Canary env file to write during approved apply.")
    parser.add_argument("--live-bridge-models", default="", help="Comma-separated bridge model ids to enable live in provider catalog.")
    parser.add_argument("--bridge-provider-release-statuses-json", default="", help="JSON object of provider release statuses; values are metadata only, never secrets.")
    parser.add_argument("--bridge-live-expanded-providers", default="", help="Comma-separated providers explicitly expanded beyond L8 default scope.")
    parser.add_argument("--verify-env-approved-expanded-providers", default="", help="Comma-separated expanded providers explicitly approved for readiness verification only.")
    parser.add_argument("--format", choices=("text", "json"), default="text")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_arg_parser()
    args = parser.parse_args(argv)
    live_bridge_models = _parse_models(args.live_bridge_models)
    try:
        provider_release_statuses = _parse_provider_release_statuses(args.bridge_provider_release_statuses_json)
    except (ValueError, json.JSONDecodeError) as exc:
        print(f"Refusing to apply: {_safe_cli_error(exc)}", file=sys.stderr)
        return 2
    expanded_live_providers = _parse_models(args.bridge_live_expanded_providers)
    approved_expanded_live_providers = _parse_models(args.verify_env_approved_expanded_providers)
    if args.apply:
        if not args.user_approved_db_write or not args.postgres_container:
            print(
                "Refusing to apply: dry-run only unless --user-approved-db-write and --postgres-container are provided.",
                file=sys.stderr,
            )
            return 2
        try:
            result = apply_bridge_groups(
                postgres_container=args.postgres_container,
                env_out=args.env_out,
                target=args.target,
                live_bridge_models=live_bridge_models,
                runtime_target=args.runtime_target or None,
                runtime_hash=args.runtime_hash or None,
                overlay_hash=args.overlay_hash or None,
                deepseek_anthropic_fixture_green=not args.deepseek_openai_fallback,
                provider_release_statuses=provider_release_statuses,
                expanded_live_providers=expanded_live_providers,
            )
        except Exception as exc:  # noqa: BLE001 - CLI must fail closed without leaking command details.
            print(f"Refusing to apply: {_safe_cli_error(exc)}", file=sys.stderr)
            return 2
        if args.format == "json":
            print(json.dumps(result, ensure_ascii=True, indent=2, sort_keys=True))
        else:
            print("Claude Code Runtime canary config applied")
            print(f"target: {result['target']}")
            print(f"groups: {len(result['groups'])}")
            print(f"env_out: {result['env_out']}")
        return 0

    if args.verify_env:
        result = build_canary_env_readiness_metadata(
            _read_env_file(args.env_out),
            approved_expanded_live_providers=approved_expanded_live_providers,
            expected_runtime_hash=args.runtime_hash or None,
        )
        if args.format == "json":
            print(json.dumps(result, ensure_ascii=True, indent=2, sort_keys=True))
        else:
            print("Claude Code Runtime canary env readiness")
            print(f"ready: {str(result['ready']).lower()}")
            print(f"catalog_parse_status: {result['catalog_parse_status']}")
            print(f"deepseek_protocols: {','.join(result['deepseek']['live_selected_protocols'])}")
            print(f"deepseek_cache_evidence_eligible: {str(result['deepseek']['cache_evidence_eligible']).lower()}")
            if result["issues"]:
                print("issues:")
                for issue in result["issues"]:
                    print(f"- {issue}")
        return 0 if result["ready"] else 1

    if args.preview_env:
        try:
            result = build_preview_env_metadata(
                _read_env_file(args.env_out),
                target=args.target,
                runtime_target=args.runtime_target or None,
                runtime_hash=args.runtime_hash or None,
                overlay_hash=args.overlay_hash or None,
                live_bridge_models=live_bridge_models,
                deepseek_anthropic_fixture_green=not args.deepseek_openai_fallback,
                provider_release_statuses=provider_release_statuses,
                expanded_live_providers=expanded_live_providers,
                approved_expanded_live_providers=approved_expanded_live_providers,
            )
        except Exception as exc:  # noqa: BLE001 - CLI must fail closed without leaking env contents.
            print(f"Refusing to preview env: {_safe_cli_error(exc)}", file=sys.stderr)
            return 2
        if args.format == "json":
            print(json.dumps(result, ensure_ascii=True, indent=2, sort_keys=True))
        else:
            print("Claude Code Runtime canary env preview")
            print("writes_enabled: false")
            print("would_touch_3017: false")
            print("would_touch_3012: false")
            print(f"existing_ready: {str(result['existing_readiness']['ready']).lower()}")
            print(f"candidate_ready: {str(result['candidate_readiness']['ready']).lower()}")
            if result["candidate_readiness"]["issues"]:
                print("candidate_issues:")
                for issue in result["candidate_readiness"]["issues"]:
                    print(f"- {issue}")
        return 0 if result["candidate_readiness"]["ready"] else 1

    plan = build_bridge_placeholder_plan(args.target)
    if args.format == "json":
        print(json.dumps(plan, ensure_ascii=True, indent=2, sort_keys=True))
    else:
        sys.stdout.write(_render_text(plan))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
