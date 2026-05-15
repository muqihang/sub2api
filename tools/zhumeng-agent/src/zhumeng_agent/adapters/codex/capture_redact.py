from __future__ import annotations

RAW_ALLOWED_SOURCES = {"codex_open_source", "codex_builtin", "desktop_bundled_builtin"}
SHAPE_ONLY_SOURCES = {"user_config", "project_doc", "tool_output", "browser_content", "screenshot", "unknown"}


def classify_source(source: str | None) -> str:
    if source in RAW_ALLOWED_SOURCES:
        return source
    if source in SHAPE_ONLY_SOURCES:
        return source
    return "unknown"


def content_policy_for_class(source_class: str) -> str:
    if source_class in RAW_ALLOWED_SOURCES:
        return "raw_allowed"
    return "shape_only"


def content_class_for_tool(tool_name: str, result_content_type: str | None = None) -> str:
    lowered = tool_name.lower()
    if "computer" in lowered or result_content_type == "image":
        return "screenshot"
    if "chrome" in lowered:
        return "browser_content"
    if "browser" in lowered:
        return "browser_content"
    if "shell" in lowered or "exec" in lowered or "command" in lowered:
        return "command_output"
    if "file" in lowered or "read" in lowered:
        return "file_content"
    if lowered.startswith("mcp__"):
        return "tool_output"
    return "unknown"


def redaction_reason(policy: str) -> str:
    if policy == "raw_allowed":
        return "desktop_builtin_content"
    if policy == "hash_only":
        return "non_json_or_malformed_payload"
    return "default_no_user_content"
