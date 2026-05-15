from __future__ import annotations

import os
from pathlib import Path


def resolve_codex_home() -> Path:
    env_value = os.environ.get("CODEX_HOME")
    if env_value:
        return Path(env_value).expanduser()
    return Path.home() / ".codex"
