from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import subprocess
import sys
import time
from contextlib import contextmanager
from dataclasses import dataclass, field
from enum import StrEnum
from pathlib import Path
from typing import Iterator, Mapping
from urllib.parse import urlparse

_MANAGED_NO_PROXY = "127.0.0.1,localhost,::1"
_OFFICIAL_HOST_MARKERS = ("anthropic.com", "claude.ai", "claude.com")
_SENSITIVE_ENV_MARKERS = ("TOKEN", "API_KEY", "COOKIE", "SESSION", "PROXY", "BASE_URL")
_CP0_OVERLAY_HASH = "sha256:" + hashlib.sha256(b"zhumeng-claude-runtime-overlay:cp0-native-only").hexdigest()
_UNKNOWN_HASH = "sha256:" + ("0" * 64)


class NativeGuardMode(StrEnum):
    LAB = "lab"
    STAGING = "staging"
    PRODUCTION = "production"


@dataclass(frozen=True, slots=True)
class NativeGuardConfig:
    mode: NativeGuardMode
    listen_port: int
    upstream_base: str
    sub2api_auth: str = field(repr=False)
    summary_path: Path
    repo_root: Path
    listen_host: str = "127.0.0.1"
    control_plane_intent_url: str | None = None
    control_plane_intent_auth: str | None = field(default=None, repr=False)
    capture_level: str = "summary"
    connect_mode: str | None = None
    max_messages: int = 0
    attestation_secret: str | None = field(default=None, repr=False)
    attestation_key_id: str = "guard_v1"
    hmac_key: str | None = field(default=None, repr=False)
    hmac_key_id: str = "local_guard_v1"
    allow_nonloopback_upstream: bool = False
    managed_session_id: str | None = field(default=None, repr=False)
    device_id: int | None = None
    agent_version: str = "0.1.0"
    route_hint_secret: str | None = field(default=None, repr=False)
    route_hint_catalog_version: str = "cp4-cli-fixture-v1"
    runtime_hash: str | None = None
    overlay_hash: str | None = None
    bridge_live_models: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if self.mode not in set(NativeGuardMode):
            raise ValueError("unsupported Claude Code native guard mode")
        if self.listen_host not in {"127.0.0.1", "localhost", "::1"}:
            raise ValueError("native guard must listen on loopback")
        if self.listen_port <= 0 or self.listen_port > 65535:
            raise ValueError("native guard listen_port must be a valid TCP port")
        object.__setattr__(
            self,
            "upstream_base",
            _validate_loopback_origin(
                self.upstream_base,
                field_name="upstream_base",
                allow_nonloopback=self.allow_nonloopback_upstream,
            ),
        )
        if self.control_plane_intent_url is not None:
            object.__setattr__(
                self,
                "control_plane_intent_url",
                _validate_loopback_origin(self.control_plane_intent_url, field_name="control_plane_intent_url", allow_path=True),
            )
        if self.capture_level not in {"summary", "deep"}:
            raise ValueError("native guard capture_level must be summary or deep")


@dataclass(frozen=True, slots=True)
class NativeGuardPlan:
    command: list[str]
    env: dict[str, str] = field(repr=False)
    cwd: Path
    config: NativeGuardConfig
    will_start_process: bool = False


@dataclass(frozen=True, slots=True)
class RunningNativeGuard:
    process: subprocess.Popen[str]
    ready: dict[str, object]


def build_native_guard_plan(
    config: NativeGuardConfig,
    *,
    inherited_env: Mapping[str, str] | None = None,
    python_executable: Path | str | None = None,
) -> NativeGuardPlan:
    if not config.attestation_secret:
        raise ValueError("native guard attestation_secret is required")
    env = _build_guard_env(config, inherited_env=inherited_env)
    python_bin = str(python_executable or sys.executable)
    command = [
        python_bin,
        str(config.repo_root / "tools" / "cli_control_plane_guard.py"),
        "--listen-host",
        config.listen_host,
        "--listen-port",
        str(config.listen_port),
        "--upstream-base",
        config.upstream_base,
        "--sub2api-auth-env",
        "ZHUMENG_CLAUDE_NATIVE_SUB2API_AUTH",
        "--native-attestation",
        "--summary-path",
        str(config.summary_path),
        "--connect-mode",
        config.connect_mode or _connect_mode_for(config.mode),
        "--capture-level",
        config.capture_level,
        "--max-messages",
        str(config.max_messages),
        "--cost-max-tokens",
        "200000",
        "--cost-max-body-bytes",
        str(50 * 1024 * 1024),
        "--cost-max-tools",
        "512",
        "--cost-max-messages",
        "2048",
        "--cost-max-content-blocks",
        "8192",
        "--cost-max-text-bytes",
        str(32 * 1024 * 1024),
        "--cost-max-system-bytes",
        str(8 * 1024 * 1024),
        "--cost-max-tool-def-bytes",
        str(16 * 1024 * 1024),
        "--cost-max-thinking-budget-tokens",
        "200000",
        "--cost-allow-stream",
        "--cost-allow-thinking",
        "--cost-allow-assistant-messages",
        "--cost-allow-tool-content",
    ]
    if config.allow_nonloopback_upstream:
        command.append("--allow-nonloopback-upstream")
    if config.managed_session_id:
        command.extend(["--managed-session", config.managed_session_id])
    if config.device_id is not None:
        command.extend(["--device-id", str(config.device_id)])
    if config.agent_version:
        command.extend(["--agent-version", config.agent_version])
    if config.control_plane_intent_url is not None:
        command.extend(["--control-plane-intent-url", config.control_plane_intent_url])
    if config.route_hint_secret:
        command.extend(["--route-hint-secret-env", "ZHUMENG_CLAUDE_ROUTE_HINT_SECRET"])
        command.extend(["--route-hint-catalog-version", config.route_hint_catalog_version])
    return NativeGuardPlan(command=command, env=env, cwd=config.repo_root, config=config)


@contextmanager
def start_native_guard(plan: NativeGuardPlan, *, ready_timeout_seconds: float = 10.0) -> Iterator[RunningNativeGuard]:
    command = list(plan.command)
    env = dict(plan.env)
    process = subprocess.Popen(
        command,
        cwd=str(plan.cwd),
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        ready = _read_guard_ready(process, timeout_seconds=ready_timeout_seconds)
        yield RunningNativeGuard(process=process, ready=ready)
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)


def _build_guard_env(config: NativeGuardConfig, *, inherited_env: Mapping[str, str] | None) -> dict[str, str]:
    env: dict[str, str] = {}
    for key, value in (inherited_env or {}).items():
        if _can_inherit_env_key(key):
            env[key] = value
    env["PYTHONPATH"] = _prepend_pythonpath(str(config.repo_root), env.get("PYTHONPATH"))
    env["NO_PROXY"] = _MANAGED_NO_PROXY
    env["no_proxy"] = _MANAGED_NO_PROXY
    env["ZHUMENG_CLAUDE_NATIVE_SUB2API_AUTH"] = config.sub2api_auth
    env["ZHUMENG_CLAUDE_RUNTIME_HASH"] = config.runtime_hash or _sha256_file(config.repo_root / "tools" / "cli_control_plane_guard.py")
    env["ZHUMENG_CLAUDE_OVERLAY_HASH"] = config.overlay_hash or _CP0_OVERLAY_HASH
    env["ZHUMENG_CLAUDE_CATALOG_HASH"] = _route_catalog_content_hash(config.repo_root, config.route_hint_catalog_version, config.bridge_live_models)
    if config.bridge_live_models:
        env["ZHUMENG_CLAUDE_BRIDGE_LIVE_MODELS"] = ",".join(config.bridge_live_models)
    if config.control_plane_intent_auth is not None:
        env["SUB2API_CONTROL_PLANE_INTENT_TOKEN"] = config.control_plane_intent_auth
    if config.attestation_secret is not None:
        env["SUB2API_CONTROL_PLANE_ATTESTATION_SECRET"] = config.attestation_secret
        env["SUB2API_CONTROL_PLANE_ATTESTATION_CURRENT_KEY_ID"] = config.attestation_key_id
        env["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET"] = config.attestation_secret
        env["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID"] = config.attestation_key_id
    if config.route_hint_secret is not None:
        env["ZHUMENG_CLAUDE_ROUTE_HINT_SECRET"] = config.route_hint_secret
    if config.hmac_key is not None:
        env["SUB2API_CONTROL_PLANE_HMAC_KEY"] = config.hmac_key
        env["SUB2API_CONTROL_PLANE_HMAC_KEY_ID"] = config.hmac_key_id
    return env


def _can_inherit_env_key(key: str) -> bool:
    upper_key = key.upper()
    if upper_key in {"PATH", "HOME", "SHELL", "TERM", "LANG"} or key.startswith("LC_"):
        return True
    if upper_key == "PYTHONPATH":
        return True
    if any(marker in upper_key for marker in _SENSITIVE_ENV_MARKERS):
        return False
    return False


def _prepend_pythonpath(repo_root: str, current: str | None) -> str:
    if not current:
        return repo_root
    parts = current.split(os.pathsep)
    if repo_root in parts:
        return current
    return repo_root + os.pathsep + current


def _connect_mode_for(mode: NativeGuardMode) -> str:
    return "block"


def _validate_loopback_origin(url: str, *, field_name: str, allow_path: bool = False, allow_nonloopback: bool = False) -> str:
    parsed = urlparse(url)
    if parsed.scheme not in {"http", "https"}:
        raise ValueError(f"{field_name} must use http(s)")
    if parsed.username or parsed.password:
        raise ValueError(f"{field_name} must not contain credentials")
    host = parsed.hostname
    if not host:
        raise ValueError(f"{field_name} must include host")
    lowered = host.lower()
    if any(marker in lowered for marker in _OFFICIAL_HOST_MARKERS):
        raise ValueError(f"{field_name} must not target official Claude/Anthropic hosts")
    if lowered not in {"127.0.0.1", "localhost", "::1"}:
        if allow_nonloopback:
            return url
        raise ValueError(f"{field_name} must target loopback only")
    if not allow_path and (parsed.path not in {"", "/"} or parsed.params or parsed.query or parsed.fragment):
        raise ValueError(f"{field_name} must be an origin without path/query")
    if allow_path and (parsed.params or parsed.query or parsed.fragment):
        raise ValueError(f"{field_name} must not include params/query/fragment")
    return url


def _read_guard_ready(process: subprocess.Popen[str], *, timeout_seconds: float) -> dict[str, object]:
    assert process.stdout is not None
    deadline = time.time() + timeout_seconds
    lines: list[str] = []
    while time.time() < deadline:
        line = process.stdout.readline()
        if line:
            lines.append(line.rstrip("\n"))
            try:
                return json.loads(line)
            except json.JSONDecodeError:
                continue
        if process.poll() is not None:
            break
        time.sleep(0.05)
    stderr = ""
    if process.stderr is not None:
        try:
            stderr = process.stderr.read()
        except OSError:
            stderr = ""
    raise RuntimeError(f"native guard did not become ready; stdout={lines!r}; stderr={stderr[:1000]}")


def _sha256_file(path: Path) -> str:
    try:
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
    except OSError:
        return _UNKNOWN_HASH
    return "sha256:" + digest


def _route_catalog_content_hash(repo_root: Path, catalog_version: str, bridge_live_models: tuple[str, ...] = ()) -> str:
    route_trust = _load_route_trust_module(repo_root)
    catalog = route_trust.cp4_fixture_route_catalog(
        runtime_hash=_UNKNOWN_HASH,
        overlay_hash=_UNKNOWN_HASH,
        catalog_hash=_UNKNOWN_HASH,
        catalog_version=catalog_version,
        bridge_live_models=bridge_live_models,
    )
    return str(route_trust.route_catalog_content_hash(catalog))


def _load_route_trust_module(repo_root: Path):
    module_path = repo_root / "tools" / "claude_code_route_trust.py"
    spec = importlib.util.spec_from_file_location("zhumeng_claude_code_route_trust", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load Claude Code route trust module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module
