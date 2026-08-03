from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path
from typing import Any

from .capture_config import CorrelationHasher
from .capture_writer import write_jsonl


IDENTIFIER_FIELDS = ["session_id", "thread_id", "turn_id", "x_client_request_id", "window_id"]


def build_correlation_hashes(identifiers: dict[str, object], hasher: CorrelationHasher) -> dict[str, str]:
    if hasher.confidence_mode != "shared_key":
        return {}
    normalized = normalize_identifier_fields(identifiers)
    output: dict[str, str] = {}
    for field in IDENTIFIER_FIELDS:
        value = normalized.get(field)
        if value:
            output[f"{field}_hash"] = hasher.hash_identifier(value)
    return output


def normalize_identifier_fields(identifiers: dict[str, object]) -> dict[str, object]:
    aliases = {
        "session_id": "session_id",
        "x-codex-session-id": "session_id",
        "thread_id": "thread_id",
        "x-codex-thread-id": "thread_id",
        "turn_id": "turn_id",
        "x-codex-turn-id": "turn_id",
        "x_client_request_id": "x_client_request_id",
        "x-client-request-id": "x_client_request_id",
        "window_id": "window_id",
        "x-codex-window-id": "window_id",
    }
    output: dict[str, object] = {}
    for key, value in identifiers.items():
        normalized_key = aliases.get(str(key).lower())
        if normalized_key and value:
            output[normalized_key] = value
    return output


def link_traces(desktop_events: list[dict[str, Any]], gateway_events: list[dict[str, Any]]) -> list[dict[str, object]]:
    links: list[dict[str, object]] = []
    for desktop in desktop_events:
        for gateway in gateway_events:
            linked_by = matching_hash_field(desktop.get("correlation_hashes", {}), gateway.get("correlation_hashes", {}))
            delta = abs(parse_ts_ms(str(desktop.get("ts", ""))) - parse_ts_ms(str(gateway.get("ts", ""))))
            same_shape = desktop.get("model") == gateway.get("model") and desktop.get("request_path") == gateway.get("request_path")
            if linked_by:
                confidence = "high"
                degraded_reason = None
            elif same_shape and delta <= 5000:
                linked_by = "time_model_path"
                confidence = "low"
                degraded_reason = "shared_correlation_hash_missing"
            else:
                continue
            links.append({
                "schema_version": 1,
                "desktop_trace_id": desktop.get("desktop_trace_id"),
                "gateway_trace_id": gateway.get("gateway_trace_id"),
                "linked_by": linked_by,
                "confidence": confidence,
                "correlation_hashes": shared_hashes(desktop.get("correlation_hashes", {}), gateway.get("correlation_hashes", {})),
                "time_delta_ms": delta,
                "model": desktop.get("model") or gateway.get("model"),
                "request_path": desktop.get("request_path") or gateway.get("request_path"),
                "pass_fail_rule": "prefer_shared_hash_else_time_model_path",
                **({"degraded_reason": degraded_reason} if degraded_reason else {}),
            })
    return links


def write_trace_links(path: Path, links: list[dict[str, object]]) -> None:
    write_jsonl(path, links)


def matching_hash_field(left: object, right: object) -> str | None:
    if not isinstance(left, dict) or not isinstance(right, dict):
        return None
    for field in ["x_client_request_id_hash", "thread_id_hash", "session_id_hash", "turn_id_hash", "window_id_hash"]:
        if (
            isinstance(left.get(field), str)
            and str(left.get(field)).startswith("hmac-sha256:")
            and left.get(field) == right.get(field)
        ):
            return field
    return None


def shared_hashes(left: object, right: object) -> dict[str, object]:
    if not isinstance(left, dict) or not isinstance(right, dict):
        return {}
    return {key: value for key, value in left.items() if right.get(key) == value}


def parse_ts_ms(value: str) -> int:
    if not value:
        return 0
    normalized = value.replace("Z", "+00:00")
    try:
        return int(datetime.fromisoformat(normalized).timestamp() * 1000)
    except ValueError:
        return 0


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    events: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(event, dict):
            events.append(event)
    return events
