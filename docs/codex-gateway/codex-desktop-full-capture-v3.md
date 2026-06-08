# Codex Desktop Full Capture Coverage Matrix V3

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:writing-plans first if you intend to extend or implement this matrix end-to-end. Use superpowers:executing-plans for individual checkpoints.

**Goal:** Produce an evidence-backed plan that lets us fully reconstruct Codex Desktop behavior locally. Cover every observable signal source (renderer, app-server v2, native pipes, OAuth/device pairing, control-plane HTTP, telemetry exhaust, auto-update, binary baseline) and define how each one is captured, redacted, replayed, and version-pinned.

**Architecture stance:**

- shape-only is the default everywhere; raw payloads are gated behind explicit local unlocks.
- the renderer-side bridge stays the primary return path (CDP `Runtime.addBinding`); native HTTP/WS/beacon to `127.0.0.1` remain fallbacks.
- the gateway-side capture continues to own upstream provider protocol shape; desktop-side capture owns app-server v2 + Electron IPC + native pipes.
- TLS interception is opt-in and only applies to first-party Codex Desktop traffic for self-replay; it is not used against third-party providers and never overlaps with the Claude anti-ban posture.
- nothing in this matrix replaces app-server v2; the gateway remains the custom Responses provider, while desktop capture observes and reports app-server behavior.

**Tech Stack:** Python `zhumeng-agent` adapters, Go `codex-gateway` capture package, CDP over the official Codex Desktop `--remote-debugging-port`, optional Frida / DTrace / dyld-interpose for native pipe taps, mitmproxy for opt-in MITM of self-traffic, Sparkle/codesign tooling for binary baselines.

---

## Native Parity Standard For Capture

- every Codex Desktop capability that affects user-visible behavior must have at least shape-only capture coverage.
- every signal we capture must round-trip into a redacted report and a replayable fixture; no signal is allowed to remain in raw-only form.
- every capture surface must declare its raw-unlock requirement, retention rule, and whether it depends on Sparkle/build-flavor pinning.
- any capture failure must produce a doctor-reported gap rather than silently degrading; gaps are tracked in the matrix below until closed.

## Coverage Matrix

The matrix is the source of truth. Each row is a signal source. Add a row before adding capture code; mark `Status` only after the coverage is shipped and verified.

| Row | Signal source | Current coverage | Gap (must close) | Capture mechanism | Redaction default | Raw unlock | Status |
|-----|---------------|------------------|------------------|-------------------|-------------------|------------|--------|
| C1  | app-server v2 method enumeration (`thread/*`, `turn/*`, `plugin/*`, `app/list`, `mcpServer/*`, `fs/*`, plus `account/*`, `skills/*`, `experimentalFeature/list`, `config/batchWrite`, `externalAgentConfig/*`, `attestation/generate`, `connectors/*`, `command/exec/*`, `item/*`, `hook/*`, `serverRequest/resolved`, `model/{rerouted,verification}`, `process/*`, `rawResponseItem/completed`, `windows/*`, `windowsSandbox/*`, `fuzzyFileSearch/*`, `mcpServer/oauthLogin/completed`, `remoteControl/status/changed`, `thread/realtime/*`, `thread/{goal,name,status,tokenUsage}/*`, `turn/{diff,plan}/updated`) | partial; only the original 6 namespaces are listed in spec | full method/notification table with parameter shape and notification direction | renderer addBinding hook around `appServerClient.sendAppServerRequest`, `sendRequest`, `sendNotification`, `registerInternalNotificationHandler` | shape-only key set + length + sha256 of large strings | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C2  | renderer fetch/SSE to chatgpt.com `/backend-api`, `api.openai.com/auth`, `api.openai.com/profile`, `auth.openai.com`, `developers.openai.com/codex/*`, `registry.npmjs.org/@openai%2Fcodex` | none | full HTTP shape + headers + body sketch + SSE frame order | CDP `Network.requestWillBeSent`, `Network.responseReceived`, `Network.loadingFinished`, `Network.eventSourceMessageReceived`; optional `Fetch.requestPaused` for body capture | header allowlist; bodies hashed unless unlocked | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NETWORK` | gap |
| C3  | OAuth / PKCE / device pairing (`S256`, `attestation/generate`, `https://api.openai.com/auth`, `https://api.openai.com/mfa`, `chatgpt_account_id`, `chatgpt_account_user_id`, ECDSA-P256-SHA256 enroll for `electron-remote-control-client-enrollments`, `hardware_secure_enclave` / `hardware_tpm` / `os_protected_nonextractable`) | redactor only | full token payload field map (without secret values), enroll keypair lifecycle, refresh path | combine C1 + C2 hooks with a dedicated OAuth state machine recorder | strip JWT body but keep claim keys; never persist refresh tokens | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_AUTH` | gap |
| C4  | Sentry telemetry (`https://6719eaa18601933a26ac21499dcaba2f@o33249.ingest.us.sentry.io/4510999349821440`, `codex_desktop:trigger-sentry-test`, `codex_desktop:get-sentry-init-options`) | none | DSN, transport mode (envelope vs store), event/transaction/profile shape, scrubbing config | renderer-side `getSentryInitOptions` IPC tap + main-process exporter shim | tag/context shape, no message body | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_TELEMETRY` | gap |
| C5  | OpenTelemetry exporters (`@opentelemetry/instrumentation-*`, `pino.logger.level`, MCP attributes `mcp.protocol.version`, `mcp.session.id`, `mcp.request.id`, plus the AspNetCore/AWS/etc attribute schemas embedded in `app-session-O7kcZj7R.js`) | none | exporter endpoint, batch size, span name surface, attribute key surface | replace transport with a local sink shim during capture sessions | attribute key set + cardinality only | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_TELEMETRY` | gap |
| C6  | Sparkle auto-update channel (strings `Sparkle`, `UpdateR`, `updater` in `main-BwqrdVu3.js`) and the `https://oaisidekickupdates.blob.core.windows.net/owl` blob feed | none | feed URL, appcast format, signature verification path, rollback behavior, Codex companion update channel | static updater inventory plus opt-in process/path-scoped MITM for the feed host; signed-payload archive into a `data/codex-desktop-baselines/<version>/sparkle/` tree | feed XML and archive metadata; never persist Apple/codesign private keys | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_UPDATE` | gap |
| C7  | Codex Computer Use native pipe (`SKY_CUA_SERVICE_PATH`, `SKY_CUA_NATIVE_PIPE`, `SKY_CUA_NATIVE_PIPE_DIRECTORY`, macOS `Codex Computer Use.app`, Windows `node_modules/@oai/sky/bin/windows/codex-computer-use.exe`, Windows helper transport at `node_modules/@oai/sky/dist/project/cua/sky_js/src/targets/windows/internal/helper_transport.js`) | renderer-only `start/onCDPEvent/onDownloadChange/moveMouse` shape | full JSON-RPC over named pipe; CDP forwarded events; download lifecycle | passive tap by intercepting pipe `connect/open` plus `read/write/send/recv` frame APIs; on macOS prefer signed Frida helper or DTrace where permitted; on Windows use ETW or named-pipe duplication | command method shape; raw screenshots only when unlocked | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_CUA` | gap |
| C8  | Browser-use native pipe (`/tmp/codex-browser-use`, `\\.\pipe\codex-browser-use`, `CODEX_BROWSER_USE_PEER_AUTHORIZATION`, `CODEX_BROWSER_USE_DEFAULT_VIEWPORT_SIZE`, trustedBrowserClientSha256s gate, `Page.navigationBlocked`) | none | JSON-RPC method table, CDP relay events, peer auth handshake | passive pipe tap + CDP relay mirror | URL allowlist for shape; full body only when unlocked | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_BROWSER` | gap |
| C9  | node_repl native pipe and trust gates (`NODE_REPL_NATIVE_PIPE_CONNECT_TIMEOUT_MS`, `NODE_REPL_TRUST_ALL_CODE`, `NODE_REPL_TRUSTED_BROWSER_CLIENT_SHA256S`, `NODE_REPL_REQUEST_META`, `NODE_REPL_TRACE_META`, `NODE_REPL_NODE_MODULE_DIRS`, `NODE_REPL_NODE_PATH`, `NODE_REPL_BROWSER_CLIENT_MARKETPLACE_NAME`, `NODE_REPL_EXTERNAL_MODULE`) | none | invocation lifecycle, allowlist evaluation, code-execution payload shape | pipe tap + env capture at process spawn | code-stripped shape; raw code only with unlock | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NODE_REPL` | gap |
| C10 | Electron IPC channels (`codex_desktop:*` set: `browser-sidebar-runtime-message`, `get-build-flavor`, `get-fast-mode-rollout-metrics`, `get-sentry-init-options`, `get-shared-object-snapshot`, `get-system-theme-variant`, `mcp-app-sandbox-guest-message`, `mcp-app-sandbox-host-message`, `message-for-view`, `message-from-view`, `show-application-menu`, `show-context-menu`, `system-theme-variant-updated`, `trigger-sentry-test`, `worker:*:from-view`, `system-theme-variant-updated`) and the explicit method ids `create-worktree`, `delete-worktree`, `resolve-worktree-for-thread`, `set-worktree-owner-thread`, `codex-worktrees`, `worker-exit`, `process-output-delta`, `fs-watch-changed`, `fs-watch-closed`, `query-cache-invalidate`, `ide-context`, `git-origins`, `getFrontmostWindow`, `computer-use-capture-updated` | none | full ipcMain.handle / ipcMain.on / ipcRenderer.invoke / ipcRenderer.on inventory with payload shape | three-layer inventory: static extraction from `main-*.js` / `comment-preload.js`, runtime `ipcMain`/`ipcRenderer` wrappers when hookable, and dev-only preload patch as fallback | shape-only; large blobs hashed | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C11 | macOS XPC, Windows codesign / signing helpers, launchd plists, `Codex Computer Use.app` bundle inventory | none | per-version map of helper executables, code signature, entitlements, launchd plist (when present) | scripted `codesign --display --requirements - --verbose=4`, `otool -L`, `plutil`, `asar list`, archived under `data/codex-desktop-baselines/<version>/binary/` | binary metadata only; never persist signing private keys | n/a | gap |
| C12 | app.asar binary baseline (per-version sha256, asar listing, deobfuscated `main-*.js`, `app-session-*.js`, `comment-preload.js` strings + symbol diff) | partial; the unpacked files exist at the repo root but are not pinned per release | a per-version archived baseline + diff between releases | extend `zhumeng-agent codex baseline` to include `--mode binary`; webcrack/Babel pass for `comment-preload.js` (~36MB); store under `data/codex-desktop-baselines/<version>/asar/` | hashes + listings only; raw deobfuscated source kept locally and gitignored | n/a | gap |
| C13 | Windows-specific surfaces (`CODEX_ELECTRON_ENABLE_WINDOWS_COMPUTER_USE`, Windows sandbox setup, Windows helper transport, `windowsSandbox/setupCompleted`, `windows/worldWritableWarning`) | none | Windows-only handshake and sandbox lifecycle | reuse C7/C8/C10 hooks plus ETW where pipe taps are not available | shape-only | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_CUA` | gap |
| C14 | WebSocket / SSE frame ordering for app-server notifications (`thread/*`, `turn/*`, `item/*`, `process/outputDelta`, `command/exec/outputDelta`, `thread/realtime/*`) | renderer addBinding shape only | shape-only frame order, latency, drop detection (no body) | extend C1 hook to record event-order timeline; capture WebSocket via CDP `Network.webSocketFrame*` | event names + timestamps + hash | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C15 | Local control surfaces and developer URLs (`http://localhost:5175/`, `http://localhost:8000/api`, `http://localhost:8969/stream`, dev parent-pid watchdog `CODEX_ELECTRON_DEV_PARENT_PID`, `CODEX_ELECTRON_DEV_WEBVIEW_PID`, build-flavor IPC, dev-only `CODEX_ELECTRON_DESKTOP_FEATURE_OVERRIDES`) | none | per-flavor dev URL inventory and feature override schema | C2 + C10 hooks + env capture | shape-only | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_DEV` | gap |
| C16 | Realtime audio / SDP path (`thread/realtime/sdp`, `thread/realtime/outputAudio/delta`, `thread/realtime/transcript/*`) | none | SDP shape, transcript event order, audio chunk size and codec class | C1 + C2 hooks; tagged separately because audio data is large and sensitive | shape-only; binary audio never persisted in shape mode | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_REALTIME` | gap |
| C17 | app-server v2 transport/protocol semantics | none | startup mode, endpoint/pipe/socket, handshake/version, request id, cancel/abort, error schema, notification registration, reconnect, permission, concurrency and ordering semantics | static bundle inventory + renderer/main hooks + connection lifecycle capture | shape-only ids with keyed HMAC | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C18 | local persistent state and secrets storage (`~/Library/Application Support/Codex`, Keychain/Credential Manager, IndexedDB/LocalStorage, session JSONL, thread/window/worktree/cache/model catalog state) | none | schema, filenames, keychain item names, migration rules, TTL, cache invalidation, storage-to-app-server mapping | filesystem schema scan + OS credential item name inventory + renderer storage API shape capture | schema/hash only; secret values never persisted | n/a | gap |
| C19 | static bundle inventory coverage denominator (`main-*.js`, `app-session-*.js`, `comment-preload.js`, asar listing) | partial manual grep | extract expected URL, IPC, env var, native executable, app-server method, notification, telemetry, updater symbols; compare dynamic `seen_count` vs static denominator | `codex capture baseline --mode binary` static extractor + report coverage diff | symbol names + counts; raw deobfuscated source kept local/gitignored | n/a | gap |
| C20 | MCP / plugins / subagents / skills | partial through C1 | plugin manifest/cache/marketplace schema, MCP stdio/http/oauth transports, sandbox app messages, `multi_agent_v1.*` lifecycle, skill trigger/loader/cache, tool discovery/deferred tool output families | C1/C10/C19 plus plugin filesystem baseline and gateway tool-event correlation | shape-only; tool outputs hashed | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C21 | remote-control protocol | partial status event + enrollment hints | pairing lifecycle, permission prompts, command protocol, push/WebSocket channel, enrollment storage, revoke/logout path | C1/C2/C3/C10/C18 combined recorder | shape-only; enrollment keys never persisted | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_AUTH` | gap |
| C22 | external-agent import (`externalAgentConfig/*`, Claude Code/Cowork import) | partial through C1 | detect/import schema, source discovery, transcript/session migration, generated-file staging, completion notifications, failure/retry rules | C1 + C18 + static import adapter inventory | path HMAC + schema only | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C23 | connected apps / connectors / cloud entitlements | partial host-level C2 | connector registry, OAuth consent/scopes, entitlement/plan/feature flag/model registry/device config/org switching endpoints and response shape | C2 path-level inventory + C3 auth state + C18 local connector cache | endpoint path + schema; tokens and user data stripped | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NETWORK` | gap |
| C24 | renderer/UI behavior state | partial IPC C10 | route/view state, command palette, menus/context menus, keyboard shortcuts, webview/worker lifecycle, shared-object snapshots, UI-to-app-server chains | C10 IPC + renderer route/store snapshot shape + static React/shared-object inventory | shape-only snapshots; labels hashed when user-derived | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | gap |
| C25 | local logs, Crashpad, console, error boundaries | partial telemetry only | Chromium Crashpad, main/renderer console, pino/local log files, error-boundary reports, crash dump scrub policy | log schema scan + telemetry shim + crash directory inventory | class/key set only; dumps never persisted by default | `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_TELEMETRY` | gap |

Add new rows whenever a new signal surface is discovered. Treat the matrix as a living document; every capture PR must add or update at least one row.

## Files

- Add: `docs/codex-gateway/codex-desktop-full-capture-v3.md` (this file).
- Add: `docs/codex-gateway/protocol-capture-design.md` (architecture skeleton; see plan checkpoints below).
- Add: `docs/codex-gateway/protocol-capture-implementation-plan.md` (checkpoint-driven plan; see plan checkpoints below).
- Modify: `docs/codex-gateway/README.md`
  - cross-link the new V3 matrix beside the existing design and implementation references.
- Modify: `docs/codex-gateway/smoke.md`
  - add a coverage smoke section keyed by matrix row id (`C1`-`C16`).
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`
  - add app-server v2 method enumerator hook (C1) and an event-order timeline (C14).
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_shape.py`
  - extend the report shape with a `coverage_matrix_rows[]` array; each row records `id`, `status`, `last_seen_ts`, `redaction_mode`, and `unlock_used`.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_redact.py`
  - add per-row redaction modes; never persist secrets even when raw is unlocked.
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_network.py`
  - CDP `Network.*` and `Fetch.requestPaused` capture for C2/C3/C4/C5/C6/C15/C16.
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_native_pipe.py`
  - shared pipe-tap interface for C7/C8/C9/C13.
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_baseline_binary.py`
  - per-version asar listing, sha256, codesign metadata for C11/C12.
- Modify: `backend/internal/service/codex_gateway_capture_links.go`
  - link gateway captures to matrix-row evidence so a Desktop trace dir can be diffed against a backend trace dir per row.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/cli.py`, `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
  - surface coverage gaps as actionable doctor warnings; extend `codex capture baseline --mode binary` and add the planned `codex capture matrix` CLI subcommand.

## Roadmap

The roadmap is ordered by evidence dependency. Static coverage and IPC/app-server observability come before deeper network, telemetry, and native-pipe capture. A row moves to `shipped` only after it has (1) static denominator where applicable, (2) dynamic evidence, (3) redaction-negative tests, and (4) replay/golden fixture semantics or an explicit reason that replay is not meaningful for that row.

- [ ] **R0: Static denominator and binary baseline first (C6, C11, C12, C19)**

  Extend the existing `zhumeng-agent codex capture baseline --out <dir> [--app <app>]` command with a planned `--mode binary` option instead of inventing a separate `codex baseline binary` namespace. Extract asar listing, sha256, codesign/entitlements, updater symbols, URL/IPC/env/native executable/app-server symbols, and per-version diffs. The report must include `coverage_denominator_count`, `seen_count`, `unseen_required[]`, and `sampled_optional[]`.

- [ ] **R1: app-server v2 transport + method surface + event order (C1, C14, C17)**

  First capture connection semantics: endpoint/pipe/socket, handshake/version, request id, cancel/abort, reconnect, error schema, notification registration, permissions, ordering. Then wrap `sendAppServerRequest`, `sendRequest`, `sendNotification`, and notification handlers where hookable. If renderer globals are closure-local, fall back to preload/main-process discovery documented by C10/C19. Canonical event file remains the current `app_server_v2.jsonl`; aliases may be written later but tests must read the existing name until code is changed.

- [ ] **R1b: Electron IPC and UI chain inventory (C10, C24)**

  Run IPC inventory alongside R1, not after telemetry. Use static extraction plus runtime wrappers. Treat `comment-preload.js` patching as invasive dev-only fallback requiring explicit user confirmation, backup/restore, codesign awareness, and Sparkle drift detection. Link IPC events to app-server calls when a high-confidence shared HMAC or request-id chain exists.

- [ ] **R2: CDP Network / Fetch path-level control-plane inventory (C2, C3, C4, C5, C6, C15, C16, C21, C23)**

  Persist body shape/hash by default. Raw bodies require the row-specific `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_*` variable. Inventory endpoint paths and response schemas for auth/profile/MFA, chatgpt backend-api, entitlement/feature/model/device config, connector consent, telemetry, registry, updater, dev URLs, realtime SDP/WebSocket/SSE.

- [ ] **R3: Local persistent state and secrets storage (C18, plus C3/C20/C21/C22/C23 dependencies)**

  Capture filesystem and OS credential-store schema only: app support directory, session/thread/worktree/cache/model catalog files, IndexedDB/LocalStorage schemas, Keychain/Credential Manager item names, connector cache, enrollment records. Secret values are never captured, even in raw mode.

- [ ] **R4: OAuth, device pairing, remote-control enrollment split (C3, C21)**

  Split state machines for OAuth/PKCE/MFA, device attestation/enrollment, and remote-control pairing/revoke. Use keyed HMAC for account/org/thread/device ids; never store token values.

- [ ] **R5: MCP / plugin / subagent / skills and external-agent import (C20, C22)**

  Capture plugin manifest/cache/marketplace, MCP transports and OAuth, sandbox messages, subagent lifecycle, deferred tool families, skill trigger/loader/cache, and external-agent detect/import/completion paths. Cross-check gateway tool events and local app-server notifications.

- [ ] **R6: Native pipe taps split by subsystem (C7, C8, C9, C13)**

  Implement separate checkpoints for Computer Use, Browser-use, node_repl, and Windows sandbox. Taps must intercept frame-carrying APIs (`read/write/send/recv/ReadFile/WriteFile`) rather than only `open/fopen`. Each checkpoint needs platform preflight, permission guidance, fallback behavior, crash/restart capture, stderr/stdout/process argv/env shape, and redaction-negative tests.

- [ ] **R7: Telemetry, local logs, and Crashpad (C4, C5, C25)**

  Depends on R1b IPC for `get-sentry-init-options` and R2 network for exporter paths. Capture DSN/endpoint/transport/span/event/log/crash schema. Crash dumps are inventoried by file metadata only unless an explicit telemetry raw unlock and retention policy are present.

- [ ] **R8: Realtime/WebRTC deepening (C16)**

  Extend beyond SDP/transcript order to PeerConnection lifecycle, ICE/STUN/TURN hints, DataChannel, mic permission, audio device, interrupt/barge-in, VAD/turn-taking, reconnect and permission-denied paths.

- [ ] **R9: Optional first-party MITM lane**

  MITM is not a blanket host allowlist. It must be process-scoped to Codex Desktop and path-scoped to the specific endpoint classes under review (auth/profile/MFA, backend-api control plane, Sentry ingest, Sparkle feed, registry lookup). The mitmproxy addon must refuse all other traffic, record refusal evidence, document CA key storage/cleanup/trust-store removal, and mark every event with `mitm_lane=true`.

## Review Loop

Review this matrix in passes before starting implementation:

1. Coverage closure: every gap row must have a concrete capture mechanism and an explicit raw-unlock policy.
2. Native parity: every signal must round-trip into a fixture that can replay through the existing zhumeng-agent + codex-gateway stack.
3. Safety: no row may default to raw payload capture; every raw unlock must be a distinct env var so that one unlock does not implicitly grant another.
4. Observability: every row must surface gap status through the doctor and the capture report; no silent partial coverage.
5. Versioning: every row must declare whether it is sensitive to Sparkle/build-flavor; signal sources that change between versions must be re-baselined per release.
6. Subagent reviewer pass: dispatch a reviewer to challenge the matrix against the unpacked desktop bundle and the `codex_desktop:*` IPC inventory; merge blocking feedback before implementation.

Stop and ask before:

- enabling a default-raw row,
- broadening MITM beyond first-party Codex Desktop hosts,
- shipping any capture that writes raw screenshots, audio, or third-party provider bodies without an explicit unlock and an explicit retention rule.

## Out Of Scope

- non-Codex apps. Claude Code anti-ban capture (`.worktrees/claude-antiban-implementation/docs/anti-ban/*`) keeps its existing posture; this matrix is strictly about Codex Desktop.
- replacing `app-server v2` itself. Codex Desktop continues to own the local control plane; the gateway remains the custom Responses provider.
- production deployment of a self-hosted Codex Desktop replica. This document covers capture, replay fixtures, and reconstruction primitives sufficient to specify a replica. Building and shipping a production-grade replica remains a follow-on workstream.
