from __future__ import annotations

import re
from dataclasses import dataclass, field
from enum import StrEnum
from pathlib import Path
from typing import Mapping
from urllib.parse import urlparse


class CaptureMode(StrEnum):
    LAB_MESSAGES_ONLY = "messages_only_lab"
    LAB_EGRESS_GUARD = "egress_guard_lab"
    STAGING = "native_takeover_staging"
    PRODUCTION = "native_takeover_production"
    SHADOW_TELEMETRY = "shadow_telemetry"


class ToolSearchMode(StrEnum):
    AUTO = "auto"
    TRUE = "true"
    STANDARD = "standard"


class FgtsMode(StrEnum):
    OBSERVE_ONLY = "observe_only"
    DISABLED = "disabled"
    ENABLED = "enabled"


@dataclass(frozen=True, slots=True)
class ClaudeCodeCapabilityProfile:
    profile_id: str
    claude_code_version_family: str
    persona_profile_id: str
    tool_search_mode: ToolSearchMode = ToolSearchMode.AUTO
    fgts_mode: FgtsMode = FgtsMode.OBSERVE_ONLY
    control_plane_policy_version: str = ""
    capture_level: str = "summary"
    netwatch_required: bool = True
    server_shape_healthcheck_version: str = ""
    tool_search_healthcheck_passed: bool = False
    kill_switches: tuple[str, ...] = field(default_factory=tuple)


@dataclass(frozen=True, slots=True)
class ClaudeCodeProfile:
    profile_id: str
    guard_base_url: str
    zhumeng_entry_api_key: str = field(repr=False)
    config_dir: Path
    capture_mode: CaptureMode = CaptureMode.PRODUCTION
    netwatch_interval: float = 2.0
    node_extra_ca_certs: Path | None = None
    enable_tool_search: str | None = None
    fgts_enabled: bool | None = None
    anthropic_betas: tuple[str, ...] = field(default_factory=tuple)
    disable_experimental_betas: bool | None = None
    capability_profile_id: str | None = None
    persona_profile_id: str | None = None
    control_plane_policy_version: str | None = None
    server_shape_healthcheck_version: str | None = None


_SAFE_PROFILE_SEGMENT_RE = re.compile(r"[^A-Za-z0-9_.-]+")
_INHERIT_EXACT = {"PATH", "HOME", "SHELL", "TERM"}
_MANAGED_NO_PROXY = "127.0.0.1,localhost,::1"


def build_isolated_config_dir(root: Path, *, profile_id: str) -> Path:
    safe_profile_id = safe_profile_segment(profile_id)
    config_dir = root.expanduser() / "claude-code" / safe_profile_id / "config"
    _validate_isolated_config_dir(config_dir)
    return config_dir


def safe_profile_segment(profile_id: str) -> str:
    return _SAFE_PROFILE_SEGMENT_RE.sub("-", profile_id).strip(".-_") or "default"


def validate_loopback_guard_base_url(base_url: str) -> str:
    parsed = urlparse(base_url)
    if parsed.scheme not in {"http", "https"}:
        raise ValueError("Claude Code guard base URL must use http(s) loopback")
    if parsed.username or parsed.password:
        raise ValueError("Claude Code guard base URL must not contain proxy credentials")
    host = parsed.hostname
    if host not in {"127.0.0.1", "localhost", "::1"}:
        raise ValueError("Claude Code guard base URL must point to loopback")
    if parsed.path not in {"", "/"} or parsed.params or parsed.query or parsed.fragment:
        raise ValueError("Claude Code guard base URL must be an origin without path/query")
    display_host = f"[{host}]" if ":" in host else host
    return f"{parsed.scheme}://{display_host}{f':{parsed.port}' if parsed.port else ''}"


def build_safe_env(
    profile: ClaudeCodeProfile,
    *,
    inherited_env: Mapping[str, str] | None = None,
    project_cwd: Path | None = None,
    include_ssh_auth_sock: bool = False,
) -> dict[str, str]:
    guard_base_url = validate_loopback_guard_base_url(profile.guard_base_url)
    _validate_isolated_config_dir(profile.config_dir)
    if not profile.zhumeng_entry_api_key:
        raise ValueError("Claude Code profile requires a zhumeng entry API key")

    inherited_env = inherited_env or {}
    env: dict[str, str] = {}
    for key, value in inherited_env.items():
        if _can_inherit_env_key(key, include_ssh_auth_sock=include_ssh_auth_sock):
            env[key] = value

    if project_cwd is not None:
        env["PWD"] = str(project_cwd)

    env.update(
        {
            "CLAUDE_CONFIG_DIR": str(profile.config_dir.expanduser()),
            "ANTHROPIC_BASE_URL": guard_base_url,
            "CLAUDE_CODE_API_BASE_URL": guard_base_url,
            "ANTHROPIC_API_KEY": profile.zhumeng_entry_api_key,
            "HTTP_PROXY": guard_base_url,
            "HTTPS_PROXY": guard_base_url,
            "NO_PROXY": _MANAGED_NO_PROXY,
            "no_proxy": _MANAGED_NO_PROXY,
            "ZHUMENG_CLAUDE_CAPTURE_MODE": profile.capture_mode.value,
            "ZHUMENG_NETWATCH_INTERVAL": str(profile.netwatch_interval),
        }
    )

    if profile.node_extra_ca_certs is not None:
        env["NODE_EXTRA_CA_CERTS"] = str(profile.node_extra_ca_certs.expanduser())
    tool_search_env_value = profile.enable_tool_search or "auto"
    env["ENABLE_TOOL_SEARCH"] = tool_search_env_value
    if profile.fgts_enabled is not None:
        env["CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING"] = "1" if profile.fgts_enabled else "0"
    if profile.anthropic_betas:
        env["ANTHROPIC_BETAS"] = ",".join(profile.anthropic_betas)
    if profile.disable_experimental_betas is not None:
        env["CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS"] = "1" if profile.disable_experimental_betas else "0"
    if profile.capability_profile_id is not None:
        env["ZHUMENG_CLAUDE_CAPABILITY_PROFILE_ID"] = profile.capability_profile_id
    if profile.persona_profile_id is not None:
        env["ZHUMENG_CLAUDE_PERSONA_PROFILE_ID"] = profile.persona_profile_id
    if profile.control_plane_policy_version is not None:
        env["ZHUMENG_CLAUDE_CONTROL_PLANE_POLICY_VERSION"] = profile.control_plane_policy_version
    if profile.server_shape_healthcheck_version is not None:
        env["ZHUMENG_CLAUDE_SHAPE_HEALTHCHECK_VERSION"] = profile.server_shape_healthcheck_version
    env["ZHUMENG_CLAUDE_TOOL_SEARCH_MODE"] = tool_search_env_value

    return env


def apply_capability_profile(
    profile: ClaudeCodeProfile,
    capability: ClaudeCodeCapabilityProfile,
    *,
    tool_search_env_value: str | None = None,
) -> ClaudeCodeProfile:
    enable_tool_search = tool_search_env_value or _toolsearch_env_value(capability)
    fgts_enabled = None
    if capability.fgts_mode == FgtsMode.ENABLED:
        fgts_enabled = True
    elif capability.fgts_mode == FgtsMode.DISABLED:
        fgts_enabled = False
    return ClaudeCodeProfile(
        profile_id=profile.profile_id,
        guard_base_url=profile.guard_base_url,
        zhumeng_entry_api_key=profile.zhumeng_entry_api_key,
        config_dir=profile.config_dir,
        capture_mode=profile.capture_mode,
        netwatch_interval=profile.netwatch_interval,
        node_extra_ca_certs=profile.node_extra_ca_certs,
        enable_tool_search=enable_tool_search,
        fgts_enabled=fgts_enabled,
        anthropic_betas=profile.anthropic_betas,
        disable_experimental_betas=profile.disable_experimental_betas,
        capability_profile_id=capability.profile_id,
        persona_profile_id=capability.persona_profile_id,
        control_plane_policy_version=capability.control_plane_policy_version,
        server_shape_healthcheck_version=capability.server_shape_healthcheck_version,
    )


def _toolsearch_env_value(capability: ClaudeCodeCapabilityProfile) -> str:
    if capability.tool_search_mode == ToolSearchMode.STANDARD:
        return "false"
    return "auto"


def _can_inherit_env_key(key: str, *, include_ssh_auth_sock: bool) -> bool:
    upper_key = key.upper()
    if _is_sensitive_env_key(upper_key):
        return False
    if upper_key in _INHERIT_EXACT or upper_key == "LANG":
        return True
    if upper_key == "PWD":
        return True
    if key.startswith("LC_"):
        return True
    if include_ssh_auth_sock and upper_key == "SSH_AUTH_SOCK":
        return True
    return False


def _is_sensitive_env_key(upper_key: str) -> bool:
    sensitive_markers = ("TOKEN", "API_KEY", "COOKIE", "SESSION")
    if any(marker in upper_key for marker in sensitive_markers):
        return True
    if "PROXY" in upper_key or "BASE_URL" in upper_key:
        return True
    return upper_key in {
        "ANTHROPIC_AUTH_TOKEN",
        "ANTHROPIC_BETAS",
        "CLAUDE_CONFIG_DIR",
        "CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS",
        "ENABLE_TOOL_SEARCH",
        "CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING",
    }


def _validate_isolated_config_dir(config_dir: Path) -> None:
    expanded = config_dir.expanduser()
    default_claude_dir = Path.home() / ".claude"
    try:
        if expanded == default_claude_dir or default_claude_dir in expanded.parents:
            raise ValueError("Claude Code config dir must be isolated from default ~/.claude")
    except RuntimeError:
        raise ValueError("Claude Code config dir must be isolated from default ~/.claude") from None
    if expanded.name == ".claude" or ".claude" in expanded.parts:
        raise ValueError("Claude Code config dir must not use a .claude path segment")
