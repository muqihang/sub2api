import json
import shutil
import subprocess
from pathlib import Path

import zhumeng_agent.adapters.codex.capture_injector as capture_injector
from zhumeng_agent.cli import main
from zhumeng_agent.adapters.codex.capture_config import CodexDesktopCaptureConfig
from zhumeng_agent.adapters.codex.capture_config import CorrelationHasher
from zhumeng_agent.adapters.codex.capture_injector import (
    HOOK_SCRIPT,
    route_cdp_binding_payload,
    build_runtime_hook_script,
    capture_installation_enabled,
    install_capture_hook,
    route_capture_event,
    uninstall_capture_hook,
)


def parse_output(capsys):
    output = capsys.readouterr().out.strip()
    assert output, "expected CLI to print JSON"
    return json.loads(output)


def test_capture_install_writes_readonly_manifest_without_modifying_asar(tmp_path: Path):
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    asar = resources / "app.asar"
    asar.write_bytes(b"original-asar")
    config = CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures")

    result = install_capture_hook(app, config)

    assert result["status"] == "installed"
    assert result["hook_mode"] == "renderer_readonly"
    assert result["app_asar_modified"] is False
    assert asar.read_bytes() == b"original-asar"
    manifest = json.loads((tmp_path / "captures" / "capture_install.json").read_text(encoding="utf-8"))
    assert manifest["app_asar_modified"] is False
    assert str(tmp_path) not in json.dumps(manifest)


def test_capture_uninstall_disables_manifest_without_touching_asar(tmp_path: Path):
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    asar = resources / "app.asar"
    asar.write_bytes(b"original-asar")
    config = CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures")
    install_capture_hook(app, config)

    result = uninstall_capture_hook(app, config)

    assert result["status"] == "uninstalled"
    assert asar.read_bytes() == b"original-asar"
    manifest = json.loads((tmp_path / "captures" / "capture_install.json").read_text(encoding="utf-8"))
    assert manifest["enabled"] is False


def test_renderer_hook_wraps_fetch_and_websocket_without_replacing_return_values():
    assert "globalThis.fetch" in HOOK_SCRIPT
    assert "WebSocket.prototype.send" in HOOK_SCRIPT
    assert "Reflect.apply(originalFetch" in HOOK_SCRIPT
    assert "Reflect.apply(originalSend" in HOOK_SCRIPT
    assert "zhumeng-codex-capture-v2" in HOOK_SCRIPT
    assert "console.info" in HOOK_SCRIPT
    assert "__ZHUMENG_CAPTURE_ENDPOINT__" in HOOK_SCRIPT
    assert "originalFetch(ZHUMENG_CAPTURE_ENDPOINT" in HOOK_SCRIPT


def test_runtime_hook_script_uses_real_loopback_endpoint():
    script = build_runtime_hook_script(18765)

    assert "http://127.0.0.1:18765/codex-desktop-capture-v2" in script
    assert "__ZHUMENG_CAPTURE_ENDPOINT__" not in script
    assert "globalThis.http://127.0.0.1" not in script


def test_runtime_hook_script_is_valid_javascript_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "hook.js"
    script_path.write_text(build_runtime_hook_script(18765), encoding="utf-8")

    completed = subprocess.run([node, "--check", str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_preserves_fetch_and_websocket_return_and_errors_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "exercise-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "const calls = [];",
            "globalThis.fetch = function originalFetch(url, init) { calls.push(['fetch', url, init && init.body]); if (url === 'throw') throw new Error('fetch-boom'); return 'fetch-return'; };",
            "globalThis.WebSocket = function WebSocket() {};",
            "globalThis.WebSocket.prototype.send = function originalSend(payload) { calls.push(['send', payload]); if (payload === 'throw') throw new Error('send-boom'); return 'send-return'; };",
            build_runtime_hook_script(18765),
            "const fetchReturn = globalThis.fetch('https://app-server.local/rpc', { body: '{\"id\":1,\"method\":\"model/list\"}' });",
            "const ws = new globalThis.WebSocket();",
            "const sendReturn = ws.send('{\"id\":2,\"method\":\"tool/call\"}');",
            "let fetchError = '';",
            "try { globalThis.fetch('throw'); } catch (err) { fetchError = err.message; }",
            "let sendError = '';",
            "try { ws.send('throw'); } catch (err) { sendError = err.message; }",
            "setTimeout(() => {",
            "  if (fetchReturn !== 'fetch-return') process.exit(11);",
            "  if (sendReturn !== 'send-return') process.exit(12);",
            "  if (fetchError !== 'fetch-boom') process.exit(13);",
            "  if (sendError !== 'send-boom') process.exit(14);",
            "  if (calls[0][0] !== 'fetch' || calls[1][0] !== 'send') process.exit(15);",
            "}, 0);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_handles_missing_queue_microtask_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "no-queue-microtask.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.queueMicrotask = undefined;",
            "globalThis.fetch = function originalFetch() { return 'fetch-return'; };",
            "globalThis.WebSocket = function WebSocket() {};",
            "globalThis.WebSocket.prototype.send = function originalSend() { return 'send-return'; };",
            build_runtime_hook_script(18765),
            "const fetchReturn = globalThis.fetch('/rpc', { body: '{\"id\":1}' });",
            "const ws = new globalThis.WebSocket();",
            "const sendReturn = ws.send('{\"id\":2}');",
            "if (fetchReturn !== 'fetch-return') process.exit(31);",
            "if (sendReturn !== 'send-return') process.exit(32);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_captures_fetch_response_and_websocket_message_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "inbound-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const posted = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.fetch = function originalFetch(url, init) {",
            "  if (url === 'http://127.0.0.1:18765/codex-desktop-capture-v2') { posted.push(JSON.parse(init.body)); return Promise.resolve({ ok: true }); }",
            "  return Promise.resolve({ clone() { return { text() { return Promise.resolve('{\"id\":1,\"result\":{\"models\":[{\"model\":\"deepseek\"}]}}'); } }; } });",
            "};",
            "globalThis.WebSocket = function WebSocket() { this.listeners = {}; };",
            "globalThis.WebSocket.prototype.addEventListener = function (name, cb) { this.listeners[name] = cb; };",
            "globalThis.WebSocket.prototype.send = function originalSend() { return 'send-return'; };",
            build_runtime_hook_script(18765),
            "globalThis.fetch('/rpc', { headers: { 'x-client-request-id': 'request-1' }, body: '{\"id\":1,\"method\":\"model/list\"}' });",
            "const ws = new globalThis.WebSocket();",
            "ws.send('{\"id\":2,\"method\":\"turn/start\"}');",
            "ws.listeners.message({ data: '{\"id\":2,\"result\":{\"ok\":true}}' });",
            "setTimeout(() => {",
            "  if (!posted.some((event) => event.direction === 'app_server_to_desktop' && String(event.frame_text).includes('models'))) process.exit(41);",
            "  if (!posted.some((event) => event.direction === 'app_server_to_desktop' && String(event.frame_text).includes('ok'))) process.exit(42);",
            "  if (!posted.some((event) => event.correlation_ids && event.correlation_ids.x_client_request_id === 'request-1')) process.exit(43);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_does_not_clone_non_frame_fetch_response_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "non-frame-fetch-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const posted = [];",
            "let cloneCount = 0;",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.fetch = function originalFetch(url, init) {",
            "  if (url === 'http://127.0.0.1:18765/codex-desktop-capture-v2') { posted.push(JSON.parse(init.body)); return Promise.resolve({ ok: true }); }",
            "  return Promise.resolve({ clone() { cloneCount += 1; return { text() { return Promise.resolve('{\"large\":\"config\"}'); } }; } });",
            "};",
            build_runtime_hook_script(18765),
            "globalThis.fetch('/asset.json', { body: '{\"large\":\"config\"}' });",
            "setTimeout(() => {",
            "  if (cloneCount !== 0) process.exit(71);",
            "  if (posted.some((event) => event.type === 'app_server_frame')) process.exit(72);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_does_not_post_non_frame_fetch_or_websocket_events_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "non-frame-quiet-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const posted = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.fetch = function originalFetch(url, init) {",
            "  if (url === 'http://127.0.0.1:18765/codex-desktop-capture-v2') posted.push(JSON.parse(init.body));",
            "  return Promise.resolve({ ok: true });",
            "};",
            "globalThis.WebSocket = function WebSocket() { this.listeners = {}; };",
            "globalThis.WebSocket.prototype.addEventListener = function (name, cb) { this.listeners[name] = cb; };",
            "globalThis.WebSocket.prototype.send = function originalSend() { return 'send-return'; };",
            build_runtime_hook_script(18765),
            "globalThis.fetch('/asset.json', { body: '{\"large\":\"config\"}' });",
            "const ws = new globalThis.WebSocket();",
            "ws.send('plain text heartbeat');",
            "setTimeout(() => {",
            "  if (posted.length !== 0) process.exit(81);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_prefers_send_beacon_for_capture_endpoint_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "beacon-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const beacons = [];",
            "const fetches = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "Object.defineProperty(globalThis, 'navigator', { value: { sendBeacon(url, body) { beacons.push([url, String(body)]); return true; } }, configurable: true });",
            "globalThis.fetch = function originalFetch(url, init) { fetches.push([url, init && init.body]); return Promise.resolve({ ok: true }); };",
            "globalThis.WebSocket = undefined;",
            build_runtime_hook_script(18765),
            "globalThis.__zhumengCodexDesktopCaptureV2.observe({ type: 'model_picker', selected_model: 'deepseek' });",
            "setTimeout(() => {",
            "  if (beacons.length !== 1) process.exit(91);",
            "  if (fetches.length !== 0) process.exit(92);",
            "  if (!beacons[0][0].includes('127.0.0.1:18765')) process.exit(93);",
            "  if (!beacons[0][1].includes('model_picker')) process.exit(94);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_prefers_capture_websocket_when_available_in_node(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "capture-websocket-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const sent = [];",
            "const beacons = [];",
            "const sockets = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "Object.defineProperty(globalThis, 'navigator', { value: { sendBeacon(url, body) { beacons.push([url, String(body)]); return true; } }, configurable: true });",
            "globalThis.fetch = function originalFetch() { return Promise.resolve({ ok: true }); };",
            "globalThis.WebSocket = function WebSocket(url) { this.url = url; this.readyState = 1; sockets.push(this); };",
            "globalThis.WebSocket.OPEN = 1;",
            "globalThis.WebSocket.CONNECTING = 0;",
            "globalThis.WebSocket.prototype.addEventListener = function () {};",
            "globalThis.WebSocket.prototype.send = function (payload) { sent.push([this.url, payload]); };",
            build_runtime_hook_script(18765),
            "globalThis.__zhumengCodexDesktopCaptureV2.observe({ type: 'model_picker', selected_model: 'deepseek' });",
            "setTimeout(() => {",
            "  if (sent.length !== 1) process.exit(101);",
            "  if (beacons.length !== 0) process.exit(102);",
            "  if (!sent[0][0].includes('ws://127.0.0.1:18765/codex-desktop-capture-v2/ws')) process.exit(103);",
            "  if (!String(sent[0][1]).includes('model_picker')) process.exit(104);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_prefers_cdp_binding_when_available_in_node(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "binding-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const bound = [];",
            "const sent = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.__zhumengCodexCaptureEmit = function (payload) { bound.push(payload); };",
            "globalThis.fetch = function originalFetch() { return Promise.resolve({ ok: true }); };",
            "globalThis.WebSocket = function WebSocket(url) { this.url = url; this.readyState = 1; };",
            "globalThis.WebSocket.prototype.addEventListener = function () {};",
            "globalThis.WebSocket.prototype.send = function (payload) { sent.push(payload); };",
            build_runtime_hook_script(18765),
            "globalThis.__zhumengCodexDesktopCaptureV2.observe({ type: 'model_picker', selected_model: 'deepseek' });",
            "setTimeout(() => {",
            "  if (bound.length !== 1) process.exit(111);",
            "  if (sent.length !== 0) process.exit(112);",
            "  if (!String(bound[0]).includes('model_picker')) process.exit(113);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_cdp_binding_payload_routes_shape_only_trace_without_raw_ids(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    written = route_cdp_binding_payload(json.dumps({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "seq": 17,
        "frame_text": json.dumps({
            "id": 17,
            "method": "model/list",
            "params": {"x-client-request-id": "request-raw"},
        }),
    }), tmp_path, config)

    assert written is True
    dumped = (tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8")
    assert "request-raw" not in dumped
    assert "x_client_request_id_hash" in dumped
    assert "payload_shape" in dumped


def test_cdp_binding_payload_ignores_non_object_payload(tmp_path: Path):
    assert route_cdp_binding_payload('"not-an-event"', tmp_path, CodexDesktopCaptureConfig.defaults()) is False
    assert not (tmp_path / "app_server_v2.jsonl").exists()


def test_cdp_binding_payload_reports_false_when_jsonl_write_fails(tmp_path: Path, monkeypatch):
    monkeypatch.setattr(capture_injector.JsonlTraceWriter, "safe_write", lambda self, event: False)

    written = route_cdp_binding_payload(json.dumps({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "frame_text": '{"id":1,"method":"model/list"}',
    }), tmp_path, CodexDesktopCaptureConfig.defaults())

    assert written is False


def test_cdp_binding_payload_hashes_sensitive_metadata_fields(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    route_cdp_binding_payload(json.dumps({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "frame_text": '{"id":1,"method":"/Users/alice/private","result":{"tool_name":"mcp__chrome__Bearer sk-secret","content":"RAW_BROWSER_TEXT"}}',
        "model": "Bearer sk-model",
        "request_path": "/Users/alice/repo",
    }), tmp_path, config)

    app_server = (tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8")
    tool_events = (tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8")
    dumped = app_server + tool_events

    assert "Bearer sk-model" not in dumped
    assert "/Users/alice" not in dumped
    assert "mcp__chrome__Bearer" not in dumped
    assert "RAW_BROWSER_TEXT" not in dumped
    assert "model_hash" in dumped
    assert "request_path_hash" in dumped
    assert "tool_name_hash" in dumped


def test_direct_renderer_model_picker_hashes_sensitive_selected_model(tmp_path: Path):
    route_capture_event({
        "type": "model_picker",
        "selected_model": "Bearer sk-model",
    }, tmp_path, CodexDesktopCaptureConfig.defaults())

    dumped = (tmp_path / "model_picker.jsonl").read_text(encoding="utf-8")

    assert "Bearer sk-model" not in dumped
    assert "selected_model_hash" in dumped


def test_runtime_hook_extracts_websocket_correlation_from_frame_body_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "websocket-correlation-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const posted = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.console = { info() {} };",
            "globalThis.fetch = function originalFetch(url, init) {",
            "  if (url === 'http://127.0.0.1:18765/codex-desktop-capture-v2') posted.push(JSON.parse(init.body));",
            "  return Promise.resolve({ ok: true });",
            "};",
            "globalThis.WebSocket = function WebSocket(url, protocol) { this.url = url; this.protocol = protocol; this.listeners = {}; };",
            "globalThis.WebSocket.prototype.addEventListener = function (name, cb) { this.listeners[name] = cb; };",
            "globalThis.WebSocket.prototype.send = function originalSend() { return 'send-return'; };",
            build_runtime_hook_script(18765),
            "const ws = new globalThis.WebSocket('ws://local/app-server?x-client-request-id=query-request', 'x-codex-window-id=window-1');",
            "ws.send('{\"id\":2,\"method\":\"turn/start\",\"params\":{\"thread_id\":\"thread-1\"}}');",
            "ws.listeners.message({ data: '{\"id\":2,\"result\":{\"ok\":true,\"turn_id\":\"turn-1\"}}' });",
            "setTimeout(() => {",
            "  if (!posted.some((event) => event.correlation_ids && event.correlation_ids.x_client_request_id === 'query-request' && event.correlation_ids.window_id === 'window-1' && event.correlation_ids.thread_id === 'thread-1')) process.exit(61);",
            "  if (!posted.some((event) => event.direction === 'app_server_to_desktop' && event.correlation_ids && event.correlation_ids.turn_id === 'turn-1')) process.exit(62);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_does_not_emit_raw_frame_to_page_or_console_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "redaction-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "let dispatched = null;",
            "let logged = null;",
            "globalThis.dispatchEvent = (event) => { dispatched = event.detail; return true; };",
            "globalThis.console = { info(name, event) { logged = event; } };",
            "globalThis.fetch = function originalFetch() { return 'ok'; };",
            build_runtime_hook_script(18765),
            "globalThis.__zhumengCodexDesktopCaptureV2.observe({ type: 'app_server_frame', frame_text: 'SECRET_PROMPT', correlation_ids: { thread_id: 'thread-1' } });",
            "if (JSON.stringify(dispatched).includes('SECRET_PROMPT')) process.exit(21);",
            "if (JSON.stringify(logged).includes('SECRET_PROMPT')) process.exit(22);",
            "if (JSON.stringify(dispatched).includes('thread-1')) process.exit(23);",
            "if (dispatched.frame_chars !== 13) process.exit(24);",
            "if (dispatched.correlation_ids_present !== true) process.exit(25);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_public_event_uses_whitelisted_summary_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "public-whitelist-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "let dispatched = null;",
            "globalThis.dispatchEvent = (event) => { dispatched = event.detail; return true; };",
            "globalThis.console = { info() {} };",
            "globalThis.fetch = function originalFetch() { return 'ok'; };",
            build_runtime_hook_script(18765),
            "globalThis.__zhumengCodexDesktopCaptureV2.observe({ type: 'tool.lifecycle', tool_name: 'mcp__chrome__read_page', result_text: 'SECRET_PAGE', nested: { raw_content: 'SECRET_RAW' } });",
            "const dumped = JSON.stringify(dispatched);",
            "if (dumped.includes('SECRET_PAGE') || dumped.includes('SECRET_RAW')) process.exit(51);",
            "if (dispatched.type !== 'tool.lifecycle') process.exit(52);",
            "if (dispatched.tool_name !== 'mcp__chrome__read_page') process.exit(53);",
            "if ('nested' in dispatched) process.exit(54);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_runtime_hook_derives_subagent_registration_from_frames_and_console_when_node_is_available(tmp_path: Path):
    node = shutil.which("node")
    if node is None:
        return
    script_path = tmp_path / "subagent-registration-hook.js"
    script_path.write_text(
        "\n".join([
            "globalThis.CustomEvent = class CustomEvent { constructor(name, init) { this.name = name; this.detail = init.detail; } };",
            "const posted = [];",
            "const originalInfos = [];",
            "globalThis.dispatchEvent = () => true;",
            "globalThis.fetch = function originalFetch(url, init) {",
            "  if (url === 'http://127.0.0.1:18765/codex-desktop-capture-v2') posted.push(JSON.parse(init.body));",
            "  return Promise.resolve({ ok: true });",
            "};",
            "globalThis.console = { info(...args) { originalInfos.push(args); }, warn(...args) { originalInfos.push(args); } };",
            "globalThis.WebSocket = undefined;",
            build_runtime_hook_script(18765),
            "globalThis.fetch('/rpc', { body: JSON.stringify({ id: 1, method: 'item/started', params: { conversation_id: 'conversation-secret', thread_id: 'thread-secret', prompt: 'SECRET_PROMPT' } }) });",
            "globalThis.console.warn('unknown conversation: conversation-secret SECRET_PROMPT');",
            "globalThis.console.info('maybe_resume_success conversation-secret');",
            "setTimeout(() => {",
            "  const registrations = posted.filter((event) => event.type === 'subagent.registration');",
            "  if (!registrations.some((event) => event.event_name === 'item/started')) process.exit(121);",
            "  if (!registrations.some((event) => event.event_name === 'console' && event.message === 'unknown conversation')) process.exit(122);",
            "  if (!registrations.some((event) => event.event_name === 'console' && event.message === 'maybe_resume_success')) process.exit(123);",
            "  const dumped = JSON.stringify(registrations);",
            "  if (dumped.includes('SECRET_PROMPT') || dumped.includes('conversation-secret') || dumped.includes('thread-secret')) process.exit(124);",
            "}, 20);",
        ]),
        encoding="utf-8",
    )

    completed = subprocess.run([node, str(script_path)], capture_output=True, text=True, check=False)

    assert completed.returncode == 0, completed.stderr


def test_capture_event_router_writes_subagent_registration_and_report(capsys, tmp_path: Path):
    trace_dir = tmp_path / "traces"
    config = CodexDesktopCaptureConfig.defaults()
    route_capture_event({
        "type": "subagent.registration",
        "event_name": "item/started",
        "conversation_id": "conversation-secret",
        "thread_id": "thread-secret",
        "ts": "2026-06-03T11:48:40.000Z",
    }, trace_dir, config)
    route_capture_event({
        "type": "subagent.registration",
        "event_name": "console",
        "conversation_id": "conversation-secret",
        "thread_id": "thread-secret",
        "message": "unknown conversation: conversation-secret",
        "ts": "2026-06-03T11:48:41.000Z",
    }, trace_dir, config)
    route_capture_event({
        "type": "subagent.registration",
        "event_name": "console",
        "conversation_id": "conversation-secret",
        "thread_id": "thread-secret",
        "message": "maybe_resume_success",
        "ts": "2026-06-03T11:48:42.000Z",
    }, trace_dir, config)

    rows = (trace_dir / "subagent_registration.jsonl").read_text(encoding="utf-8")
    assert "conversation-secret" not in rows
    assert "unknown conversation" not in rows
    assert "message_class" in rows

    exit_code = main(["codex", "capture", "report", "--trace-dir", str(trace_dir)])

    assert exit_code == 0
    data = parse_output(capsys)
    assert data["subagent_registration_race_suspected"] is True
    assert data["first_item_before_conversation_registered"] is True
    assert data["unknown_conversation_count"] == 1
    assert data["maybe_resume_success_after_unknown_conversation"] is True


def test_capture_event_router_derives_subagent_registration_and_deferred_tool_search_from_frame(tmp_path: Path):
    route_capture_event({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "seq": 9,
        "frame_text": json.dumps({
            "id": 9,
            "method": "item/started",
            "params": {
                "conversation_id": "conversation-secret",
                "thread_id": "thread-secret",
                "prompt": "SECRET_PROMPT",
            },
        }),
    }, tmp_path, CodexDesktopCaptureConfig.defaults())
    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "seq": 10,
        "frame_text": json.dumps({
            "id": 10,
            "result": {
                "type": "tool_search_call",
                "call_id": "call-secret",
                "arguments": {"query": "SECRET_QUERY"},
            },
        }),
    }, tmp_path, CodexDesktopCaptureConfig.defaults())
    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "seq": 11,
        "frame_text": json.dumps({
            "id": 11,
            "result": {
                "type": "tool_search_output",
                "call_id": "call-secret",
                "tools": [{
                    "name": "spawn_agent",
                    "input_schema": {"properties": {"model": {"enum": ["claude-sonnet-4-6"]}}},
                    "description": "SECRET_DESCRIPTION",
                }],
            },
        }),
    }, tmp_path, CodexDesktopCaptureConfig.defaults())

    registration = (tmp_path / "subagent_registration.jsonl").read_text(encoding="utf-8")
    deferred = (tmp_path / "deferred_tool_search.jsonl").read_text(encoding="utf-8")

    assert "item/started" in registration
    assert "conversation-secret" not in registration
    assert "thread-secret" not in registration
    assert "SECRET_PROMPT" not in registration
    assert "tool_search_call" in deferred
    assert "tool_search_output" in deferred
    assert "spawn_agent" in deferred
    assert "claude-sonnet-4-6" in deferred
    assert "call-secret" not in deferred
    assert "SECRET_QUERY" not in deferred
    assert "SECRET_DESCRIPTION" not in deferred


def test_capture_event_router_preserves_safe_deferred_tool_family_names(tmp_path: Path):
    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "seq": 11,
        "frame_text": json.dumps({
            "id": 11,
            "result": {
                "type": "tool_search_output",
                "tools": [
                    {"name": "browser", "tools": [{"name": "navigate", "description": "SECRET_URL"}]},
                    {"name": "computer_use", "tools": [{"name": "list_apps"}]},
                    {"name": "documents", "tools": [{"name": "redline"}]},
                    {"name": "unsafe secret name with spaces", "tools": [{"name": "Bearer sk-secret"}]},
                ],
            },
        }),
    }, tmp_path, CodexDesktopCaptureConfig.defaults())

    deferred = (tmp_path / "deferred_tool_search.jsonl").read_text(encoding="utf-8")

    assert "browser" in deferred
    assert "navigate" in deferred
    assert "computer_use" in deferred
    assert "list_apps" in deferred
    assert "documents" in deferred
    assert "redline" in deferred
    assert "SECRET_URL" not in deferred
    assert "unsafe secret name with spaces" not in deferred
    assert "Bearer sk-secret" not in deferred

def test_capture_event_router_filters_unsafe_deferred_tool_model_enums(tmp_path: Path):
    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "seq": 11,
        "frame_text": json.dumps({
            "id": 11,
            "result": {
                "type": "tool_search_output",
                "tools": [{
                    "name": "spawn_agent",
                    "input_schema": {
                        "properties": {
                            "model": {"enum": ["claude-sonnet-4-6", "Bearer sk-secret", "prompt with spaces"]}
                        }
                    },
                }],
            },
        }),
    }, tmp_path, CodexDesktopCaptureConfig.defaults())

    deferred = (tmp_path / "deferred_tool_search.jsonl").read_text(encoding="utf-8")

    assert "claude-sonnet-4-6" in deferred
    assert "Bearer sk-secret" not in deferred
    assert "prompt with spaces" not in deferred


def test_capture_event_router_writes_expected_jsonl(tmp_path: Path):
    route_capture_event({"type": "websocket.send", "argCount": 1}, tmp_path)
    route_capture_event({"type": "tool.lifecycle", "tool_name": "mcp__exa__web_search_exa"}, tmp_path)
    route_capture_event({"type": "model_picker", "selected_model": "deepseek-v4-pro"}, tmp_path)

    assert (tmp_path / "app_server_v2.jsonl").exists()
    assert (tmp_path / "tool_lifecycle.jsonl").exists()
    assert (tmp_path / "model_picker.jsonl").exists()


def test_capture_event_router_shapes_runtime_app_server_frame_with_shared_key(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    route_capture_event({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "seq": 7,
        "frame_text": '{"id":1,"method":"model/list","params":{"prompt":"SECRET_PROMPT"}}',
        "correlation_ids": {"x_client_request_id": "request-1"},
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
    }, tmp_path, config)

    event = json.loads((tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert event["protocol"] == "app_server_v2"
    assert event["method"] == "model/list"
    assert event["seq"] == 7
    assert event["payload_policy"] == "shape_only"
    assert event["payload_hash"].startswith("hmac-sha256:")
    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert "SECRET_PROMPT" not in json.dumps(event)
    assert "request-1" not in json.dumps(event)


def test_capture_event_router_extracts_frame_body_correlation_without_leaking_raw_ids(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    route_capture_event({
        "type": "app_server_frame",
        "direction": "desktop_to_app_server",
        "frame_text": json.dumps({
            "id": 1,
            "method": "turn/start",
            "params": {
                "x-client-request-id": "request-1",
                "thread_id": "thread-1",
            },
        }),
    }, tmp_path, config)

    event = json.loads((tmp_path / "app_server_v2.jsonl").read_text(encoding="utf-8").splitlines()[0])
    dumped = json.dumps(event)

    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert event["correlation_hashes"]["thread_id_hash"].startswith("hmac-sha256:")
    assert "request-1" not in dumped
    assert "thread-1" not in dumped


def test_capture_event_router_derives_model_picker_and_tool_events_from_frames(tmp_path: Path):
    config = CodexDesktopCaptureConfig.defaults()

    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "frame_text": json.dumps({
            "id": 1,
            "result": {
                "models": [{"model": "deepseek-v4-pro", "displayName": "DeepSeek", "hidden": False}]
            },
        }),
    }, tmp_path, config)
    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "frame_text": json.dumps({
            "id": 2,
            "result": {
                "tool_name": "mcp__chrome__read_page",
                "content": "RAW_BROWSER_TEXT",
            },
        }),
    }, tmp_path, config)

    model_event = json.loads((tmp_path / "model_picker.jsonl").read_text(encoding="utf-8").splitlines()[0])
    tool_event = json.loads((tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert model_event["event_type"] == "model_picker_state"
    assert model_event["app_server_model_ids"] == ["deepseek-v4-pro"]
    assert tool_event["event_type"] == "tool_lifecycle"
    assert tool_event["content_class"] == "browser_content"
    assert "RAW_BROWSER_TEXT" not in json.dumps(tool_event)


def test_capture_event_router_adds_tool_content_policy_proof_fields(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)
    route_capture_event({
        "type": "tool.lifecycle",
        "tool_name": "mcp__chrome__read_page",
        "content_class": "browser_content",
        "result_chars": 1234,
        "sent_back_to_model": True,
    }, tmp_path, config)

    event = json.loads((tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert event["event_type"] == "tool_lifecycle"
    assert event["content_class"] == "browser_content"
    assert event["policy_decision"] == "shape_only"
    assert event["redaction_reason"] == "default_no_user_content"
    assert event["schema_hash"].startswith("hmac-sha256:")
    assert event["result_hash"].startswith("hmac-sha256:")
    assert event["result_chars"] == 1234
    assert event["sent_back_to_model"] is True


def test_capture_event_router_preserves_ui_matrix_and_trace_correlation_fields(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)
    route_capture_event({
        "type": "tool.lifecycle",
        "tool_name": "shell_exec",
        "content_class": "command_output",
        "result_chars": 88,
        "sent_back_to_model": True,
        "desktop_trace_id": "cd_runtime",
        "correlation_ids": {"x_client_request_id": "request-1"},
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "ui_matrix": {
            "command_collapsed": True,
            "command_expandable": True,
            "tool_detail_expandable": False,
            "diff_entry_visible": True,
            "file_open_action_available": True,
        },
        "degraded_reason": "file_open_not_supported_for_remote_diff",
        "pass_fail_rule": "command rows must stay collapsed but expandable",
    }, tmp_path, config)

    event = json.loads((tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert event["ui_matrix"]["command_collapsed"] is True
    assert event["ui_matrix"]["file_open_action_available"] is True
    assert event["degraded_reason"] == "file_open_not_supported_for_remote_diff"
    assert event["pass_fail_rule"] == "command rows must stay collapsed but expandable"
    assert event["desktop_trace_id"] == "cd_runtime"
    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert event["trace_correlation"]["strategy"] == "shared_hash"
    assert event["trace_correlation"]["link_ready"] is True


def test_capture_event_router_rehashes_renderer_result_hash_with_shared_key(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)
    route_capture_event({
        "type": "tool.lifecycle",
        "tool_name": "mcp__chrome__read_page",
        "content_class": "browser_content",
        "result_hash": "sha256:" + "a" * 64,
        "result_chars": 12,
    }, tmp_path, config)

    event = json.loads((tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert event["result_hash"].startswith("hmac-sha256:")
    assert event["result_hash"] != "sha256:" + "a" * 64
    assert event["renderer_result_hash_present"] is True


def test_capture_event_router_derives_tool_trace_chain_and_ui_matrix_from_frame(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    route_capture_event({
        "type": "app_server_frame",
        "direction": "app_server_to_desktop",
        "desktop_trace_id": "cd_frame_1",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "frame_text": json.dumps({
            "id": 2,
            "params": {
                "x-client-request-id": "request-1",
            },
            "result": {
                "tool_name": "shell_exec",
                "content": "RAW_COMMAND_OUTPUT",
                "ui_matrix": {
                    "command_collapsed": True,
                    "command_expandable": True,
                    "tool_detail_expandable": True,
                    "diff_entry_visible": False,
                    "file_open_action_available": False,
                },
                "degraded_reason": "diff_entry_missing",
                "pass_fail_rule": "diff rows must be visible for file mutations",
            },
        }),
    }, tmp_path, config)

    event = json.loads((tmp_path / "tool_lifecycle.jsonl").read_text(encoding="utf-8").splitlines()[0])

    assert event["desktop_trace_id"] == "cd_frame_1"
    assert event["ui_matrix"]["tool_detail_expandable"] is True
    assert event["degraded_reason"] == "diff_entry_missing"
    assert event["pass_fail_rule"] == "diff rows must be visible for file mutations"
    assert event["trace_correlation"]["strategy"] == "shared_hash"
    assert event["correlation_hashes"]["x_client_request_id_hash"].startswith("hmac-sha256:")


def test_capture_installation_enabled_checks_current_app_hash(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures", correlation_hash_key_file=key)
    first_app = tmp_path / "Codex.app"
    second_app = tmp_path / "OtherCodex.app"
    (first_app / "Contents" / "Resources").mkdir(parents=True)
    (second_app / "Contents" / "Resources").mkdir(parents=True)
    (first_app / "Contents" / "Resources" / "app.asar").write_bytes(b"asar")
    (second_app / "Contents" / "Resources" / "app.asar").write_bytes(b"asar")
    install_capture_hook(first_app, config)

    assert capture_installation_enabled(first_app, config) is True
    assert capture_installation_enabled(second_app, config) is False


def test_capture_install_refuses_missing_asar_and_enabled_detects_asar_change(tmp_path: Path):
    config = CodexDesktopCaptureConfig.defaults(base_dir=tmp_path / "captures")
    missing_app = tmp_path / "Missing.app"
    (missing_app / "Contents" / "Resources").mkdir(parents=True)

    try:
        install_capture_hook(missing_app, config)
    except ValueError as err:
        assert "app.asar" in str(err)
    else:
        raise AssertionError("missing app.asar should be refused")

    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    asar = resources / "app.asar"
    asar.write_bytes(b"first")
    install_capture_hook(app, config)
    assert capture_installation_enabled(app, config) is True
    asar.write_bytes(b"second")
    assert capture_installation_enabled(app, config) is False
