from __future__ import annotations

import json
import os
import socket
import tempfile
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

import tomllib

from .detect import resolve_codex_home

COMMON_PROXY_PORTS = {1080, 1086, 7890, 7891, 7897, 8080, 9090}


@dataclass(slots=True)
class ConfigPlan:
    config_text: str
    auth_payload: dict[str, str]
    backup_paths: list[Path]


class CodexConfigManager:
    def __init__(self, codex_home: Path | None = None):
        self.codex_home = codex_home or resolve_codex_home()
        self.config_path = self.codex_home / "config.toml"
        self.auth_path = self.codex_home / "auth.json"
        self.backup_dir = self.codex_home / "backups"

    def read_state(self) -> dict[str, object]:
        return {
            "codex_home": str(self.codex_home),
            "config_exists": self.config_path.exists(),
            "auth_exists": self.auth_path.exists(),
        }

    def plan_configure(self, profile: dict[str, object], local_proxy_port: int, loopback_secret: str) -> ConfigPlan:
        provider = str(profile.get("model_provider", "zhumeng-managed"))
        config_text = (
            f'model_provider = "{provider}"\n\n'
            '[features]\n'
            'responses_websockets_v2 = true\n\n'
            f'[model_providers.{provider}]\n'
            'name = "openai"\n'
            f'base_url = "http://127.0.0.1:{local_proxy_port}/v1"\n'
            f'wire_api = "{profile.get("wire_api", "responses")}"\n'
            f'requires_openai_auth = {str(bool(profile.get("requires_openai_auth", True))).lower()}\n'
            f'supports_websockets = {str(bool(profile.get("supports_websockets", True))).lower()}\n'
        )
        auth_payload = {
            "OPENAI_API_KEY": f"zhumeng-local-managed-{loopback_secret}",
        }
        return ConfigPlan(config_text=config_text, auth_payload=auth_payload, backup_paths=[])

    def apply_configure(self, plan: ConfigPlan) -> None:
        self.codex_home.mkdir(parents=True, exist_ok=True)
        plan.backup_paths.extend(self._backup_existing())
        self._write_text_atomic(self.config_path, plan.config_text)
        self._write_json_atomic(self.auth_path, plan.auth_payload)

    def repair(self, profile: dict[str, object], local_proxy_port: int, loopback_secret: str) -> None:
        if self.config_path.exists():
            try:
                tomllib.loads(self.config_path.read_text(encoding="utf-8"))
            except tomllib.TOMLDecodeError:
                pass
        plan = self.plan_configure(profile, local_proxy_port, loopback_secret)
        self.apply_configure(plan)

    def restore_backup(self, backup_path: Path) -> None:
        target = self.config_path if backup_path.name.startswith("config.toml") else self.auth_path
        self._write_text_atomic(target, backup_path.read_text(encoding="utf-8"))

    def _backup_existing(self) -> list[Path]:
        self.backup_dir.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now(UTC).strftime("%Y%m%d%H%M%S")
        backups: list[Path] = []
        for source in (self.config_path,):
            if not source.exists():
                continue
            backup = self.backup_dir / f"{source.name}.{timestamp}.bak"
            backup.write_text(source.read_text(encoding="utf-8"), encoding="utf-8")
            backups.append(backup)
        return backups

    def _write_text_atomic(self, path: Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        fd, temp_path = tempfile.mkstemp(prefix=path.name, suffix=".tmp", dir=str(path.parent))
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(content)
            if os.name == "posix":
                os.chmod(temp_path, 0o600)
            os.replace(temp_path, path)
        finally:
            if os.path.exists(temp_path):
                os.unlink(temp_path)

    def _write_json_atomic(self, path: Path, payload: dict[str, str]) -> None:
        self._write_text_atomic(path, json.dumps(payload, ensure_ascii=True, indent=2))


def choose_local_proxy_port(preferred: int | None = None) -> int:
    if preferred and preferred not in COMMON_PROXY_PORTS and preferred > 0:
        return preferred

    while True:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        if port not in COMMON_PROXY_PORTS:
            return port
