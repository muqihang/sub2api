# Codex Desktop Protocol Capture Architecture

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:writing-skills if you intend to extend this design surface; use superpowers:executing-plans for the matching implementation plan in `docs/codex-gateway/protocol-capture-implementation-plan.md`.

This document is the architecture of safe protocol capture for Codex Desktop and the Codex Gateway. It explains why capture exists, what we capture by default, what we never capture by default, how we redact, and how each capture surface ties into reproducible Desktop replay. It is paired with the live coverage matrix in `docs/codex-gateway/codex-desktop-full-capture-v3.md`.

## Goal

Reconstruct Codex Desktop behavior locally with enough fidelity that we can replay every supported workflow without depending on the closed-source app-server v2 implementation. We do this by capturing protocol shape across every signal source the app exposes, while keeping user content, secrets, and third-party provider bodies behind explicit unlocks.

## Non-Goals

- This design does not replace `app-server v2`. The Codex Gateway continues to be the custom Responses provider. Capture observes app-server, never substitutes it.
- This design is not a wire format for production telemetry. Captured artifacts are local diagnostic evidence, not analytics events.
- This design does not assume MITM by default. MITM is an opt-in lane scoped to first-party Codex Desktop hosts only.
- This design does not change Claude Code anti-ban posture. Anti-ban capture (`docs/anti-ban/*`) follows its own rules and is not affected.

## Capture Layers

Capture is a stack of independent layers. Each layer can run on its own. They are joined by `trace_id`, `host_id`, and per-row matrix evidence in the report.

1. **Renderer addBinding bridge.** A CDP `Runtime.addBinding` channel installed by `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`. The bridge wraps `appServerClient.sendAppServerRequest`, `sendRequest`, `sendNotification`, and `registerInternalNotificationHandler`. It is the primary return path because Codex Desktop's `app://` renderer policy frequently blocks direct local network delivery.
2. **Renderer network capture.** CDP `Network.*` plus opt-in `Fetch.requestPaused`. Used for chatgpt.com, api.openai.com, auth.openai.com, developers.openai.com, sentry.io, and any other first-party host. Records request shape, response shape, SSE frame order, WebSocket frame order. Body capture is gated.
3. **Native pipe taps.** Out-of-process taps that observe `SKY_CUA_*`, `\\.\pipe\codex-browser-use`, `/tmp/codex-browser-use`, `NODE_REPL_NATIVE_PIPE_*`. Macros are macOS Frida `_open` interpose, Linux `LD_PRELOAD`, Windows ETW or named-pipe duplication. Pipe taps capture JSON-RPC frame shape; bodies are gated.
4. **Electron IPC mirror.** A patched preload that mirrors `ipcMain.handle / ipcMain.on / ipcRenderer.invoke / ipcRenderer.on` channels into addBinding. Used for the `codex_desktop:*` channel set.
5. **Sparkle and binary baseline.** Per-version archive of asar listing, sha256, codesign output, entitlements, plist, deobfuscated `main-*.js / app-session-*.js / comment-preload.js` strings. Sparkle feed and signed payload are mirrored under `data/codex-desktop-baselines/<version>/`. Drives drift detection.
6. **Static denominator and local-state baseline.** Per-version symbol extraction from asar bundles, helper executables, update feeds, and local state schemas. This layer defines the expected coverage denominator used by every dynamic report.
7. **Gateway protocol capture.** The existing `backend/internal/service/codex_gateway_capture*.go` set. Owns upstream provider request/response shape and SSE event order. Cross-linked to renderer captures via `trace_link.jsonl`.

Optional layer:

8. **First-party MITM lane.** Opt-in mitmproxy profile scoped to the Codex Desktop process and to explicit endpoint paths. Used only when CDP body capture is not enough (typical case: signed payload integrity verification, OAuth redirect edge cases, SDP/body shape). Off by default and never a broad host-only intercept.

## Default Capture Mode

Default is **shape-only**. Shape-only persists:

- top-level field names and key path set,
- value classes (`null`, `bool`, `number`, `string<len>`, `array<len>`, `object<keys>`),
- string lengths and keyed HMAC-SHA256 truncated to 16 hex chars,
- protocol method names, notification names, IPC channel names,
- frame ordering and timestamps,
- header allowlist (`content-type`, `cache-control`, `x-request-id`, `openai-organization`, etc.),
- HTTP status, gRPC status, JSON-RPC error code,
- correlation ids that the app derives from inputs (always recorded with the shared keyed HMAC, not the raw id).

Shape-only intentionally does **not** persist:

- raw user prompts or assistant outputs,
- raw tool outputs (including Computer Use screenshots),
- raw browser HTML/text,
- access tokens, refresh tokens, device tokens, API keys,
- absolute file system paths, repo URLs, branch names, commit hashes,
- raw Codex thread/turn/window/conversation ids (only hashed forms are kept),
- third-party provider request/response bodies.

## Raw Unlocks

Raw payload capture is gated behind explicit local environment variables. Each unlock corresponds to one matrix row class. One unlock never implicitly grants another; this is enforced in `capture_redact.py` and verified by the doctor.

| Unlock env var | Scope | Default |
|----------------|-------|---------|
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK` | desktop renderer addBinding bridge (C1, C10, C14) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NETWORK` | renderer fetch / SSE / WebSocket bodies (C2) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_AUTH` | OAuth / token / device pairing payloads (C3) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_TELEMETRY` | Sentry / OTEL bodies (C4, C5) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_UPDATE` | Sparkle feed payload archive (C6) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_CUA` | Computer Use pipe payloads (C7, C13) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_BROWSER` | Browser-use pipe payloads (C8) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_NODE_REPL` | node_repl pipe payloads (C9) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_DEV` | local dev URLs and feature override env (C15) | not set |
| `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_REALTIME` | realtime audio / SDP payloads (C16) | not set |

Each Desktop unlock value must equal `I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_DESKTOP_PROTOCOL_PAYLOADS`. This is intentionally different from the gateway unlock value (`I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_PROTOCOL_PAYLOADS`). Any other value, including unset, keeps the capture in shape-only mode for that row. Current code implements only `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK`; the per-row variables above are planned extensions and must be implemented before rows using them can move to `shipped`.

Even with a raw unlock, the redactor never persists secrets:

- JWT bodies are decoded but only the claim key set is recorded; values are sha256 truncated.
- API keys, refresh tokens, device tokens, and Sparkle/codesign private keys are scrubbed unconditionally.
- Authentication headers are key-listed only; values are sha256.
- File system absolute paths under `~/.codex` and `~/Library/Application Support/Codex` are reduced to a hashed form.

## Storage Layout

```
data/codex-desktop-captures/<YYYY-MM-DD>/<trace_id>/
  summary.json
  app_server_v2.jsonl          # C1, C14, C17; current canonical file name
  network.events.jsonl         # C2, C23
  oauth.state.jsonl            # C3, C21
  telemetry.shape.jsonl        # C4, C5, C25
  pipe.cua.jsonl               # C7, C13
  pipe.browser.jsonl           # C8
  pipe.node_repl.jsonl         # C9
  ipc.events.jsonl             # C10, C24
  realtime.events.jsonl        # C16
  errors.jsonl
  redactions.jsonl
  local_state.schema.json      # C18
  static_denominator.json      # C19
  replay_fixtures/             # per-row replay/golden fixtures or explicit not-replayable markers
  trace_link.jsonl             # joins to backend captures

data/codex-desktop-baselines/<version>/
  asar/
    listing.json
    files.sha256.json
    main.deobf.txt
    app-session.deobf.txt
    comment-preload.deobf.txt
    diff_vs_prev.txt
  binary/
    codesign.txt
    entitlements.plist
    otool.txt
    bundle.tree.txt
  sparkle/
    appcast.xml
    payload.sig
    payload.meta.json
```

Existing gateway capture output stays at `data/codex-gateway-captures/<YYYY-MM-DD>/<trace_id>/`. The unified report is produced by `zhumeng-agent codex capture report --trace-dir <desktop-trace-dir> --gateway-trace-dir <gateway-trace-dir>` and reuses the existing `trace_link.jsonl` writer.

## Redaction Rules

- Per-row redaction modes are declared in the matrix and enforced in `capture_redact.py`.
- Default redaction is shape-only with keyed HMAC-SHA256 hashing for any string longer than 64 bytes or for any identifier-like value.
- Network header allowlist defaults to: `content-type`, `cache-control`, `etag`, `x-request-id`, `x-content-type-options`, `openai-organization`, `openai-version`, `cf-ray`, `mcp-protocol-version`, `mcp-session-id`. Anything not on the allowlist is recorded as `{name, value_sha256}`.
- Bodies are recorded as `{key_paths, types, lengths, hmac_sha256_truncated}` in shape-only mode. The matching raw unlock permits raw body archive only for that row; it is never required for body shape capture.
- Authorization, cookie, `openai-organization`, and `x-stainless-*`-style headers are always redacted to key + keyed HMAC even with raw unlock.
- Tool outputs that look like screenshots (image MIME) are recorded by class (`image/png`, `image/jpeg`) and dimension shape only; raw image bytes only land if `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_CUA` is set.
- Realtime audio chunks are recorded by codec class and chunk count only; raw bytes only land if `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK_REALTIME` is set.
- Codesign and Sparkle private keys are never persisted, regardless of unlock state.

## Doctor And Coverage Reporting

`zhumeng-agent doctor` and `codex capture matrix` walk the matrix in `codex-desktop-full-capture-v3.md`. Each row reports:

- `id` (matrix row id),
- `status` (`gap`, `partial`, `shipped`),
- `last_seen_ts`,
- `redaction_mode`,
- `unlock_used`,
- `evidence_paths`,
- `notes`.

A coverage gap is a first-class doctor warning. Capture sessions never silently degrade; if a row cannot be captured, the writer records the failure into `errors.jsonl` and updates the row to `gap` until the cause is resolved.

## Trace Correlation Contract

Desktop and gateway captures must not rely on timestamp-only joins for shipped rows. The contract is:

- the desktop trace has a local `trace_id` generated by the attach command;
- the gateway trace has its existing gateway `trace_id`;
- both sides compute keyed HMACs over safe shared identifiers where available (`response_id`, `previous_response_id`, hashed thread/conversation id, model id, provider request id, tool call id);
- the HMAC key is loaded from `--correlation-hash-key-file`; if no key is configured, the report marks joins as `low_confidence`;
- high-confidence joins require at least one shared HMAC identifier plus a compatible method/model/time window;
- fallback `time_model_path` links are allowed only as `low_confidence` and must not satisfy `shipped` smoke for rows that require gateway correlation;
- missing expected peers are emitted as `unmatched_expected` rows in `trace_link.jsonl`.

## Cross-Linking With Gateway Capture

Desktop capture and gateway capture are joined by `trace_id`. Each row that has a likely upstream effect declares one or more gateway rows it should match. Examples:

- C1 method `turn/started` is expected to produce a corresponding gateway capture summary (`client_request.shape.json`).
- C2 fetch to `chatgpt.com/backend-api` is not expected to produce a gateway capture; absence is the correct outcome.
- C7 Computer Use pipe payloads are expected to align with `tool_search` / `tool_search_output` items in the gateway streaming capture.

The linker writes `trace_link.jsonl` rows that point both directions and explicitly mark missing peers as `unmatched_expected`.

## Lifecycle And Retention

- Capture sessions default to a `--ttl 14d` retention policy. Older sessions are pruned by `codex capture retention apply`.
- Baselines under `data/codex-desktop-baselines/<version>/` are never auto-pruned; they are versioned evidence and follow Sparkle's release cadence.
- Raw mode sessions get a `--ttl 7d` default and the doctor warns if a raw session is still on disk after the TTL.

## Failure Modes

- CDP attach fails: the injector emits a `gap` row for C1/C2/C14/C15 and reverts to HTTP/beacon fallback if the renderer permits it; otherwise the doctor reports the gap.
- Native pipe tap is blocked by SIP / hardened runtime: preflight records the missing permission, the matching row goes `gap`, and the doctor surfaces the cause; the user is given a documented choice (use a signed Frida helper, use DTrace where permitted, run Windows ETW, or skip that row). Taps must intercept frame APIs (`read/write/send/recv/ReadFile/WriteFile`) rather than only `open/fopen`.
- Sparkle replaces `app.asar` mid-session: the binary baseline writer detects the version change, opens a new baseline directory, and the linker tags the active capture session with `binary_baseline_changed=true`.
- Telemetry exporters refuse to be replaced (e.g. native HTTP transport): the doctor reports the row as `partial`; raw unlock does not bypass this constraint. Local logs and Crashpad dumps are inventoried by metadata only unless an explicit telemetry raw unlock and retention rule are present.


## Stage Completion Standard

This stage is complete when the capture system can produce: (1) a static denominator for every bundle-visible surface, (2) dynamic shape evidence for every shipped row, (3) redaction-negative tests for secrets, tokens, screenshots, audio, browser text, local paths, repo URLs, branch names, and raw code, (4) replay/golden fixtures for rows where replay is meaningful, and (5) high-confidence trace links for rows that cross Desktop and Gateway. It is not complete merely because artifact files exist.

## Open Questions

- whether to add a Frida helper bundle to the repo or document an out-of-tree install;
- whether the binary baseline should also include WSL-specific bundles when Windows support is exercised;
- whether realtime audio capture warrants a dedicated dataset boundary (it is large enough to overflow normal capture session sizes).

These questions block their respective rows from moving from `gap` to `shipped`. Track them in the implementation plan checkpoints.
