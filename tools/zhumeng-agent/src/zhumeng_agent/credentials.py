from __future__ import annotations

import json
import os
from pathlib import Path


class FileCredentialStore:
    def __init__(self, path: Path):
        self.path = path

    def save(self, payload: dict[str, object]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.path.write_text(json.dumps(payload, ensure_ascii=True, indent=2), encoding="utf-8")
        if os.name == "posix":
            os.chmod(self.path, 0o600)

    def load(self) -> dict[str, object]:
        if not self.path.exists():
            return {}
        return json.loads(self.path.read_text(encoding="utf-8"))

    def delete(self) -> None:
        if self.path.exists():
            self.path.unlink()
