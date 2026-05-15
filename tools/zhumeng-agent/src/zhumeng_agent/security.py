from __future__ import annotations

import re
import secrets


SENSITIVE_PATTERNS = [
    (re.compile(r"(Authorization:\s*Bearer\s+)([^\s]+)", re.IGNORECASE), r"\1[REDACTED]"),
    (re.compile(r"([?&](?:code|token)=)([^&\s]+)", re.IGNORECASE), r"\1[REDACTED]"),
    (re.compile(r"((?:refresh_token|device_secret|managed_session_id)\s*[:=]\s*)([^\s,]+)", re.IGNORECASE), r"\1[REDACTED]"),
    (re.compile(r"sk-[A-Za-z0-9_-]+"), "sk-[REDACTED]"),
]


def generate_loopback_secret(byte_length: int = 24) -> str:
    return secrets.token_hex(byte_length)


def redact_text(text: str) -> str:
    redacted = text
    for pattern, replacement in SENSITIVE_PATTERNS:
        redacted = pattern.sub(replacement, redacted)
    return redacted
