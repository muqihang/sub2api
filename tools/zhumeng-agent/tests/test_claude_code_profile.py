from __future__ import annotations

from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.profile import (
    CaptureMode,
    ClaudeCodeProfile,
    build_isolated_config_dir,
    build_safe_env,
    validate_loopback_guard_base_url,
)


def test_isolated_config_dir_never_uses_default_claude_home(tmp_path: Path):
    config_dir = build_isolated_config_dir(tmp_path, profile_id="prod/main")

    assert config_dir == tmp_path / "claude-code" / "prod-main" / "config"
    assert config_dir != Path.home() / ".claude"
    assert ".claude" not in config_dir.parts


def test_safe_env_scrubs_sensitive_inherited_values_and_uses_zhumeng_key(tmp_path: Path):
    inherited = {
        "PATH": "/usr/bin",
        "HOME": str(tmp_path / "home"),
        "SHELL": "/bin/zsh",
        "TERM": "xterm-256color",
        "LANG": "en_US.UTF-8",
        "LC_ALL": "en_US.UTF-8",
        "PWD": "/work/project",
        "ANTHROPIC_API_KEY": "real-anthropic-key",
        "ANTHROPIC_AUTH_TOKEN": "oauth-token",
        "ANTHROPIC_BASE_URL": "https://api.anthropic.com",
        "CLAUDE_CODE_API_BASE_URL": "https://api.anthropic.com",
        "CLAUDE_CONFIG_DIR": str(Path.home() / ".claude"),
        "ANTHROPIC_BETAS": "shell-beta",
        "CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS": "1",
        "ENABLE_TOOL_SEARCH": "false",
        "HTTP_PROXY": "http://proxy.example:8080",
        "HTTPS_PROXY": "http://proxy.example:8443",
        "ALL_PROXY": "socks5://proxy.example:1080",
        "NO_PROXY": "api.anthropic.com",
        "GITHUB_TOKEN": "ghp_secret",
        "SERVICE_API_KEY": "service-secret",
        "SESSION_COOKIE": "cookie-secret",
    }
    profile = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="zhumeng-entry-key",
        config_dir=tmp_path / "isolated" / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    env = build_safe_env(profile, inherited_env=inherited, project_cwd=Path("/work/project"))

    assert env["PATH"] == "/usr/bin"
    assert env["ANTHROPIC_API_KEY"] == "zhumeng-entry-key"
    assert env["ANTHROPIC_BASE_URL"] == "http://127.0.0.1:43117"
    assert env["CLAUDE_CODE_API_BASE_URL"] == "http://127.0.0.1:43117"
    assert env["CLAUDE_CONFIG_DIR"] == str(tmp_path / "isolated" / "config")
    assert env["ZHUMENG_CLAUDE_CAPTURE_MODE"] == "native_takeover_production"
    assert env["NO_PROXY"] == "127.0.0.1,localhost,::1"
    assert env["no_proxy"] == "127.0.0.1,localhost,::1"
    assert env["HTTP_PROXY"] == "http://127.0.0.1:43117"
    assert env["HTTPS_PROXY"] == "http://127.0.0.1:43117"
    assert "ALL_PROXY" not in env

    joined = "\n".join(f"{key}={value}" for key, value in env.items())
    assert "real-anthropic-key" not in joined
    assert "oauth-token" not in joined
    assert "https://api.anthropic.com" not in joined
    assert "shell-beta" not in joined
    assert "ghp_secret" not in joined
    assert "service-secret" not in joined
    assert "cookie-secret" not in joined
    assert str(Path.home() / ".claude") not in joined


@pytest.mark.parametrize(
    "base_url",
    [
        "https://api.anthropic.com",
        "http://api.anthropic.com",
        "http://192.168.1.20:8080",
        "http://0.0.0.0:8080",
        "http://example.local:8080",
    ],
)
def test_loopback_guard_base_url_rejects_non_loopback(base_url: str):
    with pytest.raises(ValueError, match="loopback"):
        validate_loopback_guard_base_url(base_url)


@pytest.mark.parametrize(
    "mode,expected",
    [
        (CaptureMode.PRODUCTION, "native_takeover_production"),
        (CaptureMode.STAGING, "native_takeover_staging"),
        (CaptureMode.LAB_EGRESS_GUARD, "egress_guard_lab"),
        (CaptureMode.LAB_MESSAGES_ONLY, "messages_only_lab"),
        (CaptureMode.SHADOW_TELEMETRY, "shadow_telemetry"),
    ],
)
def test_safe_env_supports_known_capture_modes(tmp_path: Path, mode: CaptureMode, expected: str):
    profile = ClaudeCodeProfile(
        profile_id="mode-test",
        guard_base_url="http://localhost:43117",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "config",
        capture_mode=mode,
    )

    env = build_safe_env(profile, inherited_env={})

    assert env["ZHUMENG_CLAUDE_CAPTURE_MODE"] == expected


def test_profile_repr_does_not_expose_entry_api_key(tmp_path: Path):
    profile = ClaudeCodeProfile(
        profile_id="prod",
        guard_base_url="http://127.0.0.1:43117",
        zhumeng_entry_api_key="secret-entry-key",
        config_dir=tmp_path / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    assert "secret-entry-key" not in repr(profile)


def test_cp3_static_anthropic_default_model_env_is_not_inherited(tmp_path):
    from zhumeng_agent.adapters.claude_code.profile import CaptureMode, ClaudeCodeProfile, build_safe_env  # noqa: PLC0415

    profile = ClaudeCodeProfile(
        profile_id="cp3",
        guard_base_url="http://127.0.0.1:19999",
        zhumeng_entry_api_key="entry-key",
        config_dir=tmp_path / "managed" / "claude-code" / "cp3" / "config",
        capture_mode=CaptureMode.PRODUCTION,
    )

    env = build_safe_env(
        profile,
        inherited_env={
            "PATH": "/bin",
            "ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-haiku-would-leak",
            "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-sonnet-would-leak",
            "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-opus-would-leak",
        },
    )

    assert env["PATH"] == "/bin"
    assert "ANTHROPIC_DEFAULT_HAIKU_MODEL" not in env
    assert "ANTHROPIC_DEFAULT_SONNET_MODEL" not in env
    assert "ANTHROPIC_DEFAULT_OPUS_MODEL" not in env
