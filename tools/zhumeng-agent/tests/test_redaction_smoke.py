from __future__ import annotations

from zhumeng_agent.security import redact_text


def test_redaction_smoke_blocks_representative_secrets():
    sample = """
Authorization: Bearer managed-access-token
refresh_token=refresh-secret
device_secret=device-secret
https://example.com/setup?code=grant-code&token=query-token
sk-1234567890abcdef
"""
    redacted = redact_text(sample)
    for secret in (
        "managed-access-token",
        "refresh-secret",
        "device-secret",
        "grant-code",
        "query-token",
        "sk-1234567890abcdef",
    ):
        assert secret not in redacted
