#!/usr/bin/env python3
"""Local Claude Code CLI guard for localhost-only /v1/messages validation."""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Mapping
from urllib.parse import urlsplit
import argparse
import base64
import hmac
import http.client
import ipaddress
import json
import os
import re
import secrets
import socket
import ssl
import subprocess
import threading
import time
import urllib.error
import urllib.request

from tools.cli_control_plane_intent import IntentValidationError, build_control_plane_intent
from tools.cli_guard_attestation import (
    ATTESTATION_HEADER,
    ATTESTATION_SIGNATURE_HEADER,
    AttestationValidationError,
    build_guard_attestation,
    guard_attestation_config_from_env,
)
from tools.claude_code_route_trust import (
    RouteCatalog,
    RouteDecision,
    RouteHintReplayCache,
    cp4_fixture_route_catalog,
    route_catalog_content_hash,
    verify_signed_route_hint_headers,
)
from tools.cli_control_plane_policy import (
    ControlPlanePolicy,
    PolicyConfigError,
    PolicyDecision,
    load_default_policy,
)
from tools.cli_session_budget import SessionBudgetLedger, SessionBudgetPolicy, session_key_from_headers

SENSITIVE_HEADERS = {"authorization", "x-api-key", "proxy-authorization", "cookie", "set-cookie"}
CLAUDE_CODE_UPSTREAM_HEADER_ALLOWLIST = {
    "accept",
    "accept-encoding",
    "content-type",
    "user-agent",
    "anthropic-beta",
    "anthropic-version",
    "x-app",
    "x-stainless-lang",
    "x-stainless-runtime",
    "x-stainless-package-version",
    "x-stainless-runtime-version",
}
SELECTED_HEADERS = CLAUDE_CODE_UPSTREAM_HEADER_ALLOWLIST | {"x-claude-code-session-id"}
SENSITIVE_BODY_KEYS = {"messages", "prompt", "body", "cch"}
NATIVE_CLIENT_TYPE = "claude_code_native"
NATIVE_ATTESTATION_SCOPE = "claude_code_native_takeover"
NATIVE_HEALTHCHECK_PROFILE = "real_claude_code_native_takeover_v1"
NATIVE_ROUTE = "claude_code_native"
NATIVE_PROVIDER_OWNER = "zhumeng_managed"
NATIVE_CREDENTIAL_SCOPE = "formal_pool"
NATIVE_GATEWAY_LOCATION = "cloud"
NATIVE_UNKNOWN_HASH = "sha256:" + ("0" * 64)
CAPTURE_LEVELS = {"summary", "deep", "local-raw"}
SAFE_VALUE_KEYS = {
    "event_name",
    "event_type",
    "model",
    "role",
    "stop_reason",
    "type",
}
_DEFAULT_POLICY: ControlPlanePolicy | None = None
_DEFAULT_POLICY_LOCK = threading.Lock()
_SAFE_HEADER_NAME_RE = re.compile(r"^[a-z0-9-]+$")
_SENSITIVE_TEXT_SPLIT_RE = re.compile(r"[^a-z0-9]+")
_BLOCKED_UPSTREAM_HOST_SNIPPETS = ("anthropic.com", "claude.ai", "claude.com")
_DEFAULT_COST_ENVELOPE_LIMITS = {
    "max_body_bytes": 2 * 1024 * 1024,
    "max_tokens": 32000,
    "max_tools": 40,
    "max_messages": 32,
    "max_content_blocks": 128,
    "max_text_bytes": 512 * 1024,
    "max_system_bytes": 256 * 1024,
    "max_tool_def_bytes": 256 * 1024,
    "allow_stream": True,
    "allow_thinking": True,
    "max_thinking_budget_tokens": 32000,
    "allow_assistant_messages": True,
    "allow_tool_content": True,
    "allowed_output_config_keys": ("format", "effort"),
}


class ExecutionController:
    """Policy hook for stopping a CLI process after the first canary response."""

    def __init__(self, mode: str = "none", stop_grace_seconds: float = 1.0):
        if mode not in {"none", "canary_single_message", "session_budgeted"}:
            raise ValueError(f"unsupported execution controller mode: {mode}")
        self.mode = mode
        self.stop_grace_seconds = stop_grace_seconds
        self._process: subprocess.Popen | None = None
        self._lock = threading.Lock()
        self._stop_requested = False

    def register_cli_process(self, process: subprocess.Popen) -> None:
        self._process = process

    def on_message_completed(self) -> dict[str, str] | None:
        if self.mode != "canary_single_message":
            return None
        with self._lock:
            if self._stop_requested:
                return None
            self._stop_requested = True
        thread = threading.Thread(target=self._terminate_registered_process, daemon=True)
        thread.start()
        return {"event": "execution_controller_stop_requested", "mode": self.mode}

    def _terminate_registered_process(self) -> None:
        proc = self._process
        if proc is None or proc.poll() is not None:
            return
        try:
            proc_group = os.getpgid(proc.pid)
            current_group = os.getpgrp()
            if proc_group != current_group:
                os.killpg(proc_group, 15)
            else:
                proc.terminate()
        except Exception:  # noqa: BLE001 - best effort termination only
            try:
                proc.terminate()
            except Exception:  # noqa: BLE001
                return
        try:
            proc.wait(timeout=self.stop_grace_seconds)
        except subprocess.TimeoutExpired:
            try:
                proc_group = os.getpgid(proc.pid)
                current_group = os.getpgrp()
                if proc_group != current_group:
                    os.killpg(proc_group, 9)
                else:
                    proc.kill()
            except Exception:  # noqa: BLE001
                try:
                    proc.kill()
                except Exception:  # noqa: BLE001
                    pass


@dataclass(frozen=True)
class GuardConfig:
    listen_host: str
    listen_port: int
    upstream_base: str
    sub2api_auth: str
    summary_path: Path
    control_plane_intent_url: str | None = None
    control_plane_intent_auth: str | None = None
    connect_mode: str = "block"
    cert_path: Path | None = None
    key_path: Path | None = None
    max_messages: int | None = None
    policy: ControlPlanePolicy | None = None
    extra_forward_headers: Mapping[str, str] | None = None
    cost_envelope_limits: Mapping[str, Any] | None = None
    session_budget_ledger: SessionBudgetLedger | None = None
    capture_level: str = "summary"
    local_raw_dir: Path | None = None
    allow_nonloopback_upstream: bool = False
    native_attestation_secret: str | None = None
    route_hint_secret: str | None = None
    route_hint_catalog: RouteCatalog | None = None
    route_hint_replay_cache: RouteHintReplayCache | None = None
    managed_session_id: str | None = None
    device_id: str | None = None
    agent_version: str | None = None

    def __post_init__(self) -> None:
        policy = self.policy or default_policy()
        if self.capture_level not in CAPTURE_LEVELS:
            raise ValueError(f"capture_level must be one of {sorted(CAPTURE_LEVELS)}")
        object.__setattr__(self, "policy", policy)
        object.__setattr__(
            self,
            "upstream_base",
            validate_loopback_url(
                self.upstream_base,
                allow_nonloopback=self.allow_nonloopback_upstream,
            ),
        )
        if self.capture_level == "local-raw" and self.local_raw_dir is None:
            object.__setattr__(self, "local_raw_dir", self.summary_path.parent / "raw-secure")
        if self.control_plane_intent_url is not None:
            object.__setattr__(
                self,
                "control_plane_intent_url",
                validate_loopback_url(self.control_plane_intent_url, field_name="control_plane_intent_url"),
            )
            if self.control_plane_intent_auth is None:
                default_intent_auth = os.environ.get("SUB2API_CONTROL_PLANE_INTENT_TOKEN")
                object.__setattr__(self, "control_plane_intent_auth", default_intent_auth or None)
        if self.max_messages is None:
            object.__setattr__(self, "max_messages", _policy_messages_config(policy).get("max_messages", 0))
        if bool(self.route_hint_secret) != bool(self.route_hint_catalog):
            raise ValueError("route_hint_secret and route_hint_catalog must be configured together")
        if self.route_hint_secret and self.route_hint_catalog is not None and self.route_hint_replay_cache is None:
            object.__setattr__(self, "route_hint_replay_cache", RouteHintReplayCache())


@dataclass(frozen=True)
class CostEnvelopeDecision:
    allowed: bool
    status: int
    reason: str
    metrics: dict[str, Any]


def default_policy() -> ControlPlanePolicy:
    global _DEFAULT_POLICY
    if _DEFAULT_POLICY is None:
        with _DEFAULT_POLICY_LOCK:
            if _DEFAULT_POLICY is None:
                _DEFAULT_POLICY = load_default_policy()
    return _DEFAULT_POLICY


def load_policy_from_path(path: Path | None) -> ControlPlanePolicy:
    if path is None:
        return default_policy()
    with path.open("r", encoding="utf-8") as handle:
        payload = json.load(handle)
    return ControlPlanePolicy.from_dict(payload)


def validate_loopback_url(url: str, field_name: str = "upstream_base", *, allow_nonloopback: bool = False) -> str:
    parsed = urlsplit(url)
    if parsed.scheme not in {"http", "https"}:
        raise ValueError(f"{field_name} must use http or https")
    host = parsed.hostname
    if host is None:
        raise ValueError(f"{field_name} must include a host")
    lowered_host = host.lower()
    if any(snippet in lowered_host for snippet in _BLOCKED_UPSTREAM_HOST_SNIPPETS):
        raise ValueError(f"{field_name} must not target external Claude hosts")
    if lowered_host == "localhost":
        return url
    try:
        ip = ipaddress.ip_address(lowered_host)
    except ValueError as exc:
        if allow_nonloopback:
            return url
        raise ValueError(f"{field_name} must target loopback only") from exc
    if not ip.is_loopback:
        if allow_nonloopback:
            return url
        raise ValueError(f"{field_name} must target loopback only")
    return url


def evaluate_cost_envelope(body: bytes, policy_or_config: GuardConfig | ControlPlanePolicy | Mapping[str, Any] | None = None) -> CostEnvelopeDecision:
    limits = _cost_envelope_limits(policy_or_config)
    metrics: dict[str, Any] = {
        "body_bytes": len(body),
        "max_body_bytes": limits["max_body_bytes"],
        "max_tokens_limit": limits["max_tokens"],
        "max_tools_limit": limits["max_tools"],
        "max_messages_limit": limits["max_messages"],
        "max_content_blocks_limit": limits["max_content_blocks"],
        "allow_stream": limits["allow_stream"],
        "allow_thinking": limits["allow_thinking"],
        "max_thinking_budget_tokens_limit": limits["max_thinking_budget_tokens"],
        "allow_assistant_messages": limits["allow_assistant_messages"],
        "allow_tool_content": limits["allow_tool_content"],
        "allowed_output_config_keys": list(limits["allowed_output_config_keys"]),
    }
    if len(body) > limits["max_body_bytes"]:
        return CostEnvelopeDecision(False, 413, "body_size_limit_exceeded", metrics)
    try:
        payload = json.loads(body.decode("utf-8")) if body else {}
    except Exception:  # noqa: BLE001 - fail closed on malformed payloads
        return CostEnvelopeDecision(False, 400, "invalid_messages_json", metrics)
    if not isinstance(payload, dict):
        return CostEnvelopeDecision(False, 422, "messages_shape_blocked", metrics)

    max_tokens = payload.get("max_tokens")
    metrics["max_tokens"] = _sanitize_max_tokens_value(max_tokens)
    if isinstance(max_tokens, int) and not isinstance(max_tokens, bool) and max_tokens > limits["max_tokens"]:
        return CostEnvelopeDecision(False, 422, "max_tokens_limit_exceeded", metrics)

    tools = payload.get("tools")
    tools_count = len(tools) if isinstance(tools, list) else 0
    metrics["tools_count"] = tools_count
    if tools_count > limits["max_tools"]:
        return CostEnvelopeDecision(False, 422, "tools_limit_exceeded", metrics)
    if tools is not None:
        tool_def_bytes = len(json.dumps(tools, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
        metrics["tool_def_bytes"] = tool_def_bytes
        if tool_def_bytes > limits["max_tool_def_bytes"]:
            return CostEnvelopeDecision(False, 413, "tool_definition_size_limit_exceeded", metrics)

    if payload.get("stream") is True and not limits["allow_stream"]:
        return CostEnvelopeDecision(False, 422, "stream_disabled", metrics)

    thinking = payload.get("thinking")
    if thinking is not None:
        metrics["thinking_present"] = True
        if not limits["allow_thinking"]:
            if isinstance(thinking, Mapping) and isinstance(thinking.get("budget_tokens"), int):
                metrics["thinking_budget_tokens"] = thinking.get("budget_tokens")
            return CostEnvelopeDecision(False, 422, "thinking_blocked", metrics)
        if not isinstance(thinking, Mapping):
            return CostEnvelopeDecision(False, 422, "thinking_shape_blocked", metrics)
        thinking_keys = sorted(key for key in thinking.keys() if isinstance(key, str))
        metrics["thinking_keys"] = [
            sanitized or "redacted-key"
            for key in thinking_keys
            for sanitized in [_sanitize_body_key(key)]
        ]
        if any(key not in {"type", "budget_tokens"} for key in thinking_keys):
            return CostEnvelopeDecision(False, 422, "thinking_shape_blocked", metrics)
        thinking_type = thinking.get("type")
        if not isinstance(thinking_type, str) or not thinking_type or _looks_sensitive_text(thinking_type):
            return CostEnvelopeDecision(False, 422, "thinking_shape_blocked", metrics)
        budget_tokens = thinking.get("budget_tokens")
        if budget_tokens is not None:
            if not isinstance(budget_tokens, int) or isinstance(budget_tokens, bool) or budget_tokens < 0:
                return CostEnvelopeDecision(False, 422, "thinking_shape_blocked", metrics)
            metrics["thinking_budget_tokens"] = budget_tokens
            if budget_tokens > limits["max_thinking_budget_tokens"]:
                return CostEnvelopeDecision(False, 422, "thinking_budget_limit_exceeded", metrics)
    else:
        metrics["thinking_present"] = False

    output_config = payload.get("output_config")
    if output_config is not None:
        if not isinstance(output_config, dict):
            return CostEnvelopeDecision(False, 422, "output_config_shape_blocked", metrics)
        output_keys = sorted(
            key for key in output_config.keys()
            if isinstance(key, str)
        )
        metrics["output_config_keys"] = [
            sanitized or "redacted-key"
            for key in output_keys
            for sanitized in [_sanitize_body_key(key)]
        ]
        allowed_keys = set(limits["allowed_output_config_keys"])
        if any(key not in allowed_keys for key in output_keys):
            return CostEnvelopeDecision(False, 422, "output_config_shape_blocked", metrics)

    if _has_tool_loop_markers(
        payload,
        allow_assistant_messages=bool(limits["allow_assistant_messages"]),
        allow_tool_content=bool(limits["allow_tool_content"]),
    ):
        return CostEnvelopeDecision(False, 422, "tool_loop_blocked", metrics)

    messages = payload.get("messages")
    if not isinstance(messages, list):
        return CostEnvelopeDecision(False, 422, "messages_shape_blocked", metrics)
    metrics["messages_count"] = len(messages)
    if len(messages) > limits["max_messages"]:
        return CostEnvelopeDecision(False, 422, "messages_limit_exceeded", metrics)

    content_metrics = _message_content_metrics(payload)
    metrics.update(content_metrics)
    if content_metrics["content_blocks_count"] > limits["max_content_blocks"]:
        return CostEnvelopeDecision(False, 422, "content_blocks_limit_exceeded", metrics)
    if content_metrics["text_bytes"] > limits["max_text_bytes"]:
        return CostEnvelopeDecision(False, 413, "text_size_limit_exceeded", metrics)
    if content_metrics["system_bytes"] > limits["max_system_bytes"]:
        return CostEnvelopeDecision(False, 413, "system_size_limit_exceeded", metrics)
    if content_metrics["tool_content_present"] and not limits["allow_tool_content"]:
        return CostEnvelopeDecision(False, 422, "tool_content_blocked", metrics)

    return CostEnvelopeDecision(True, 200, "allowed", metrics)


def is_allowed_messages_route(method: str, path: str, policy: ControlPlanePolicy | None = None) -> bool:
    return classify_request(method, path, policy=policy).action == "forward_messages"


def classify_request(method: str, path: str, policy: ControlPlanePolicy | None = None) -> PolicyDecision:
    return (policy or default_policy()).decide(method, path)


def redact_headers(headers: Mapping[str, str]) -> dict[str, Any]:
    auth_shape: dict[str, str] = {}
    selected: dict[str, object] = {}
    names: list[str] = []
    for key, value in headers.items():
        lower = key.lower()
        names.append(_sanitize_header_name(lower))
        if lower in SENSITIVE_HEADERS:
            if lower == "authorization":
                auth_shape[lower] = value.split(" ", 1)[0] if " " in value else "present-no-scheme"
            else:
                auth_shape[lower] = "present"
        elif lower in SELECTED_HEADERS:
            if lower == "x-claude-code-session-id":
                selected[lower] = {"len": len(value), "uuid_like": bool(re.match(r"^[0-9a-fA-F-]{36}$", value))}
            else:
                selected[lower] = _sanitize_selected_header_value(value)
    return {"header_names": names, "selected": selected, "auth_shape": auth_shape}


def body_summary(body: bytes) -> dict[str, Any]:
    summary: dict[str, Any] = {"body_size": len(body)}
    try:
        obj = json.loads(body.decode("utf-8")) if body else {}
    except Exception as exc:  # noqa: BLE001 - summary only
        summary["json_error"] = type(exc).__name__
        return summary
    if isinstance(obj, dict):
        visible_keys = sorted({
            sanitized
            for key in obj.keys()
            if key not in SENSITIVE_BODY_KEYS
            for sanitized in [_sanitize_body_key(key)]
            if sanitized is not None
        })
        summary["body_keys"] = visible_keys
        summary["model"] = _sanitize_model_value(obj.get("model"))
        summary["max_tokens"] = _sanitize_max_tokens_value(obj.get("max_tokens"))
        summary["tools_count"] = len(obj.get("tools") or []) if isinstance(obj.get("tools"), list) else 0
        summary["thinking_present"] = "thinking" in obj
        summary["context_management_present"] = "context_management" in obj
        if isinstance(obj.get("messages"), list):
            summary["messages_count"] = len(obj["messages"])
        if isinstance(obj.get("thinking"), dict):
            summary["thinking_keys"] = sorted({
                sanitized
                for key in obj["thinking"].keys()
                for sanitized in [_sanitize_body_key(key)]
                if sanitized is not None
            })
        if isinstance(obj.get("output_config"), dict):
            summary["output_config_keys"] = sorted({
                sanitized
                for key in obj["output_config"].keys()
                for sanitized in [_sanitize_body_key(key)]
                if sanitized is not None
            })
            summary["output_config_value_types"] = {
                sanitized or "redacted-key": type(value).__name__
                for key, value in obj["output_config"].items()
                for sanitized in [_sanitize_body_key(key)]
            }
            nested: dict[str, list[str]] = {}
            for key, value in obj["output_config"].items():
                sanitized = _sanitize_body_key(key) or "redacted-key"
                if isinstance(value, dict):
                    nested[sanitized] = sorted(
                        child
                        for child_key in value.keys()
                        for child in [_sanitize_body_key(child_key)]
                        if child is not None
                    )
            if nested:
                summary["output_config_nested_keys"] = nested
    return summary


def deep_body_summary(body: bytes) -> dict[str, Any]:
    summary: dict[str, Any] = {"body_size": len(body)}
    if not body:
        summary["content_kind"] = "empty"
        return summary
    try:
        obj = json.loads(body.decode("utf-8"))
    except Exception as exc:  # noqa: BLE001 - summary only
        summary["content_kind"] = "non_json_or_invalid"
        summary["json_error"] = type(exc).__name__
        return summary
    summary["content_kind"] = "json"
    summary["json_tree"] = _json_shape_tree(obj)
    event_names = sorted(_extract_event_names(obj))
    if event_names:
        summary["event_names"] = event_names[:100]
        summary["event_names_truncated"] = len(event_names) > 100
    return summary


def _json_shape_tree(value: Any, *, depth: int = 0) -> Any:
    if depth >= 5:
        return {"type": _json_type_name(value), "truncated": "max_depth"}
    if isinstance(value, Mapping):
        keys = sorted(str(key) for key in value.keys())
        children: dict[str, Any] = {}
        for key in keys[:80]:
            child_value = value.get(key)
            safe_key = _sanitize_body_key(key) or "redacted-key"
            children[safe_key] = _json_shape_tree(child_value, depth=depth + 1)
        result: dict[str, Any] = {"type": "object", "keys": [_sanitize_body_key(key) or "redacted-key" for key in keys[:80]], "children": children}
        if len(keys) > 80:
            result["truncated_keys"] = len(keys) - 80
        return result
    if isinstance(value, list):
        result = {"type": "array", "length": len(value)}
        if value:
            result["first"] = _json_shape_tree(value[0], depth=depth + 1)
        return result
    if isinstance(value, str):
        return {"type": "string", "length": len(value)}
    if isinstance(value, bool):
        return {"type": "bool"}
    if isinstance(value, int) and not isinstance(value, bool):
        return {"type": "int", "bucket": _number_bucket(value)}
    if isinstance(value, float):
        return {"type": "float"}
    if value is None:
        return {"type": "null"}
    return {"type": type(value).__name__}


def _extract_event_names(value: Any) -> set[str]:
    found: set[str] = set()
    if isinstance(value, Mapping):
        for key, child in value.items():
            if key in {"event_name", "event_type"} and isinstance(child, str) and not _looks_sensitive_text(child):
                found.add(child[:200])
            found.update(_extract_event_names(child))
    elif isinstance(value, list):
        for child in value[:200]:
            found.update(_extract_event_names(child))
    return found


def _sanitized_local_raw_headers(headers: Mapping[str, str]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in headers.items():
        lower = _sanitize_header_name(key.lower())
        if key.lower() in SENSITIVE_HEADERS:
            result[lower] = {"present": True, "scheme": value.split(" ", 1)[0] if " " in value else "present-no-scheme"}
        elif lower == "redacted-header":
            result[lower] = {"present": True, "value": "redacted"}
        else:
            result[lower] = _safe_local_raw_scalar(value, key=lower)
    return result


def _sanitized_local_raw_body(body: bytes) -> dict[str, Any]:
    result: dict[str, Any] = {"size": len(body)}
    if not body:
        result["content_kind"] = "empty"
        return result
    try:
        obj = json.loads(body.decode("utf-8"))
    except Exception as exc:  # noqa: BLE001
        result["content_kind"] = "non_json_or_invalid"
        result["decode_error"] = type(exc).__name__
        return result
    result["content_kind"] = "json_redacted"
    result["json"] = _redact_json_value(obj)
    return result


def _redact_json_value(value: Any, *, key: str | None = None, depth: int = 0) -> Any:
    lowered_key = (key or "").lower()
    if lowered_key in SENSITIVE_BODY_KEYS or _looks_sensitive_text(lowered_key):
        return {"redacted": True, "type": _json_type_name(value)}
    if depth >= 8:
        return {"type": _json_type_name(value), "truncated": "max_depth"}
    if isinstance(value, Mapping):
        items = list(value.items())
        result: dict[str, Any] = {}
        for child_key, child_value in items[:200]:
            safe_key = _sanitize_body_key(child_key) or "redacted-key"
            result[safe_key] = _redact_json_value(child_value, key=str(child_key), depth=depth + 1)
        if len(items) > 200:
            result["__truncated_keys__"] = len(items) - 200
        return result
    if isinstance(value, list):
        return {
            "type": "array",
            "length": len(value),
            "items": [_redact_json_value(item, key=key, depth=depth + 1) for item in value[:50]],
            **({"truncated_items": len(value) - 50} if len(value) > 50 else {}),
        }
    return _safe_local_raw_scalar(value, key=lowered_key)


def _safe_local_raw_scalar(value: Any, *, key: str | None = None) -> Any:
    lowered_key = (key or "").lower()
    if isinstance(value, str):
        if lowered_key in SAFE_VALUE_KEYS and not _looks_sensitive_text(value):
            return value[:500]
        return {"type": "string", "length": len(value)}
    if isinstance(value, bool) or value is None:
        return value
    if isinstance(value, int) and not isinstance(value, bool):
        return {"type": "int", "bucket": _number_bucket(value)}
    if isinstance(value, float):
        return {"type": "float"}
    return {"type": type(value).__name__}


def _json_type_name(value: Any) -> str:
    if isinstance(value, Mapping):
        return "object"
    if isinstance(value, list):
        return "array"
    if isinstance(value, str):
        return "string"
    if isinstance(value, bool):
        return "bool"
    if isinstance(value, int) and not isinstance(value, bool):
        return "int"
    if isinstance(value, float):
        return "float"
    if value is None:
        return "null"
    return type(value).__name__


def _number_bucket(value: int) -> str:
    abs_value = abs(value)
    if abs_value <= 10:
        return "0_10"
    if abs_value <= 100:
        return "11_100"
    if abs_value <= 1000:
        return "101_1000"
    if abs_value <= 10000:
        return "1001_10000"
    if abs_value <= 100000:
        return "10001_100000"
    return "100001_plus"


def _safe_artifact_name(value: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9_.-]+", "-", value).strip("-").lower()
    return cleaned[:80] or "capture"


class RedactingForwarder:
    def __init__(self, config: GuardConfig, execution_controller: ExecutionController | None = None):
        self.config = config
        self.execution_controller = execution_controller or ExecutionController(
            mode="canary_single_message" if _policy_messages_config(self.config.policy).get("stop_cli_after_first_response") else "none"
        )
        self._server: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None
        self._message_count = 0
        self._message_lock = threading.Lock()
        self._artifact_count = 0
        self._artifact_lock = threading.Lock()

    def start_background(self) -> None:
        handler = self._make_handler()
        self._server = ThreadingHTTPServer((self.config.listen_host, self.config.listen_port), handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()

    def stop(self) -> None:
        if self._server:
            self._server.shutdown()
            self._server.server_close()

    def _claim_message_slot(self) -> bool:
        if not self.config.max_messages or self.config.max_messages <= 0:
            return True
        with self._message_lock:
            if self._message_count >= self.config.max_messages:
                return False
            self._message_count += 1
            return True

    def _managed_forward_headers(self) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self.config.managed_session_id:
            headers["X-Zhumeng-Managed-Session"] = self.config.managed_session_id
        if self.config.device_id:
            headers["X-Zhumeng-Device-ID"] = self.config.device_id
        if self.config.agent_version:
            headers["X-Zhumeng-Agent-Version"] = self.config.agent_version
        return headers

    def _record(self, obj: Mapping[str, Any]) -> None:
        self.config.summary_path.parent.mkdir(parents=True, exist_ok=True)
        with self.config.summary_path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(dict(obj), ensure_ascii=False, sort_keys=True) + "\n")

    def _messages_route_decision(self, body: bytes, request_path: str, headers: Mapping[str, str]) -> RouteDecision | None:
        if self.config.route_hint_secret and self.config.route_hint_catalog is not None:
            try:
                return verify_signed_route_hint_headers(
                    source_headers=headers,
                    body=body,
                    request_path=request_path,
                    catalog=self.config.route_hint_catalog,
                    session_ref=_route_hint_session_ref(headers),
                    secret=self.config.route_hint_secret,
                    replay_cache=self.config.route_hint_replay_cache,
                )
            except RuntimeError as exc:
                self._record({
                    "ts": time.time(),
                    "event": "messages_gate_block",
                    "decision": "block_403",
                    "reason": "route_hint_invalid" if _has_route_hint_headers(headers) else "route_hint_unavailable",
                    "path": request_path,
                    "error_type": type(exc).__name__,
                })
                return None
        self._record({
            "ts": time.time(),
            "event": "messages_gate_block",
            "decision": "block_403",
            "reason": "route_hint_required",
            "path": request_path,
        })
        return None

    def _capture_record(
        self,
        *,
        event: str,
        method: str,
        path: str,
        headers: Mapping[str, str],
        body: bytes,
        status: int | None = None,
    ) -> dict[str, Any]:
        if self.config.capture_level != "local-raw":
            return {}
        raw_dir = self.config.local_raw_dir
        if raw_dir is None:
            return {}
        with self._artifact_lock:
            self._artifact_count += 1
            index = self._artifact_count
        raw_dir.mkdir(parents=True, exist_ok=True)
        try:
            raw_dir.chmod(0o700)
        except OSError:
            pass
        filename = f"{index:06d}-{_safe_artifact_name(event)}.json"
        artifact_path = raw_dir / filename
        payload = {
            "event": event,
            "method": method.upper(),
            "path_template": _safe_fallback_path(path),
            "status": status,
            "headers": _sanitized_local_raw_headers(headers),
            "body": _sanitized_local_raw_body(body),
            "safety": {
                "raw_token_persisted": False,
                "prompt_text_persisted": False,
                "request_payload_persisted": False,
                "string_values_are_redacted_by_default": True,
            },
        }
        artifact_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        try:
            artifact_path.chmod(0o600)
        except OSError:
            pass
        return {
            "local_raw_ref": {
                "path": str(artifact_path),
                "mode": "redacted_local_only",
                "raw_token_persisted": False,
                "request_payload_persisted": False,
            }
        }

    def _ssl_context(self) -> ssl.SSLContext:
        cert = self.config.cert_path or (self.config.summary_path.parent / "api.anthropic.com.pem")
        key = self.config.key_path or (self.config.summary_path.parent / "api.anthropic.com.key")
        if not cert.exists() or not key.exists():
            cert.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run(
                [
                    "openssl",
                    "req",
                    "-x509",
                    "-newkey",
                    "rsa:2048",
                    "-nodes",
                    "-keyout",
                    str(key),
                    "-out",
                    str(cert),
                    "-days",
                    "1",
                    "-subj",
                    "/CN=api.anthropic.com",
                    "-addext",
                    "subjectAltName=DNS:api.anthropic.com",
                ],
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            try:
                key.chmod(0o600)
                cert.chmod(0o644)
            except OSError:
                pass
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(str(cert), str(key))
        return context

    def _make_handler(self):
        parent = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):  # type: ignore[no-untyped-def]
                return

            def do_CONNECT(self):  # noqa: N802
                target = self.path
                if not parent._is_allowed_stub_target(target):
                    parent._record({
                        "ts": time.time(),
                        "event": "connect_blocked",
                        **_sanitize_connect_target(target, allowed=False),
                    })
                    self.send_response(403)
                    self.send_header("content-length", "0")
                    self.end_headers()
                    return
                parent._record({
                    "ts": time.time(),
                    "event": "connect_stubbed",
                    **_sanitize_connect_target(target, allowed=True),
                })
                self.connection.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                try:
                    tls_context = parent._ssl_context()
                    with tls_context.wrap_socket(self.connection, server_side=True) as tls:
                        self._handle_stubbed_tls(tls, target)
                except Exception as exc:  # noqa: BLE001 - fail closed with safe summary only
                    parent._record({
                        "ts": time.time(),
                        "event": "connect_stub_error",
                        **_sanitize_connect_target(target, allowed=True),
                        "error_type": type(exc).__name__,
                    })
                    try:
                        self.connection.close()
                    except OSError:
                        pass

            def _handle_stubbed_tls(self, tls: ssl.SSLSocket, target: str) -> None:
                tls.settimeout(10)
                data = b""
                while b"\r\n\r\n" not in data and len(data) < 1024 * 1024:
                    chunk = tls.recv(4096)
                    if not chunk:
                        return
                    data += chunk
                head, _, body_start = data.partition(b"\r\n\r\n")
                lines = head.decode("iso-8859-1", "replace").split("\r\n")
                first = lines[0].split() if lines else []
                method = first[0] if len(first) >= 1 else ""
                path = first[1] if len(first) >= 2 else ""
                headers: dict[str, str] = {}
                for line in lines[1:]:
                    if ":" in line:
                        key, value = line.split(":", 1)
                        headers[key] = value.strip()
                content_length = int(headers.get("Content-Length", headers.get("content-length", "0")) or 0)
                body = body_start
                remaining = max(0, content_length - len(body_start))
                while remaining > 0:
                    chunk = tls.recv(min(65536, remaining))
                    if not chunk:
                        break
                    body += chunk
                    remaining -= len(chunk)
                decision = classify_request(method, path, policy=parent.config.policy)
                if decision.action == "forward_messages":
                    decision = PolicyDecision(action="quarantine_block", reason="direct_messages_route_blocked", status=403)
                record, effective_decision = parent._evaluate_control_plane(
                    event="https_control_plane",
                    method=method,
                    path=path,
                    headers=headers,
                    body=body,
                    decision=decision,
                    target=target,
                    declared_content_length=content_length,
                )
                parent._record(record)
                self._send_tls_decision(tls, effective_decision)

            def _send_tls_decision(self, tls: ssl.SSLSocket, decision: PolicyDecision) -> None:
                if decision.action == "suppress_204":
                    tls.sendall(b"HTTP/1.1 204 No Content\r\ncontent-length: 0\r\nconnection: close\r\n\r\n")
                    return
                if decision.action == "stub_json":
                    data = json.dumps(decision.body or {}).encode("utf-8")
                    tls.sendall(
                        b"HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: "
                        + str(len(data)).encode("ascii")
                        + b"\r\nconnection: close\r\n\r\n"
                        + data
                    )
                    return
                status_text = {
                    403: b"403 Forbidden",
                    502: b"502 Bad Gateway",
                    504: b"504 Gateway Timeout",
                }.get(decision.status, f"{decision.status} Control Plane Decision".encode("ascii", "replace"))
                tls.sendall(b"HTTP/1.1 " + status_text + b"\r\ncontent-length: 0\r\nconnection: close\r\n\r\n")

            def do_GET(self):  # noqa: N802
                self._handle_without_forward()

            def do_POST(self):  # noqa: N802
                request_path = _request_target_path(self.path)
                decision = classify_request("POST", request_path, policy=parent.config.policy)
                length = int(self.headers.get("content-length", "0") or 0)
                body = self.rfile.read(length) if length else b""
                if decision.action == "forward_messages":
                    route_decision = parent._messages_route_decision(body, request_path, dict(self.headers))
                    if route_decision is None:
                        self.send_response(403)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    validation_error = validate_cp5_bridge_body(route_decision, body)
                    if validation_error:
                        parent._record({
                            "ts": time.time(),
                            "event": "messages_gate_block",
                            "decision": "block_403",
                            "reason": "messages_body_invalid",
                            "validation_error": validation_error,
                            "path": request_path,
                            **messages_route_summary_markers(route_decision, dict(self.headers)),
                        })
                        self.send_response(403)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    envelope = evaluate_cost_envelope(body, parent.config)
                    request_record = {
                        "ts": time.time(),
                        "event": "request",
                        "method": "POST",
                        "path": request_path,
                        "decision": decision.action,
                        "reason": decision.reason,
                        **messages_route_summary_markers(route_decision, dict(self.headers)),
                        **redact_headers(dict(self.headers)),
                        **body_summary(body),
                    }
                    if parent.config.capture_level in {"deep", "local-raw"}:
                        request_record["deep_body_summary"] = deep_body_summary(body)
                    request_record.update(parent._capture_record(
                        event="messages_request",
                        method="POST",
                        path=request_path,
                        headers=dict(self.headers),
                        body=body,
                    ))
                    parent._record(request_record)
                    if not envelope.allowed:
                        parent._record({
                            "ts": time.time(),
                            "event": "messages_cost_envelope_block",
                            "decision": "block_local_cost_envelope",
                            "reason": envelope.reason,
                            "path": request_path,
                            "cost_envelope": envelope.metrics,
                        })
                        self.send_response(envelope.status)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    if parent.config.session_budget_ledger is not None:
                        session_key = session_key_from_headers(dict(self.headers))
                        budget_decision = parent.config.session_budget_ledger.check_and_record(session_key, envelope.metrics)
                        if not budget_decision.allowed:
                            parent._record({
                                "ts": time.time(),
                                "event": "session_budget_block",
                                "decision": "block_session_budget",
                                "reason": budget_decision.reason,
                                "path": request_path,
                                "session_budget": budget_decision.summary,
                            })
                            self.send_response(budget_decision.status)
                            self.send_header("content-length", "0")
                            self.end_headers()
                            return
                    if not parent._claim_message_slot():
                        parent._record({
                            "ts": time.time(),
                            "event": "messages_gate_block",
                            "decision": "block_403",
                            "reason": "max_messages_exceeded",
                            "path": request_path,
                        })
                        self.send_response(409)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    self._forward_messages(body, request_path, route_decision)
                    return
                record, effective_decision = parent._evaluate_control_plane(
                    event="request",
                    method="POST",
                    path=request_path,
                    headers=dict(self.headers),
                    body=body,
                    decision=decision,
                )
                parent._record(record)
                self._respond_decision(effective_decision)

            def _handle_without_forward(self) -> None:
                request_path = _request_target_path(self.path)
                decision = classify_request(self.command, request_path, policy=parent.config.policy)
                record, effective_decision = parent._evaluate_control_plane(
                    event="request",
                    method=self.command,
                    path=request_path,
                    headers=dict(self.headers),
                    body=b"",
                    decision=decision,
                )
                parent._record(record)
                self._respond_decision(effective_decision)

            def _respond_decision(self, decision: PolicyDecision) -> None:
                if decision.action == "suppress_204":
                    self.send_response(204)
                    self.send_header("content-length", "0")
                    self.end_headers()
                    return
                if decision.action == "stub_json":
                    data = json.dumps(decision.body or {}).encode("utf-8")
                    self.send_response(decision.status)
                    if decision.content_type:
                        self.send_header("content-type", decision.content_type)
                    self.send_header("content-length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                    return
                self.send_response(decision.status)
                self.send_header("content-length", "0")
                self.end_headers()

            def _forward_messages(self, body: bytes, request_path: str, route_decision: RouteDecision) -> None:
                if not route_decision.native_attestation_allowed and not route_decision.live_request_allowed:
                    if _request_target_path(request_path) == "/v1/messages/count_tokens":
                        parent._record({
                            "ts": time.time(),
                            "event": "messages_gate_block",
                            "decision": "block_403",
                            "reason": "bridge_count_tokens_unavailable",
                            "path": request_path,
                            **bridge_skeleton_audit_markers(route_decision),
                        })
                        self.send_response(403)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    self._respond_cp5_bridge_skeleton(body, request_path, route_decision)
                    return
                url = parent.config.upstream_base.rstrip("/") + request_path
                headers = {
                    key: value
                    for key, value in self.headers.items()
                    if key.lower() in CLAUDE_CODE_UPSTREAM_HEADER_ALLOWLIST
                }
                headers["Authorization"] = f"Bearer {parent.config.sub2api_auth}"
                if route_decision.native_attestation_allowed:
                    try:
                        headers.update(build_native_messages_attestation_headers(
                            body,
                            request_path,
                            dict(self.headers),
                            secret=parent.config.native_attestation_secret,
                            route_decision=route_decision,
                        ))
                    except RuntimeError as exc:
                        parent._record({
                            "ts": time.time(),
                            "event": "messages_gate_block",
                            "decision": "block_403",
                            "reason": "native_attestation_unavailable",
                            "path": request_path,
                            "error_type": type(exc).__name__,
                        })
                        self.send_response(403)
                        self.send_header("content-length", "0")
                        self.end_headers()
                        return
                    headers["x-sub2api-route"] = route_decision.route
                    headers["x-sub2api-route-catalog-version"] = route_decision.catalog_version
                else:
                    headers["x-sub2api-client-type"] = route_decision.client_type
                    headers["x-sub2api-route"] = route_decision.route
                    headers["x-sub2api-route-catalog-version"] = route_decision.catalog_version
                    headers["x-zhumeng-claude-code-route-hint"] = self.headers.get("x-zhumeng-claude-code-route-hint", "")
                    headers["x-zhumeng-claude-code-route-signature"] = self.headers.get("x-zhumeng-claude-code-route-signature", "")
                for key, value in parent._managed_forward_headers().items():
                    headers[key] = value
                if parent.config.extra_forward_headers:
                    for key, value in parent.config.extra_forward_headers.items():
                        if key.lower() in CLAUDE_CODE_UPSTREAM_HEADER_ALLOWLIST:
                            headers[key] = value
                request = urllib.request.Request(url, data=body, method="POST", headers=headers)
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                stop_event: dict[str, str] | None = None
                try:
                    with opener.open(request, timeout=30) as response:
                        data = response.read()
                        response_record = {
                            "ts": time.time(),
                            "event": "messages_upstream_response",
                            "decision": "forward_messages",
                            "path": request_path,
                            "status": response.status,
                            "response_content_type": response.headers.get("content-type"),
                            "response_body_size": len(data),
                        }
                        if parent.config.capture_level in {"deep", "local-raw"}:
                            response_record["response_deep_body_summary"] = deep_body_summary(data)
                        response_record.update(parent._capture_record(
                            event="messages_response",
                            method="POST",
                            path=request_path,
                            headers=dict(response.headers),
                            body=data,
                            status=response.status,
                        ))
                        parent._record(response_record)
                        self.send_response(response.status)
                        for key, value in response.headers.items():
                            if key.lower() not in {"transfer-encoding", "connection", "content-length"}:
                                self.send_header(key, value)
                        self.send_header("content-length", str(len(data)))
                        self.end_headers()
                        self.wfile.write(data)
                except urllib.error.HTTPError as exc:
                    data = exc.read()
                    error_record = {
                        "ts": time.time(),
                        "event": "messages_upstream_response",
                        "decision": "forward_messages",
                        "path": request_path,
                        "status": exc.code,
                        "response_content_type": exc.headers.get("content-type"),
                        "response_body_size": len(data),
                    }
                    if parent.config.capture_level in {"deep", "local-raw"}:
                        error_record["response_deep_body_summary"] = deep_body_summary(data)
                    error_record.update(parent._capture_record(
                        event="messages_response",
                        method="POST",
                        path=request_path,
                        headers=dict(exc.headers),
                        body=data,
                        status=exc.code,
                    ))
                    parent._record(error_record)
                    self.send_response(exc.code)
                    content_type = exc.headers.get("content-type")
                    if content_type:
                        self.send_header("content-type", content_type)
                    self.send_header("content-length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                except (urllib.error.URLError, socket.timeout, TimeoutError, http.client.RemoteDisconnected, ConnectionResetError, OSError) as exc:
                    error_type = type(exc).__name__
                    status = 504 if _is_timeout_error(exc) else 502
                    parent._record({
                        "ts": time.time(),
                        "event": "messages_upstream_error",
                        "decision": "upstream_failure",
                        "path": "/v1/messages?beta=true",
                        "status": status,
                        "error_type": error_type,
                    })
                    self.send_response(status)
                    self.send_header("content-length", "0")
                    self.end_headers()
                finally:
                    stop_event = parent.execution_controller.on_message_completed()
                    if stop_event is not None:
                        parent._record({"ts": time.time(), **stop_event})

            def _respond_cp5_bridge_skeleton(self, body: bytes, request_path: str, route_decision: RouteDecision) -> None:
                validation_error = validate_cp5_bridge_body(route_decision, body)
                if validation_error:
                    parent._record({
                        "ts": time.time(),
                        "event": "messages_gate_block",
                        "decision": "block_403",
                        "reason": "bridge_body_invalid",
                        "validation_error": validation_error,
                        "path": request_path,
                        **bridge_skeleton_audit_markers(route_decision),
                    })
                    self.send_response(403)
                    self.send_header("content-length", "0")
                    self.end_headers()
                    stop_event = parent.execution_controller.on_message_completed()
                    if stop_event is not None:
                        parent._record({"ts": time.time(), **stop_event})
                    return

                data = cp5_bridge_skeleton_sse_body(route_decision, body=body)
                parent._record({
                    "ts": time.time(),
                    "event": "messages_bridge_skeleton_response",
                    "decision": "bridge_skeleton_cp5",
                    "path": request_path,
                    "status": 200,
                    **bridge_skeleton_audit_markers(route_decision),
                    "response_content_type": "text/event-stream",
                    "response_body_size": len(data),
                })
                self.send_response(200)
                self.send_header("content-type", "text/event-stream")
                self.send_header("content-length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                stop_event = parent.execution_controller.on_message_completed()
                if stop_event is not None:
                    parent._record({"ts": time.time(), **stop_event})

        return Handler

    def _is_allowed_stub_target(self, target: str) -> bool:
        if self.config.connect_mode != "stub":
            return False
        allowed = {value.lower() for value in self.config.policy.connect.get("allowed_stub_targets", [])}
        return target.lower() in allowed

    def _evaluate_control_plane(
        self,
        *,
        event: str,
        method: str,
        path: str,
        headers: Mapping[str, str],
        body: bytes,
        decision: PolicyDecision,
        target: str | None = None,
        declared_content_length: int | None = None,
    ) -> tuple[dict[str, Any], PolicyDecision]:
        defaults = self.config.policy.control_plane_defaults
        routing_intent = "local_stub_or_suppress" if decision.action in {"suppress_204", "stub_json"} else "local_block_403"
        if decision.reason == "direct_messages_route_blocked":
            intent = _quarantine_control_plane_intent(
                method=method,
                path=path,
                body=body,
                classification="direct_messages_route_blocked",
                defaults=defaults,
                routing_intent="local_quarantine_block",
            )
            record: dict[str, Any] = {
                "ts": time.time(),
                "event": event,
                "decision": decision.action,
                "reason": decision.reason,
                "transport_summary": redact_headers(headers),
                "control_plane_body_summary": body_summary(body),
                **intent,
            }
            if self.config.capture_level in {"deep", "local-raw"}:
                record["control_plane_deep_body_summary"] = deep_body_summary(body)
            record.update(self._capture_record(
                event=f"control_plane_{event}",
                method=method,
                path=path,
                headers=headers,
                body=body,
                status=decision.status,
            ))
            if target is not None:
                record.update(_sanitize_connect_target(target, allowed=self._is_allowed_stub_target(target)))
            if declared_content_length is not None:
                record["declared_content_length"] = declared_content_length
            return record, decision
        try:
            intent = build_control_plane_intent(
                method=method,
                path=path,
                headers=headers,
                body=body,
                classification=_decision_classification(decision),
                policy_version=defaults["policy_version"],
                strategy_version=defaults["strategy_version"],
                response_schema_version=defaults["response_schema_version"],
                routing_intent=routing_intent,
            )
            effective_decision = decision
            if self.config.control_plane_intent_url is not None:
                effective_decision = self._submit_control_plane_intent(intent, source_headers=headers)
        except IntentValidationError:
            effective_decision = PolicyDecision(
                action="quarantine_block",
                reason="intent_validation_failed",
                status=403,
            )
            intent = _quarantine_control_plane_intent(
                method=method,
                path=path,
                body=body,
                classification="intent_validation_failed",
                defaults=defaults,
                routing_intent="local_quarantine_block",
            )
        record: dict[str, Any] = {
            "ts": time.time(),
            "event": event,
            "decision": effective_decision.action,
            "reason": effective_decision.reason,
            "transport_summary": redact_headers(headers),
            "control_plane_body_summary": body_summary(body),
            **intent,
        }
        if self.config.capture_level in {"deep", "local-raw"}:
            record["control_plane_deep_body_summary"] = deep_body_summary(body)
        record.update(self._capture_record(
            event=f"control_plane_{event}",
            method=method,
            path=path,
            headers=headers,
            body=body,
            status=effective_decision.status,
        ))
        if target is not None:
            record.update(_sanitize_connect_target(target, allowed=self._is_allowed_stub_target(target)))
        if declared_content_length is not None:
            record["declared_content_length"] = declared_content_length
        return record, effective_decision

    def _submit_control_plane_intent(self, intent: Mapping[str, Any], *, source_headers: Mapping[str, str]) -> PolicyDecision:
        headers = {"content-type": "application/json"}
        if self.config.control_plane_intent_auth:
            headers["x-sub2api-intent-auth"] = self.config.control_plane_intent_auth
        try:
            attestation, signature = build_guard_attestation(
                intent,
                request_headers=source_headers,
                config=guard_attestation_config_from_env(),
            )
        except AttestationValidationError:
            return PolicyDecision(action="quarantine_block", reason="intent_attestation_unavailable", status=502)
        headers[ATTESTATION_HEADER] = attestation
        headers[ATTESTATION_SIGNATURE_HEADER] = signature
        request = urllib.request.Request(
            self.config.control_plane_intent_url,
            data=json.dumps(dict(intent), ensure_ascii=False, separators=(",", ":")).encode("utf-8"),
            method="POST",
            headers=headers,
        )
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        try:
            with opener.open(request, timeout=10) as response:
                payload = json.loads(response.read().decode("utf-8") or "{}")
        except urllib.error.HTTPError as exc:
            if exc.code == 403:
                return PolicyDecision(action="quarantine_block", reason="intent_endpoint_rejected", status=403)
            return PolicyDecision(action="quarantine_block", reason="intent_endpoint_http_error", status=502)
        except (urllib.error.URLError, socket.timeout, TimeoutError, OSError, ValueError, json.JSONDecodeError):
            return PolicyDecision(action="quarantine_block", reason="intent_endpoint_unavailable", status=502)

        action = payload.get("decision")
        reason = payload.get("reason")
        status = payload.get("status")
        if action not in {"suppress_204", "stub_json", "quarantine_block"}:
            return PolicyDecision(action="quarantine_block", reason="intent_endpoint_invalid_response", status=502)
        if not isinstance(reason, str) or not reason:
            return PolicyDecision(action="quarantine_block", reason="intent_endpoint_invalid_response", status=502)
        if not isinstance(status, int) or status < 200 or status > 599:
            return PolicyDecision(action="quarantine_block", reason="intent_endpoint_invalid_response", status=502)
        content_type = payload.get("content_type")
        body = payload.get("body")
        return PolicyDecision(
            action=action,
            reason=reason,
            status=status,
            content_type=content_type if isinstance(content_type, str) and content_type else None,
            body=body,
        )


def _cost_envelope_limits(policy_or_config: GuardConfig | ControlPlanePolicy | Mapping[str, Any] | None) -> dict[str, Any]:
    limits = dict(_DEFAULT_COST_ENVELOPE_LIMITS)
    policy: ControlPlanePolicy | None = None
    config_overrides: Mapping[str, Any] | None = None
    config_max_messages: int | None = None
    if isinstance(policy_or_config, GuardConfig):
        policy = policy_or_config.policy
        config_max_messages = policy_or_config.max_messages
        config_overrides = policy_or_config.cost_envelope_limits
    elif isinstance(policy_or_config, ControlPlanePolicy):
        policy = policy_or_config
    elif isinstance(policy_or_config, Mapping):
        limits.update(dict(policy_or_config))
        return limits

    if policy is None:
        return limits
    source_dict = getattr(policy, "_source_dict", {})
    messages = source_dict.get("messages", {})
    if not isinstance(messages, dict):
        return limits
    if isinstance(messages.get("max_messages"), int) and messages["max_messages"] > 0:
        limits["max_messages"] = messages["max_messages"]
    gate = messages.get("cost_envelope_gate", {})
    if not isinstance(gate, dict):
        return limits
    for key, value in gate.items():
        if key == "allowed_output_config_keys" and isinstance(value, (list, tuple)):
            limits[key] = tuple(str(item) for item in value if isinstance(item, str))
        elif key in limits:
            limits[key] = value
    if config_max_messages and config_max_messages > 0:
        limits["max_messages"] = config_max_messages
    if config_overrides:
        limits.update(dict(config_overrides))
    return limits


def _has_tool_loop_markers(
    payload: Mapping[str, Any],
    *,
    allow_assistant_messages: bool = False,
    allow_tool_content: bool = False,
) -> bool:
    if payload.get("tool_loop") or payload.get("append_round"):
        return True
    metadata = payload.get("metadata")
    if isinstance(metadata, Mapping) and (metadata.get("tool_loop") or metadata.get("append_round")):
        return True
    messages = payload.get("messages")
    if not isinstance(messages, list):
        return False
    for message in messages:
        if not isinstance(message, Mapping):
            continue
        if message.get("role") == "assistant" and not allow_assistant_messages:
            return True
        if message.get("tool_loop") or message.get("append_round"):
            return True
        content = message.get("content")
        if isinstance(content, list):
            for block in content:
                if isinstance(block, Mapping) and (block.get("tool_loop") or block.get("append_round")):
                    return True
    return False


def _message_content_metrics(payload: Mapping[str, Any]) -> dict[str, Any]:
    metrics = {
        "content_blocks_count": 0,
        "text_bytes": 0,
        "system_bytes": 0,
        "tool_content_present": False,
    }

    system = payload.get("system")
    if isinstance(system, str):
        metrics["system_bytes"] += len(system.encode("utf-8"))
    elif isinstance(system, list):
        for item in system:
            if isinstance(item, str):
                metrics["system_bytes"] += len(item.encode("utf-8"))
            elif isinstance(item, Mapping):
                metrics["system_bytes"] += len(json.dumps(item, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))

    messages = payload.get("messages")
    if not isinstance(messages, list):
        return metrics
    for message in messages:
        if not isinstance(message, Mapping):
            continue
        content = message.get("content")
        if isinstance(content, str):
            metrics["content_blocks_count"] += 1
            metrics["text_bytes"] += len(content.encode("utf-8"))
            continue
        if not isinstance(content, list):
            continue
        for block in content:
            metrics["content_blocks_count"] += 1
            if not isinstance(block, Mapping):
                continue
            block_type = block.get("type")
            if block_type in {"tool_use", "tool_result"}:
                metrics["tool_content_present"] = True
            text_value = block.get("text")
            if isinstance(text_value, str):
                metrics["text_bytes"] += len(text_value.encode("utf-8"))
            elif isinstance(block.get("content"), str):
                metrics["text_bytes"] += len(block["content"].encode("utf-8"))
    return metrics


def _policy_messages_config(policy: ControlPlanePolicy) -> dict[str, Any]:
    source_dict = getattr(policy, "_source_dict", {})
    messages = source_dict.get("messages", {})
    return messages if isinstance(messages, dict) else {}


def _decision_classification(decision: PolicyDecision) -> str:
    if decision.reason == "direct_messages_route_blocked":
        return "direct_messages_route_blocked"
    if decision.action == "suppress_204":
        return "telemetry_or_eval_suppressed"
    if decision.action == "stub_json":
        if "bootstrap_settings" in decision.reason:
            return "bootstrap_settings_or_feature_flag_stubbed"
        return "mcp_or_registry_stubbed"
    return "unknown_quarantined"


def _quarantine_control_plane_intent(
    *,
    method: str,
    path: str,
    body: bytes,
    classification: str,
    defaults: Mapping[str, int],
    routing_intent: str,
) -> dict[str, Any]:
    return {
        "method": method.upper(),
        "path_template": _safe_fallback_path(path),
        "normalized_query": {},
        "query_ref": None,
        "query_omitted_reason": "no_query",
        "classification": classification,
        "policy_version": defaults["policy_version"],
        "strategy_version": defaults["strategy_version"],
        "response_schema_version": defaults["response_schema_version"],
        "routing_intent": routing_intent,
        "body_length_bucket": _fallback_body_length_bucket(body),
        "schema_summary": {"content_kind": "omitted", "top_level_type": "omitted"},
        "body_omitted_reason": "high_risk_body_not_retained" if body else "not_applicable",
        "digest_omitted_reason": "raw_body_digest_forbidden_by_policy",
        "redaction_proof": {
            "sensitive_scan": "clean",
            "path_identifiers_redacted": True,
            "raw_query_persisted": False,
            "body_persisted": False,
            "raw_body_digest_persisted": False,
        },
    }


def _safe_fallback_path(path: str) -> str:
    parsed = urlsplit(path)
    value = parsed.path if parsed.path.startswith("/") else "/redacted"
    if _looks_sensitive_text(value):
        return "/redacted"
    for segment in value.split("/"):
        if not segment:
            continue
        if _looks_unsafe_dynamic_identifier(segment):
            return "/redacted"
    return value


def _is_timeout_error(exc: BaseException) -> bool:
    if isinstance(exc, (socket.timeout, TimeoutError)):
        return True
    if isinstance(exc, urllib.error.URLError):
        reason = getattr(exc, "reason", None)
        return isinstance(reason, (socket.timeout, TimeoutError))
    return False


def _sanitize_header_name(name: str) -> str:
    normalized = name.strip().lower()
    if not normalized or _SAFE_HEADER_NAME_RE.fullmatch(normalized) is None:
        return "redacted-header"
    if _looks_sensitive_text(normalized):
        return "redacted-header"
    return normalized


def _sanitize_body_key(key: object) -> str | None:
    if not isinstance(key, str):
        return "redacted-key"
    normalized = key.strip()
    if not normalized:
        return "redacted-key"
    if _looks_sensitive_text(normalized):
        return "redacted-key"
    return normalized


def _sanitize_model_value(value: object) -> str | int | float | bool | None:
    if isinstance(value, str):
        return "redacted-model" if _looks_sensitive_text(value) else value
    if isinstance(value, (int, float, bool)) or value is None:
        return value
    return "redacted-model"


def _sanitize_selected_header_value(value: object) -> str:
    if not isinstance(value, str):
        return "redacted-header-value"
    truncated = value[:500]
    return "redacted-header-value" if _looks_sensitive_text(truncated) else truncated


def _sanitize_max_tokens_value(value: object) -> int | None | str:
    if value is None:
        return None
    if isinstance(value, int) and not isinstance(value, bool):
        return value
    return "redacted-non-int"


def _fallback_body_length_bucket(body: bytes) -> str:
    size = len(body)
    if size <= 0:
        return "empty"
    if size <= 255:
        return "1_255_bytes"
    if size <= 1023:
        return "256_1023_bytes"
    if size <= 4095:
        return "1024_4095_bytes"
    if size <= 16383:
        return "4096_16383_bytes"
    return "16384_plus_bytes"


def _sanitize_connect_target(target: str, *, allowed: bool) -> dict[str, Any]:
    host, port = _split_connect_target(target)
    safe_known_hosts = {
        "api.anthropic.com",
        "platform.claude.com",
        "claude.ai",
        "claude.com",
        "mcp-proxy.anthropic.com",
    }
    safe_target: dict[str, Any] = {}
    if host in safe_known_hosts:
        safe_target["target_host"] = host
        safe_target["target_port"] = port
    return {
        "target_kind": "allowed_stub_target" if allowed else "blocked_connect_target",
        "target_allowed": allowed,
        "target_ref": _scoped_hmac_ref(target, scope="control_plane_connect_target"),
        **safe_target,
    }


def _split_connect_target(target: str) -> tuple[str, int | None]:
    if target.startswith("[") and "]" in target:
        end = target.find("]")
        host = target[1:end].lower()
        rest = target[end + 1 :]
        if rest.startswith(":") and rest[1:].isdigit():
            return host, int(rest[1:])
        return host, None
    if ":" in target:
        host, port_raw = target.rsplit(":", 1)
        return host.lower(), int(port_raw) if port_raw.isdigit() else None
    return target.lower(), None


def _request_target_path(raw_target: str) -> str:
    parsed = urlsplit(raw_target)
    if parsed.scheme and parsed.netloc:
        path = parsed.path or "/"
        return path + (("?" + parsed.query) if parsed.query else "")
    return raw_target


def _cli_cost_envelope_limits(args: argparse.Namespace) -> dict[str, Any]:
    limits: dict[str, Any] = {}
    option_map = {
        "cost_max_tokens": "max_tokens",
        "cost_max_body_bytes": "max_body_bytes",
        "cost_max_tools": "max_tools",
        "cost_max_messages": "max_messages",
        "cost_max_content_blocks": "max_content_blocks",
        "cost_max_text_bytes": "max_text_bytes",
        "cost_max_system_bytes": "max_system_bytes",
        "cost_max_tool_def_bytes": "max_tool_def_bytes",
        "cost_max_thinking_budget_tokens": "max_thinking_budget_tokens",
    }
    for arg_name, limit_name in option_map.items():
        value = getattr(args, arg_name, None)
        if value is not None:
            limits[limit_name] = value
    if getattr(args, "cost_allow_stream", False):
        limits["allow_stream"] = True
    if getattr(args, "cost_allow_thinking", False):
        limits["allow_thinking"] = True
    if getattr(args, "cost_allow_assistant_messages", False):
        limits["allow_assistant_messages"] = True
    if getattr(args, "cost_allow_tool_content", False):
        limits["allow_tool_content"] = True
    return limits


def _looks_sensitive_text(value: str) -> bool:
    lowered = value.lower()
    if "@" in lowered:
        return True
    if lowered.startswith("sk-"):
        return True
    parts = tuple(part for part in _SENSITIVE_TEXT_SPLIT_RE.split(lowered) if part)
    joined = "".join(parts)
    sensitive_parts = {"prompt", "token", "secret", "cookie", "credential"}
    if any(part in sensitive_parts for part in parts):
        return True
    if "rawprompt" in joined or "accesstoken" in joined:
        return True
    return False


def _looks_unsafe_dynamic_identifier(value: str) -> bool:
    stripped = value.strip()
    if re.fullmatch(r"(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{32})", stripped):
        return True
    if "@" in stripped:
        return True
    lowered = stripped.lower()
    if lowered.startswith(("local-org-", "local-account-", "local-user-")):
        return True
    if re.match(r"^(?:account|org|organization|user|session|project)(?:[_-].+)$", lowered):
        return True
    return any(marker in lowered for marker in ("org-secret", "account-secret", "user-secret", "session-id"))


def _scoped_hmac_ref(value: str, *, scope: str) -> dict[str, Any]:
    key_id = os.environ.get("SUB2API_CONTROL_PLANE_HMAC_KEY_ID", "local_guard_v1")
    version = int(os.environ.get("SUB2API_CONTROL_PLANE_HMAC_VERSION", "1"))
    secret = os.environ.get("SUB2API_CONTROL_PLANE_HMAC_KEY", "sub2api-control-plane-dev-key")
    material = scope.encode("utf-8") + b"\x00" + value.encode("utf-8")
    return {
        "key_id": key_id,
        "scope": scope,
        "version": version,
        "value": "hmac-sha256:" + hmac.new(secret.encode("utf-8"), material, sha256).hexdigest(),
    }


def build_native_messages_attestation_headers(
    body: bytes,
    request_path: str,
    source_headers: Mapping[str, str],
    *,
    secret: str | None = None,
    route_decision: RouteDecision | None = None,
) -> dict[str, str]:
    """Attach internal native takeover markers; never include raw prompt/body."""
    now = int(time.time())
    key_id = os.environ.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID", "guard_v1")
    version = int(os.environ.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_VERSION", "1"))
    if secret is None:
        secret = os.environ.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "")
    if not secret:
        raise RuntimeError("explicit Claude Code native attestation secret is required")
    local_session_ref = session_key_from_headers(source_headers)
    local_session_value = str(local_session_ref.get("value", ""))
    model_id = _native_attestation_model_id(body)
    body_shape_hash = _native_body_shape_hash(body)
    runtime_hash = route_decision.runtime_hash if route_decision is not None else _native_env_hash("ZHUMENG_CLAUDE_RUNTIME_HASH")
    overlay_hash = route_decision.overlay_hash if route_decision is not None else _native_env_hash("ZHUMENG_CLAUDE_OVERLAY_HASH")
    catalog_hash = route_decision.catalog_hash if route_decision is not None else _native_env_hash("ZHUMENG_CLAUDE_CATALOG_HASH")
    catalog_version = route_decision.catalog_version if route_decision is not None else "legacy-native"
    payload = {
        "key_id": key_id,
        "scope": os.environ.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SCOPE", NATIVE_ATTESTATION_SCOPE),
        "version": version,
        "issued_at": now,
        "nonce": secrets.token_hex(16),
        "method": "POST",
        "request_uri": request_path,
        "client_type": NATIVE_CLIENT_TYPE,
        "guard_attested": True,
        "guard_version": key_id,
        "claude_code_version": _safe_claude_code_version(source_headers),
        "local_session_ref": local_session_value,
        "netwatch_required": True,
        "shape_healthcheck_profile": NATIVE_HEALTHCHECK_PROFILE,
        "route": NATIVE_ROUTE,
        "model_id": model_id,
        "provider_owner": NATIVE_PROVIDER_OWNER,
        "credential_scope": NATIVE_CREDENTIAL_SCOPE,
        "gateway_location": NATIVE_GATEWAY_LOCATION,
        "runtime_hash": runtime_hash,
        "overlay_hash": overlay_hash,
        "catalog_hash": catalog_hash,
        "catalog_version": catalog_version,
        "session_ref": local_session_value,
        "body_shape_hash": body_shape_hash,
    }
    encoded = base64.urlsafe_b64encode(json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")).decode("ascii").rstrip("=")
    signature = _sign_native_messages_attestation(encoded, "POST", request_path, body, secret)
    return {
        "x-sub2api-client-type": NATIVE_CLIENT_TYPE,
        "x-sub2api-guard-attested": "true",
        "x-sub2api-guard-version": key_id,
        "x-sub2api-claude-code-version": payload["claude_code_version"],
        "x-sub2api-local-session-ref": local_session_value,
        "x-sub2api-netwatch-required": "true",
        "x-sub2api-native-attestation": encoded,
        "x-sub2api-native-signature": signature,
    }


def _native_attestation_model_id(body: bytes) -> str:
    try:
        obj = json.loads(body.decode("utf-8")) if body else {}
    except Exception:
        return "unknown"
    if not isinstance(obj, dict):
        return "unknown"
    model = _sanitize_model_value(obj.get("model"))
    return model if isinstance(model, str) and model else "unknown"


def _native_body_shape_hash(body: bytes) -> str:
    try:
        decoded = json.loads(body.decode("utf-8")) if body else {}
    except Exception:
        decoded = {"body_size": len(body), "type": "invalid_json"}
    shape = _native_shape_value(decoded)
    digest = sha256(json.dumps(shape, sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest()
    return "sha256:" + digest


def _native_shape_value(value: object) -> object:
    if isinstance(value, dict):
        children: dict[str, object] = {}
        for key, child in value.items():
            safe_key = _native_shape_key(str(key))
            children[safe_key] = _native_shape_value(child)
        return {"children": children, "keys": sorted(children), "type": "object"}
    if isinstance(value, list):
        limit = min(len(value), 32)
        return {
            "items": [_native_shape_value(value[index]) for index in range(limit)],
            "len": len(value),
            "truncated": len(value) > 32,
            "type": "array",
        }
    if isinstance(value, str):
        return {"type": "string"}
    if isinstance(value, bool):
        return {"type": "bool"}
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return {"type": "number"}
    if value is None:
        return {"type": "null"}
    return {"type": "unknown"}


def _native_shape_key(key: str) -> str:
    key = key.strip()
    if not key or len(key) > 128 or _looks_sensitive_text(key):
        return "redacted-key"
    return key if re.fullmatch(r"[A-Za-z0-9_.-]+", key) else "redacted-key"


def _native_env_hash(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if re.fullmatch(r"sha256:[0-9a-fA-F]{64}", value):
        return "sha256:" + value.split(":", 1)[1].lower()
    return NATIVE_UNKNOWN_HASH


def native_messages_summary_markers(source_headers: Mapping[str, str]) -> dict[str, Any]:
    local_session = session_key_from_headers(source_headers)
    return {
        "client_type": NATIVE_CLIENT_TYPE,
        "native_attested": True,
        "netwatch_required": True,
        "shape_healthcheck_profile": NATIVE_HEALTHCHECK_PROFILE,
        "local_session_ref": local_session.get("value", ""),
        "raw_body_persisted": False,
        "raw_attestation_persisted": False,
    }


def messages_route_summary_markers(route_decision: RouteDecision, source_headers: Mapping[str, str]) -> dict[str, Any]:
    local_session = session_key_from_headers(source_headers)
    return {
        "client_type": route_decision.client_type,
        "route": route_decision.route,
        "provider": route_decision.provider,
        "live_request_allowed": bool(route_decision.live_request_allowed),
        "native_attested": bool(route_decision.native_attestation_allowed),
        "formal_pool_allowed": bool(route_decision.formal_pool_allowed),
        "netwatch_required": bool(route_decision.native_attestation_allowed),
        "shape_healthcheck_profile": NATIVE_HEALTHCHECK_PROFILE if route_decision.native_attestation_allowed else "bridge_skeleton_cp5",
        "runtime_hash": route_decision.runtime_hash,
        "overlay_hash": route_decision.overlay_hash,
        "catalog_hash": route_decision.catalog_hash,
        "catalog_version": route_decision.catalog_version,
        "provider_owner": route_decision.provider_owner,
        "credential_scope": route_decision.credential_scope,
        "gateway_location": route_decision.gateway_location,
        "local_session_ref": local_session.get("value", ""),
        "raw_body_persisted": False,
        "raw_attestation_persisted": False,
    }


def bridge_skeleton_audit_markers(route_decision: RouteDecision) -> dict[str, Any]:
    return {
        "route": route_decision.route,
        "client_type": route_decision.client_type,
        "provider": route_decision.provider,
        "model": route_decision.model_id,
        "live_request_allowed": bool(route_decision.live_request_allowed),
        "native_attested": False,
        "formal_pool_allowed": False,
        "runtime_hash": route_decision.runtime_hash,
        "overlay_hash": route_decision.overlay_hash,
        "catalog_hash": route_decision.catalog_hash,
        "catalog_version": route_decision.catalog_version,
        "provider_owner": route_decision.provider_owner,
        "credential_scope": route_decision.credential_scope,
        "gateway_location": route_decision.gateway_location,
    }


OPENAI_ONLY_BRIDGE_TOP_LEVEL_FIELDS = {
    "audio",
    "frequency_penalty",
    "background",
    "conversation",
    "function_call",
    "functions",
    "include",
    "input",
    "instructions",
    "logit_bias",
    "logprobs",
    "max_completion_tokens",
    "max_output_tokens",
    "modalities",
    "n",
    "parallel_tool_calls",
    "presence_penalty",
    "previous_response_id",
    "prompt",
    "prompt_cache_key",
    "reasoning",
    "response_format",
    "seed",
    "stop",
    "store",
    "stream_options",
    "text",
    "top_logprobs",
    "truncation",
    "user",
}


def validate_cp5_bridge_body(route_decision: RouteDecision, body: bytes) -> str | None:
    try:
        payload = json.loads(body.decode("utf-8"))
    except Exception:  # noqa: BLE001 - fail closed without echoing malformed input
        return "json_body_required"
    if not isinstance(payload, dict):
        return "json_object_required"
    model = payload.get("model")
    if not isinstance(model, str) or model.strip() != route_decision.model_id:
        return "model_binding_mismatch"
    for field in OPENAI_ONLY_BRIDGE_TOP_LEVEL_FIELDS:
        if field in payload:
            return "openai_only_body_shape"
    messages = payload.get("messages")
    if not isinstance(messages, list):
        return "messages_required"
    for item in messages:
        if not isinstance(item, dict):
            return "message_shape_invalid"
        if item.get("role") not in {"system", "user", "assistant"}:
            return "message_role_invalid"
    tool_names: set[str] = set()
    tools = payload.get("tools")
    if tools is not None:
        if not isinstance(tools, list):
            return "tool_shape_invalid"
        for item in tools:
            if not isinstance(item, dict):
                return "tool_shape_invalid"
            if item.get("type") == "function" or "function" in item:
                return "openai_function_tool_shape"
            name = item.get("name")
            schema = item.get("input_schema")
            if not isinstance(name, str) or not _safe_bridge_tool_name(name.strip()) or not isinstance(schema, dict):
                return "tool_shape_invalid"
            tool_names.add(name.strip())
    if "tool_choice" in payload:
        choice = payload.get("tool_choice")
        if not isinstance(choice, dict):
            return "tool_choice_shape_invalid"
        if choice.get("type") == "function" or "function" in choice:
            return "openai_function_tool_shape"
        choice_type = choice.get("type")
        if choice_type == "tool":
            name = choice.get("name")
            if not isinstance(name, str) or not _safe_bridge_tool_name(name.strip()):
                return "tool_choice_shape_invalid"
            if name.strip() not in tool_names:
                return "tool_choice_shape_invalid"
        elif choice_type not in {"auto", "any", "none"}:
            return "tool_choice_shape_invalid"
    return None


def cp5_bridge_skeleton_sse_body(route_decision: RouteDecision, *, body: bytes | None) -> bytes:
    model = json.dumps(route_decision.model_id, separators=(",", ":"))
    tool_name = _bridge_skeleton_tool_name(body)
    if tool_name:
        return _cp5_bridge_tool_use_sse_body(model=model, tool_name=tool_name)
    content = "bridge skeleton"
    return (
        "event: message_start\n"
        f"data: {{\"type\":\"message_start\",\"message\":{{\"id\":\"msg_bridge_skeleton_cp5\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":{model},\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{{\"input_tokens\":1,\"output_tokens\":1}}}}}}\n\n"
        "event: content_block_start\n"
        "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
        "event: content_block_delta\n"
        f"data: {{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{{\"type\":\"text_delta\",\"text\":{json.dumps(content)}}}}}\n\n"
        "event: content_block_stop\n"
        "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
        "event: message_delta\n"
        "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"
        "event: message_stop\n"
        "data: {\"type\":\"message_stop\"}\n\n"
    ).encode("utf-8")


def cp4_bridge_stub_sse_body(route_decision: RouteDecision) -> bytes:
    return cp5_bridge_skeleton_sse_body(route_decision, body=None)


def _bridge_skeleton_tool_name(body: bytes | None) -> str:
    if not body:
        return ""
    try:
        payload = json.loads(body.decode("utf-8"))
    except Exception:  # noqa: BLE001 - skeleton falls back to text on malformed body
        return ""
    if not isinstance(payload, dict):
        return ""
    choice = payload.get("tool_choice")
    if isinstance(choice, dict) and choice.get("type") == "tool" and isinstance(choice.get("name"), str):
        name = choice["name"].strip()
        if _safe_bridge_tool_name(name):
            return name
    tools = payload.get("tools")
    if isinstance(tools, list) and tools:
        first = tools[0]
        if isinstance(first, dict) and isinstance(first.get("name"), str):
            name = first["name"].strip()
            if _safe_bridge_tool_name(name):
                return name
    return ""


def _safe_bridge_tool_name(value: str) -> bool:
    return re.fullmatch(r"[A-Za-z0-9_-]{1,64}", value) is not None


def _cp5_bridge_tool_use_sse_body(*, model: str, tool_name: str) -> bytes:
    escaped_tool = json.dumps(tool_name, separators=(",", ":"))
    partial_json = json.dumps(json.dumps({"city": "San Francisco"}, separators=(",", ":")), separators=(",", ":"))
    return (
        "event: message_start\n"
        f"data: {{\"type\":\"message_start\",\"message\":{{\"id\":\"msg_bridge_skeleton_cp5\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":{model},\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{{\"input_tokens\":1,\"output_tokens\":1}}}}}}\n\n"
        "event: content_block_start\n"
        f"data: {{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{{\"type\":\"tool_use\",\"id\":\"toolu_bridge_skeleton_cp5\",\"name\":{escaped_tool},\"input\":{{}}}}}}\n\n"
        "event: content_block_delta\n"
        f"data: {{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{{\"type\":\"input_json_delta\",\"partial_json\":{partial_json}}}}}\n\n"
        "event: content_block_stop\n"
        "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
        "event: message_delta\n"
        "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n"
        "event: message_stop\n"
        "data: {\"type\":\"message_stop\"}\n\n"
    ).encode("utf-8")


def _route_hint_session_ref(headers: Mapping[str, str]) -> str:
    for key, value in headers.items():
        if key.lower() == "x-claude-code-session-id" and isinstance(value, str) and value.strip():
            return value.strip()
    return str(session_key_from_headers(headers).get("value", ""))


def _has_route_hint_headers(headers: Mapping[str, str]) -> bool:
    return any(key.lower() in {
        "x-zhumeng-claude-code-route-hint",
        "x-zhumeng-claude-code-route-signature",
    } for key in headers)


def _sign_native_messages_attestation(encoded: str, method: str, request_path: str, body: bytes, secret: str) -> str:
    material = b"\n".join(
        [
            encoded.encode("ascii"),
            method.upper().encode("ascii"),
            request_path.encode("utf-8"),
            sha256(body).hexdigest().encode("ascii"),
        ]
    )
    digest = hmac.new(secret.encode("utf-8"), material, sha256).digest()
    return base64.urlsafe_b64encode(digest).decode("ascii").rstrip("=")


def _safe_claude_code_version(headers: Mapping[str, str]) -> str:
    ua = ""
    for key, value in headers.items():
        if key.lower() == "user-agent":
            ua = value
            break
    match = re.search(r"(?:claude-cli|claude-code)/([0-9]+(?:\.[0-9]+){1,2})", ua, flags=re.IGNORECASE)
    if not match:
        return "unknown"
    return match.group(1)



def _cli_session_budget_ledger(args: argparse.Namespace) -> SessionBudgetLedger | None:
    if getattr(args, "disable_session_budget", False):
        return None
    if not getattr(args, "enforce_session_budget", False):
        return None
    return SessionBudgetLedger(SessionBudgetPolicy(
        max_messages_per_session=_cli_int_or_default(args, "session_budget_max_messages", SessionBudgetPolicy.max_messages_per_session),
        max_rich_messages_per_session=_cli_int_or_default(args, "session_budget_max_rich_messages", SessionBudgetPolicy.max_rich_messages_per_session),
        max_total_body_bytes_per_session=_cli_int_or_default(args, "session_budget_max_body_bytes", SessionBudgetPolicy.max_total_body_bytes_per_session),
        max_total_tool_def_bytes_per_session=_cli_int_or_default(args, "session_budget_max_tool_def_bytes", SessionBudgetPolicy.max_total_tool_def_bytes_per_session),
        max_thinking_messages_per_session=_cli_int_or_default(args, "session_budget_max_thinking_messages", SessionBudgetPolicy.max_thinking_messages_per_session),
    ))


def _cli_int_or_default(args: argparse.Namespace, name: str, default: int) -> int:
    value = getattr(args, name, None)
    return default if value is None else value

def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Redacting Claude Code local forwarder for localhost-only canary validation")
    parser.add_argument("--listen-host", default="127.0.0.1")
    parser.add_argument("--listen-port", type=int, required=True)
    parser.add_argument("--upstream-base", required=True)
    parser.add_argument("--sub2api-auth")
    parser.add_argument("--sub2api-auth-env", default="ZHUMENG_API_KEY")
    parser.add_argument("--native-attestation", action="store_true", help="Enable request-bound Claude Code native attestation for /v1/messages.")
    parser.add_argument("--route-hint-secret-env", help="Enable CP4 signed per-request route hints with the secret from this env var.")
    parser.add_argument("--route-hint-catalog-version", default="cp4-cli-fixture-v1")
    parser.add_argument("--summary-path", type=Path, required=True)
    parser.add_argument("--control-plane-intent-url")
    parser.add_argument("--control-plane-intent-auth")
    parser.add_argument("--connect-mode", choices=["block", "stub"], default="block")
    parser.add_argument("--cert-path", type=Path)
    parser.add_argument("--key-path", type=Path)
    parser.add_argument("--max-messages", type=int)
    parser.add_argument("--policy-path", type=Path)
    parser.add_argument("--cost-max-tokens", type=int)
    parser.add_argument("--cost-max-body-bytes", type=int)
    parser.add_argument("--cost-max-tools", type=int)
    parser.add_argument("--cost-max-messages", type=int)
    parser.add_argument("--cost-max-content-blocks", type=int)
    parser.add_argument("--cost-max-text-bytes", type=int)
    parser.add_argument("--cost-max-system-bytes", type=int)
    parser.add_argument("--cost-max-tool-def-bytes", type=int)
    parser.add_argument("--cost-allow-stream", action="store_true")
    parser.add_argument("--cost-allow-thinking", action="store_true")
    parser.add_argument("--cost-max-thinking-budget-tokens", type=int)
    parser.add_argument("--cost-allow-assistant-messages", action="store_true")
    parser.add_argument("--cost-allow-tool-content", action="store_true")
    parser.add_argument("--enforce-session-budget", action="store_true", help="Explicitly enforce session budget limits. Defaults to observe-only/no hard budget for production safety.")
    parser.add_argument("--disable-session-budget", action="store_true", help="Deprecated compatibility flag; session budget enforcement is disabled by default.")
    parser.add_argument("--session-budget-max-messages", type=int)
    parser.add_argument("--session-budget-max-rich-messages", type=int)
    parser.add_argument("--session-budget-max-body-bytes", type=int)
    parser.add_argument("--session-budget-max-tool-def-bytes", type=int)
    parser.add_argument("--session-budget-max-thinking-messages", type=int)
    parser.add_argument("--capture-level", choices=sorted(CAPTURE_LEVELS), default="summary")
    parser.add_argument("--local-raw-dir", type=Path)
    parser.add_argument(
        "--allow-nonloopback-upstream",
        action="store_true",
        help="Allow forwarding /v1/messages to a non-loopback Zhumeng/Sub2API upstream. Claude/Anthropic hosts remain forbidden.",
    )
    parser.add_argument("--managed-session")
    parser.add_argument("--device-id")
    parser.add_argument("--agent-version", default="0.1.0")
    args = parser.parse_args(argv)
    sub2api_auth = args.sub2api_auth or os.environ.get(args.sub2api_auth_env or "")
    if not sub2api_auth:
        print("missing Sub2API auth: pass --sub2api-auth or set --sub2api-auth-env", file=os.sys.stderr)
        return 2

    try:
        policy = load_policy_from_path(args.policy_path)
    except (OSError, json.JSONDecodeError, PolicyConfigError) as exc:
        print(f"invalid policy config: {exc}", file=os.sys.stderr)
        return 2

    cost_limits = _cli_cost_envelope_limits(args)
    session_budget_ledger = _cli_session_budget_ledger(args)
    route_hint_secret = os.environ.get(args.route_hint_secret_env or "") if args.route_hint_secret_env else None
    route_hint_catalog = None
    if route_hint_secret:
        catalog_version = args.route_hint_catalog_version
        route_hint_catalog = cp4_fixture_route_catalog(
            runtime_hash=_native_env_hash("ZHUMENG_CLAUDE_RUNTIME_HASH"),
            overlay_hash=_native_env_hash("ZHUMENG_CLAUDE_OVERLAY_HASH"),
            catalog_hash=NATIVE_UNKNOWN_HASH,
            catalog_version=catalog_version,
        )
        route_hint_catalog = cp4_fixture_route_catalog(
            runtime_hash=route_hint_catalog.runtime_hash,
            overlay_hash=route_hint_catalog.overlay_hash,
            catalog_hash=route_catalog_content_hash(route_hint_catalog),
            catalog_version=catalog_version,
        )
        if _native_env_hash("ZHUMENG_CLAUDE_CATALOG_HASH") != route_hint_catalog.catalog_hash:
            print("invalid guard config: route hint catalog hash mismatch", file=os.sys.stderr)
            return 2

    try:
        forwarder = RedactingForwarder(
            GuardConfig(
                listen_host=args.listen_host,
                listen_port=args.listen_port,
                upstream_base=args.upstream_base,
                sub2api_auth=sub2api_auth,
                summary_path=args.summary_path,
                control_plane_intent_url=args.control_plane_intent_url,
                control_plane_intent_auth=args.control_plane_intent_auth,
                connect_mode=args.connect_mode,
                cert_path=args.cert_path,
                key_path=args.key_path,
                max_messages=args.max_messages,
                policy=policy,
                cost_envelope_limits=cost_limits,
                session_budget_ledger=session_budget_ledger,
                capture_level=args.capture_level,
                local_raw_dir=args.local_raw_dir,
                allow_nonloopback_upstream=args.allow_nonloopback_upstream,
                native_attestation_secret=os.environ.get("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET") if args.native_attestation else "",
                route_hint_secret=route_hint_secret,
                route_hint_catalog=route_hint_catalog,
                managed_session_id=args.managed_session,
                device_id=args.device_id,
                agent_version=args.agent_version,
            )
        )
    except ValueError as exc:
        print(f"invalid guard config: {exc}", file=os.sys.stderr)
        return 2
    forwarder.start_background()
    print(json.dumps({"listen": f"http://{args.listen_host}:{args.listen_port}", "summary": str(args.summary_path)}), flush=True)
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        forwarder.stop()
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
