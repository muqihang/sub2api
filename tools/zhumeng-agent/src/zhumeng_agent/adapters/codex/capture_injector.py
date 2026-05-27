from __future__ import annotations

import base64
import json
import hashlib
import os
import select
import socket
import struct
import time
import urllib.error
import urllib.request
from urllib.parse import urlparse
from datetime import datetime, timezone
from pathlib import Path

from .capture_config import CodexDesktopCaptureConfig, CorrelationHasher, file_hash, path_identity
from .capture_shape import (
    add_safe_metadata,
    capture_model_picker_state,
    normalize_ui_matrix,
    shape_app_server_frame,
    shape_tool_lifecycle_event,
)
from .capture_writer import JsonlTraceWriter


ENDPOINT_PLACEHOLDER = "__ZHUMENG_CAPTURE_ENDPOINT__"
CDP_CAPTURE_BINDING_NAME = "__zhumengCodexCaptureEmit"


HOOK_SCRIPT = """(() => {
  if (globalThis.__zhumengCodexDesktopCaptureV2) return;
  const ZHUMENG_CAPTURE_ENDPOINT = "__ZHUMENG_CAPTURE_ENDPOINT__";
  const originalFetch = globalThis.fetch;
  const OriginalWebSocketForCapture = globalThis.WebSocket;
  let captureSocket = null;
  let captureSocketQueue = [];
  let sequence = 0;
  function schedule(fn) {
    try {
      if (typeof globalThis.queueMicrotask === "function") return globalThis.queueMicrotask(fn);
      if (typeof Promise === "function") return Promise.resolve().then(fn).catch(() => {});
      if (typeof globalThis.setTimeout === "function") return globalThis.setTimeout(fn, 0);
    } catch (_) {}
  }
  function textFrame(value) {
    try {
      if (typeof value === "string") return value;
      if (value instanceof URLSearchParams) return value.toString();
      if (value && typeof value === "object" && typeof value.body === "string") return value.body;
    } catch (_) {}
    return null;
  }
  function isLikelyAppServerFrame(value) {
    const frameText = textFrame(value);
    if (frameText == null) return false;
    if (frameText.length > 262144) return false;
    try {
      const parsed = JSON.parse(frameText);
      const items = Array.isArray(parsed) ? parsed : [parsed];
      return items.some((item) => item && typeof item === "object" && (
        "method" in item || "id" in item || "result" in item || "error" in item
      ));
    } catch (_) {
      return false;
    }
  }
  function headerValue(headers, names) {
    if (!headers) return null;
    for (const name of names) {
      try {
        if (typeof headers.get === "function") {
          const value = headers.get(name);
          if (value) return String(value);
        }
        if (Array.isArray(headers)) {
          const found = headers.find((item) => Array.isArray(item) && String(item[0]).toLowerCase() === name.toLowerCase());
          if (found && found[1]) return String(found[1]);
        }
        if (typeof headers === "object") {
          for (const key of Object.keys(headers)) {
            if (key.toLowerCase() === name.toLowerCase() && headers[key]) return String(headers[key]);
          }
        }
      } catch (_) {}
    }
    return null;
  }
  function correlationFromHeaders(headers) {
    const ids = {};
    const requestId = headerValue(headers, ["x-client-request-id", "x_client_request_id"]);
    const windowId = headerValue(headers, ["x-codex-window-id", "window_id"]);
    const sessionId = headerValue(headers, ["x-codex-session-id", "session_id"]);
    const threadId = headerValue(headers, ["x-codex-thread-id", "thread_id"]);
    const turnId = headerValue(headers, ["x-codex-turn-id", "turn_id"]);
    if (requestId) ids.x_client_request_id = requestId;
    if (windowId) ids.window_id = windowId;
    if (sessionId) ids.session_id = sessionId;
    if (threadId) ids.thread_id = threadId;
    if (turnId) ids.turn_id = turnId;
    return Object.keys(ids).length ? ids : null;
  }
  function mergeIds(...sources) {
    const output = {};
    for (const source of sources) {
      if (!source) continue;
      for (const key of Object.keys(source)) {
        if (source[key] && !(key in output)) output[key] = source[key];
      }
    }
    return Object.keys(output).length ? output : null;
  }
  function correlationFromUrl(value) {
    try {
      const text = typeof value === "string" ? value : value && typeof value === "object" && value.url ? String(value.url) : "";
      if (!text) return null;
      const url = new URL(text, "http://127.0.0.1");
      const headers = {};
      for (const [key, val] of url.searchParams.entries()) headers[key] = val;
      return correlationFromHeaders(headers);
    } catch (_) {
      return null;
    }
  }
  function correlationFromProtocols(protocols) {
    try {
      const items = Array.isArray(protocols) ? protocols : protocols ? [protocols] : [];
      const headers = {};
      for (const item of items) {
        const text = String(item);
        const index = text.indexOf("=");
        if (index > 0) headers[text.slice(0, index)] = text.slice(index + 1);
      }
      return correlationFromHeaders(headers);
    } catch (_) {
      return null;
    }
  }
  function visitCorrelationObject(value, output, depth) {
    if (!value || typeof value !== "object" || depth > 4) return;
    if (Array.isArray(value)) {
      for (const item of value.slice(0, 20)) visitCorrelationObject(item, output, depth + 1);
      return;
    }
    const aliases = {
      "session_id": "session_id",
      "x-codex-session-id": "session_id",
      "thread_id": "thread_id",
      "x-codex-thread-id": "thread_id",
      "turn_id": "turn_id",
      "x-codex-turn-id": "turn_id",
      "x_client_request_id": "x_client_request_id",
      "x-client-request-id": "x_client_request_id",
      "window_id": "window_id",
      "x-codex-window-id": "window_id"
    };
    for (const key of Object.keys(value)) {
      const mapped = aliases[String(key).toLowerCase()];
      if (mapped && value[key] != null && !(mapped in output)) output[mapped] = String(value[key]);
      else visitCorrelationObject(value[key], output, depth + 1);
    }
  }
  function correlationFromFrame(value) {
    const frameText = textFrame(value);
    if (frameText == null) return null;
    try {
      const parsed = JSON.parse(frameText);
      const output = {};
      visitCorrelationObject(parsed, output, 0);
      return Object.keys(output).length ? output : null;
    } catch (_) {
      return null;
    }
  }
  function fetchHeaders(args) {
    const init = args.length > 1 && args[1] && typeof args[1] === "object" ? args[1] : null;
    if (init && init.headers) return init.headers;
    try {
      if (args[0] && typeof args[0] === "object" && args[0].headers) return args[0].headers;
    } catch (_) {}
    return null;
  }
  function emitFrame(direction, value, extra) {
    const frameText = textFrame(value);
    if (frameText == null) return;
    if (!isLikelyAppServerFrame(frameText)) return;
    emit({
      type: "app_server_frame",
      direction,
      seq: ++sequence,
      frame_text: frameText,
      ts: Date.now(),
      ...(extra || {})
    });
  }
  function publicEventForRenderer(event) {
    const publicEvent = {};
    for (const key of ["type", "direction", "seq", "ts", "argCount", "tool_name", "selected_model", "status", "content_class", "result_chars", "sent_back_to_model"]) {
      if (key in event) publicEvent[key] = event[key];
    }
    if ("frame_text" in event) publicEvent.frame_chars = String(event.frame_text).length;
    if ("frame_base64" in event) publicEvent.frame_bytes_encoded = String(event.frame_base64).length;
    if ("correlation_ids" in event) publicEvent.correlation_ids_present = true;
    return publicEvent;
  }
  function captureFetchResponse(result, correlationIds) {
    try {
      const handleResponse = (response) => {
        try {
          if (response && typeof response.clone === "function") {
            const cloned = response.clone();
            if (cloned && typeof cloned.text === "function") {
              cloned.text()
                .then((text) => emitFrame("app_server_to_desktop", text, correlationIds ? { correlation_ids: correlationIds } : {}))
                .catch(() => {});
            }
          }
        } catch (_) {}
        return response;
      };
      if (result && typeof result.then === "function") result.then(handleResponse, () => {});
      else handleResponse(result);
    } catch (_) {}
  }
  function ensureWebSocketMessageHook(socket) {
    try {
      if (!socket || socket.__zhumengCaptureMessageHooked) return;
      socket.__zhumengCaptureMessageHooked = true;
      if (typeof socket.addEventListener === "function") {
        socket.addEventListener("message", (event) => {
          try {
            const data = event && "data" in event ? event.data : event;
            emitFrame("app_server_to_desktop", data, websocketExtra(socket, data));
          } catch (_) {}
        });
      }
    } catch (_) {}
  }
  function fetchBody(args) {
    const init = args.length > 1 && args[1] && typeof args[1] === "object" ? args[1] : null;
    if (init && "body" in init) return init.body;
    return args[0];
  }
  function fetchExtra(args) {
    const ids = mergeIds(correlationFromHeaders(fetchHeaders(args)), correlationFromUrl(args[0]), correlationFromFrame(fetchBody(args)));
    return ids ? { correlation_ids: ids } : {};
  }
  function websocketExtra(socket, frame) {
    const ids = mergeIds(socket && socket.__zhumengCaptureCorrelationIds, correlationFromFrame(frame));
    return ids ? { correlation_ids: ids } : {};
  }
  function originalFetchPost(event) {
    try {
      if (typeof originalFetch === "function" && ZHUMENG_CAPTURE_ENDPOINT.startsWith("http://127.0.0.1:")) {
        const body = JSON.stringify(event);
        if (cdpBindingPost(body)) return;
        if (captureWebSocketPost(body)) return;
        if (globalThis.navigator && typeof globalThis.navigator.sendBeacon === "function") {
          try {
            if (globalThis.navigator.sendBeacon(ZHUMENG_CAPTURE_ENDPOINT, body)) return;
          } catch (_) {}
        }
        originalFetch(ZHUMENG_CAPTURE_ENDPOINT, {
          method: "POST",
          keepalive: true,
          headers: { "content-type": "text/plain;charset=UTF-8" },
          body
        }).catch(() => {});
      }
    } catch (_) {}
  }
  function cdpBindingPost(body) {
    try {
      if (typeof globalThis.__zhumengCodexCaptureEmit === "function") {
        globalThis.__zhumengCodexCaptureEmit(body);
        return true;
      }
    } catch (_) {}
    return false;
  }
  function captureWebSocketEndpoint() {
    return ZHUMENG_CAPTURE_ENDPOINT.replace("http://", "ws://") + "/ws";
  }
  function captureWebSocketPost(body) {
    try {
      if (typeof OriginalWebSocketForCapture !== "function") return false;
      if (!ZHUMENG_CAPTURE_ENDPOINT.startsWith("http://127.0.0.1:")) return false;
      if (!captureSocket || captureSocket.readyState > 1) {
        captureSocket = new OriginalWebSocketForCapture(captureWebSocketEndpoint());
        captureSocketQueue = [];
        if (typeof captureSocket.addEventListener === "function") {
          captureSocket.addEventListener("open", () => {
            try {
              const queued = captureSocketQueue.slice(0, 100);
              captureSocketQueue = [];
              for (const item of queued) captureSocket.send(item);
            } catch (_) {}
          });
          captureSocket.addEventListener("close", () => { captureSocket = null; captureSocketQueue = []; });
          captureSocket.addEventListener("error", () => { captureSocket = null; captureSocketQueue = []; });
        }
      }
      if (captureSocket.readyState === 1) {
        captureSocket.send(body);
        return true;
      }
      if (captureSocket.readyState === 0 && captureSocketQueue.length < 100) {
        captureSocketQueue.push(body);
        return true;
      }
    } catch (_) {}
    return false;
  }
  function emitPublic(event) {
    try {
      const publicEvent = publicEventForRenderer(event);
      globalThis.dispatchEvent(new CustomEvent("zhumeng-codex-capture-v2", { detail: publicEvent }));
      console.info("zhumeng-codex-capture-v2", publicEvent);
    } catch (_) {}
  }
  function cloneEvent(event) {
    try {
      return JSON.parse(JSON.stringify(event));
    } catch (_) {
      return null;
    }
  }
  function emit(event) {
    const frozen = cloneEvent(event);
    if (!frozen) return;
    emitPublic(frozen);
    originalFetchPost(frozen);
  }
  globalThis.__zhumengCodexDesktopCaptureV2 = {
    schemaVersion: 1,
    hookMode: "renderer_readonly",
    appAsarModified: false,
    observe(event) {
      emit(event);
      return event;
    }
  };
  if (typeof originalFetch === "function") {
    globalThis.fetch = function zhumengCaptureFetch(...args) {
      const body = fetchBody(args);
      const shouldCapture = isLikelyAppServerFrame(body);
      const extra = shouldCapture ? fetchExtra(args) : {};
      const result = Reflect.apply(originalFetch, this, args);
      if (shouldCapture) {
        schedule(() => {
          try {
          if (isLikelyAppServerFrame(body)) {
            emitFrame("desktop_to_app_server", body, extra);
            captureFetchResponse(result, extra.correlation_ids);
          }
        } catch (_) {}
        });
      }
      return result;
    };
  }
  const OriginalWebSocket = globalThis.WebSocket;
  if (typeof OriginalWebSocket === "function") {
    const CapturedWebSocket = function zhumengCaptureWebSocket(...args) {
      const socket = Reflect.construct(OriginalWebSocket, args, new.target || OriginalWebSocket);
      socket.__zhumengCaptureCorrelationIds = mergeIds(correlationFromUrl(args[0]), correlationFromProtocols(args[1]));
      ensureWebSocketMessageHook(socket);
      return socket;
    };
    CapturedWebSocket.prototype = OriginalWebSocket.prototype;
    try { Object.setPrototypeOf(CapturedWebSocket, OriginalWebSocket); } catch (_) {}
    globalThis.WebSocket = CapturedWebSocket;
  }
  const originalSend = globalThis.WebSocket && globalThis.WebSocket.prototype && globalThis.WebSocket.prototype.send;
  if (typeof originalSend === "function") {
    globalThis.WebSocket.prototype.send = function zhumengCaptureWebSocketSend(...args) {
      ensureWebSocketMessageHook(this);
      const shouldCapture = isLikelyAppServerFrame(args[0]);
      const extra = shouldCapture ? websocketExtra(this, args[0]) : {};
      const result = Reflect.apply(originalSend, this, args);
      if (shouldCapture) {
        schedule(() => {
          try {
            emitFrame("desktop_to_app_server", args[0], extra);
          } catch (_) {}
        });
      }
      return result;
    };
  }
})();
"""


def install_capture_hook(app_path: Path, config: CodexDesktopCaptureConfig) -> dict[str, object]:
    config.validate()
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    if not asar_path.exists() or not asar_path.is_file():
        raise ValueError("Codex Desktop app.asar is missing or unreadable")
    config.base_dir.mkdir(parents=True, exist_ok=True)
    hook_path = config.base_dir / "renderer_capture_hook.js"
    hook_path.write_text(HOOK_SCRIPT, encoding="utf-8")
    manifest = {
        "schema_version": 1,
        "enabled": True,
        "hook_mode": "renderer_readonly",
        "app_asar_modified": False,
        "installed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        **path_identity(app_path, hasher),
        "app_asar_hash": file_hash(asar_path, hasher),
        "hook_script_hash": hasher.hash_identifier(HOOK_SCRIPT),
    }
    (config.base_dir / "capture_install.json").write_text(json.dumps(manifest, indent=2, sort_keys=True), encoding="utf-8")
    return {"status": "installed", "hook_mode": "renderer_readonly", "app_asar_modified": False}


def uninstall_capture_hook(app_path: Path, config: CodexDesktopCaptureConfig) -> dict[str, object]:
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    config.base_dir.mkdir(parents=True, exist_ok=True)
    manifest = {
        "schema_version": 1,
        "enabled": False,
        "hook_mode": "renderer_readonly",
        "app_asar_modified": False,
        "uninstalled_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        **path_identity(app_path, hasher),
    }
    (config.base_dir / "capture_install.json").write_text(json.dumps(manifest, indent=2, sort_keys=True), encoding="utf-8")
    return {"status": "uninstalled", "hook_mode": "renderer_readonly", "app_asar_modified": False}


def capture_installation_enabled(app_path: Path, config: CodexDesktopCaptureConfig) -> bool:
    manifest_path = config.base_dir / "capture_install.json"
    if not manifest_path.exists():
        return False
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return False
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    identity = path_identity(app_path, hasher)
    current_asar_hash = file_hash(app_path / "Contents" / "Resources" / "app.asar", hasher)
    if current_asar_hash is None:
        return False
    return (
        bool(manifest.get("enabled"))
        and manifest.get("app_asar_modified") is False
        and manifest.get("desktop_app_path_hash") == identity["desktop_app_path_hash"]
        and manifest.get("desktop_app_basename_hash") == identity["desktop_app_basename_hash"]
        and manifest.get("app_asar_hash") == current_asar_hash
    )


def build_runtime_hook_script(capture_port: int) -> str:
    endpoint = f"http://127.0.0.1:{capture_port}/codex-desktop-capture-v2"
    return HOOK_SCRIPT.replace(f'"{ENDPOINT_PLACEHOLDER}"', json.dumps(endpoint))


def inject_capture_hook_via_cdp(cdp_port: int, config: CodexDesktopCaptureConfig, capture_port: int | None = None) -> dict[str, object]:
    hook_path = config.base_dir / "renderer_capture_hook.js"
    script = hook_path.read_text(encoding="utf-8") if hook_path.exists() else HOOK_SCRIPT
    script = build_runtime_hook_script(capture_port) if capture_port else script
    targets = fetch_cdp_targets(cdp_port)
    injected = 0
    errors: list[str] = []
    for target in targets:
        websocket_url = target.get("webSocketDebuggerUrl")
        if not websocket_url:
            continue
        try:
            cdp_runtime_evaluate(str(websocket_url), script)
            injected += 1
        except Exception as exc:
            errors.append(type(exc).__name__)
    if injected == 0 and errors:
        return {"status": "degraded", "hook_mode": "renderer_readonly", "targets_injected": 0, "errors": errors[:3]}
    return {"status": "injected" if injected else "no_targets", "hook_mode": "renderer_readonly", "targets_injected": injected}


def route_cdp_binding_payload(payload_text: str, trace_dir: Path, config: CodexDesktopCaptureConfig | None = None) -> bool:
    try:
        event = json.loads(payload_text)
    except json.JSONDecodeError:
        return False
    if not isinstance(event, dict):
        return False
    return route_capture_event(event, trace_dir, config)


def attach_capture_bridge_via_cdp(
    cdp_port: int,
    trace_dir: Path,
    config: CodexDesktopCaptureConfig | None = None,
    *,
    capture_port: int = 0,
    timeout_seconds: float = 600,
    target_wait_seconds: float = 10,
    once: bool = False,
) -> dict[str, object]:
    config = config or CodexDesktopCaptureConfig.defaults()
    trace_dir.mkdir(parents=True, exist_ok=True)
    targets = wait_for_cdp_targets(cdp_port, target_wait_seconds)
    sessions: list[CdpSession] = []
    errors: list[str] = []
    for target in targets:
        websocket_url = target.get("webSocketDebuggerUrl")
        if not websocket_url:
            continue
        try:
            session = CdpSession(str(websocket_url))
            session.send("Runtime.enable")
            session.send("Runtime.addBinding", {"name": CDP_CAPTURE_BINDING_NAME})
            session.send("Runtime.evaluate", {"expression": build_runtime_hook_script(capture_port), "awaitPromise": False})
            sessions.append(session)
        except Exception as exc:
            errors.append(type(exc).__name__)
    if not sessions:
        return {
            "status": "no_targets" if not errors else "degraded",
            "hook_mode": "renderer_readonly",
            "bridge": "cdp_binding",
            "targets_attached": 0,
            "events_written": 0,
            "errors": errors[:3],
        }

    events_seen = 0
    events_written = 0
    deadline = time.monotonic() + max(timeout_seconds, 0)
    try:
        while sessions and time.monotonic() <= deadline:
            readable, _, _ = select.select([session.sock for session in sessions], [], [], min(1.0, max(deadline - time.monotonic(), 0)))
            if not readable:
                continue
            for sock in readable:
                session = next((item for item in sessions if item.sock is sock), None)
                if session is None:
                    continue
                try:
                    message_text = recv_websocket_text(sock)
                    if not message_text:
                        continue
                    message = json.loads(message_text)
                    params = message.get("params") if isinstance(message, dict) else None
                    if message.get("method") == "Runtime.bindingCalled" and isinstance(params, dict) and params.get("name") == CDP_CAPTURE_BINDING_NAME:
                        events_seen += 1
                        payload = params.get("payload")
                        if isinstance(payload, str) and route_cdp_binding_payload(payload, trace_dir, config):
                            events_written += 1
                            if once:
                                return attach_bridge_result(len(sessions), events_seen, events_written, errors)
                except Exception as exc:
                    errors.append(type(exc).__name__)
                    session.close()
                    sessions = [item for item in sessions if item is not session]
    finally:
        for session in sessions:
            session.close()
    return attach_bridge_result(len(sessions), events_seen, events_written, errors)


def wait_for_cdp_targets(cdp_port: int, timeout_seconds: float) -> list[dict[str, object]]:
    deadline = time.monotonic() + max(timeout_seconds, 0)
    while True:
        targets = fetch_cdp_targets(cdp_port)
        if targets or time.monotonic() >= deadline:
            return targets
        time.sleep(0.25)


def attach_bridge_result(targets_attached: int, events_seen: int, events_written: int, errors: list[str]) -> dict[str, object]:
    return {
        "status": "attached",
        "hook_mode": "renderer_readonly",
        "bridge": "cdp_binding",
        "targets_attached": targets_attached,
        "binding_events": events_seen,
        "events_written": events_written,
        "errors": errors[:3],
    }


def route_capture_event(
    event: dict[str, object],
    trace_dir: Path,
    config: CodexDesktopCaptureConfig | None = None,
) -> bool:
    config = config or CodexDesktopCaptureConfig.defaults()
    event_type = str(event.get("type", ""))
    if event_type.startswith("tool."):
        filename = "tool_lifecycle.jsonl"
        payload = sanitize_renderer_tool_event(event, config)
    elif event_type == "model_picker":
        filename = "model_picker.jsonl"
        payload = sanitize_renderer_event(event)
    elif event_type == "app_server_frame":
        filename = "app_server_v2.jsonl"
        payload = sanitize_renderer_app_server_event(event, config)
        written = JsonlTraceWriter(trace_dir / filename).safe_write(payload)
        write_derived_frame_events(event, trace_dir, config)
        return written
    else:
        filename = "app_server_v2.jsonl"
        payload = sanitize_renderer_event(event)
    return JsonlTraceWriter(trace_dir / filename).safe_write(payload)


def write_derived_frame_events(event: dict[str, object], trace_dir: Path, config: CodexDesktopCaptureConfig) -> None:
    frame = parse_renderer_frame(event)
    if not isinstance(frame, dict):
        return
    context = {
        "desktop_trace_id": str(event.get("desktop_trace_id") or "cd_runtime"),
        "correlation_ids": merge_correlation_ids(
            event.get("correlation_ids") if isinstance(event.get("correlation_ids"), dict) else None,
            extract_correlation_ids_from_frame_bytes(renderer_frame_bytes(event)),
        ),
        "model": str(event.get("model")) if event.get("model") else None,
        "request_path": str(event.get("request_path")) if event.get("request_path") else None,
    }
    model_event = model_picker_event_from_frame(frame)
    if model_event:
        JsonlTraceWriter(trace_dir / "model_picker.jsonl").safe_write(model_event)
    tool_event = tool_event_from_frame(frame, config, context=context)
    if tool_event:
        JsonlTraceWriter(trace_dir / "tool_lifecycle.jsonl").safe_write(tool_event)


def parse_renderer_frame(event: dict[str, object]) -> object | None:
    frame = renderer_frame_bytes(event)
    try:
        return json.loads(frame.decode("utf-8"))
    except Exception:
        return None


def model_picker_event_from_frame(frame: dict[str, object]) -> dict[str, object] | None:
    result = frame.get("result")
    if not isinstance(result, dict):
        return None
    models = result.get("models") or result.get("model_list")
    if not isinstance(models, list) or not all(isinstance(model, dict) for model in models):
        return None
    return capture_model_picker_state(
        app_server_models=models,
        selected_model=str(result.get("selected_model")) if result.get("selected_model") else None,
        selected_reasoning_effort=str(result.get("selected_reasoning_effort")) if result.get("selected_reasoning_effort") else None,
        ui_visible_model_ids=[str(model.get("model", "")) for model in models if not bool(model.get("hidden", False))],
        model_picker_patch_state={"status": "not_modified"},
    )


def tool_event_from_frame(
    frame: dict[str, object],
    config: CodexDesktopCaptureConfig,
    *,
    context: dict[str, object] | None = None,
) -> dict[str, object] | None:
    result = frame.get("result")
    method = str(frame.get("method") or "")
    candidate = result if isinstance(result, dict) else frame
    if not isinstance(candidate, dict):
        return None
    tool_name = candidate.get("tool_name") or candidate.get("name")
    if not tool_name and "tool" in method:
        tool_name = method
    if not tool_name:
        return None
    result_payload = candidate.get("content") or candidate.get("result") or candidate.get("output") or candidate
    result_text = result_payload if isinstance(result_payload, str) else json.dumps(result_payload, sort_keys=True, default=str)
    return shape_tool_lifecycle_event(
        tool_name=str(tool_name),
        call_id=str(candidate.get("call_id") or frame.get("id") or "unknown_call"),
        item_id=str(candidate.get("item_id") or frame.get("id") or "unknown_item"),
        schema=candidate.get("schema") if isinstance(candidate.get("schema"), dict) else {},
        result=result_text,
        content_class=infer_tool_content_class(str(tool_name), str(candidate.get("content_class") or "")),
        status=str(candidate.get("status") or "observed"),
        duration_ms=int(candidate.get("duration_ms")) if isinstance(candidate.get("duration_ms"), int) and int(candidate.get("duration_ms")) >= 0 else 0,
        sent_back_to_model=bool(candidate.get("sent_back_to_model", True)),
        hasher=CorrelationHasher.from_key_file(config.correlation_hash_key_file),
        desktop_trace_id=str(context.get("desktop_trace_id")) if isinstance(context, dict) and context.get("desktop_trace_id") else None,
        correlation_ids=context.get("correlation_ids") if isinstance(context, dict) and isinstance(context.get("correlation_ids"), dict) else None,
        model=str(context.get("model")) if isinstance(context, dict) and context.get("model") else None,
        request_path=str(context.get("request_path")) if isinstance(context, dict) and context.get("request_path") else None,
        ui_matrix=event_ui_matrix(candidate),
        degraded_reason=str(candidate.get("degraded_reason")) if candidate.get("degraded_reason") else None,
        pass_fail_rule=str(candidate.get("pass_fail_rule")) if candidate.get("pass_fail_rule") else None,
    )


def sanitize_renderer_event(event: dict[str, object]) -> dict[str, object]:
    allowed: dict[str, object] = {
        "schema_version": 1,
        "source": "codex_desktop",
        "hook_mode": "renderer_readonly",
    }
    hasher = CorrelationHasher.from_key_file(None)
    for key in ("type", "argCount", "ts"):
        if key in event:
            allowed[key] = event[key]
    for key in ("tool_name", "selected_model"):
        if key in event:
            add_safe_metadata(allowed, key, event[key], hasher)
    return allowed


def sanitize_renderer_app_server_event(event: dict[str, object], config: CodexDesktopCaptureConfig) -> dict[str, object]:
    frame = renderer_frame_bytes(event)
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    correlation_ids = merge_correlation_ids(
        event.get("correlation_ids") if isinstance(event.get("correlation_ids"), dict) else None,
        extract_correlation_ids_from_frame_bytes(frame),
    )
    return shape_app_server_frame(
        frame,
        direction=str(event.get("direction") or "unknown"),
        hasher=hasher,
        desktop_trace_id=str(event.get("desktop_trace_id") or "cd_runtime"),
        seq=int(event.get("seq")) if isinstance(event.get("seq"), int) else 1,
        correlation_ids=correlation_ids,
        model=str(event.get("model")) if event.get("model") else None,
        request_path=str(event.get("request_path")) if event.get("request_path") else None,
    )


def renderer_frame_bytes(event: dict[str, object]) -> bytes:
    if isinstance(event.get("frame_text"), str):
        return str(event["frame_text"]).encode("utf-8")
    if isinstance(event.get("frame_base64"), str):
        try:
            return base64.b64decode(str(event["frame_base64"]), validate=True)
        except Exception:
            return b""
    return b""


CORRELATION_ALIASES = {
    "session_id": "session_id",
    "x-codex-session-id": "session_id",
    "thread_id": "thread_id",
    "x-codex-thread-id": "thread_id",
    "turn_id": "turn_id",
    "x-codex-turn-id": "turn_id",
    "x_client_request_id": "x_client_request_id",
    "x-client-request-id": "x_client_request_id",
    "window_id": "window_id",
    "x-codex-window-id": "window_id",
}


def merge_correlation_ids(*sources: object) -> dict[str, object] | None:
    output: dict[str, object] = {}
    for source in sources:
        if not isinstance(source, dict):
            continue
        for key, value in source.items():
            normalized = CORRELATION_ALIASES.get(str(key).lower(), str(key))
            if normalized in CORRELATION_ALIASES.values() and value and normalized not in output:
                output[normalized] = value
    return output or None


def extract_correlation_ids_from_frame_bytes(frame: bytes) -> dict[str, object] | None:
    try:
        payload = json.loads(frame.decode("utf-8"))
    except Exception:
        return None
    output: dict[str, object] = {}
    visit_correlation_values(payload, output, 0)
    return output or None


def visit_correlation_values(value: object, output: dict[str, object], depth: int) -> None:
    if depth > 4:
        return
    if isinstance(value, dict):
        for key, child in value.items():
            normalized = CORRELATION_ALIASES.get(str(key).lower())
            if normalized and child and normalized not in output:
                output[normalized] = child
            else:
                visit_correlation_values(child, output, depth + 1)
    elif isinstance(value, list):
        for item in value[:20]:
            visit_correlation_values(item, output, depth + 1)


def sanitize_renderer_tool_event(event: dict[str, object], config: CodexDesktopCaptureConfig) -> dict[str, object]:
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    tool_name = str(event.get("tool_name") or event.get("name") or "unknown_tool")
    content_class = infer_tool_content_class(tool_name, str(event.get("content_class") or ""))
    schema_shape = event.get("schema_shape") if isinstance(event.get("schema_shape"), dict) else {}
    result_shape = event.get("result_shape") if isinstance(event.get("result_shape"), dict) else {}
    result_chars = event.get("result_chars")
    if not isinstance(result_chars, int) or result_chars < 0:
        result_chars = 0
    renderer_result_hash_present = isinstance(event.get("result_hash"), str)
    sanitized = shape_tool_lifecycle_event(
        tool_name=tool_name,
        call_id=str(event.get("call_id") or "unknown_call"),
        item_id=str(event.get("item_id") or "unknown_item"),
        schema=schema_shape,
        result={
            "content_class": content_class,
            "result_shape": result_shape,
            "result_chars": result_chars,
        },
        content_class=content_class,
        status=str(event.get("status") or "observed"),
        duration_ms=int(event.get("duration_ms")) if isinstance(event.get("duration_ms"), int) and int(event.get("duration_ms")) >= 0 else 0,
        sent_back_to_model=bool(event.get("sent_back_to_model", False)),
        hasher=hasher,
        desktop_trace_id=str(event.get("desktop_trace_id")) if event.get("desktop_trace_id") else None,
        correlation_ids=event.get("correlation_ids") if isinstance(event.get("correlation_ids"), dict) else None,
        model=str(event.get("model")) if event.get("model") else None,
        request_path=str(event.get("request_path")) if event.get("request_path") else None,
        ui_matrix=event_ui_matrix(event),
        degraded_reason=str(event.get("degraded_reason")) if event.get("degraded_reason") else None,
        pass_fail_rule=str(event.get("pass_fail_rule")) if event.get("pass_fail_rule") else None,
    )
    sanitized["hook_mode"] = "renderer_readonly"
    sanitized["renderer_event_type"] = str(event.get("type", ""))
    sanitized["result_content_type"] = str(event.get("result_content_type") or "shape")
    sanitized["result_chars"] = result_chars
    sanitized["renderer_result_hash_present"] = renderer_result_hash_present
    return sanitized


def event_ui_matrix(event: dict[str, object]) -> dict[str, bool] | None:
    ui_matrix = normalize_ui_matrix(event.get("ui_matrix"))
    if ui_matrix:
        return ui_matrix
    return normalize_ui_matrix(event)


def infer_tool_content_class(tool_name: str, explicit: str) -> str:
    if explicit in {
        "browser_content",
        "command_output",
        "file_content",
        "json_metadata",
        "screenshot",
        "tool_output",
    }:
        return explicit
    lowered = tool_name.lower()
    if "screenshot" in lowered or "computer_use" in lowered:
        return "screenshot"
    if "browser" in lowered or "chrome" in lowered:
        return "browser_content"
    if lowered.startswith("shell") or "exec" in lowered:
        return "command_output"
    if "file" in lowered or "read" in lowered:
        return "file_content"
    if "plugin" in lowered or "json" in lowered:
        return "json_metadata"
    return "tool_output"


def tool_namespace(tool_name: str) -> str:
    if tool_name.startswith("mcp__"):
        parts = tool_name.split("__")
        if len(parts) >= 3:
            return "__".join(parts[:3])
    return tool_name.split("_", 1)[0]


def fetch_cdp_targets(cdp_port: int) -> list[dict[str, object]]:
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{cdp_port}/json", timeout=2) as response:
            payload = response.read().decode("utf-8")
    except (urllib.error.URLError, TimeoutError, OSError):
        return []
    try:
        targets = json.loads(payload)
    except json.JSONDecodeError:
        return []
    return targets if isinstance(targets, list) else []


def cdp_runtime_evaluate(websocket_url: str, expression: str) -> None:
    session = CdpSession(websocket_url)
    try:
        session.send("Runtime.evaluate", {"expression": expression, "awaitPromise": False})
    finally:
        session.close()


class CdpSession:
    def __init__(self, websocket_url: str):
        parsed = urlparse(websocket_url)
        if parsed.hostname not in {"127.0.0.1", "localhost"}:
            raise ValueError("refusing non-loopback CDP target")
        self.sock = socket.create_connection((parsed.hostname or "127.0.0.1", parsed.port or 80), timeout=2)
        self._next_id = 1
        try:
            key = base64.b64encode(os.urandom(16)).decode("ascii")
            path = parsed.path or "/"
            request = (
                f"GET {path} HTTP/1.1\r\n"
                f"Host: {parsed.hostname}:{parsed.port}\r\n"
                "Upgrade: websocket\r\n"
                "Connection: Upgrade\r\n"
                f"Sec-WebSocket-Key: {key}\r\n"
                "Sec-WebSocket-Version: 13\r\n\r\n"
            )
            self.sock.sendall(request.encode("ascii"))
            response = self.sock.recv(4096)
            if b" 101 " not in response.split(b"\r\n", 1)[0]:
                raise OSError("CDP websocket handshake failed")
            expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest())
            if expected not in response:
                raise OSError("CDP websocket accept mismatch")
        except Exception:
            self.close()
            raise

    def send(self, method: str, params: dict[str, object] | None = None) -> int:
        message_id = self._next_id
        self._next_id += 1
        message = json.dumps({"id": message_id, "method": method, "params": params or {}})
        self.sock.sendall(encode_websocket_text(message))
        return message_id

    def close(self) -> None:
        try:
            self.sock.close()
        except Exception:
            pass


def encode_websocket_text(message: str) -> bytes:
    payload = message.encode("utf-8")
    header = bytearray([0x81])
    length = len(payload)
    if length < 126:
        header.append(0x80 | length)
    elif length < 65536:
        header.extend([0x80 | 126, (length >> 8) & 0xFF, length & 0xFF])
    else:
        header.append(0x80 | 127)
        header.extend(length.to_bytes(8, "big"))
    mask = os.urandom(4)
    header.extend(mask)
    masked = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
    return bytes(header) + masked


def recv_websocket_text(sock: socket.socket) -> str:
    first, second = recv_exact(sock, 2)
    length = second & 0x7F
    if length == 126:
        length = struct.unpack("!H", recv_exact(sock, 2))[0]
    elif length == 127:
        length = struct.unpack("!Q", recv_exact(sock, 8))[0]
    if second & 0x80:
        mask = recv_exact(sock, 4)
        payload = bytes(byte ^ mask[index % 4] for index, byte in enumerate(recv_exact(sock, length)))
    else:
        payload = recv_exact(sock, length)
    opcode = first & 0x0F
    if opcode == 8:
        raise OSError("CDP websocket closed")
    if opcode != 1:
        return ""
    return payload.decode("utf-8", errors="replace")


def recv_exact(sock: socket.socket, count: int) -> bytes:
    data = b""
    while len(data) < count:
        chunk = sock.recv(count - len(data))
        if not chunk:
            raise OSError("CDP websocket closed")
        data += chunk
    return data
