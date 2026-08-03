# 逐梦 Agent Claude Code Native Takeover Baseline Memo

日期：2026-06-08
阶段：CP0 材料审计与基线冻结
执行 worktree：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-native-takeover`
计划：`docs/anti-ban/46-zhumeng-agent-claude-code-native-takeover-plan.md`

## 1. CP0 结论

本阶段只冻结 Claude Code CLI native takeover 基线，不启动真实 Claude Code，不发送真实 Anthropic/Claude 请求，不读取或上传本机默认 `~/.claude` OAuth、cookie、setup-token。

46 号计划的落地方向保持为：

```text
真实 Claude Code CLI
  -> 逐梦 Agent launcher
  -> isolated CLAUDE_CONFIG_DIR
  -> local loopback guard
  -> Sub2API /v1/messages
  -> CC Gateway /v1/messages?beta=true internal route
  -> persona / CCH signing / egress bucket / raw-safe audit
  -> formal pool upstream
```

本阶段明确不做 GPT、DeepSeek、其他供应商模型注入 Claude Code；多模型混合只保留为后续 47 号计划。

## 2. 本地状态与冲突边界

- 主 worktree：存在脏文件和未跟踪文件，未触碰。
- 本 worktree：从 `main` 新建分支 `codex/claude-code-native-takeover`，创建后基线 clean。
- 并行 worktree：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/codex-gateway-responses-gap-audit` 正在执行 Codex Gateway Responses Gap Audit。
- CP0 未修改以下禁止范围：
  - `backend/internal/service/codex_gateway_*`
  - `backend/internal/pkg/apicompat/*`
  - `docs/codex-gateway/*`
  - DeepSeek / AGNES / Codex Desktop Gateway 专项逻辑
- `/Users/muqihang/chelingxi_workspace/cc-gateway` 仅做只读状态确认，未修改；当时 `git status --short` 仅显示未跟踪 `.claude/` 与 `.worktrees/`。

## 3. 材料基线

### 3.1 46 号计划

46 号计划定义了 CP0-CP8 的落地顺序和硬边界：localhost/mock only、messages/control-plane/local observability 三链路分离、native 与 compat 区分、direct official bypass fail closed、全链路只记录脱敏摘要。

关键硬边界：

1. 用户必须通过逐梦 Agent 显式启动 Claude Code CLI。
2. 主 messages 请求必须经过本机 loopback guard，再进入 Sub2API / CC Gateway。
3. Control-plane 只能生成 safe intent 并由中心策略 suppress/stub/block/shadow，不能复用 messages CCH signing。
4. Process netwatch 必须记录 destination bucket，并能发现 guard bypass。
5. 不读取、保存、上传 raw token、prompt、body、telemetry、CCH、email、账号/组织 UUID、proxy credential。
6. 发现 `api.anthropic.com`、`platform.claude.com`、`claude.ai` direct egress bypass 必须 fail closed。
7. 本阶段不 patch Claude Code CLI 源码，不篡改请求 body，不用全局系统代理或无提示 MITM。

### 3.2 45 号 custom Base URL 能力差异

`docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md` 的基线结论：

- 裸改 `ANTHROPIC_BASE_URL` 会触发 first-party host gate 差异。
- custom Base URL 下 ToolSearch / `tool_reference` / `defer_loading` 可能默认关闭。
- FGTS / eager input streaming、request id、policy limits、remote settings、settings sync、team memory、model capabilities、GrowthBook、event logging 均可能产生 host-gated 差异。
- `ENABLE_TOOL_SEARCH=auto` 只是保守 fallback；固定 MCP/deferred-tool shape healthcheck 通过后，可信 version/profile 才能升级为 `true`。
- 不应为了 FGTS 或 request id 伪装 official host；native path 不应把 server-filled shape 伪装成真实 CLI-through body。
- 1m context、thinking、Opus/Sonnet 不应因 custom Base URL 被误关。

### 3.3 V1/V2 本机采集

只读取 safe summary 文件名、报告摘要和计数字段；未读取 raw token/cookie/setup-token。

采集根目录：`/Users/muqihang/.zhumeng/claude-code-lab/captures`

| Capture | Mode | Messages | Control-plane | Netwatch | Sensitive scan |
|---|---:|---:|---:|---:|---:|
| `20260529-042841` | `egress_guard` | 109 requests, 107 responses | 260 control-plane requests, 376 CONNECT | 702 connections, 139 potential bypass | PASS |
| `20260530-025229` | `egress_guard` | 11 requests, 11 responses | 15 control-plane requests, 26 CONNECT | 15 connections, 0 potential bypass | PASS |
| `20260601-003152` | `egress_guard` | 82 requests, count_tokens observed | 326 control-plane requests, 430 CONNECT | 3492 connections, 0 potential bypass | PASS |
| `20260601-194006` | `egress_guard` | 477 requests, count_tokens observed | 713 control-plane requests, 848 CONNECT | 5058 connections, 0 potential bypass | PASS |
| `20260602-223311` | `egress_guard` | safe guard/netwatch JSONL present | safe guard/netwatch JSONL present | 243 connections | report pending/not present |

Observed safe summaries support these CP0 assumptions:

- `/v1/messages?beta=true` can be routed through local guard.
- `/v1/messages/count_tokens` appears and must be treated as a native-aware route, not as generic control-plane.
- Event logging and eval paths appear as control-plane traffic and must not be raw-uploaded.
- Official-domain CONNECT/direct egress attempts are observable and require block/stub/fail-closed policy.
- Safe reports store counts, route templates, header presence, model names, body key sets, size buckets and destination buckets; they do not persist raw prompt/body/token/telemetry/CCH.

### 3.4 Control-plane strategy documents

Relevant docs:

- `docs/anti-ban/30-claude-code-control-plane-classification-strategy.md`
- `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md`
- `docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md`

Frozen principles:

- Messages is the primary data path; control-plane is a separate safety domain.
- Local guard may inspect only to classify/redact/forward; safe deliverables cannot persist raw body or prompt.
- Control-plane intent must include route template, method, host bucket, header names, auth presence shape, schema summary and safe refs; plain deterministic body/query/account hashes are forbidden.
- Telemetry/eval: suppress or shadow-only; raw telemetry body never enters Sub2API, CC Gateway or Anthropic.
- Bootstrap/settings/policy/MCP/team-memory: stub/cache/block/shadow according to explicit policy.
- Unknown drift: quarantine/block by default.
- B2 production does not mean raw control-plane forwarding; it means all control-plane safe intents enter central decisioning first.

### 3.5 Formal pool, session budget, onboarding, compat

Relevant docs/files:

- `docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md`
- `docs/anti-ban/39-formal-pool-session-budget-strategy.md`
- `docs/anti-ban/40-formal-pool-new-account-hard-gates.md`
- `docs/anti-ban/41-formal-pool-claude-account-onboarding-wizard-plan.md`
- `docs/anti-ban/44-non-claude-code-client-compat-adapter-design.md`
- `backend/internal/service/claude_code_compat_protocol.go`
- `backend/internal/service/claude_code_compat_shape.go`
- `backend/internal/service/claude_code_compat_shape_healthcheck.go`
- `backend/internal/service/session_budget.go`
- `backend/internal/service/control_plane_intent.go`
- `backend/internal/service/control_plane_attestation.go`

Frozen principles:

- New account lifecycle remains `imported -> refreshed -> runtime_registered -> healthcheck_passed -> warming -> production -> quarantined`.
- Native takeover healthchecks can provide client profile evidence, but cannot replace temporary-key, single-account-pin, no-stale-evidence account onboarding gates.
- Session budget in production remains observe-only except P0 blocks: verifier failure, fallback, proxy mismatch, 401/403/risk/hold, unsafe control-plane, raw sensitive leak, direct official bypass.
- 44 compat path is L2 server-filled high-fidelity compatibility and must not advertise native capability.
- 46 native path must carry guard-attested `client_type=claude_code_native` and remain distinct from `claude_code_compat`.

### 3.6 CC Gateway read-only baseline

The plan references `/Users/muqihang/chelingxi_workspace/cc-gateway` algorithms:

- `src/policy.ts`: route policy and shared-pool route decisions.
- `src/rewriter.ts`: final request rewrite/signing pipeline boundaries.
- `src/proxy.ts`: proxy execution and audit capture.
- `src/persona-registry.ts` and `src/persona-resolver.ts`: dynamic persona/profile selection.
- `src/upstream-safety.ts`: upstream safety verification.

CP0 did not modify `cc-gateway`. Future CP5 changes in Sub2API/CC Gateway scope require a fresh status check and explicit conflict review before editing.

## 4. B2 control-plane path matrix baseline

CP0 freezes the B2 matrix as configuration-oriented policy. Every row must be represented by explicit config fields: `action`, `cache_scope`, `schema_allowlist`, `ttl`, `quarantine_on_mismatch`, and `raw_forbidden=true`.

| Path family | Initial action | Cache scope | Schema allowlist | TTL baseline | Quarantine on mismatch | Raw forbidden |
|---|---|---|---|---|---:|---:|
| bootstrap / feature flags | `stub + safe_intent` | `version_profile` | required | short | yes | yes |
| policy limits | `stub + safe_intent` | `account_profile` | required | short | yes | yes |
| public MCP registry | `stub_or_block + safe_intent` | `public_registry` | required | medium | yes | yes |
| MCP server metadata | `block_or_shadow + safe_intent` | `user_account_profile` | required before any upgrade | short | yes | yes |
| settings / team memory | `block_or_shadow + safe_intent` | `none_or_user_account_profile` | required before any upgrade | none/short | yes | yes |
| telemetry / eval | `suppress_or_shadow + safe_intent` | `none_or_synthetic` | synthetic-only later | none | yes | yes |
| unknown drift | `block + quarantine` | `none` | none | none | yes | yes |

This matrix is a guardrail for CP2 and CP5. It must not be implemented as permissive wildcard forwarding.

## 5. Existing implementation baseline

### 5.1 zhumeng-agent package

Current package state before CP1:

- Missing directory: `tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/`
- Existing adapter base: `tools/zhumeng-agent/src/zhumeng_agent/adapters/base.py`
- Existing CLI is Codex-oriented: `tools/zhumeng-agent/src/zhumeng_agent/cli.py`
- Existing doctor reports Codex only: `tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
- Existing Codex adapter/capture code lives under `tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/` and is not Claude Code native takeover.

### 5.2 Root-level lab/prototype tools

Reusable productization inputs:

- `tools/claude_code_lab_capture.py`
- `tools/cli_control_plane_guard.py`
- `tools/claude_code_lab_netwatch.py`
- `tools/claude_code_lab_report.py`
- `tools/cli_guard_attestation.py`
- `tools/cli_control_plane_intent.py`
- `tools/cli_session_budget.py`

These are currently prototype/tooling-level modules, not a zhumeng-agent native adapter package.

## 6. CP1-CP8 execution boundaries

- CP1 should add `tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/` with version detection, profile dataclasses, isolated config path builder and safe env builder. It must not launch real Claude Code.
- CP2 should wrap guard functionality in agent native mode and preserve localhost/mock-only behavior.
- CP3 should productize netwatch summaries without payload/header recording.
- CP4 should introduce capability profile and ToolSearch doctor logic without relying on inherited shell defaults.
- CP5 may touch only Claude Code native attestation / compat distinction / control-plane intent / shape healthcheck / session budget / gateway native marker files. It must not touch codex gateway response audit files without explicit approval.
- CP6 should add native shape healthcheck fixtures using mock upstream only.
- CP7 should add user/operator status and runbook.
- CP8 should run final tests and sensitive scan.

## 7. CP0 verification commands

Planned/used CP0 verification:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-native-takeover
python3 tools/safe_deliverable_sensitive_scan.py --root docs/anti-ban/46-zhumeng-agent-claude-code-native-baseline-memo.md --max-findings 100
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
uv run --python /opt/homebrew/bin/python3 --with pytest python -m pytest tools/tests/test_safe_deliverable_sensitive_scan.py -q
```

Default `python` in the shell currently resolves to conda Python 3.13.2 and segfaults while importing `readline` during pytest startup. CP0 verification therefore used Homebrew Python 3.14 via `uv run --python /opt/homebrew/bin/python3 --with pytest`, which passed. This is an environment issue, not a CP0 deliverable failure.

Focused tests for later checkpoints:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-native-takeover/tools/zhumeng-agent
python -m pytest tests/test_codex_launcher.py tests/test_codex_capture_config.py tests/test_codex_capture_injector.py tests/test_codex_capture_shape.py tests/test_cli.py -q
```

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-native-takeover
python -m pytest tools/tests/test_cli_control_plane_guard.py tools/tests/test_cli_control_plane_guard_integration.py tools/tests/test_cli_control_plane_network_safety.py tools/tests/test_cli_guard_attestation.py tools/tests/test_claude_code_lab_netwatch.py tools/tests/test_cli_control_plane_policy.py tools/tests/test_cli_session_budget.py -q
```

## 8. CP0 acceptance status

- Localhost/mock only: PASS
- No real Anthropic/Claude request: PASS
- No default `~/.claude` token/cookie/setup-token read: PASS
- No raw sensitive added to deliverable: PASS (`files_scanned=1`, `findings=0`)
- No multi-model injection: PASS
- Forbidden Codex Gateway / DeepSeek / AGNES areas untouched: PASS
- Parallel worktree conflict avoided: PASS
