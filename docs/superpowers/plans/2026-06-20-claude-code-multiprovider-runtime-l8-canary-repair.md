# Claude Code Multiprovider Runtime L8 Canary Repair Plan

> **Status:** final investigation-backed repair plan for user review. Do **not** implement code changes until this plan is approved.  
> **For future implementation workers:** use `superpowers:subagent-driven-development` or `superpowers:executing-plans`; keep each workstream test-gated and review-gated before moving to the next.

**Goal:** Before L8 canary resumes, make the Zhumeng-managed Claude Code CLI 2.1.177 runtime a safe, coherent multi-provider runtime: native Claude formal-pool remains protected, bridge models behave as close to Claude-native as feasible, per-model effort is truthful, prompt/cache behavior is observable and provider-correct, and CP0-CP8 gates from docs 45/47 remain intact.

**Worktree:** `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`  
**Branch:** `codex/claude-code-multiprovider-runtime`  
**Canary service allowed to change later:** `3017` / `sub2api-canary-app-main-f3a9f235d-cc-runtime-current-3017`  
**Service forbidden to touch:** `3012`  
**Runtime under test:** Claude Code `2.1.177`

---

## 0. User Intent, Restated

The user is not asking for a one-off screenshot fix. The target product behavior is:

1. **Take over Claude Code CLI safely.** A local Zhumeng-managed Claude Code session should be able to use native Claude models and non-Claude gateway/bridge models from one `/model` surface.
2. **Keep Claude native formal-pool safe.** In one long session, the user may switch from DeepSeek/GPT/Kimi/AGNES/GLM back to Claude Opus/Sonnet. When that happens, foreign reasoning blocks, provider-private signatures, bridge tool internals, and non-Claude transport artifacts must never reach the Claude native formal-pool upstream.
3. **Bridge models should feel native in Claude Code.** DeepSeek, GPT, AGNES, GLM, and Kimi should support Claude Code tools, Agent/subagent flows, streaming semantics, tool-result loops, cache behavior, and effort controls as close to native Claude as their real upstreams allow.
4. **Truthful capabilities over fake parity.** If a provider cannot safely support a feature yet, it should be catalog-visible but live-disabled or feature-disabled; do not fake support and let it fail later.
5. **Preserve future local Anthropic OAuth design.** Future user-owned local Anthropic OAuth must be isolated from Zhumeng formal-pool credentials and represented through explicit provider-owner / credential-scope / gateway-location metadata.
6. **One systematic repair pass.** The current L8 canary exposed repeated gaps. This plan aims to collect all confirmed root causes and likely omissions so implementation can happen once, with tight tests and safe evidence.

---

## 1. Non-Negotiable Constraints

- Do not touch the main checkout at `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main`.
- Do not stop, restart, reconfigure, or probe port `3012`.
- Do not print, persist, or commit API keys, tokens, cookies, Authorization headers, raw prompts, raw request bodies, raw responses, or raw CCH material.
- Do not route bridge models into Claude native formal-pool or native attestation.
- Do not perform unsafe official-host spoofing or direct official egress. It is allowed and expected to use managed runtime config/preload/patch hooks to make the local Sub2API/guard path capability-equivalent to Anthropic where we can prove shape parity and keep control-plane safety.
- Do not run broad live probes against the temporary formal-pool ingress. Use 3017 canary only after approval.
- Destructive operations remain prohibited unless the user explicitly approves them.

---

## 1A. Source Documents For This Plan

This repair plan incorporates and supersedes the relevant L8 repair implications from:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-claude-code-control-plane-classification-matrix.md`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md`

If these documents and this plan conflict, pause implementation and resolve the conflict in this plan before touching code.

---

## 2. Confirmed Root Causes and Evidence

### 2.1 `/model` effort UI ignores our `supportedEffortLevels`

**Confirmed cause:** We wrote the right-looking metadata to `gateway-models.json`, but Claude Code 2.1.177 does not use that field for the `/model` effort UI.

Repo-side writer:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/launcher.py`
  - `_bridge_reasoning_effort_levels()`
  - `_gateway_model_cache_entry()`
  - `_route_hint_catalog_payload()`

Runtime bundle evidence from 2.1.177 shows UI capability is generated internally by functions equivalent to:

- `zP(model)` -> supports effort
- `oyH(model)` -> supports max
- `KMH(model)` -> supports xhigh
- `Qs(model, effort)` -> effective effort
- `$Q9(...)` -> model picker metadata
- `AxO(...)` -> effort cycling sequence

The gateway cache path strips/ignores extra fields and keeps only model id/display name for the picker. Therefore:

- DeepSeek can still show `Medium effort` even though desired policy is `High/Max`.
- GPT 5.5 can still show `Max effort` even though desired policy is `Low/Medium/High/XHigh`.
- Outbound preload clamping protects the upstream request, but does not fix the user-visible UI or selectable capability state.

**Repair implication:** Patch the actual runtime capability control surface for bridge models only; do not rely on `gateway-models.json.supportedEffortLevels` as a UI control surface.

---

### 2.2 DeepSeek cache did not hit because the previous hypothesis was incomplete

**Confirmed cause cluster:** The previous fix proved Anthropic-style `cache_control` was injected in code/tests, but it did not prove live upstream behavior. Official DeepSeek docs also make clear that DeepSeek Anthropic-compatible fields named `cache_control` are ignored, while DeepSeek Context Caching is enabled by default and works through persisted matching prefixes.

Official DeepSeek Context Caching says:

- context caching is enabled by default;
- subsequent requests hit when they reuse persisted overlapping prefixes;
- hit status appears in `usage.prompt_cache_hit_tokens` and `usage.prompt_cache_miss_tokens`;
- the cache is best-effort and not a guaranteed 100% hit.

Official DeepSeek Anthropic API compatibility table says `cache_control` on tools and message content is **Ignored**.

Current code path:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_stream.go`
  - `claudeCodeBridgeShouldInjectAnthropicCacheControl()`
  - `injectClaudeCodeBridgeAnthropicCacheControl()`
  - `rewriteClaudeCodeBridgeAnthropicBodyModel()`

Current audit gap:

- Existing audit can record route/protocol and response cache token counts, but cannot prove request-side cache anchors, path choice, prefix stability, or whether live actually used DeepSeek Anthropic `/v1/messages` vs fallback.

**Repair implication:** For DeepSeek, do not treat Anthropic `cache_control` as the cache mechanism. Treat DeepSeek cache as automatic prefix-based KV cache; stabilize prefixes, add safe request/response audit, and use `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` as live evidence.

---

### 2.3 Bare custom/loopback Base URL causes first-party capability gaps; managed takeover must close them

Docs 45/47 established that when an unpatched Claude Code sees a custom/loopback `ANTHROPIC_BASE_URL`, it can disable or alter first-party-only capabilities before any request leaves the CLI. That is a diagnosis of the **bare custom URL** failure mode, not an acceptance criterion. Because this project manages the local Claude Code runtime, the repair target is to use configuration, preload hooks, runtime patching when approved, guard policy, and Sub2API/CC Gateway shape preservation so the local Sub2API URL behaves like Anthropic upstream for supported capabilities while still blocking unsafe official egress and raw control-plane leakage. Current implementation is only **safe takeover + partial shape preservation**; L8 repair must move it toward proven first-party capability parity.

Current strengths:

- `build_safe_env()` forces loopback guard, isolated config, scrubbed inherited env, and `ENABLE_TOOL_SEARCH=auto`.
- Guard/control-plane policy suppresses/stubs/quarantines unsafe control-plane traffic instead of allowing direct official bypass.
- Route-hint preload/provider-profile/gateway overlay make bridge models discoverable and routable.

Confirmed gaps:

- `ClaudeCodeCapabilityProfile`, `evaluate_toolsearch_profile()`, shape healthcheck, and status concepts exist but are not fully wired into the production `claude-code start` path.
- `ENABLE_TOOL_SEARCH=auto` is safer than unset, but it is not the final target. ToolSearch parity requires wiring the real runtime switch/profile/doctor path so `true` is enabled when fixed MCP/deferred-tool shape healthchecks prove the local Sub2API path preserves `ToolSearchTool`, `tool_reference`, and `defer_loading` exactly enough for Claude Code.
- FGTS / eager input streaming also needs capability-gate tracing. Default to observe-only until the responsible runtime gate is found, then promote to parity target only if local injection/config preserves safety and shape.
- Policy limits, remote settings, settings sync, team memory, dynamic model capabilities, GrowthBook, and similar control planes remain stub/block/suppress rather than equivalent restored services.
- Native prompt caching parity under loopback is not proven; we only have partial field preservation evidence.

**Repair implication:** Wire capability profile + doctor + shape healthcheck into launch/status gates; identify and patch/configure the actual runtime gates for ToolSearch, Prompt Caching, context-management, and other recoverable first-party capabilities; make control-plane policy explicit so unsupported/unsafe remote services remain safely stubbed while supported request-shape capabilities reach parity.

---

### 2.4 Bridge model parity is uneven by provider

Current rough provider state:

- **GPT/OpenAI:** closest to usable, but strict tools, multi-turn tool-result loops, and cache audit parity are not proven enough.
- **DeepSeek:** best candidate for Anthropic-native feel, but cache mechanism/audit and Anthropic-compatible stream terminal/error safety still need work.
- **AGNES:** basic bridge exists, but live proof/tool/cache/effort parity coverage is thin.
- **GLM/Kimi:** catalog/overlay prepared, but not final live-ready. Treat as catalog-visible but live-disabled unless provider probes and CP gates pass.

Confirmed gaps:

- CP8 strict-live provenance currently focuses on Claude/OpenAI/DeepSeek and does not fully include AGNES/GLM/Kimi.
- Provider probe is still too fixture-oriented for AGNES/GLM/Kimi.
- Anthropic-compatible live path lacks the same terminal/error fail-safe rigor as OpenAI bridge.
- OpenAI bridge tools are not proven strict-tools equivalent.
- Go bridge tests do not yet prove `tool_use -> tool_result -> next turn` end-to-end for GPT/DeepSeek/AGNES.
- Cache audit fields are not normalized across providers.
- Bridge behavior under `ENABLE_TOOL_SEARCH=true` is under-specified. If Claude Code emits `ToolSearchTool`, `tool_reference`, or `defer_loading` shapes for a bridge route, DeepSeek/GPT/AGNES cannot be assumed to understand those lazy/deferred shapes. The bridge must either materialize them before provider dispatch, provide an explicit ToolSearch shim, or disable ToolSearch per bridge model with a degraded reason.

**Repair implication:** Provider live enablement must be probe-driven and evidence-driven. Unsupported/untested providers stay fail-closed. ToolSearch/deferred-tool parity is part of bridge live enablement, not only a native Claude capability check.

---

### 2.5 Claude native replay safety is directionally correct but needs stronger end-to-end gates

Current safety code appears aligned:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/cli_control_plane_guard.py`
  - `native_replay_safety_decision()`
  - `_sanitize_native_replay_message()`
  - `_sanitize_native_replay_block()`

It strips/summarizes foreign reasoning, provider-private fields, signatures, raw tool internals, bridge Agent artifacts, tainted tool_use/tool_result, and provider-private markers before Claude native replay.

Remaining gap:

- The safety contract is strongest in Python/runtime guard layers. We still need canary-focused end-to-end tests and backend/provenance checks so runtime drift cannot silently bypass the boundary.

**Repair implication:** Add exact hot-switch tests: DeepSeek/GPT with foreign artifacts -> switch to Claude native -> prove final Claude upstream shape is replay-safe and formal-pool eligible only for native Claude.

---

## 3. External Cache Semantics to Respect

Use these as design constraints, not as raw quoted implementation assumptions:

- **DeepSeek Context Caching:** automatic/default, prefix-unit based, best-effort; response usage exposes `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens`; DeepSeek Anthropic compatibility marks `cache_control` fields as ignored. Sources: DeepSeek [Context Caching](https://api-docs.deepseek.com/guides/kv_cache), DeepSeek [Anthropic API](https://api-docs.deepseek.com/guides/anthropic_api).
- **OpenAI Prompt Caching:** automatic for recent models when prompt is at least 1024 tokens; exact prefix matches matter; `prompt_cache_key` can influence routing; usage exposes `cached_tokens`. Source: OpenAI [Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching).
- **Anthropic Prompt Caching:** supports top-level automatic `cache_control` and explicit block-level breakpoints; cache input spans tools/system/messages order; usage has cache creation/read fields. Source: Anthropic [Prompt caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching).

---

## 4A. Implementation Preconditions To Freeze Before Coding

These decisions must be closed before implementation starts; otherwise workstreams will branch and cause rework.

1. **L8 live provider scope:** immediate L8 live target is Claude native + GPT/OpenAI + DeepSeek. AGNES is allowed only if provider probe and strict-live evidence pass during this repair. GLM and Kimi default to catalog-visible/live-disabled unless the user explicitly expands L8 scope.
2. **Managed runtime patch policy:** default route is preload/metadata injection plus backend enforcement. A direct managed-runtime patch to the external `claude` binary requires a separate approval checkpoint after capability-source discovery proves no safer control surface exists.
3. **DeepSeek `cache_control` policy:** because official DeepSeek Anthropic compatibility says `cache_control` is ignored, do not treat it as a cache mechanism. Implementation should either remove it or keep it only as compatibility metadata with `cache_control_provider_ignored=true`; the final choice must be documented before rollout.
4. **`/model` UI evidence standard:** minimum evidence is a machine-readable pseudo-UI/runtime capability probe. If manual UI differs, add screenshot evidence under the run_id evidence directory.
5. **ToolSearch parity policy:** L8 target is `ENABLE_TOOL_SEARCH=true` for eligible models/projects once doctor + fixed MCP/deferred-tool shape healthcheck pass. `auto`/`false` are temporary degraded states with explicit reasons, not the desired endpoint.
6. **Audit digest policy:** no naked SHA over prompt/prefix/summary material. Use purpose-scoped HMAC with key id and rotation period for any MAC/digest that could correlate user content across sessions/accounts.
   - HMAC keys must be local/canary audit keys, never provider/API keys; key ids must be scoped by run/provider/account boundary; keys rotate at least per canary run or per configured audit epoch; missing HMAC key fails closed for evidence generation; key ids must not be reused across formal-pool and bridge-pool scopes.

---

## 4B. CP0-CP8 Hard Gates

Implementation must keep these gates green. Each gate needs a local/fixture/live status and safe evidence path.

| Gate | Must remain true | Blocking evidence before next phase |
| --- | --- | --- |
| CP0 native guard | Real launcher/start path uses native guard attestation; attestation binds route/model/runtime/overlay/catalog/session/body-shape/nonce/time | guarded launch artifact + attestation summary under `<run_id>/preflight/` |
| CP1 runtime install | Runtime is hash-locked/manifested, isolated from default `~/.claude`, rollback metadata exists, unknown version fails closed | runtime manifest/rollback metadata summary, no default OAuth/cookie/setup-token inheritance |
| CP2 overlay | Bridge models cannot be live before CP4/CP5; Claude native shape equality remains unchanged | model overlay proof + native shape equality result |
| CP3 background/subagent | Background/compact/title/summary/Agent model resolution follows active provider; replay-safe transcript boundary exists across provider switches | provider-profile resolver artifact + transcript-boundary tests |
| CP4 route trust | Per-request signed route hint and backend catalog validation fail closed on model/route/client/hash/nonce/body mismatch | route trust negative tests and replay-cache tests |
| CP5 provider isolation | Non-Claude never enters formal_pool/native attestation; usage/cache/audit separated by provider/credential scope | provider registry negative tests + bridge/native spoof rejection |
| CP6 bridge parity | Text/tools/SSE/order/usage/cache/error/reasoning mapping pass or degrade explicitly; DeepSeek prefers Anthropic messages when probe passes | provider probe + stream/tool/cache unit tests |
| CP7 UX/rollback | Official `claude` is not overwritten; rollback/disable path works without destructive cleanup | rollback metadata + dry-run rollback command output |
| CP8 strict-live | 3017 live evidence covers native, bridge, subagent, model switch, ToolSearch/MCP, workflow, long context, interruption, cache/account audit, netwatch/direct-bypass | strict-live evidence bundle after approved rollout |

**Control-plane matrix rule:** before modifying control-plane code, update `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-claude-code-control-plane-classification-matrix.md` or a linked repair addendum. Every known path must have action, safe intent schema, cache scope, TTL, isolation, redaction, and fail-closed behavior.

**Formal-pool ingress rule:** 3017 canary may pass only if evidence proves reviewed remote ingress/app-level gateway config, no ordinary Anthropic-compatible passthrough, selected account/persona/egress bucket fail-closed when missing, `x-sub2api-*` local trust headers are stripped/not forwarded as remote trust, and account 132 remains untouched.

---

## 4. Final Repair Workstreams

### Workstream A — CP gate preflight and 2.1.177 drift gate

**Purpose:** Prevent fixing one symptom while regressing docs 45/47 hard gates.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/model_overlay.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/claude_code_route_trust.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/live_matrix.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md`

**Plan:**

- Add/refresh a preflight checklist for CP0-CP8 before implementation and before 3017 rollout.
- Treat Claude Code 2.1.177 as candidate drift from older doc baselines: bundle strings, control-plane shape, model capability functions, and unknown path behavior must be audited.
- Require all changed launch/runtime artifacts to bind runtime hash, overlay hash, catalog hash, session/run id, and route decision.
- Fail closed if a model/route capability is unknown.

**Acceptance:** A single repair-run evidence bundle can say which CP gates are local-pass, fixture-pass, or strict-live-pass, without raw body/secrets.

---

### Workstream B — `/model` per-model effort UI and backend effort enforcement

**Purpose:** Make UI effort controls truthful per bridge model and enforce the same truth server-side.

**Files to inspect/modify as needed:**

- `/Users/muqihang/Library/Application Support/zhumeng-agent/runtimes/claude-code/cache/manual/2.1.177/claude` (read-only bundle audit / patch target only if approved)
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/launcher.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/model_overlay.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_provider_registry.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_stream.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_openai.go`

**Provider policy:**

- Claude native: unchanged official behavior.
- GPT 5.5 / 5.4 / 5.4-mini: `low`, `medium`, `high`, `xhigh`; never `max`.
- DeepSeek V4 Pro / Flash: `high`, `max`; never `medium` as default/effective selected level.
- GLM 5.2: `high`, `max` if and only if live/probe policy passes.
- AGNES / Kimi: no effort UI and no outbound effort unless future provider probe changes this explicitly.

**Plan:**

- Finish tracing runtime source for the actual `us(model, key)` / capability data source behind `zP`, `oyH`, `KMH`, `Qs`, `$Q9`, and `AxO`.
- Prefer preload/runtime metadata injection over binary patch. If impossible, design a narrow bridge-only patch that leaves Claude native capability functions untouched.
- Add a runtime capability probe or pseudo-UI probe that proves `/model` is reading the patched capability source, not merely that outbound preload clamps requests.
- Keep outbound `sanitizeBridgeEffort()` as a second safety layer.
- Add backend validation so unsupported `output_config.effort` is rejected or normalized according to provider policy before upstream dispatch.

**Acceptance tests:**

- DeepSeek selected model never displays/effectively selects Medium.
- GPT selected model never displays/effectively selects Max.
- AGNES/Kimi display no effort selector and send no effort.
- Claude native snapshot remains unchanged.
- Backend rejects or clamps unsupported effort even if a malicious client bypasses UI.

---

### Doc 45 Capability Delta Classification

| Capability affected by custom/loopback Base URL | L8 stance | Required evidence |
| --- | --- | --- |
| ToolSearch / `ToolSearchTool` / `tool_reference` / `defer_loading` | restore to local Sub2API parity via runtime config/preload/patch once doctor + fixed MCP/deferred-tool healthcheck pass; for bridge routes, additionally materialize deferred tools, implement a ToolSearch shim, or disable ToolSearch per bridge model with an explicit degraded reason | shape A/B + start-path env/status + native preserved tool_reference/defer_loading evidence + bridge GPT/DeepSeek ToolSearch/MCP dispatch evidence |
| FGTS / eager input streaming | trace gate first; restore only if managed local injection preserves safety, otherwise degraded/observe-only with reason | safe boolean audit + gate trace + no direct official egress |
| Prompt caching for Claude native | parity target: preserve/enable Claude prompt-caching request shape through local Sub2API/CC Gateway and prove upstream usage fields | prompt-caching beta/context/cache_control presence + cache_creation/cache_read usage + no raw payload |
| 1M context / thinking / context management | must not be regressed | native shape snapshot + long-context smoke |
| `x-client-request-id` | field-parity audit only; do not synthesize unless reviewed | present/absent safe audit |
| policy limits / remote settings / settings sync / team memory / model capabilities / GrowthBook | stub/block/suppress with explicit matrix; not restored parity | control-plane matrix + unknown-drift quarantine tests |
| official CONNECT/direct egress | fail-closed | netwatch/direct-bypass evidence |

### Workstream C — Capability profile, ToolSearch/Prompt Caching parity, FGTS gate tracing, and control-plane matrix

**Purpose:** Close doc 45 capability-loss gaps by making the managed local Sub2API path capability-equivalent to Anthropic for supported request-shape features, without unsafe direct official egress or raw control-plane leakage.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/cli.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/profile.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/doctor.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/shape_check.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/status.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/cli_control_plane_guard.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/cli_control_plane_policy.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/control_plane_path_matrix_config.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/control_plane_policy.go`

**Plan:**

- Wire `ClaudeCodeCapabilityProfile`, `evaluate_toolsearch_profile()`, and `apply_capability_profile()` into the real `claude-code start` path.
- Make `ENABLE_TOOL_SEARCH=true` the intended healthy state after version-family match, fixed MCP/deferred-tool healthcheck, pending/deferred tool evidence, model support, and no kill switch. Otherwise choose explicit `auto` or `false` as degraded states with status reasons.
- Add localhost-only A/B shape checks for unset/auto/true and resume history containing `tool_reference` / `defer_loading`.
- Add bridge-specific ToolSearch/deferred-tool handling before any bridge provider is marked native-like under `ENABLE_TOOL_SEARCH=true`:
  - preferred path: materialize `tool_reference` / deferred MCP tools into ordinary Claude tool definitions before bridge provider dispatch, preserving tool names, input schemas, descriptions, and route-trust metadata;
  - alternate path: implement a bridge ToolSearch shim that resolves search/deferred references locally without sending unsupported lazy shapes to non-Claude providers;
  - fail-closed path: set per-model ToolSearch disabled/degraded status for that bridge route with explicit reasons, and ensure the bridge request cannot receive unresolved `ToolSearchTool`, `tool_reference`, or `defer_loading` payloads.
- Add Prompt Caching parity checks for Claude native: ensure top-level or block-level `cache_control`/prompt-caching beta/context-management shape survives runtime -> guard -> Sub2API/CC Gateway, and verify safe usage fields such as cache creation/read tokens when upstream returns them.
- Trace the FGTS/eager input streaming gate. Keep it observe-only until evidence proves a local managed injection/config path is reliable and safe; never solve it by direct official egress.
- Expand explicit control-plane matrix for policy limits, remote managed settings, settings sync, team memory, model capabilities, GrowthBook/feature flags, count_tokens, event logging, bootstrap, and official CONNECT/direct bypass.
- Unknown paths remain quarantine/block, but known doc-45 paths should have explicit action, safe intent schema, cache scope, TTL, and no-raw policy.

**Acceptance tests:**

- Production launch env changes based on doctor decision, not fixed `auto`.
- Native ToolSearch fixture proves `ToolSearchTool`, `tool_reference`, and `defer_loading` are preserved when enabled.
- Bridge ToolSearch fixture proves GPT and DeepSeek receive either materialized ordinary tools or a resolved shim result, never unresolved `tool_reference` / `defer_loading` shapes.
- If bridge ToolSearch healthcheck fails, the affected bridge model is marked `toolsearch_degraded` or disabled for ToolSearch with explicit reasons; the request path fails closed rather than silently sending unsupported shapes upstream.
- If healthcheck fails, status says `toolsearch_degraded` or equivalent with reasons.
- Official CONNECT/direct bypass is fail-closed.
- No raw telemetry/body/CCH persisted.

---

### Workstream D — DeepSeek cache and Anthropic-compatible transport truthfulness

**Purpose:** Stop guessing about DeepSeek cache and prove actual live path/cache behavior safely.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_stream.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_provider_registry.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/cli_control_plane_guard.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/live_matrix.py`

**Plan:**

- Confirm DeepSeek bridge uses Anthropic-compatible endpoint shape, preferably DeepSeek Anthropic base plus `/v1/messages`, and does not silently fall back to OpenAI-compatible paths unless probe/catalog explicitly says so.
- Change audit from “we injected `cache_control`” to provider-truthful evidence:
  - provider, route, client_type, preferred_protocol, selected_protocol, fallback_protocol, fallback_reason;
  - sanitized upstream path kind such as `/anthropic/v1/messages` or `/v1/messages` without credentials;
  - `deepseek_anthropic_cache_control_ignored_by_provider_docs=true` when applicable;
  - stable prefix digest(s) over redacted canonicalized prefix, never raw text;
  - request prefix token estimate bucket, not content;
  - response `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`;
  - whether fallback was used.
- Decide whether to remove DeepSeek `cache_control` injection or keep it as harmless compatibility metadata. Because DeepSeek docs say it is ignored, it must not be represented as the cache mechanism.
- Stabilize prefix serialization/order for system/history/tools; avoid changing stable prefix between retries or model switches except for explicit replay-safe boundary summaries.
- Add a two-pass canary cache test using a long stable prefix and short variable suffix, then verify response usage tokens and audit fields.

**Acceptance tests:**

- Unit tests prove DeepSeek cache audit records route/protocol/path/cache usage without raw body.
- Live 3017 evidence proves whether prompt cache hit/miss tokens appear and whether the second request improves.
- If no hit occurs, evidence identifies whether it is prefix instability, too-short prefix, fallback transport, provider best-effort miss, or missing usage fields.

---

### Workstream E — OpenAI/GPT and AGNES cache/effort/tool semantics

**Purpose:** Bring Responses/OpenAI-compatible bridge behavior closer to Claude Code native expectations.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/pkg/apicompat/anthropic_to_responses.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_openai.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_provider_registry.go`

**Plan:**

- Normalize GPT effort to OpenAI-supported levels and ensure `max` maps/rejects according to provider policy.
- Add safe audit for OpenAI cache behavior:
  - `cached_tokens` from `usage.prompt_tokens_details`;
  - optional `prompt_cache_key_present` if configured;
  - retention/prefix routing policy as enum, not raw values.
- Prove tools conversion preserves tool names, IDs, arguments, and tool-result pairing through a second turn.
- Re-evaluate `Strict=false` conversion: either preserve strictness where upstream supports it or explicitly record/justify degraded strict semantics.
- Apply the same cache/effort/tool audit framework to AGNES if it uses OpenAI/Responses-compatible transport.

**Acceptance tests:**

- GPT bridge rejects/disallows Max effort.
- GPT cache audit includes `cached_tokens` when provided and never logs prompt text.
- GPT/AGNES tool loop e2e proves `tool_use -> tool_result -> next turn`.
- Strict tools parity is either achieved or explicitly marked degraded with a fail-safe.

---

### Workstream F — Bridge streaming, tool loop, `multi_tool_use.parallel`, and Agent parity

**Purpose:** Make bridge streams valid Claude Code streams, including failure cases.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_stream.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_bridge_openai.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/server/routes/claude_code_compat_protocol_routes_test.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/provider_probe.py`

**Plan:**

- Add terminal/error fail-safe to Anthropic-compatible live path comparable to OpenAI bridge: never leave Claude Code with unterminated/invalid SSE on upstream error/truncation.
- Add golden tests for SSE order, `content_block_start`, `input_json_delta`, `content_block_stop`, `message_delta`, and `stop_reason=tool_use`.
- Add multi-turn bridge e2e tests for DeepSeek, GPT, and AGNES: first turn emits tool_use; second turn sends tool_result; bridge preserves pairing.
- Keep skeleton/mock fail-closed for `multi_tool_use.parallel` and `Agent`. Do not fabricate tool_use for unsupported live paths.
- Live bridge must explicitly allow `multi_tool_use.parallel` + `Agent` only after provider probe and route contract pass.
- When ToolSearch is enabled globally, bridge dispatch must check that deferred MCP/ToolSearch references were materialized or shim-resolved for the active provider. If unresolved lazy shapes remain, emit a safe structured bridge error or mark the route degraded before upstream dispatch.

**Acceptance tests:**

- Valid stream closure on success and failure.
- Parallel Agent requests are live-only and fail closed otherwise.
- Tool IDs remain stable across turns and providers.
- GPT and DeepSeek bridge tests cover ToolSearch/MCP enabled mode and prove unresolved `ToolSearchTool` / `tool_reference` / `defer_loading` payloads do not reach upstream.

---

### Workstream G — Cross-provider replay safety and formal-pool ingress safety

**Purpose:** Protect native Claude upstream during hot switching and future OAuth expansion.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/cli_control_plane_guard.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/transcript_boundary.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/handler/gateway_handler.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_native_attestation.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_provider_registry.go`

**Plan:**

- Add exact scenario tests: bridge model produces reasoning/provider-private/tool internals; user switches to Claude native; final native replay is sanitized.
- Add backend/provenance contract fields so native path can require replay-safe transcript markers or safe-summary hashes when foreign turns exist.
- Confirm bridge requests cannot claim `claude_code_native`, native attestation, formal_pool credential scope, or formal-pool account binding.
- Confirm local `x-sub2api-*` trust headers are not forwarded as remote trust material to formal-pool ingress.
- Verify account/formal-pool ingress stop criteria: reviewed ingress contract, managed auth, no ordinary passthrough, no account 132 edits, bridge isolated.
- Add future OAuth seam checklist: user-owned Anthropic OAuth is `provider_owner=user`, credential stored as local opaque ref, never mixed with Zhumeng formal-pool or bridge-pool credentials.

**Acceptance tests:**

- Claude native replay body contains no foreign reasoning/signature/provider-private/raw tool internals.
- Guard summary has replay-safety fields and hashes only.
- Bridge route spoof attempts fail closed.

---

### Workstream H — Provider probe truthfulness and CP8 strict-live matrix

**Purpose:** Make live enablement a release gate, not a catalog assumption.

**Files to inspect/modify as needed:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/provider_probe.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/live_matrix.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/claude_code_runtime_canary_config.py`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/claude_code_provider_registry.go`

**Plan:**

- Extend provider probes beyond fixture-only for AGNES/GLM/Kimi, or keep them live-disabled if probe truthfulness cannot be proven in this L8 scope.
- CP8 strict-live must cover at least:
  - Claude native;
  - GPT bridge;
  - DeepSeek bridge;
  - AGNES bridge if enabled;
  - GLM/Kimi only if enabled by probe;
  - subagent/Agent;
  - manual `/model` switch;
  - Claude -> DeepSeek/GPT -> Claude replay safety;
  - ToolSearch/MCP for Claude native and for at least GPT + DeepSeek bridge, proving materialization/shim success or explicit per-model degraded fail-closed behavior;
  - workflow/background/compact/title/summary resolution;
  - long context/context-management smoke;
  - interruption/cancel;
  - cache/account/audit;
  - netwatch/direct-bypass failure.
- Evidence bundle must be bound by run_id/runtime_hash/overlay_hash/catalog_hash and contain no raw bodies.

**Acceptance:** A provider is classified as exactly one of `strict-live-pass`, `degraded-pass`, `fixture-pass-only`, or `live-disabled` with explicit evidence and reason. `degraded-pass` is not native-like parity; it is allowed only for explicitly approved L8 degraded scope. No ambiguous “probably works”.

---

## 4C. Provider Nine-Point Release Matrix

Each provider must be classified as `strict-live-pass`, `degraded-pass`, `fixture-pass-only`, or `live-disabled`. No provider may be described as native-like unless every required cell has evidence.

| Provider | Tools | Agent | `multi_tool_use.parallel` | SSE success/failure closure | `tool_result` multi-turn | Cache | Effort | Live gating | CP8 strict-live |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Claude native | Official behavior unchanged | Native Agent behavior unchanged | Native behavior unchanged | Native shape equality | Native tool loop unaffected | Anthropic prompt caching/context-management observed safely | Official defaults | native attestation + formal_pool only | required |
| DeepSeek | Anthropic-compatible tool shape, e2e tested | live-only or disabled with reason | explicit pass/degraded/disabled reason | terminal/error fail-safe required | required | prefix KV via hit/miss usage; `cache_control` ignored/audited | high/max only | probe + CP4/CP5 | required |
| GPT/OpenAI | Responses conversion e2e tested; strictness resolved | live-only or disabled with reason | explicit pass/degraded/disabled reason | stream closure via OpenAI bridge required | required | `cached_tokens`/prompt-cache-key audit | low/medium/high/xhigh only | probe + CP4/CP5 | required |
| AGNES | If Responses-compatible, same framework as GPT; otherwise disabled | live-only or disabled with reason | explicit pass/degraded/disabled reason | provider-specific closure required before live | required before native-like claim | mechanism must be identified before live claim | no effort by default | probe + CP4/CP5; otherwise disabled | required if enabled |
| GLM | Not L8 live by default; tool proof required before enabling | disabled unless probe proves | disabled unless probe proves | Anthropic-compatible closure proof required | required before enabling | provider-specific cache evidence required | high/max only if enabled | catalog-visible/live-disabled by default | required if enabled |
| Kimi | Not L8 live by default; tool proof required before enabling | disabled unless probe proves | disabled unless probe proves | closure proof required before enabling | required before enabling | provider-specific cache evidence required | no fake effort | catalog-visible/live-disabled by default | required if enabled |

---

## 5. Provider Release Gate Checklist

### Claude native

- Native formal-pool only for allowlisted Claude provider/model.
- Native attestation and route trust pass.
- Replay safety cleans all foreign artifacts after provider switch.
- Prompt caching/context-management/thinking fields are preserved/enabled through local Sub2API/CC Gateway where supported and proven by safe usage/shape evidence; no raw payload.
- Long-context smoke does not regress.

### DeepSeek

- Preferred protocol is Anthropic-compatible `/v1/messages` via DeepSeek Anthropic base.
- No OpenAI-compatible fallback unless probe/catalog says why.
- Cache judged by prefix stability and `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`, not by Anthropic `cache_control` injection.
- `cache_control` ignored status documented/audited if fields remain.
- Effort: High/Max only.
- Tool/Agent/parallel live tested or fail-closed.

### GPT/OpenAI

- Responses path selected intentionally.
- Effort: Low/Medium/High/XHigh only; no Max.
- Cache audit uses `cached_tokens` and optional `prompt_cache_key_present` enum.
- Strict/tool loop behavior proven or degraded explicitly.

### AGNES

- No effort unless provider profile/probe explicitly changes.
- Tool/cache/strict-live evidence required before claiming native-like support.
- If not proven, remain live-disabled or marked degraded.

### GLM

- Effort High/Max only if live/probe-enabled.
- Anthropic-compatible shape and stream closure proven before live.
- Otherwise catalog-visible but live-disabled.

### Kimi

- No fake effort.
- Provider-specific cache strategy separated from DeepSeek/OpenAI.
- Live only after probe + CP4/CP5/CP8 evidence.

---

## 6. Implementation Order After Approval

1. **Read-only preflight:** CP gates, 2.1.177 bundle strings, current guard summary, 3017 logs with safe filters.
2. **Effort UI fix:** add failing tests/probes first; patch runtime/preload capability surface; add backend effort enforcement.
3. **Capability/control-plane wiring:** connect capability profile/doctor/shape healthcheck/status; expand control-plane explicit matrix.
4. **Cache observability:** DeepSeek/OpenAI/Anthropic-safe audit fields; remove misleading cache claims; add deterministic prefix tests.
5. **Bridge stream/tool parity:** terminal fail-safe, golden SSE, multi-turn tool loop, Agent/parallel live gates.
6. **Replay/formal-pool safety:** cross-provider taint tests, backend/provenance contract, ingress safety checks.
7. **Provider probes/live matrix:** extend CP8 evidence and live gating for AGNES/GLM/Kimi.
8. **3017-only rollout:** rebuild/hot-switch 3017 only; run health/version/UI/cache/replay/live-matrix evidence; do not touch 3012.

---

## 6A. Phased Execution, Evidence, And Rollback

### Phase 0 — Decision freeze

Close all items in section 4A before coding. Record the decision summary at `artifacts/claude-code-canary/<run_id>/preflight/decision-freeze.json` with no secrets.

### Phase 1 — Local/unit implementation only

Do not touch 3017. Implement and test in small gates: B1 capability-source discovery, B2 backend effort enforcement, C1 capability profile wiring, C2 bridge ToolSearch/deferred-tool materialization/shim/degrade gate, D/E cache audit, F stream/tool loop, G replay/formal-pool safety, H provider live matrix.

### Phase 2 — Local runtime verification only

Run non-interactive `claude-code start -- --version`, pseudo-UI/capability probes, doctor/status/shape checks, and local live-matrix assembly without 3017 rebuild.

### Phase 3 — 3017 canary rollout only after approval

Go/no-go conditions: all required unit gates pass; provider scope is frozen; runtime patch decision is frozen; evidence redaction scan passes; rollback pointer is known. After rollout, verify health, version, `/model` effort evidence, DeepSeek two-pass cache evidence, GPT tool loop evidence, Claude->bridge->Claude replay safety, ToolSearch/MCP shape, and CP8 strict-live.

### Phase 4 — Rollback and closeout

Rollback immediately if replay safety fails, formal-pool isolation fails, redaction/secret scan fails, stream closure is invalid, provider route is ambiguous, ToolSearch escalates unsafely, or 3017 health/version fails. Rollback target is the previous known-good 3017 image/runtime pointer; use existing runtime rollback metadata/CLI where available and record rollback summaries. Do not use destructive cleanup.

### Evidence directory contract

Use this run-id layout:

```text
artifacts/claude-code-canary/<run_id>/preflight/
artifacts/claude-code-canary/<run_id>/unit/
artifacts/claude-code-canary/<run_id>/ui/
artifacts/claude-code-canary/<run_id>/cache/
artifacts/claude-code-canary/<run_id>/replay/
artifacts/claude-code-canary/<run_id>/live-matrix/
artifacts/claude-code-canary/<run_id>/rollback/
```

Every evidence file must pass a redaction scan before being shared or used as final proof. Evidence must include run_id, runtime_hash, overlay_hash, catalog_hash, provider scope, and audit schema version. It must not include raw bodies, raw prompts, raw responses, keys, cookies, Authorization values, or unscoped content hashes.

---

## 7. Targeted Test Commands To Preserve/Extend

Python targeted baseline:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/tools/zhumeng-agent
.venv/bin/python -m pytest \
  tests/test_claude_code_launcher.py \
  tests/test_claude_code_toolsearch_profile.py \
  tests/test_claude_code_provider_probe_cp6.py \
  tests/test_claude_code_live_matrix_cp8.py \
  tests/test_claude_code_transcript_boundary_cp6.py \
  -q
```

Go targeted baseline:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service ./internal/server/routes ./internal/pkg/apicompat \
  -run 'ClaudeCode|AnthropicCompat|CP6|CP8|PromptCache|ToolUse|Effort|RouteTrust' \
  -count=1
```

Non-interactive runtime launch preflight:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
tools/zhumeng-agent/.venv/bin/python -m zhumeng_agent.cli claude-code start \
  --state-root artifacts/claude-code-canary \
  --runtime-root "/Users/muqihang/Library/Application Support/zhumeng-agent/runtimes" \
  --project-cwd "$PWD" \
  --guard-port 39817 \
  -- --version
```

3017 health after approved rollout only:

```bash
curl -sS http://127.0.0.1:3017/health
```

---

## 8. Safe Audit Fields To Add Or Normalize

No field may contain raw prompt/body/response/secret/header value.

Suggested versioned audit groups:

```json
{
  "audit_schema_version": "claude_code_l8_repair_v1",
  "route": {
    "provider": "deepseek|openai|claude|agnes|glm|kimi",
    "client_type": "claude_code_bridge_*|claude_code_native",
    "preferred_protocol": "anthropic_messages|openai_responses|...",
    "selected_protocol": "...",
    "fallback_protocol": "...",
    "fallback_reason": "..."
  },
  "capability": {
    "toolsearch_mode": "auto|true|false",
    "toolsearch_status": "ready|degraded|profile_mismatch",
    "fgts_mode": "observe_only|disabled|enabled",
    "effort_allowed_levels": ["low", "medium", "high", "xhigh", "max"],
    "effort_effective": "..."
  },
  "cache": {
    "provider_cache_mechanism": "anthropic_cache_control|deepseek_prefix_kv|openai_prompt_cache|none",
    "upstream_path_kind": "/v1/messages|/anthropic/v1/messages|/responses",
    "stable_prefix_hmac": "hmac-sha256:<key-id>:<purpose-scoped-redacted-prefix-canonical-mac>",
    "stable_prefix_token_bucket": "lt_1k|1k_4k|4k_16k|gt_16k",
    "cache_control_present": true,
    "cache_control_locations": ["system", "tools", "history", "top_level"],
    "cache_control_provider_ignored": true,
    "prompt_cache_key_present": false,
    "cache_read_tokens": 0,
    "cache_write_tokens": 0,
    "cache_miss_tokens": 0,
    "cached_tokens": 0
  },
  "replay_safety": {
    "foreign_provider_history_present": true,
    "foreign_reasoning_stripped": true,
    "foreign_tool_internals_stripped": true,
    "safe_summary_hmacs": ["hmac-sha256:<key-id>:..."]
  }
}
```

---

## 9. Decisions That Must Be Reconfirmed Before Implementation

Section 4A records the default proposed decisions for implementation. Before coding, the implementer must explicitly confirm them with the user or mark them accepted in the run preflight evidence. If any answer changes, update this plan before touching code.

---

## 10. Non-Goals

- Do not redesign all provider registry architecture beyond needed seams/gates.
- Do not implement local Anthropic OAuth UI in this repair; only preserve credential-scope/owner seams.
- Do not claim parity for control-plane services that remain intentionally stubbed/suppressed; do claim and test parity for request-shape capabilities we explicitly restore through managed runtime hooks, such as ToolSearch and Prompt Caching.
- Do not broaden live probing against formal-pool ingress beyond approved Claude Code runtime paths.
- Do not weaken replay safety to preserve foreign reasoning continuity.
