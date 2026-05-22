from __future__ import annotations

import json
import platform
import re
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
SENSITIVE_KEYS_RE = re.compile(r"(token|secret|password|api[_-]?key|cookie)", re.IGNORECASE)
EMAIL_RE = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}")
BEARER_RE = re.compile(r"Bearer\s+[A-Za-z0-9._~+/=-]+", re.IGNORECASE)
LOCAL_MANAGED_SECRET_RE = re.compile(r"zhumeng-local-managed-[A-Za-z0-9._~+/=-]+")
LONG_SECRET_RE = re.compile(r"(?<![A-Za-z0-9])[A-Za-z0-9_-]{20,}(?![A-Za-z0-9])")
TOKEN_WORD_RE = re.compile(r"\b(?:access|refresh)[-_]token[-_A-Za-z0-9]*\b|\bloopback[-_](?:secret|token)[-_A-Za-z0-9]*\b|\bmanaged[-_]session[-_]id[-_A-Za-z0-9]*\b", re.IGNORECASE)


def envelope(
    *,
    command: str,
    ok: bool,
    status: str,
    data: dict[str, Any] | None = None,
    warnings: list[dict[str, Any]] | None = None,
    error: dict[str, Any] | None = None,
) -> dict[str, Any]:
    payload = {
        "schema_version": SCHEMA_VERSION,
        "ok": ok,
        "command": command,
        "status": status,
        "data": redact(data or {}),
        "warnings": redact(warnings or []),
        "error": redact(error),
    }
    return payload


def emit_envelope(payload: dict[str, Any]) -> int:
    print(json.dumps(payload, ensure_ascii=True, separators=(",", ":")))
    return 0 if payload.get("ok") else 1


def error_envelope(command: str, code: str, message: str, *, status: str = "error") -> dict[str, Any]:
    return envelope(
        command=command,
        ok=False,
        status=status,
        error={"code": code, "message": message},
    )


def redact(value: Any) -> Any:
    if isinstance(value, dict):
        redacted: dict[str, Any] = {}
        for key, child in value.items():
            key_text = str(key)
            if SENSITIVE_KEYS_RE.search(key_text):
                redacted[key_text] = "<redacted>" if child else child
            else:
                redacted[key_text] = redact(child)
        return redacted
    if isinstance(value, list):
        return [redact(item) for item in value]
    if isinstance(value, tuple):
        return [redact(item) for item in value]
    if isinstance(value, Path):
        return redact_string(str(value))
    if isinstance(value, str):
        return redact_string(value)
    return value


def redact_string(text: str) -> str:
    redacted = text
    home = str(Path.home())
    if home and home != "/":
        redacted = redacted.replace(home, "<redacted_home>")
    node = platform.node()
    if node:
        redacted = redacted.replace(node, "<redacted_machine>")
    redacted = EMAIL_RE.sub("<redacted_email>", redacted)
    redacted = BEARER_RE.sub("Bearer <redacted>", redacted)
    redacted = LOCAL_MANAGED_SECRET_RE.sub("zhumeng-local-managed-<redacted>", redacted)
    redacted = TOKEN_WORD_RE.sub("<redacted_token>", redacted)
    return redacted
