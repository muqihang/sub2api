#!/usr/bin/env python3
"""Local-only A/B diff gate for canary message shapes."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping


REQUIRED_FIELDS = (
    'messages_count',
    'model',
    'user_agent',
    'beta',
    'context_1m',
    'body_keys',
    'body_size',
    'max_tokens',
    'tools_count',
    'output_config_keys',
    'thinking_present',
    'context_management_present',
    'session_uuid_like',
    'retry_count',
    'error_count',
    'extra_message_count',
)
P1_FIELDS = {'retry_count', 'error_count', 'extra_message_count'}


@dataclass(frozen=True)
class ABDiffResult:
    status: str
    allows_real_canary: bool
    readiness_status: str
    readiness_block_reason: str | None
    differences: list[str]
    missing_fields: list[str]


def message_shape(**overrides: Any) -> dict[str, Any]:
    shape = {
        'messages_count': 1,
        'model': 'claude-sonnet-4-6',
        'user_agent': 'claude-cli/2.1.150 (external, sdk-cli)',
        'beta': True,
        'context_1m': False,
        'body_keys': ['max_tokens', 'messages', 'model'],
        'body_size': 256,
        'max_tokens': 32,
        'tools_count': 0,
        'output_config_keys': ['format'],
        'thinking_present': False,
        'context_management_present': False,
        'session_uuid_like': True,
        'retry_count': 0,
        'error_count': 0,
        'extra_message_count': 0,
    }
    shape.update(overrides)
    return shape


def compare_ab_message_shapes(block: Mapping[str, Any], stub: Mapping[str, Any]) -> ABDiffResult:
    missing_fields = sorted(
        field
        for field in REQUIRED_FIELDS
        if field not in block or field not in stub
    )
    differences: list[str] = []
    highest_status = 'PASS'

    for field in REQUIRED_FIELDS:
        if field in missing_fields:
            continue
        block_value = _normalize_value(field, block[field])
        stub_value = _normalize_value(field, stub[field])
        if block_value == stub_value:
            continue
        differences.append(_difference_text(field, block_value, stub_value))
        if field in P1_FIELDS:
            highest_status = 'P1'
        elif highest_status == 'PASS':
            highest_status = 'UNKNOWN'

    if missing_fields and highest_status == 'PASS':
        highest_status = 'UNKNOWN'
    allows_real_canary = highest_status == 'PASS' and not missing_fields and not differences
    readiness_status = 'READY' if allows_real_canary else 'BLOCKED'
    readiness_block_reason = None if allows_real_canary else _readiness_block_reason(highest_status, missing_fields, differences)
    return ABDiffResult(
        status=highest_status,
        allows_real_canary=allows_real_canary,
        readiness_status=readiness_status,
        readiness_block_reason=readiness_block_reason,
        differences=differences,
        missing_fields=missing_fields,
    )


def evaluate_ab_diff_gate(block_summary: Mapping[str, Any], stub_summary: Mapping[str, Any]) -> ABDiffResult:
    return compare_ab_message_shapes(
        sanitize_shape_summary(block_summary),
        sanitize_shape_summary(stub_summary),
    )


def sanitize_shape_summary(summary: Mapping[str, Any]) -> dict[str, Any]:
    return {
        field: summary[field]
        for field in REQUIRED_FIELDS
        if field in summary
    }


def readiness_payload(result: ABDiffResult) -> dict[str, Any]:
    return {
        'status': result.status,
        'readiness_status': result.readiness_status,
        'allows_real_canary': result.allows_real_canary,
        'readiness_block_reason': result.readiness_block_reason,
        'differences': list(result.differences),
        'missing_fields': list(result.missing_fields),
    }


def _normalize_value(field: str, value: Any) -> Any:
    if field in {'body_keys', 'output_config_keys'} and isinstance(value, (list, tuple, set)):
        return tuple(sorted(str(item) for item in value))
    return value


def _difference_text(field: str, block_value: Any, stub_value: Any) -> str:
    if field == 'body_size' and isinstance(block_value, int) and isinstance(stub_value, int):
        delta = stub_value - block_value
        bucket_relation = 'same_bucket' if _body_size_bucket(block_value) == _body_size_bucket(stub_value) else 'different_bucket'
        return (
            f"{field}: block={block_value!r} stub={stub_value!r} "
            f"delta={delta} block_bucket={_body_size_bucket(block_value)} "
            f"stub_bucket={_body_size_bucket(stub_value)} {bucket_relation}"
        )
    return f'{field}: block={block_value!r} stub={stub_value!r}'


def _body_size_bucket(value: int) -> str:
    if value < 256:
        return 'lt_256'
    if value < 1024:
        return '256_to_1023'
    if value < 4096:
        return '1024_to_4095'
    return 'gte_4096'


def _readiness_block_reason(status: str, missing_fields: list[str], differences: list[str]) -> str:
    if missing_fields:
        return f"ab_diff_status_{status}: missing_required_fields={','.join(missing_fields)}"
    if differences:
        return f"ab_diff_status_{status}: {differences[0]}"
    return f'ab_diff_status_{status}'
