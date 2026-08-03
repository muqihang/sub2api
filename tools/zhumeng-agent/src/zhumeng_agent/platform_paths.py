from __future__ import annotations

import os
from pathlib import Path

from platformdirs import PlatformDirs


def app_dirs(app_name: str = "zhumeng-agent") -> PlatformDirs:
    return PlatformDirs(appname=app_name, appauthor=False)


def state_dir(app_name: str = "zhumeng-agent") -> Path:
    override = os.environ.get("ZHUMENG_AGENT_STATE_DIR")
    if override:
        return Path(override).expanduser()
    return Path(app_dirs(app_name).user_state_dir)


def config_dir(app_name: str = "zhumeng-agent") -> Path:
    override = os.environ.get("ZHUMENG_AGENT_CONFIG_DIR")
    if override:
        return Path(override).expanduser()
    return Path(app_dirs(app_name).user_config_dir)
