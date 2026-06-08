# Codex Desktop Protocol Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Treat `docs/codex-gateway/protocol-capture-design.md` as the architecture source of truth and `docs/codex-gateway/codex-desktop-full-capture-v3.md` as the live coverage matrix.

**Goal:** Ship the capture coverage described by the V3 matrix, in dependency order, without regressing the existing shape-only default. Every checkpoint adds tests and updates the matrix row status from `gap` to `partial` or `shipped`.

**Tech Stack:** Python `zhumeng-agent` adapters with `pytest`, Go `codex-gateway` capture package with `testing` plus `stretchr/testify`, CDP over `--remote-debugging-port`, optional Frida / DTrace / dyld-interpose for native pipe taps.

**Architecture:** Layered capture as defined in `protocol-capture-design.md`. Renderer addBinding bridge stays the primary path; native HTTP/beacon/WS to `127.0.0.1` remain fallbacks; native pipes and binary baseline run independently and join via `trace_id`.

## Quality Review Status

A two-agent review on 2026-06-07 returned `PASS WITH BLOCKERS`. Before code implementation starts, this plan must address: raw unlock naming, shape-only body policy, MITM process/path scoping, high-confidence trace correlation, static coverage denominator, local persistent state, app-server transport semantics, IPC priority, native-pipe feasible taps, command/file-name alignment with current code, and smoke criteria that prove coverage rather than file existence.

## Context And Boundaries

- Implement in the main checkout. Treat existing dirty Codex Gateway files as protected unless their checkpoint is the one currently being worked on.
- Never change the default behavior of capture sessions to write raw payloads. Every raw mode requires the explicit env var documented in the design.
- Never enable MITM lanes by default. R8 is opt-in and scoped to first-party Codex Desktop hosts.
- Do not regress existing capture v2 behavior; reuse the existing redaction, baseline, linker, and writer modules wherever possible.

## Files

- Modify: `docs/codex-gateway/codex-desktop-full-capture-v3.md`
  - move row `Status` columns through `gap` -> `partial` -> `shipped` as checkpoints land.
- Modify: `docs/codex-gateway/protocol-capture-design.md`
  - update only when an architectural assumption changes during implementation.
- Modify: `docs/codex-gateway/README.md`
  - already references the design and implementation files; cross-link the V3 matrix when R1 ships.
- Modify: `docs/codex-gateway/smoke.md`
  - add a smoke entry per matrix row id when its checkpoint ships.
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_shape.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_redact.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_writer.py`
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_linker.py`
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_network.py`
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_native_pipe.py`
- Add: `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_baseline_binary.py` (wired as `zhumeng-agent codex capture baseline --mode binary --out <dir> [--app <app>]`; the existing `baseline` namespace is extended rather than replaced)
- Modify: `tools/zhumeng-agent/src/zhumeng_agent/cli.py`, `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
- Add: `tools/zhumeng-agent/tests/test_codex_capture_network.py`
- Add: `tools/zhumeng-agent/tests/test_codex_capture_native_pipe.py`
- Add: `tools/zhumeng-agent/tests/test_codex_capture_baseline_binary.py`
- Modify: `tools/zhumeng-agent/tests/test_codex_capture_shape.py`
- Modify: `tools/zhumeng-agent/tests/test_codex_capture_injector.py`
- Modify: `backend/internal/service/codex_gateway_capture_links.go`
- Modify: `backend/internal/service/codex_gateway_capture_test.go`

## Checkpoint Order

The order is dependency-driven. Static denominator and IPC/app-server observability come first because later rows need a coverage denominator and a chain of custody.

### R0 - Static denominator and binary baseline (matrix C6, C11, C12, C19)

Why first: without a static denominator, dynamic smoke can only prove sampled paths, not full coverage.

- [ ] **Step 1: Extend current baseline command**

  Extend `zhumeng-agent codex capture baseline --out <dir> [--app <app>]` with `--mode runtime|binary|all`. Do not introduce a separate `codex baseline binary` namespace unless the CLI is deliberately redesigned.

- [ ] **Step 2: Extract static surface denominator**

  From asar listing and unpacked/deobfuscated `main-*.js`, `app-session-*.js`, and `comment-preload.js`, extract URL, IPC channel, env var, native executable, app-server method, notification, telemetry, updater, MCP/plugin/subagent/skill symbols. Persist `static_denominator.json`.

- [ ] **Step 3: Capture binary/update metadata**

  Compute sha256, codesign output, entitlements, bundle tree, appcast metadata, signature validation status, and update-channel/version metadata. Persist raw update payload only with `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_UPDATE`.

- [ ] **Step 4: Tests**

  Use synthetic asar fixtures. Assert schema, hash stability, no raw deobfuscated source committed, and diff behavior.

- [ ] **Step 5: Move C6/C11/C12/C19 to `partial` only after a real Codex.app baseline pass succeeds.**

### R1 - App-server v2 transport, method surface, and event order (matrix C1, C14, C17)

- [ ] **Step 1: Capture transport semantics**

  Record endpoint/pipe/socket, startup mode, handshake/version, request id, notification registration, cancel/abort, error schema, reconnect, permission, concurrency, and ordering semantics into `app_server_v2.jsonl`.

- [ ] **Step 2: Discover hookable entry points**

  Do not assume global renderer objects exist. Add a discovery phase. If `appServerClient.*` or `appServerConnection.*` are closure-local, fall back to the R1b IPC/preload/main-process strategy rather than silently marking the row captured.

- [ ] **Step 3: Wrap entry points where hookable**

  Hook `sendAppServerRequest`, `sendRequest`, `sendNotification`, and notification handlers. Persist method, direction, shape, latency, error class, and keyed-HMAC ids.

- [ ] **Step 4: Add report fields**

  Extend `capture_shape.py` with `appserver_method_surface`, `appserver_notification_surface`, `coverage_denominator_count`, `seen_count`, `unseen_required`, and `sampled_optional`.

- [ ] **Step 5: Tests and status**

  Add fixtures from the static denominator and negative/error cases for cancel, permission denied, reconnect, and malformed error. Move C1/C14/C17 to `partial` only after tests and one live smoke pass.

### R1b - Electron IPC and UI chain inventory (matrix C10, C24)

- [ ] **Step 1: Static + runtime IPC inventory**

  Extract IPC channels statically and hook runtime `ipcMain`/`ipcRenderer` where possible. The dev-only `comment-preload.js` patch is fallback only, requires explicit user confirmation, backup/restore, codesign awareness, and Sparkle drift detection.

- [ ] **Step 2: UI state chain capture**

  Capture route/view state, shared-object snapshot shape, menus/context menus, command palette, keyboard shortcuts, webview/worker lifecycle, and UI-to-app-server chains.

- [ ] **Step 3: High-confidence links**

  Link IPC to app-server calls only with shared HMAC or request-id evidence. Timestamp-only links remain `low_confidence` and cannot satisfy shipped status.

- [ ] **Step 4: Tests and status**

  Add unit tests for static extraction and runtime wrapper shape. Move C10/C24 to `partial` after live evidence; leave preload-patch-only evidence as `partial`, not `shipped`.

### R2 - CDP Network / Fetch path-level inventory (matrix C2, C3, C4, C5, C6, C15, C16, C21, C23)

- [ ] **Step 1: Add `capture_network.py`**

  Subscribe to `Network.*`, `EventSource`, and WebSocket events. `Fetch.requestPaused` is optional and used only when shape-only body metadata is otherwise unavailable.

- [ ] **Step 2: Persist body shape by default**

  Persist `{key_paths, types, lengths, hmac_sha256_truncated}` in shape-only mode. Raw bodies require the row-specific Desktop unlock.

- [ ] **Step 3: Path-level control-plane inventory**

  Record endpoint path schemas for auth/profile/MFA, backend-api, entitlements, feature flags, plans, model registry, device config, org/account switching, connectors, registry, telemetry, updater, dev URLs, and realtime.

- [ ] **Step 4: Tests and status**

  Test header hashing, body-shape without unlock, raw gating, SSE/WebSocket ordering, and per-host/path policies. Move C2/C15/C23 to `partial`; keep C3/C4/C5/C6/C16/C21 as `gap` until their dedicated checkpoints.

### R3 - Local persistent state and secrets storage (matrix C18)

- [ ] **Step 1: Filesystem schema scan**

  Inventory app support dir, session JSONL, thread/window/worktree/cache/model catalog files, plugin/MCP caches, connector cache, Crashpad/log dirs. Persist schema, filename classes, mtimes, sizes, and keyed-HMAC paths only.

- [ ] **Step 2: Credential-store schema scan**

  Inventory Keychain/Credential Manager item names and account labels only. Secret values are never read or persisted.

- [ ] **Step 3: Storage API shape**

  Capture IndexedDB/LocalStorage schema and key names without values.

- [ ] **Step 4: Tests and status**

  Test that token/keychain values cannot be persisted even under raw mode. Move C18 to `partial` after a local schema-only smoke pass.

### R4 - OAuth, device pairing, and remote-control protocol (matrix C3, C21)

- [ ] **Step 1: Split state machines**

  Implement separate state machines for OAuth/PKCE/MFA, device attestation/enrollment, and remote-control pairing/command/revoke.

- [ ] **Step 2: Redaction and HMAC**

  Decode JWTs only to claim key sets. Use keyed HMAC for account/org/device/thread ids. Refresh/access/device tokens are never persisted.

- [ ] **Step 3: Replay fixtures**

  Emit redacted replay fixtures per state transition.

- [ ] **Step 4: Tests and status**

  Add explicit no-token-value tests and storage TTL checks. Move C3/C21 to `partial` only after live login + refresh + logout + revoke smoke.

### R5 - MCP / plugin / subagent / skills and external-agent import (matrix C20, C22)

- [ ] **Step 1: Plugin/MCP/skills schema**

  Capture plugin manifests, marketplace/cache shape, MCP transport types, MCP OAuth, sandbox app messages, skill trigger/loader/cache, deferred tool output families.

- [ ] **Step 2: Subagent lifecycle**

  Capture `multi_agent_v1.*` discovery/spawn/wait/close lifecycle, model override list freshness, registration races, and failure recovery.

- [ ] **Step 3: External-agent import**

  Capture `externalAgentConfig/detect`, `externalAgentConfig/import`, completion notification, transcript/session migration, generated-file staging, source discovery, retry/failure shape.

- [ ] **Step 4: Tests and status**

  Add smoke prompts for plugin open, MCP OAuth, skill trigger, subagent spawn, and external import. Move C20/C22 to `partial`.

### R6 - Native pipe taps split by subsystem (matrix C7, C8, C9, C13)

- [ ] **Step 1: Platform preflight**

  Detect macOS hardened runtime/SIP constraints, signed Frida helper availability, DTrace permissions, Windows ETW/named-pipe duplication support. If unavailable, emit actionable doctor gaps.

- [ ] **Step 2: Computer Use pipe (C7)**

  Intercept frame-carrying APIs (`read/write/send/recv/ReadFile/WriteFile`) and reconstruct JSON-RPC frames. Capture spawn argv/cwd/env shape, binary hash, trust decisions, stderr/stdout, crash/restart, permission denial.

- [ ] **Step 3: Browser-use pipe (C8)**

  Capture peer auth, CDP relay, downloads, navigation blocked events, crash/restart, permission denial.

- [ ] **Step 4: node_repl pipe (C9)**

  Capture invocation lifecycle, allowlist evaluation, request meta shape, node module dirs, stdout/stderr, and crash/restart. Strip code by default.

- [ ] **Step 5: Windows sandbox (C13)**

  Explicitly capture Windows helper transport, sandbox setup completion, world-writable warning, named-pipe/ETW flow. Move C13 separately; do not hide it under C7/C8/C9.

- [ ] **Step 6: Tests and status**

  Add synthetic frame tests, binary screenshot/audio/code negative tests, and platform preflight tests. Move each row to `partial` independently.

### R7 - Telemetry, local logs, and Crashpad (matrix C4, C5, C25)

Depends on R1b IPC and R2 network.

- [ ] **Step 1: Sentry and OTEL shims**

  Use IPC/network evidence to capture DSN, endpoint, transport mode, event/span/profile/log shape. Redirect to local sink only in capture mode.

- [ ] **Step 2: Local logs and Crashpad**

  Inventory console/main/renderer/pino log schema and Crashpad directories by metadata only. Do not persist dumps by default.

- [ ] **Step 3: Tests and status**

  Add no-raw-message/no-crashdump tests. Move C4/C5/C25 to `partial` after live evidence.

### R8 - Realtime/WebRTC deepening (matrix C16)

- [ ] **Step 1: WebRTC lifecycle**

  Capture PeerConnection lifecycle, SDP, ICE/STUN/TURN hints, DataChannel, mic permission, device selection, interrupt/barge-in, VAD/turn-taking, reconnect, permission-denied and network failure.

- [ ] **Step 2: Tests and status**

  Add realtime-specific smoke; one ordinary text turn cannot satisfy C16.

### R9 - Optional first-party MITM lane

- [ ] **Step 1: process/path-scoped mitmproxy profile**

  Allow only Codex Desktop process traffic and explicit endpoint paths. Broad hosts such as `api.openai.com` are permitted only for Codex Desktop auth/profile/MFA paths, not provider traffic.

- [ ] **Step 2: CA lifecycle**

  Document CA key location, permissions, trust-store install/removal, cleanup, and refusal evidence.

- [ ] **Step 3: Tests**

  Unit-test allow/refuse path. Do not run mitmproxy in CI.

## End-To-End Regression Matrix

- [ ] **Step 1: Gateway unit tests**

  ```bash
  cd backend
  go test ./internal/service -run 'CodexGatewayCapture|CodexGatewayCaptureLinks|CodexGatewayCaptureRedact|CodexGatewayCaptureStream' -count=1
  ```

- [ ] **Step 2: zhumeng-agent tests**

  ```bash
  cd tools/zhumeng-agent
  pytest tests/test_codex_capture_injector.py tests/test_codex_capture_shape.py tests/test_codex_capture_network.py tests/test_codex_capture_native_pipe.py tests/test_codex_capture_baseline_binary.py -q
  ```

- [ ] **Step 3: Manual capture session**

  Launch Codex Desktop with `--remote-debugging-port=<port>`. Run `zhumeng-agent codex capture install` and `zhumeng-agent codex capture attach --cdp-port <port> --trace-dir <dir>` for at least one full thread. Generate the report:

  ```bash
  zhumeng-agent codex capture report --trace-dir <desktop-trace-dir> --gateway-trace-dir <gateway-capture-dir>
  ```

  Confirm every shipped matrix row has at least one captured event in the report.

- [ ] **Step 4: Doctor coverage check**

  Run `zhumeng-agent doctor` and `zhumeng-agent codex capture matrix (planned subcommand; implementation must add it before docs mark it shipped)`. Both must surface every `gap`/`partial` row with the documented next action.

- [ ] **Step 5: Baseline pass**

  Run `zhumeng-agent codex capture baseline --mode binary --app /Applications/Codex.app`. Confirm the per-version baseline directory is populated and that `diff_vs_prev.txt` is empty on a re-run against the same version.

## Review Loop

Review this plan in passes before starting implementation:

1. Root-cause closure: every checkpoint maps to one or more matrix rows, and every row in V3 has a checkpoint.
2. Native parity: every shipped row produces a replay/golden fixture where replay is meaningful; otherwise the row records an explicit not-replayable rationale.
3. Cache protection: capture sessions never modify Codex Desktop runtime state outside the documented patches; renderer addBinding bridges and preload mirror are install/restore-symmetric.
4. Observability: every checkpoint surfaces gap status through doctor and the capture report; no silent partial coverage.
5. Versioning: every checkpoint declares whether it is sensitive to Sparkle/build-flavor and how the per-version baseline detects drift.
6. Subagent reviewer pass: dispatch a reviewer to challenge the plan against the V3 matrix and the unpacked desktop bundle. Merge blocking feedback before implementation.

Stop and ask before:

- applying invasive preload/app.asar patches, enabling native-pipe taps that require elevated OS permissions, or installing a MITM CA;
- moving any row to a default-raw mode,
- broadening MITM beyond first-party Codex Desktop hosts,
- shipping any capture surface that writes raw screenshots, audio, or third-party provider bodies without an explicit unlock and an explicit retention rule,
- patching minified bundles outside the documented backup/restore flow.

## Inline Author Review Checklist

Run the following checklist before declaring a checkpoint shipped:

1. Mechanical pass: referenced files exist; tests reference real code paths.
2. Scope pass: the checkpoint matches its matrix rows; no scope creep into unrelated matrix rows.
3. Protocol pass: redaction defaults match the design; raw unlock env vars match the documented set.
4. Overlap pass: existing capture v2 modules are extended, not duplicated.
5. Subagent reviewer pass (optional): dispatch a reviewer when the checkpoint touches multiple capture surfaces; record advisory feedback inline.
