from __future__ import annotations

import json
import re
from datetime import datetime, timezone
from typing import Any, Callable, Iterable

from .capture_config import CorrelationHasher
from .capture_linker import build_correlation_hashes
from .capture_redact import content_policy_for_class, redaction_reason


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def shape_value(value: Any) -> Any:
    if isinstance(value, dict):
        return {safe_shape_key(str(key)): shape_value(child) for key, child in value.items()}
    if isinstance(value, list):
        return {"type": "array", "length": len(value), "items": [shape_value(value[0])] if value else []}
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "bool"
    if isinstance(value, int) and not isinstance(value, bool):
        return "int"
    if isinstance(value, float):
        return "float"
    if isinstance(value, str):
        return "str"
    return type(value).__name__


SAFE_KEY_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{0,80}$")
SENSITIVE_KEY_RE = re.compile(
    r"(^/|/Users/|/Applications/|https?://|git@|[A-Fa-f0-9]{40}|Bearer\s+|Cookie\s*=|authorization|cookie|api[_-]?key|token|secret|private|branch)",
    re.IGNORECASE,
)
SAFE_METADATA_RE = re.compile(r"^[A-Za-z0-9_./:@+-]{1,160}$")
SENSITIVE_METADATA_RE = re.compile(
    r"(/Users/|/Applications/|https?://|git@|Cookie\s*=|Bearer\s+|api[_-]?key|token|secret|refs/heads/|feature/[A-Za-z0-9_./-]+|(?<!sha256:)[A-Fa-f0-9]{40}(?![A-Fa-f0-9]))",
    re.IGNORECASE,
)


def safe_shape_key(key: str) -> str:
    if SAFE_KEY_RE.match(key) and not SENSITIVE_KEY_RE.search(key):
        return key
    return "field_hash_" + CorrelationHasher.from_key_file(None).hash_identifier(key).split(":", 1)[1][:16]


def is_safe_metadata_text(value: str) -> bool:
    return bool(SAFE_METADATA_RE.match(value)) and not SENSITIVE_METADATA_RE.search(value)


def add_safe_metadata(output: dict[str, object], key: str, value: object, hasher: CorrelationHasher) -> None:
    text = str(value)
    if is_safe_metadata_text(text):
        output[key] = text
    else:
        output[f"{key}_hash"] = hasher.hash_identifier(text)


def safe_protocol_label(value: object, hasher: CorrelationHasher) -> str:
    text = str(value)
    if is_safe_metadata_text(text):
        return text
    return "hash:" + hasher.hash_identifier(text).split(":", 1)[1][:16]


def shape_app_server_frame(
    frame: bytes,
    *,
    direction: str,
    hasher: CorrelationHasher | None = None,
    desktop_trace_id: str = "cd_unknown",
    seq: int = 1,
    correlation_ids: dict[str, object] | None = None,
    model: str | None = None,
    request_path: str | None = None,
) -> dict[str, object]:
    hasher = hasher or CorrelationHasher.from_key_file(None)
    base: dict[str, object] = {
        "schema_version": 1,
        "source": "codex_desktop",
        "desktop_trace_id": desktop_trace_id,
        "seq": seq,
        "ts": utc_now(),
        "protocol": "app_server_v2",
        "direction": direction,
        "payload_hash": hasher.hash_bytes(frame),
    }
    if correlation_ids:
        hashes = build_correlation_hashes(correlation_ids, hasher)
        if hashes:
            base["correlation_hashes"] = hashes
    if model:
        add_safe_metadata(base, "model", model, hasher)
    if request_path:
        add_safe_metadata(base, "request_path", request_path, hasher)
    base["trace_correlation"] = trace_correlation_summary(
        desktop_trace_id=desktop_trace_id,
        correlation_hashes=base.get("correlation_hashes"),
        model=model,
        request_path=request_path,
    )
    try:
        decoded = frame.decode("utf-8")
        payload = json.loads(decoded)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return {
            **base,
            "payload_policy": "hash_only",
            "redaction_reason": redaction_reason("hash_only"),
            "payload_bytes": len(frame),
            "malformed": True,
        }
    if isinstance(payload, list):
        methods = [safe_protocol_label(item.get("method"), hasher) for item in payload if isinstance(item, dict) and item.get("method")]
        return {
            **base,
            "payload_policy": "shape_only",
            "method": "batch",
            "methods": methods,
            "id_present": any(isinstance(item, dict) and "id" in item for item in payload),
            "payload_shape": shape_value(payload),
        }
    if not isinstance(payload, dict):
        return {**base, "payload_policy": "shape_only", "id_present": False, "payload_shape": shape_value(payload)}
    return {
        **base,
        "payload_policy": "shape_only",
        "method": safe_protocol_label(payload.get("method") or ("error" if "error" in payload else "response" if "result" in payload else "unknown"), hasher),
        "id_present": "id" in payload,
        "payload_shape": shape_value(payload),
    }


def build_spawn_agent_model_override_report(
    *,
    events: list[dict[str, object]],
    catalog_models: list[str],
    catalog_hash: str | None = None,
    catalog_mtime: str | None = None,
) -> dict[str, object]:
    catalog_has_deepseek = any(str(model).startswith("deepseek-") for model in catalog_models)
    spawn_models: set[str] = set()
    spawn_agent_present = False
    capture_ts: str | None = None
    for event in events:
        if capture_ts is None and event.get("capture_ts"):
            capture_ts = str(event.get("capture_ts"))
        spawn_agent_present = spawn_agent_present or contains_spawn_agent(event.get("tools")) or contains_spawn_agent(event.get("tool_search_output"))
        spawn_models.update(extract_spawn_agent_model_overrides(event.get("tools")))
        spawn_models.update(extract_spawn_agent_model_overrides(event.get("tool_search_output")))
    spawn_agent_has_deepseek = any(model.startswith("deepseek-") for model in spawn_models)
    return {
        "spawn_agent_model_override_mismatch": bool(catalog_has_deepseek and spawn_agent_present and not spawn_agent_has_deepseek),
        "catalog_has_deepseek": catalog_has_deepseek,
        "spawn_agent_has_deepseek": spawn_agent_has_deepseek,
        "spawn_agent_present": spawn_agent_present,
        "spawn_agent_model_count": len(spawn_models),
        "capture_ts": capture_ts,
        "catalog_hash": catalog_hash,
        "catalog_mtime": catalog_mtime,
    }


def contains_spawn_agent(value: object) -> bool:
    if isinstance(value, dict):
        if value.get("name") == "spawn_agent":
            return True
        return any(contains_spawn_agent(child) for child in value.values())
    if isinstance(value, list):
        return any(contains_spawn_agent(child) for child in value)
    return False


def extract_spawn_agent_model_overrides(value: object) -> set[str]:
    models: set[str] = set()
    if isinstance(value, dict):
        if value.get("name") == "spawn_agent":
            models.update(extract_model_schema_values(value.get("input_schema")))
            models.update(extract_model_schema_values(value.get("parameters")))
        for child in value.values():
            models.update(extract_spawn_agent_model_overrides(child))
    elif isinstance(value, list):
        for child in value:
            models.update(extract_spawn_agent_model_overrides(child))
    return models


def extract_model_schema_values(schema: object) -> set[str]:
    models: set[str] = set()
    if not isinstance(schema, dict):
        return models
    properties = schema.get("properties")
    if isinstance(properties, dict):
        model_schema = properties.get("model")
        if isinstance(model_schema, dict):
            for key in ("enum", "oneOf", "anyOf"):
                raw = model_schema.get(key)
                if isinstance(raw, list):
                    for item in raw:
                        if isinstance(item, str):
                            models.add(item)
                        elif isinstance(item, dict):
                            const = item.get("const") or item.get("enum")
                            if isinstance(const, str):
                                models.add(const)
                            elif isinstance(const, list):
                                models.update(str(value) for value in const if isinstance(value, str))
            description = model_schema.get("description")
            if isinstance(description, str):
                for match in re.findall(r"\b(?:deepseek|claude|gpt)[A-Za-z0-9_.:-]+\b", description):
                    models.add(match.rstrip(",.;)"))
    return {model for model in models if is_safe_metadata_text(model)}

def shape_subagent_registration_event(
    *,
    event_name: str,
    conversation_id: str | None = None,
    thread_id: str | None = None,
    status: str | None = None,
    message: str | None = None,
    ts: str | None = None,
    hasher: CorrelationHasher | None = None,
) -> dict[str, object]:
    hasher = hasher or CorrelationHasher.from_key_file(None)
    event: dict[str, object] = {
        "schema_version": 1,
        "source": "codex_desktop",
        "event_type": "subagent_registration",
        "event_name": safe_protocol_label(event_name, hasher),
        "ts": ts or utc_now(),
        "payload_policy": "shape_only",
    }
    if conversation_id:
        event["conversation_id_hash"] = hasher.hash_identifier(str(conversation_id))
    if thread_id:
        event["thread_id_hash"] = hasher.hash_identifier(str(thread_id))
    message_class = classify_subagent_registration_message(message or status or event_name)
    if message_class:
        event["message_class"] = message_class
    if status and is_safe_metadata_text(str(status)) and not classify_subagent_registration_message(status):
        event["status"] = str(status)
    elif status:
        event["status_class"] = classify_subagent_registration_message(status) or "redacted"
    return event


def classify_subagent_registration_message(value: str) -> str | None:
    lowered = value.lower()
    if "unknown conversation" in lowered:
        return "unknown_conversation"
    if "thread read empty" in lowered:
        return "thread_read_empty"
    if "maybe_resume_success" in lowered:
        return "maybe_resume_success"
    return None


def build_subagent_registration_report(events: list[dict[str, object]]) -> dict[str, object]:
    ordered = sorted(events, key=lambda event: str(event.get("ts") or ""))
    order = [subagent_registration_order_entry(event) for event in ordered]
    first_item_before_registered = False
    unknown_count = 0
    thread_read_empty_count = 0
    unknown_seen_by_group: set[str] = set()
    maybe_resume_after_unknown = False
    registered_groups: set[str] = set()
    for event in ordered:
        name = str(event.get("event_name") or "")
        message_class = str(event.get("message_class") or "")
        group = subagent_registration_group_key(event)
        if name in {"thread/start", "thread/resume"}:
            registered_groups.add(group)
        if name in {"item/started", "item/completed", "thread/read"} and group not in registered_groups:
            first_item_before_registered = True
        if message_class == "unknown_conversation":
            unknown_count += 1
            unknown_seen_by_group.add(group)
        if message_class == "thread_read_empty":
            thread_read_empty_count += 1
        if message_class == "maybe_resume_success" and (group in unknown_seen_by_group or "__global__" in unknown_seen_by_group):
            maybe_resume_after_unknown = True
    suspected = first_item_before_registered or unknown_count > 0 or thread_read_empty_count > 0
    return {
        "subagent_registration_race_suspected": suspected,
        "first_item_before_conversation_registered": first_item_before_registered,
        "unknown_conversation_count": unknown_count,
        "thread_read_empty_count": thread_read_empty_count,
        "maybe_resume_success_after_unknown_conversation": maybe_resume_after_unknown,
        "subagent_registration_event_count": len(ordered),
        "subagent_registration_order": order,
    }


def subagent_registration_order_entry(event: dict[str, object]) -> dict[str, object]:
    entry: dict[str, object] = {
        "event_name": str(event.get("event_name") or "unknown"),
        "ts": str(event.get("ts") or ""),
    }
    for key in ("conversation_id_hash", "thread_id_hash", "message_class", "status", "status_class"):
        if event.get(key):
            entry[key] = event[key]
    return entry


def subagent_registration_group_key(event: dict[str, object]) -> str:
    conversation = event.get("conversation_id_hash")
    thread = event.get("thread_id_hash")
    if conversation or thread:
        return f"{conversation or ''}|{thread or ''}"
    return "__global__"

def tee_frames_without_mutation(frames: Iterable[bytes], writer: Callable[[dict[str, object]], None]) -> list[bytes]:
    output: list[bytes] = []
    for index, frame in enumerate(frames, start=1):
        try:
            writer(shape_app_server_frame(frame, direction="unknown", seq=index))
        except Exception:
            pass
        output.append(frame)
    return output


def shape_tool_lifecycle_event(
    *,
    tool_name: str,
    call_id: str,
    item_id: str,
    schema: dict[str, object],
    result: object,
    content_class: str,
    status: str,
    duration_ms: int,
    sent_back_to_model: bool,
    hasher: CorrelationHasher | None = None,
    desktop_trace_id: str | None = None,
    correlation_ids: dict[str, object] | None = None,
    model: str | None = None,
    request_path: str | None = None,
    ui_matrix: dict[str, object] | None = None,
    degraded_reason: str | None = None,
    pass_fail_rule: str | None = None,
) -> dict[str, object]:
    hasher = hasher or CorrelationHasher.from_key_file(None)
    result_text = result if isinstance(result, str) else json.dumps(result, sort_keys=True, default=str)
    policy = content_policy_for_class(content_class)
    if policy == "raw_allowed":
        policy = "raw_allowed"
    else:
        policy = "shape_only"
    namespace = tool_name.split("__", 2)[0] + "__" + tool_name.split("__", 2)[1] + "__" if tool_name.startswith("mcp__") and len(tool_name.split("__", 2)) >= 2 else tool_name.split("_", 1)[0]
    event: dict[str, object] = {
        "schema_version": 1,
        "source": "codex_desktop",
        "event_type": "tool_lifecycle",
        "call_id_hash": hasher.hash_identifier(call_id),
        "item_id_hash": hasher.hash_identifier(item_id),
        "schema_hash": hasher.hash_identifier(json.dumps(schema, sort_keys=True)),
        "content_class": content_class,
        "policy_decision": policy,
        "redaction_reason": redaction_reason(policy),
        "status": status,
        "duration_ms": duration_ms,
        "result_content_type": "text" if isinstance(result, str) else "json" if isinstance(result, (dict, list)) else "unknown",
        "result_chars": len(result_text),
        "result_hash": hasher.hash_identifier(result_text),
        "sent_back_to_model": sent_back_to_model,
    }
    if desktop_trace_id:
        event["desktop_trace_id"] = desktop_trace_id
    correlation_hashes = build_correlation_hashes(correlation_ids or {}, hasher)
    if correlation_hashes:
        event["correlation_hashes"] = correlation_hashes
    add_safe_metadata(event, "tool_name", tool_name, hasher)
    add_safe_metadata(event, "namespace", namespace, hasher)
    if model:
        add_safe_metadata(event, "model", model, hasher)
    if request_path:
        add_safe_metadata(event, "request_path", request_path, hasher)
    normalized_ui_matrix = normalize_ui_matrix(ui_matrix)
    if normalized_ui_matrix:
        event["ui_matrix"] = normalized_ui_matrix
    if degraded_reason:
        event["degraded_reason"] = degraded_reason
    if pass_fail_rule:
        event["pass_fail_rule"] = pass_fail_rule
    event["trace_correlation"] = trace_correlation_summary(
        desktop_trace_id=desktop_trace_id,
        correlation_hashes=event.get("correlation_hashes"),
        model=model,
        request_path=request_path,
    )
    return event


def normalize_ui_matrix(value: object) -> dict[str, bool] | None:
    if not isinstance(value, dict):
        return None
    output: dict[str, bool] = {}
    for key in (
        "command_collapsed",
        "command_expandable",
        "tool_detail_expandable",
        "diff_entry_visible",
        "file_open_action_available",
    ):
        if isinstance(value.get(key), bool):
            output[key] = bool(value[key])
    return output or None


def trace_correlation_summary(
    *,
    desktop_trace_id: str | None,
    correlation_hashes: object,
    model: str | None,
    request_path: str | None,
) -> dict[str, object]:
    has_hashes = isinstance(correlation_hashes, dict) and bool(correlation_hashes)
    fallback_ready = bool(model) and bool(request_path)
    if has_hashes:
        strategy = "shared_hash"
    elif fallback_ready:
        strategy = "time_model_path"
    else:
        strategy = "none"
    return {
        "desktop_trace_id_present": bool(desktop_trace_id),
        "correlation_hashes_present": has_hashes,
        "link_ready": has_hashes or fallback_ready,
        "strategy": strategy,
    }


def capture_model_picker_state(
    *,
    app_server_models: list[dict[str, Any]],
    selected_model: str | None,
    selected_reasoning_effort: str | None,
    ui_visible_model_ids: list[str],
    model_picker_patch_state: dict[str, object],
) -> dict[str, object]:
    event: dict[str, object] = {
        "schema_version": 1,
        "source": "codex_desktop",
        "event_type": "model_picker_state",
        "selected_reasoning_effort": selected_reasoning_effort,
        "model_picker_patch_state": sanitize_patch_state(model_picker_patch_state),
        "capture_modified_model_visibility": False,
    }
    safe_ids: list[str] = []
    hashed_ids: list[str] = []
    display_names: dict[str, object] = {}
    hidden_flags: dict[str, bool] = {}
    supported_reasoning_efforts: dict[str, object] = {}
    default_reasoning_efforts: dict[str, object] = {}
    hasher = CorrelationHasher.from_key_file(None)
    for model in app_server_models:
        model_id = str(model.get("model", ""))
        key = model_id if is_safe_metadata_text(model_id) else "model_hash_" + hasher.hash_identifier(model_id).split(":", 1)[1][:16]
        if is_safe_metadata_text(model_id):
            safe_ids.append(model_id)
        else:
            hashed_ids.append(hasher.hash_identifier(model_id))
        display_name = str(model.get("displayName", ""))
        display_names[key] = display_name if is_safe_metadata_text(display_name) else hasher.hash_identifier(display_name)
        hidden_flags[key] = bool(model.get("hidden", False))
        supported_reasoning_efforts[key] = model.get("supportedReasoningEfforts", [])
        default_reasoning_efforts[key] = model.get("defaultReasoningEffort")
    event["app_server_model_ids"] = safe_ids
    if hashed_ids:
        event["app_server_model_id_hashes"] = hashed_ids
    event["display_names"] = display_names
    event["hidden_flags"] = hidden_flags
    event["supported_reasoning_efforts"] = supported_reasoning_efforts
    event["default_reasoning_efforts"] = default_reasoning_efforts
    if selected_model:
        add_safe_metadata(event, "selected_model", selected_model, hasher)
    event["ui_visible_model_ids"] = [value for value in ui_visible_model_ids if is_safe_metadata_text(str(value))]
    ui_hashes = [hasher.hash_identifier(str(value)) for value in ui_visible_model_ids if not is_safe_metadata_text(str(value))]
    if ui_hashes:
        event["ui_visible_model_id_hashes"] = ui_hashes
    return event


def sanitize_patch_state(state: dict[str, object]) -> dict[str, object]:
    sanitized: dict[str, object] = {}
    for key, value in state.items():
        if is_sensitive_patch_state_field(key, value):
            sanitized[f"{key}_hash"] = CorrelationHasher.from_key_file(None).hash_identifier(value)
            continue
        sanitized[key] = value
    return sanitized


def is_sensitive_patch_state_field(key: str, value: object) -> bool:
    lowered = key.lower()
    if lowered in {"app_path", "backup_path", "repo_url", "remote_url", "branch", "commit", "revision"}:
        return True
    if any(token in lowered for token in ["token", "secret", "api_key", "apikey"]):
        return True
    if isinstance(value, str):
        if lowered.endswith("_path") and value.startswith("/"):
            return True
        if value.startswith(("https://", "http://", "git@")):
            return True
        if value.startswith(("refs/heads/", "feature/")):
            return True
        if re.fullmatch(r"[A-Fa-f0-9]{40}", value):
            return True
    return False
