from __future__ import annotations

from pathlib import Path


def renderer_script_path() -> Path:
    return Path(__file__).resolve().parents[2] / "inject" / "renderer_inject.js"
