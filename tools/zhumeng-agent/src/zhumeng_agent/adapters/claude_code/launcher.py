from __future__ import annotations

import importlib.util
import json
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping, Sequence

from .guard import NativeGuardConfig, NativeGuardMode, NativeGuardPlan, build_native_guard_plan, start_native_guard
from .profile import CaptureMode, ClaudeCodeProfile, build_isolated_config_dir, build_safe_env, safe_profile_segment

Runner = Callable[..., object]
_VERSION_RE = re.compile(r"(?:claude(?:-code)?[/ ]+v?|Claude Code\s+v?)(\d+(?:\.\d+){1,3})", re.IGNORECASE)
_FALLBACK_VERSION_RE = re.compile(r"\bv?(\d+\.\d+(?:\.\d+){0,2})\b")
_ROUTE_HINT_PRELOAD_NAME = "route-hint-preload.cjs"
_ROUTE_HINT_CATALOG_NAME = "route-hint-catalog.json"


@dataclass(frozen=True, slots=True)
class ClaudeCodeVersion:
    executable: Path
    version: str | None
    raw_output: str
    returncode: int = 0


@dataclass(frozen=True, slots=True)
class ClaudeCodeLaunchPlan:
    command: list[str]
    env: dict[str, str] = field(repr=False)
    cwd: Path | None
    profile: ClaudeCodeProfile
    will_start_process: bool = False


@dataclass(frozen=True, slots=True)
class ManagedClaudeCodeRunResult:
    returncode: int
    guard_ready: dict[str, object]
    guard_plan: NativeGuardPlan
    launch_plan: ClaudeCodeLaunchPlan


def detect_claude_code_version(
    *,
    executable: Path | str = "claude",
    runner: Runner | None = None,
    timeout_seconds: float = 3.0,
    env: Mapping[str, str] | None = None,
) -> ClaudeCodeVersion:
    executable_path = Path(executable)
    runner = runner or subprocess.run
    kwargs: dict[str, object] = {
        "capture_output": True,
        "text": True,
        "check": False,
        "timeout": timeout_seconds,
    }
    if env is not None:
        kwargs["env"] = dict(env)
    result = runner([str(executable_path), "--version"], **kwargs)
    stdout = str(getattr(result, "stdout", "") or "")
    stderr = str(getattr(result, "stderr", "") or "")
    raw_output = (stdout or stderr).strip()
    return ClaudeCodeVersion(
        executable=executable_path,
        version=_parse_claude_code_version(raw_output),
        raw_output=raw_output,
        returncode=int(getattr(result, "returncode", 0) or 0),
    )


def build_claude_code_launch_plan(
    *,
    executable: Path | str,
    profile: ClaudeCodeProfile,
    inherited_env: Mapping[str, str] | None = None,
    project_cwd: Path | None = None,
    argv: Sequence[str] | None = None,
) -> ClaudeCodeLaunchPlan:
    env = build_safe_env(profile, inherited_env=inherited_env, project_cwd=project_cwd)
    command = [str(Path(executable)), *(argv or [])]
    return ClaudeCodeLaunchPlan(
        command=command,
        env=env,
        cwd=project_cwd,
        profile=profile,
        will_start_process=False,
    )


def _parse_claude_code_version(raw_output: str) -> str | None:
    match = _VERSION_RE.search(raw_output) or _FALLBACK_VERSION_RE.search(raw_output)
    return match.group(1) if match else None


def run_managed_claude_code(
    *,
    executable: Path | str,
    repo_root: Path,
    upstream_base: str,
    sub2api_auth: str,
    attestation_secret: str,
    native_managed_access_token: str | None = None,
    route_hint_secret: str | None = None,
    route_hint_catalog_version: str = "cp4-cli-fixture-v1",
    managed_session_id: str | None = None,
    device_id: int | None = None,
    runtime_hash: str | None = None,
    overlay_hash: str | None = None,
    bridge_live_models: tuple[str, ...] = (),
    config_root: Path,
    project_cwd: Path | None = None,
    guard_listen_port: int,
    argv: Sequence[str] | None = None,
    inherited_env: Mapping[str, str] | None = None,
    profile_id: str = "prod",
    mode: NativeGuardMode = NativeGuardMode.PRODUCTION,
    start_guard=start_native_guard,
    process_runner=None,
    ready_timeout_seconds: float = 10.0,
) -> ManagedClaudeCodeRunResult:
    if not str(route_hint_secret or "").strip():
        raise ValueError("managed Claude Code runtime requires route_hint_secret for CP4 routing trust contract")
    config_root = config_root.expanduser()
    safe_profile_id = safe_profile_segment(profile_id)
    summary_path = config_root / "claude-code" / safe_profile_id / "native-guard-summary.jsonl"
    summary_path.parent.mkdir(parents=True, exist_ok=True)
    cert_path, key_path = _ensure_control_plane_stub_cert(config_root, profile_id=safe_profile_id)
    guard_config = NativeGuardConfig(
        mode=mode,
        listen_port=guard_listen_port,
        upstream_base=upstream_base,
        sub2api_auth=sub2api_auth,
        native_managed_access_token=native_managed_access_token,
        native_managed_state_path=config_root / "state.json",
        summary_path=summary_path,
        repo_root=repo_root,
        connect_mode="stub",
        cert_path=cert_path,
        key_path=key_path,
        attestation_secret=attestation_secret,
        route_hint_secret=route_hint_secret,
        route_hint_catalog_version=route_hint_catalog_version,
        allow_nonloopback_upstream=True,
        managed_session_id=managed_session_id,
        device_id=device_id,
        runtime_hash=runtime_hash,
        overlay_hash=overlay_hash,
        bridge_live_models=tuple(bridge_live_models),
    )
    guard_plan = build_native_guard_plan(guard_config, inherited_env=inherited_env)
    with start_guard(guard_plan, ready_timeout_seconds=ready_timeout_seconds) as guard:
        guard_base_url = str(guard.ready["listen"])
        profile = ClaudeCodeProfile(
            profile_id=profile_id,
            guard_base_url=guard_base_url,
            zhumeng_entry_api_key=sub2api_auth,
            config_dir=build_isolated_config_dir(config_root, profile_id=safe_profile_id),
            capture_mode=CaptureMode.PRODUCTION,
            node_extra_ca_certs=cert_path,
        )
        launch_plan = build_claude_code_launch_plan(
            executable=executable,
            profile=profile,
            inherited_env=inherited_env,
            project_cwd=project_cwd,
            argv=argv,
        )
        route_hint_env = _write_route_hint_preload_artifacts(
            config_root=config_root,
            profile_id=safe_profile_id,
            guard_plan=guard_plan,
            route_hint_secret=route_hint_secret,
        )
        launch_plan = ClaudeCodeLaunchPlan(
            command=launch_plan.command,
            env={**launch_plan.env, **route_hint_env},
            cwd=launch_plan.cwd,
            profile=launch_plan.profile,
            will_start_process=True,
        )
        runner = process_runner or _default_process_runner
        returncode = int(runner(
            launch_plan.command,
            env=launch_plan.env,
            cwd=str(launch_plan.cwd) if launch_plan.cwd is not None else None,
        ))
        return ManagedClaudeCodeRunResult(
            returncode=returncode,
            guard_ready=dict(guard.ready),
            guard_plan=guard_plan,
            launch_plan=launch_plan,
        )


def _default_process_runner(command: list[str], *, env: Mapping[str, str], cwd: str | None) -> int:
    return subprocess.call(command, env=dict(env), cwd=cwd)


def _ensure_control_plane_stub_cert(config_root: Path, *, profile_id: str) -> tuple[Path, Path]:
    cert_dir = config_root / "claude-code" / profile_id / "certs"
    cert_path = cert_dir / "control-plane-stub-ca.pem"
    key_path = cert_dir / "control-plane-stub-ca.key"
    if cert_path.exists() and key_path.exists():
        return cert_path, key_path
    cert_dir.mkdir(parents=True, exist_ok=True)
    openssl_config = cert_dir / "control-plane-stub-openssl.cnf"
    openssl_config.write_text(
        """
[req]
default_bits = 2048
prompt = no
distinguished_name = dn
x509_extensions = v3_req

[dn]
CN = Zhumeng Claude Code Control Plane Guard

[v3_req]
subjectAltName = @alt_names
basicConstraints = critical, CA:TRUE, pathlen:0
keyUsage = critical, digitalSignature, keyEncipherment, keyCertSign, cRLSign
extendedKeyUsage = serverAuth

[alt_names]
DNS.1 = api.anthropic.com
DNS.2 = platform.claude.com
DNS.3 = claude.ai
DNS.4 = claude.com
DNS.5 = mcp-proxy.anthropic.com
""".lstrip(),
        encoding="utf-8",
    )
    subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-sha256",
            "-nodes",
            "-days",
            "7",
            "-keyout",
            str(key_path),
            "-out",
            str(cert_path),
            "-config",
            str(openssl_config),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        key_path.chmod(0o600)
        openssl_config.chmod(0o600)
        cert_path.chmod(0o644)
    except OSError:
        pass
    return cert_path, key_path


def _write_route_hint_preload_artifacts(
    *,
    config_root: Path,
    profile_id: str,
    guard_plan: NativeGuardPlan,
    route_hint_secret: str,
) -> dict[str, str]:
    overlay_dir = config_root / "claude-code" / profile_id / "overlay" / "cp4-route-hint"
    overlay_dir.mkdir(parents=True, exist_ok=True)
    catalog_path = overlay_dir / _ROUTE_HINT_CATALOG_NAME
    preload_path = overlay_dir / _ROUTE_HINT_PRELOAD_NAME
    catalog_payload = _route_hint_catalog_payload(guard_plan)
    catalog_path.write_text(json.dumps(catalog_payload, ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
    preload_path.write_text(_ROUTE_HINT_PRELOAD_JS, encoding="utf-8")
    node_options = _prepend_node_require("", preload_path)
    bun_options = _prepend_bun_preload("", preload_path)
    return {
        # Claude Code 2.1.177 is a Bun-packed binary, so NODE_OPTIONS alone is
        # ignored. Keep both forms: Node for test/future runtimes, Bun for live.
        "BUN_OPTIONS": bun_options,
        "NODE_OPTIONS": node_options,
        "ZHUMENG_CLAUDE_ROUTE_HINT_PRELOAD": "enabled",
        "ZHUMENG_CLAUDE_ROUTE_HINT_SECRET": route_hint_secret,
        "ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH": str(catalog_path),
        "ZHUMENG_CLAUDE_ROUTE_HINT_PRELOAD_PATH": str(preload_path),
    }


def _route_hint_catalog_payload(guard_plan: NativeGuardPlan) -> dict[str, object]:
    route_trust = _load_route_trust_module(guard_plan.config.repo_root)
    catalog = route_trust.cp4_fixture_route_catalog(
        runtime_hash=guard_plan.env["ZHUMENG_CLAUDE_RUNTIME_HASH"],
        overlay_hash=guard_plan.env["ZHUMENG_CLAUDE_OVERLAY_HASH"],
        catalog_hash=guard_plan.env["ZHUMENG_CLAUDE_CATALOG_HASH"],
        catalog_version=guard_plan.config.route_hint_catalog_version,
        bridge_live_models=tuple(guard_plan.config.bridge_live_models),
    )
    expected_catalog_hash = route_trust.route_catalog_content_hash(catalog)
    if catalog.catalog_hash != expected_catalog_hash:
        raise RuntimeError("managed Claude Code route hint catalog hash mismatch")
    entries: dict[str, dict[str, object]] = {}
    for model_id, entry in catalog.entries.items():
        normalized = {
            "model_id": entry.model_id,
            "provider": entry.provider,
            "route": entry.route,
            "client_type": entry.client_type,
            "live_enabled": entry.live_enabled,
            "formal_pool_allowed": entry.formal_pool_allowed,
            "native_attestation_allowed": entry.native_attestation_allowed,
            "provider_owner": entry.provider_owner,
            "credential_scope": entry.credential_scope,
            "gateway_location": entry.gateway_location,
        }
        entries[model_id] = normalized
    return {
        "schema_version": "cp4-route-hint-preload-v1",
        "runtime_hash": catalog.runtime_hash,
        "overlay_hash": catalog.overlay_hash,
        "catalog_hash": catalog.catalog_hash,
        "catalog_version": catalog.catalog_version,
        "entries": entries,
    }


def _load_route_trust_module(repo_root: Path):
    module_path = repo_root / "tools" / "claude_code_route_trust.py"
    spec = importlib.util.spec_from_file_location("zhumeng_launcher_route_trust", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load Claude Code route trust module")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _prepend_node_require(existing_node_options: str, preload_path: Path) -> str:
    require_arg = f"--require={_quote_node_options_value(str(preload_path))}"
    existing = str(existing_node_options or "").strip()
    return f"{require_arg} {existing}".strip()


def _prepend_bun_preload(existing_bun_options: str, preload_path: Path) -> str:
    # Bun's option parser does not accept --preload="/path with spaces"; it
    # does accept a separate --preload argument with shell-style escaped spaces.
    preload_arg = f"--preload {_escape_bun_options_value(str(preload_path))}"
    existing = str(existing_bun_options or "").strip()
    return f"{preload_arg} {existing}".strip()


def _quote_node_options_value(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def _escape_bun_options_value(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace(" ", "\\ ")
    return escaped


_ROUTE_HINT_PRELOAD_JS = r"""'use strict';
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const https = require('node:https');

const HINT_HEADER = 'x-zhumeng-claude-code-route-hint';
const SIGNATURE_HEADER = 'x-zhumeng-claude-code-route-signature';
const SCOPE = 'claude_code_route_hint_cp4';
const VERSION = 1;
const secret = process.env.ZHUMENG_CLAUDE_ROUTE_HINT_SECRET || '';
const catalogPath = process.env.ZHUMENG_CLAUDE_ROUTE_HINT_CATALOG_PATH || '';
const catalog = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));
const originalFetch = globalThis.fetch;
const originalHttpRequest = http.request;
const originalHttpsRequest = https.request;

if (!secret) {
  throw new Error('ZHUMENG Claude route hint secret is required');
}
if (typeof originalFetch !== 'function') {
  throw new Error('global fetch is required for ZHUMENG Claude route hints');
}

function b64url(data) {
  return Buffer.from(data).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function canonicalJson(value) {
  if (Array.isArray(value)) {
    return '[' + value.map(canonicalJson).join(',') + ']';
  }
  if (value && typeof value === 'object') {
    return '{' + Object.keys(value).sort().map((key) => JSON.stringify(key) + ':' + canonicalJson(value[key])).join(',') + '}';
  }
  return JSON.stringify(value);
}

function sha256Hex(data) {
  return crypto.createHash('sha256').update(data).digest('hex');
}

function sign(encoded, requestPath, body) {
  const material = Buffer.concat([
    Buffer.from(encoded),
    Buffer.from('\nPOST\n'),
    Buffer.from(requestPath),
    Buffer.from('\n'),
    Buffer.from(sha256Hex(body)),
  ]);
  return b64url(crypto.createHmac('sha256', secret).update(material).digest());
}

function routePath(urlValue) {
  const base = process.env.ANTHROPIC_BASE_URL || process.env.CLAUDE_CODE_API_BASE_URL || 'http://127.0.0.1';
  const parsed = new URL(String(urlValue), base);
  return parsed.pathname + parsed.search;
}

function shouldSign(method, requestPath) {
  return String(method || '').toUpperCase() === 'POST' && (
    requestPath === '/v1/messages' ||
    requestPath === '/v1/messages?beta=true' ||
    requestPath === '/v1/messages/count_tokens' ||
    requestPath === '/v1/messages/count_tokens?beta=true'
  );
}

function bodyBuffer(body) {
  if (typeof body === 'string') return Buffer.from(body);
  if (Buffer.isBuffer(body)) return Buffer.from(body);
  if (body instanceof Uint8Array) return Buffer.from(body);
  if (body instanceof ArrayBuffer) return Buffer.from(body);
  throw new Error('ZHUMENG route hint requires a replayable request body');
}

function modelFromBody(body) {
  const parsed = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!parsed || typeof parsed !== 'object' || typeof parsed.model !== 'string' || !parsed.model.trim()) {
    throw new Error('ZHUMENG route hint requires body.model');
  }
  return parsed.model.trim();
}

function routeEntryForBody(body) {
  const modelId = modelFromBody(body);
  const entry = catalog.entries[modelId];
  if (!entry) {
    throw new Error('ZHUMENG route hint unknown model: ' + modelId);
  }
  return {modelId, entry};
}

function requiresRouteHint(entry) {
  // Claude native formal-pool requests are guarded by the server-side catalog
  // fallback plus native attestation over the final bytes. Keeping route hints
  // off this path avoids trusting a pre-send body snapshot from the packed CLI.
  return !(entry.provider === 'claude' && entry.route === 'claude_code_native' && entry.client_type === 'claude_code_native');
}

function signedHeaders(headers, body, requestPath) {
  const {modelId, entry} = routeEntryForBody(body);
  if (!requiresRouteHint(entry)) {
    return headers;
  }
  const sessionRef = headers.get('x-claude-code-session-id') || process.env.ZHUMENG_CLAUDE_ROUTE_HINT_SESSION_REF || '';
  if (!sessionRef) {
    throw new Error('ZHUMENG route hint requires x-claude-code-session-id');
  }
  const issuedAt = Math.floor(Date.now() / 1000);
  const payload = {
    body_model: modelId,
    body_sha256: 'sha256:' + sha256Hex(body),
    catalog_hash: catalog.catalog_hash,
    catalog_version: catalog.catalog_version,
    client_type: entry.client_type,
    credential_scope: entry.credential_scope,
    expires_at: issuedAt + 60,
    formal_pool_allowed: Boolean(entry.formal_pool_allowed),
    gateway_location: entry.gateway_location,
    issued_at: issuedAt,
    key_id: 'route_hint_v1',
    live_request_allowed: Boolean(entry.live_enabled),
    method: 'POST',
    model_id: modelId,
    native_attestation_allowed: Boolean(entry.native_attestation_allowed),
    nonce: crypto.randomBytes(16).toString('hex'),
    provider: entry.provider,
    provider_owner: entry.provider_owner,
    request_uri: requestPath,
    route: entry.route,
    runtime_hash: catalog.runtime_hash,
    scope: SCOPE,
    session_ref: sessionRef,
    overlay_hash: catalog.overlay_hash,
    version: VERSION
  };
  const encoded = b64url(Buffer.from(canonicalJson(payload)));
  headers.set(HINT_HEADER, encoded);
  headers.set(SIGNATURE_HEADER, sign(encoded, requestPath, body));
  return headers;
}

globalThis.fetch = async function zhumengRouteHintFetch(input, init) {
  const requestUrl = typeof input === 'string' || input instanceof URL ? String(input) : input.url;
  const requestPath = routePath(requestUrl);
  const method = (init && init.method) || (typeof input === 'object' && input && input.method) || 'GET';
  if (!shouldSign(method, requestPath)) {
    return originalFetch.call(this, input, init);
  }
  const finalRequest = typeof Request === 'function' ? new Request(input, init) : null;
  if (!finalRequest) {
    const headers = new Headers((typeof input === 'object' && input && input.headers) || undefined);
    if (init && init.headers) {
      new Headers(init.headers).forEach((value, key) => headers.set(key, value));
    }
    let body = init && Object.prototype.hasOwnProperty.call(init, 'body') ? init.body : undefined;
    if (body === undefined && typeof input === 'object' && input && typeof input.clone === 'function') {
      body = Buffer.from(await input.clone().arrayBuffer());
    }
    const bodyBytes = bodyBuffer(body);
    signedHeaders(headers, bodyBytes, requestPath);
    return originalFetch.call(this, input, {...(init || {}), method: 'POST', headers, body: bodyBytes});
  }
  const bodyBytes = Buffer.from(await finalRequest.clone().arrayBuffer());
  const headers = new Headers(finalRequest.headers);
  signedHeaders(headers, bodyBytes, requestPath);
  headers.set('content-length', String(bodyBytes.length));
  return originalFetch.call(this, new Request(finalRequest, {headers, body: bodyBytes, method: 'POST'}));
};

function requestOptionsToUrl(input, options, defaultProtocol) {
  if (typeof input === 'string' || input instanceof URL) {
    return new URL(String(input));
  }
  const merged = {...(input || {}), ...(options || {})};
  const protocol = merged.protocol || defaultProtocol || 'http:';
  const host = merged.hostname || merged.host || '127.0.0.1';
  const port = merged.port ? ':' + String(merged.port) : '';
  const path = merged.path || merged.pathname || '/';
  return new URL(protocol + '//' + host + port + path);
}

function normalizeHeadersObject(value) {
  const headers = new Headers();
  if (!value) return headers;
  if (Array.isArray(value)) {
    for (const pair of value) {
      if (Array.isArray(pair) && pair.length >= 2) headers.set(String(pair[0]), String(pair[1]));
    }
    return headers;
  }
  if (typeof value[Symbol.iterator] === 'function' && typeof value !== 'string') {
    for (const pair of value) {
      if (Array.isArray(pair) && pair.length >= 2) headers.set(String(pair[0]), String(pair[1]));
    }
    return headers;
  }
  for (const [key, item] of Object.entries(value)) {
    if (Array.isArray(item)) headers.set(key, item.join(', '));
    else if (item !== undefined) headers.set(key, String(item));
  }
  return headers;
}

function headersToPlainObject(headers) {
  const result = {};
  headers.forEach((value, key) => { result[key] = value; });
  return result;
}

function splitRequestArgs(args) {
  let input = args[0];
  let options = {};
  let callback;
  if (typeof args[1] === 'function') {
    callback = args[1];
  } else {
    options = args[1] || {};
    callback = typeof args[2] === 'function' ? args[2] : undefined;
  }
  if (input === undefined || typeof input === 'function') {
    input = {};
    options = {};
    callback = typeof args[0] === 'function' ? args[0] : undefined;
  }
  return {input, options, callback};
}

function patchedRequest(originalRequest, protocol) {
  return function zhumengRouteHintRequest(...args) {
    const {input, options, callback} = splitRequestArgs(args);
    const url = requestOptionsToUrl(input, options, protocol);
    if (!url.protocol) url.protocol = protocol;
    const requestPath = url.pathname + url.search;
    const method = String((options && options.method) || (input && input.method) || 'GET').toUpperCase();
    const shouldPatch = shouldSign(method, requestPath);
    if (!shouldPatch) {
      return originalRequest.apply(this, args);
    }
    const existingHeaders = normalizeHeadersObject((input && input.headers) || undefined);
    normalizeHeadersObject((options && options.headers) || undefined).forEach((value, key) => existingHeaders.set(key, value));
    let chunks = [];
    let finalized = false;
    const baseOptions = {
      ...(typeof input === 'object' && !(input instanceof URL) ? input : {}),
      ...(options || {}),
      protocol: url.protocol,
      hostname: url.hostname,
      port: url.port,
      path: requestPath,
      method: 'POST',
      headers: headersToPlainObject(existingHeaders)
    };
    const req = originalRequest.call(this, baseOptions, callback);
    const originalWrite = req.write.bind(req);
    const originalEnd = req.end.bind(req);
    function appendChunk(chunk, encoding) {
      if (chunk === undefined || chunk === null) return;
      if (typeof chunk === 'string') chunks.push(Buffer.from(chunk, encoding));
      else if (Buffer.isBuffer(chunk)) chunks.push(Buffer.from(chunk));
      else if (chunk instanceof Uint8Array) chunks.push(Buffer.from(chunk));
      else chunks.push(Buffer.from(String(chunk), encoding));
    }
    req.write = function zhumengRouteHintWrite(chunk, encoding, cb) {
      appendChunk(chunk, typeof encoding === 'string' ? encoding : undefined);
      if (typeof encoding === 'function') encoding();
      if (typeof cb === 'function') cb();
      return true;
    };
    function normalizeEndArgs(chunk, encoding, cb) {
      if (typeof chunk === 'function') {
        return {chunk: undefined, encoding: undefined, callback: chunk};
      }
      if (typeof encoding === 'function') {
        return {chunk, encoding: undefined, callback: encoding};
      }
      return {chunk, encoding, callback: cb};
    }
    req.end = function zhumengRouteHintEnd(chunk, encoding, cb) {
      const endArgs = normalizeEndArgs(chunk, encoding, cb);
      if (finalized) return originalEnd(endArgs.chunk, endArgs.encoding, endArgs.callback);
      finalized = true;
      appendChunk(endArgs.chunk, typeof endArgs.encoding === 'string' ? endArgs.encoding : undefined);
      try {
        const body = Buffer.concat(chunks);
        const headers = normalizeHeadersObject(baseOptions.headers);
        signedHeaders(headers, body, requestPath);
        headers.set('content-length', String(body.length));
        for (const [key, value] of Object.entries(headersToPlainObject(headers))) {
          req.setHeader(key, value);
        }
        originalEnd(body, endArgs.callback);
      } catch (err) {
        process.nextTick(() => req.emit('error', err));
        originalEnd();
      }
      return req;
    };
    return req;
  };
}

http.request = patchedRequest(originalHttpRequest, 'http:');
https.request = patchedRequest(originalHttpsRequest, 'https:');
"""
