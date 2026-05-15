from __future__ import annotations

import json
from pathlib import Path
from typing import Iterable


class JsonlTraceWriter:
    def __init__(self, path: Path):
        self.path = path

    def write(self, event: dict[str, object]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(event, sort_keys=True) + "\n")

    def safe_write(self, event: dict[str, object]) -> bool:
        try:
            self.write(event)
            return True
        except Exception:
            return False


def read_jsonl(path: Path) -> list[dict[str, object]]:
    if not path.exists():
        return []
    rows: list[dict[str, object]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        rows.append(json.loads(line))
    return rows


def write_jsonl(path: Path, rows: Iterable[dict[str, object]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as handle:
        for row in rows:
            handle.write(json.dumps(row, sort_keys=True) + "\n")


class BoundedCaptureQueue:
    def __init__(self, max_size: int):
        self.max_size = max_size
        self.items: list[dict[str, object]] = []
        self.dropped = 0

    def push(self, event: dict[str, object]) -> bool:
        if len(self.items) >= self.max_size:
            self.dropped += 1
            return False
        self.items.append(event)
        return True
