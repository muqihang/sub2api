#!/usr/bin/env python3
"""Session-level budget controls for Claude Code CLI-through routing.

This module is intentionally local and deterministic: it never stores raw
session ids, prompts, tokens, or Authorization values in budget summaries.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from hashlib import sha256
from threading import Lock
from typing import Any, Mapping
import hmac
import json
import os
import re


_UUID_LIKE_RE = re.compile(r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$')
_HMAC_RE = re.compile(r'^hmac-sha256:[0-9a-f]{64}$')
_REF_KEYS = {'key_id', 'scope', 'version', 'value'}
_SESSION_SCOPE = 'session_budget_session'
_SERVER_SESSION_SCOPE = 'claude_code_server_session'
_INVALID_SESSION_KEY_ID = 'session_budget_invalid'
_INVALID_SESSION_PAYLOAD = 'invalid-session-id'


@dataclass(frozen=True)
class SessionBudgetPolicy:
    # Production default is observe-only: limits are disabled unless a caller
    # explicitly supplies a budget policy for canary/fixture enforcement.
    max_messages_per_session: int = -1
    max_rich_messages_per_session: int = -1
    max_total_body_bytes_per_session: int = -1
    max_total_tool_def_bytes_per_session: int = -1
    max_thinking_messages_per_session: int = -1


@dataclass(frozen=True)
class SessionBudgetDecision:
    allowed: bool
    reason: str
    status: int
    summary: dict[str, Any]


@dataclass
class _SessionUsage:
    messages_used: int = 0
    rich_messages_used: int = 0
    thinking_messages_used: int = 0
    body_bytes_used: int = 0
    tool_def_bytes_used: int = 0


@dataclass
class SessionBudgetLedger:
    policy: SessionBudgetPolicy
    _usage: dict[str, _SessionUsage] = field(default_factory=dict)
    _lock: Lock = field(default_factory=Lock)

    def check_and_record(self, session_key: Any, metrics: Mapping[str, Any]) -> SessionBudgetDecision:
        safe_session_ref = _safe_session_key_ref(session_key)
        if _is_invalid_session_key_ref(safe_session_ref):
            return SessionBudgetDecision(False, 'session_identity_invalid', 409, _summary(safe_session_ref, _SessionUsage(), self.policy))
        safe_session_key = safe_session_ref['value']
        body_bytes = _non_negative_int(metrics.get('body_bytes'))
        tool_def_bytes = _non_negative_int(metrics.get('tool_def_bytes'))
        tools_count = _non_negative_int(metrics.get('tools_count'))
        thinking_present = bool(metrics.get('thinking_present'))
        rich_message = thinking_present or tools_count > 0 or bool(metrics.get('context_management_present'))

        with self._lock:
            current = self._usage.setdefault(safe_session_key, _SessionUsage())
            blocked = self._blocked_reason(current, body_bytes, tool_def_bytes, rich_message, thinking_present)
            if blocked:
                return SessionBudgetDecision(False, blocked, 409, _summary(safe_session_ref, current, self.policy))

            current.messages_used += 1
            current.body_bytes_used += body_bytes
            current.tool_def_bytes_used += tool_def_bytes
            if rich_message:
                current.rich_messages_used += 1
            if thinking_present:
                current.thinking_messages_used += 1
            return SessionBudgetDecision(True, 'allowed', 200, _summary(safe_session_ref, current, self.policy))

    def _blocked_reason(
        self,
        current: _SessionUsage,
        body_bytes: int,
        tool_def_bytes: int,
        rich_message: bool,
        thinking_present: bool,
    ) -> str | None:
        policy = self.policy
        if policy.max_messages_per_session >= 0 and current.messages_used >= policy.max_messages_per_session:
            return 'session_messages_budget_exceeded'
        if rich_message and policy.max_rich_messages_per_session >= 0 and current.rich_messages_used >= policy.max_rich_messages_per_session:
            return 'session_rich_messages_budget_exceeded'
        if thinking_present and policy.max_thinking_messages_per_session >= 0 and current.thinking_messages_used >= policy.max_thinking_messages_per_session:
            return 'session_thinking_messages_budget_exceeded'
        if policy.max_total_body_bytes_per_session >= 0 and current.body_bytes_used + body_bytes > policy.max_total_body_bytes_per_session:
            return 'session_body_bytes_budget_exceeded'
        if policy.max_total_tool_def_bytes_per_session >= 0 and current.tool_def_bytes_used + tool_def_bytes > policy.max_total_tool_def_bytes_per_session:
            return 'session_tool_def_bytes_budget_exceeded'
        return None


def session_key_from_headers(headers: Mapping[str, str]) -> dict[str, Any]:
    for key, value in headers.items():
        if key.lower() == 'x-claude-code-session-id' and isinstance(value, str) and value:
            canonical_session = _canonical_session_identity(value)
            if canonical_session is not None:
                return _server_session_key_ref_from_local_uuid(canonical_session)
            return _invalid_session_key_ref()
    return _invalid_session_key_ref()


def _safe_session_key_value(session_key: Any) -> str:
    if isinstance(session_key, Mapping):
        return _safe_session_key_ref(session_key)['value']
    if isinstance(session_key, str) and _HMAC_RE.fullmatch(session_key):
        return session_key
    canonical_session = _canonical_session_identity(session_key)
    if canonical_session is not None:
        return _server_session_key_ref_from_local_uuid(canonical_session)['value']
    return _invalid_session_key_ref()['value']


def _safe_session_key_ref(session_key: Any) -> dict[str, Any]:
    if isinstance(session_key, Mapping):
        missing = _REF_KEYS - set(session_key.keys())
        extra = set(session_key.keys()) - _REF_KEYS
        if missing or extra:
            raise ValueError('session key ref must match the scoped HMAC schema')
        key_id = session_key.get('key_id')
        scope = session_key.get('scope')
        version = session_key.get('version')
        value = session_key.get('value')
        if not isinstance(key_id, str) or not key_id:
            raise ValueError('session key ref.key_id must be a non-empty string')
        if scope != _SESSION_SCOPE:
            raise ValueError('session key ref.scope must match the session budget scope')
        if not isinstance(version, int) or version <= 0:
            raise ValueError('session key ref.version must be a positive int')
        if not isinstance(value, str) or _HMAC_RE.fullmatch(value) is None:
            raise ValueError('session key ref.value must be an hmac-sha256 reference')
        return {
            'key_id': key_id,
            'scope': scope,
            'version': version,
            'value': value,
        }
    if isinstance(session_key, str) and _HMAC_RE.fullmatch(session_key):
        return {
            'key_id': os.environ.get('SUB2API_SESSION_BUDGET_HMAC_KEY_ID', 'session_budget_v1'),
            'scope': _SESSION_SCOPE,
            'version': int(os.environ.get('SUB2API_SESSION_BUDGET_HMAC_VERSION', '1')),
            'value': session_key,
        }
    canonical_session = _canonical_session_identity(session_key)
    if canonical_session is not None:
        return _server_session_key_ref_from_local_uuid(canonical_session)
    return _invalid_session_key_ref()


def _canonical_session_identity(value: Any) -> str | None:
    if not isinstance(value, str):
        return None
    normalized = value.strip()
    if _UUID_LIKE_RE.fullmatch(normalized) is None:
        return None
    return normalized.lower()


def _server_session_key_ref_from_local_uuid(canonical_session: str) -> dict[str, Any]:
    return _scoped_hmac_ref(_server_session_mapping_material(canonical_session), scope=_SESSION_SCOPE)


def _invalid_session_key_ref() -> dict[str, Any]:
    ref = _scoped_hmac_ref(_INVALID_SESSION_PAYLOAD, scope=_SESSION_SCOPE)
    ref['key_id'] = _INVALID_SESSION_KEY_ID
    return ref


def _is_invalid_session_key_ref(session_key_ref: Mapping[str, Any]) -> bool:
    return session_key_ref.get('key_id') == _INVALID_SESSION_KEY_ID


def _server_session_mapping_material(canonical_session: str) -> str:
    user_scope = os.environ.get('SUB2API_SESSION_BUDGET_USER_SCOPE', '').strip() or 'claude_code_session_scope:anonymous'
    payload: dict[str, str] = {'user_scope': user_scope}
    account_ref = os.environ.get('SUB2API_SESSION_BUDGET_ACCOUNT_REF', '').strip()
    device_id = os.environ.get('SUB2API_SESSION_BUDGET_DEVICE_ID', '').strip()
    account_uuid = os.environ.get('SUB2API_SESSION_BUDGET_ACCOUNT_UUID', '').strip()
    if account_ref:
        payload['account_ref'] = account_ref
    if device_id:
        payload['device_id'] = device_id
    if account_uuid:
        payload['account_uuid'] = account_uuid
    payload['raw_session_id'] = canonical_session
    return json.dumps(payload, separators=(',', ':'), ensure_ascii=True)


def _scoped_hmac_ref(payload: str, *, scope: str) -> dict[str, Any]:
    key_id = os.environ.get('SUB2API_SESSION_BUDGET_HMAC_KEY_ID', 'session_budget_v1')
    version = int(os.environ.get('SUB2API_SESSION_BUDGET_HMAC_VERSION', '1'))
    secret = os.environ.get('SUB2API_SESSION_BUDGET_HMAC_KEY', 'sub2api-session-budget-dev-key')
    material = scope.encode('utf-8') + b'\x00v' + str(version).encode('ascii') + b'\x00' + payload.encode('utf-8')
    value = 'hmac-sha256:' + hmac.new(secret.encode('utf-8'), material, sha256).hexdigest()
    return {
        'key_id': key_id,
        'scope': scope,
        'version': version,
        'value': value,
    }


def _non_negative_int(value: Any) -> int:
    return value if isinstance(value, int) and not isinstance(value, bool) and value > 0 else 0


def _summary(session_key_ref: Mapping[str, Any], usage: _SessionUsage, policy: SessionBudgetPolicy) -> dict[str, Any]:
    return {
        'session_key_ref': dict(session_key_ref),
        'messages_used': usage.messages_used,
        'rich_messages_used': usage.rich_messages_used,
        'thinking_messages_used': usage.thinking_messages_used,
        'body_bytes_used': usage.body_bytes_used,
        'tool_def_bytes_used': usage.tool_def_bytes_used,
        'limits': {
            'max_messages_per_session': policy.max_messages_per_session,
            'max_rich_messages_per_session': policy.max_rich_messages_per_session,
            'max_thinking_messages_per_session': policy.max_thinking_messages_per_session,
            'max_total_body_bytes_per_session': policy.max_total_body_bytes_per_session,
            'max_total_tool_def_bytes_per_session': policy.max_total_tool_def_bytes_per_session,
        },
    }
