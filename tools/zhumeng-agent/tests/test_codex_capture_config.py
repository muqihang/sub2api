import os
from pathlib import Path

import pytest

from zhumeng_agent.adapters.codex.capture_config import (
    RAW_UNLOCK_VALUE,
    CaptureConfigError,
    CodexDesktopCaptureConfig,
    CorrelationHasher,
    path_identity,
)


def test_capture_config_defaults_are_shape_only(monkeypatch: pytest.MonkeyPatch, tmp_path: Path):
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.delenv("ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK", raising=False)

    config = CodexDesktopCaptureConfig.defaults()

    assert config.enabled is False
    assert config.level == "summary"
    assert config.raw_payloads is False
    assert config.retention_days == 3
    assert config.hash_mode == "hmac_sha256"
    assert config.correlation_hash_key_file is None
    assert config.raw_unlock_env == "ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK"
    assert str(config.base_dir).endswith(".zhumeng-agent/codex-desktop-captures")


def test_raw_payloads_require_explicit_unlock(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK", raising=False)
    config = CodexDesktopCaptureConfig.defaults(raw_payloads=True)

    with pytest.raises(CaptureConfigError, match="raw payload capture is disabled"):
        config.validate()

    monkeypatch.setenv("ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK", RAW_UNLOCK_VALUE)
    config.validate()


def test_raw_payloads_refuse_production_like_mode_even_with_unlock(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK", RAW_UNLOCK_VALUE)
    config = CodexDesktopCaptureConfig.defaults(raw_payloads=True, production_like=True)

    with pytest.raises(CaptureConfigError, match="production-like"):
        config.validate()


def test_correlation_hasher_uses_shared_key_without_leaking_raw_ids(tmp_path: Path):
    key_file = tmp_path / "correlation.key"
    key_file.write_bytes(b"shared-secret")
    hasher = CorrelationHasher.from_key_file(key_file)

    value = hasher.hash_identifier("session-raw-id")

    assert value.startswith("hmac-sha256:")
    assert "session-raw-id" not in value
    assert value == CorrelationHasher.from_key_file(key_file).hash_identifier("session-raw-id")
    assert value != CorrelationHasher.from_key_file(None).hash_identifier("session-raw-id")


def test_path_identity_never_returns_raw_host_path(tmp_path: Path):
    key_file = tmp_path / "correlation.key"
    key_file.write_bytes(b"shared-secret")
    app_path = Path("/Applications/Codex.app")

    identity = path_identity(app_path, CorrelationHasher.from_key_file(key_file))

    assert identity["desktop_app_path_kind"] == "system_applications"
    assert identity["desktop_app_path_hash"].startswith("hmac-sha256:")
    assert identity["desktop_app_basename_hash"].startswith("hmac-sha256:")
    assert "/Applications" not in str(identity)
    assert "Codex.app" not in identity["desktop_app_basename_hash"]
