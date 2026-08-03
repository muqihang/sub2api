from __future__ import annotations

import hashlib
import hmac
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any


RAW_UNLOCK_ENV = "ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK"
RAW_UNLOCK_VALUE = "I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_DESKTOP_PROTOCOL_PAYLOADS"


class CaptureConfigError(ValueError):
    pass


@dataclass
class CodexDesktopCaptureConfig:
    enabled: bool
    level: str
    raw_payloads: bool
    base_dir: Path
    retention_days: int
    hash_mode: str
    correlation_hash_key_file: Path | None
    production_like: bool = False
    raw_unlock_env: str = RAW_UNLOCK_ENV

    @classmethod
    def defaults(
        cls,
        *,
        enabled: bool = False,
        level: str = "summary",
        raw_payloads: bool = False,
        base_dir: Path | None = None,
        retention_days: int = 3,
        correlation_hash_key_file: Path | None = None,
        production_like: bool = False,
    ) -> "CodexDesktopCaptureConfig":
        return cls(
            enabled=enabled,
            level=level,
            raw_payloads=raw_payloads,
            base_dir=base_dir or Path.home() / ".zhumeng-agent" / "codex-desktop-captures",
            retention_days=retention_days,
            hash_mode="hmac_sha256",
            correlation_hash_key_file=correlation_hash_key_file,
            production_like=production_like,
            raw_unlock_env=RAW_UNLOCK_ENV,
        )

    def validate(self) -> None:
        if self.level not in {"summary", "detailed"}:
            raise CaptureConfigError(f"unsupported capture level: {self.level}")
        if self.raw_payloads and os.environ.get(self.raw_unlock_env) != RAW_UNLOCK_VALUE:
            raise CaptureConfigError(
                "raw payload capture is disabled by default; set "
                f"{self.raw_unlock_env}={RAW_UNLOCK_VALUE} only for local diagnostics"
            )
        if self.raw_payloads and self.production_like:
            raise CaptureConfigError("raw payload capture is not allowed in production-like mode")

    def public_dict(self) -> dict[str, object]:
        return {
            "enabled": self.enabled,
            "level": self.level,
            "raw_payloads": self.raw_payloads,
            "base_dir_hash": CorrelationHasher.from_key_file(self.correlation_hash_key_file).hash_identifier(str(self.base_dir)),
            "retention_days": self.retention_days,
            "hash_mode": self.hash_mode,
            "correlation_hash_key_file": "set" if self.correlation_hash_key_file else "unset",
            "production_like": self.production_like,
            "raw_unlock_env": self.raw_unlock_env,
        }


class CorrelationHasher:
    def __init__(self, key: bytes | None):
        self.key = key

    @classmethod
    def from_key_file(cls, key_file: Path | None) -> "CorrelationHasher":
        if key_file is None:
            return cls(None)
        try:
            data = key_file.read_bytes()
        except FileNotFoundError:
            return cls(None)
        return cls(data or None)

    def hash_identifier(self, value: object) -> str:
        data = str(value).encode("utf-8")
        if self.key:
            return "hmac-sha256:" + hmac.new(self.key, data, hashlib.sha256).hexdigest()
        return "sha256:" + hashlib.sha256(data).hexdigest()

    def hash_bytes(self, data: bytes) -> str:
        if self.key:
            return "hmac-sha256:" + hmac.new(self.key, data, hashlib.sha256).hexdigest()
        return "sha256:" + hashlib.sha256(data).hexdigest()

    @property
    def confidence_mode(self) -> str:
        return "shared_key" if self.key else "no_key"


def path_kind(path: Path) -> str:
    expanded = path.expanduser()
    text = str(expanded)
    home = str(Path.home())
    if text.startswith("/Applications/") or text == "/Applications":
        return "system_applications"
    if home and text.startswith(home + "/"):
        return "user_home"
    return "custom"


def path_identity(path: Path, hasher: CorrelationHasher) -> dict[str, Any]:
    return {
        "desktop_app_path_hash": hasher.hash_identifier(str(path)),
        "desktop_app_path_kind": path_kind(path),
        "desktop_app_basename_hash": hasher.hash_identifier(path.name),
    }


def file_hash(path: Path, hasher: CorrelationHasher) -> str | None:
    if not path.exists() or not path.is_file():
        return None
    return hasher.hash_bytes(path.read_bytes())
