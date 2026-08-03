from __future__ import annotations

import json
import os
import tempfile
from pathlib import Path

from filelock import FileLock


class JsonStateStore:
    def __init__(self, path: Path):
        self.path = path
        self.lock = FileLock(str(path) + ".lock")

    def read(self) -> dict[str, object]:
        if not self.path.exists():
            return {}
        return json.loads(self.path.read_text(encoding="utf-8"))

    def write(self, payload: dict[str, object]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.lock:
            self._write_unlocked(payload)

    def update(self, patch: dict[str, object]) -> dict[str, object]:
        with self.lock:
            current = self.read()
            current.update(patch)
            self._write_unlocked(current)
            return current

    def delete(self) -> None:
        with self.lock:
            if self.path.exists():
                self.path.unlink()

    def _write_unlocked(self, payload: dict[str, object]) -> None:
        fd, temp_path = tempfile.mkstemp(prefix=self.path.name, suffix=".tmp", dir=str(self.path.parent))
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                json.dump(payload, handle, ensure_ascii=True, indent=2)
            if os.name == "posix":
                os.chmod(temp_path, 0o600)
            os.replace(temp_path, self.path)
        finally:
            if os.path.exists(temp_path):
                os.unlink(temp_path)


def logout_local_state(store: JsonStateStore) -> None:
    store.delete()


def ensure_revoke_device_ready(state: dict[str, object]) -> None:
    required = ("device_id", "server_base_url")
    missing = [key for key in required if not state.get(key)]
    if missing:
        raise ValueError(f"missing revoke context: {', '.join(missing)}")
