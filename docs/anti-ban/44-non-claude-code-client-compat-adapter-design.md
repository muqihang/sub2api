# 非 Claude Code 客户端兼容接入层设计

日期：2026-06-02  
状态：IMPLEMENTED v1，Anthropic-only + high-fidelity Claude Code-compatible 服务端适配器  
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

关联材料：

- `docs/anti-ban/30-claude-code-control-plane-classification-strategy.md`（控制面分类）
- `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md`（正式号池控制面上传）
- `docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md`（动态 Persona 映射）
- `docs/anti-ban/37-formal-pool-control-plane-and-dynamic-persona-implementation-plan.md`（实施计划 + P0 硬约束）
- `docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md`（Synthetic Telemetry）
- `docs/anti-ban/39-formal-pool-session-budget-strategy.md`（Session Budget）
- `docs/anti-ban/40-formal-pool-new-account-hard-gates.md`（新号硬门禁）
- `docs/anti-ban/40-claude-code-local-capture-lab.md`（本机采集实验室）
- `docs/anti-ban/41-formal-pool-claude-code-shape-healthcheck.md`（形状健康检查）
- `docs/anti-ban/42-formal-pool-status-dashboard.md`（状态面板）
- `docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md`（custom Base URL 能力差异 / 逐梦 Agent 接管策略）
- `docs/anti-ban/cch-algorithm.md`（CCH 算法边界）
- `docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/field-audit-report.md`（真实字段审计）
- `docs/anti-ban/captures/real-cli-through-highmax-200-2026-05-24/report.md`（highmax canary）
- `docs/anti-ban/runtime-productization/2026-05-24-cli-through/`（产品化 runtime）
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`（body / prompt rewrite）
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`（messages signing / billing / CCH / verifier）
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-registry.ts`（Persona Registry）
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/upstream-safety.ts`（上游安全）
- `/Users/muqihang/chelingxi_workspace/reference-projects/agent-frameworks/claude_code_src/`（逆向源码）
- `/Users/muqihang/.zhumeng/claude-code-lab/captures/`（本机 V1/V2 采集）

---

## 0. 审查修订摘要

本版修正 Descartes 审查中指出的阻断问题：

1. **ClientType 检测保持 attestation-first**：外部伪造的 Claude Code header 不能被识别为 native。
2. **协议范围收敛为 Anthropic-only**：本计划只暴露/支持 Anthropic `/v1/messages` 协议；OpenAI-compatible `/v1/chat/completions`、`/v1/responses` 不属于本计划，必须 fail closed 或由单独产品线处理。
3. **目标升级为高保真 Claude Code-compatible**：非 Claude Code 客户端经服务端处理后，应尽量达到 99%+ 的 Claude Code 请求 shape 一致性，但内部必须标记 `claude_code_compat` / `server_selected_persona` / `server_filled_shape`，不得冒充 native。
4. **吸收 45 号 custom Base URL 发现**：ToolSearch、`tool_reference`、`defer_loading`、FGTS、control-plane 分叉会影响 native parity；完整 native parity 交给逐梦 Agent + loopback guard，服务端只做可审计高保真兼容和能力真实的补形态。
5. **CCH / billing / signer 链路不变**：adapter 不生成 CCH，不伪造 billing；`rewriter.ts` 只做 body/prompt rewrite 与清理；`policy.ts` signing path 负责 prepend billing、CCH、verifier。

---

## 1. 背景与目标

### 1.1 当前已验证链路

```text
真实 Claude Code CLI → Sub2API → CC Gateway → Anthropic
```

已验证或已产品化的能力边界：

- Sonnet / Opus / Haiku Claude Code 形态模型；未来 Sonnet/Opus 新版本不机械阻断。
- 1m context、tools、thinking、context_management、stream、`max_tokens=32000` 不被生产预算削弱。
- CC Gateway sign-primary / verifier / post-sign mutation 检查 / no fallback。
- proxy egress、session budget observe-only、动态 persona resolver、新号硬门禁。
- 控制面分类与本机 guard 采集已形成材料，但 synthetic telemetry 仍按 doc 38 分阶段落地。

### 1.2 本设计要解决的问题

非 Claude Code CLI 客户端在本计划中仅指：

- 使用 Anthropic `/v1/messages` 协议的 Anthropic SDK；
- 使用 Anthropic `/v1/messages` JSON shape 的自写 HTTP client；
- 其他不是 Claude Code CLI、但已经按 Anthropic messages 协议发送请求的客户端。

明确不属于本计划：

- OpenAI-compatible `/v1/chat/completions`；
- OpenAI `/v1/responses`；
- ChatBox / Cherry Studio 等如果只会发 OpenAI-compatible 协议，必须由客户端侧或单独网关先转换成 Anthropic messages，本计划不接收其 OpenAI shape。

这些 Anthropic-format 非 CLI 客户端通常缺少：

- Claude Code CLI 形态的 headers / beta / session；
- system prompt 结构与 env block；
- `metadata.user_id` shape；
- Claude Code-like tools / thinking / context_management / output_config 组合；
- 控制面行为，如 telemetry、bootstrap、MCP registry、eval、count_tokens；
- CC Gateway messages signer 需要的安全前置条件。

### 1.3 目标与非目标

目标：

- 提供一个 **Anthropic-only 非 Claude Code 客户端兼容接入层**，只接收 Anthropic `/v1/messages` 协议，并归一化成服务端选择的 high-fidelity Claude-Code-like `/v1/messages` 形态，再交给 CC Gateway 的正式 messages 签名链路。
- 目标是 **Claude Code-compatible high fidelity**：在 headers、beta、session、system/env、metadata、thinking、context_management、output_config、tools shape、CCH signing evidence 等层面尽量达到 99%+ 真实 Claude Code 请求形态一致性。
- 保持生产安全：不泄露用户 token、代理凭据、账号明文身份；不绕过新号硬门禁；不把 canary hard gate 带入生产；server-filled 字段必须可审计、可灰度、可回滚。

非目标：

- 首发不支持 OpenAI-compatible `/v1/chat/completions` 或 `/v1/responses`，这些协议请求必须 fail closed / 404 / 400 safe error。
- 首发不实现 synthetic telemetry 真实上传；只保留 shadow-only / suppressed intent 数据，为 doc 38 后续阶段服务。
- 首发不自动替用户发 `count_tokens` 控制面请求。
- 首发不宣称 compat 请求是 `ClaudeCodeNative`；完整 native parity 仍属于逐梦 Agent + loopback guard。服务端可以高保真补形态，但必须标记 `server_filled_shape`。

### 1.4 安全边界精确定义

必须区分 **业务请求 payload** 和 **敏感/审计材料**：

- 用户消息、系统提示、工具参数作为业务请求的一部分，会瞬时经过 Sub2API、CC Gateway，并发送到上游完成推理。这是主链路功能要求。
- 这些 raw payload **不得**被持久化到日志、ledger、capture safe-deliverable、审计摘要、缓存、错误报告或后台接口。
- raw token、Authorization、x-api-key、cookie、refresh/access token、proxy credential、raw CCH、raw telemetry、email、账号/组织 UUID 明文不得进入可见日志或 safe-deliverable。
- telemetry/eval/control-plane 的 raw body 首发默认不保存、不 digest、不上传；只能记录脱敏 route、字段名、bucket、scoped HMAC ref。
- 所有摘要使用 scoped keyed HMAC 或 opaque ref；禁止 plain SHA/MD5/长期 deterministic digest。

---

## 2. 真实 Claude Code 形态基线

### 2.1 HTTP headers 基线

真实 Claude Code CLI 2.1.150 请求观测到的 header shape：

```text
User-Agent: claude-cli/2.1.150 (external, sdk-cli)
anthropic-version: 2023-06-01
x-app: cli
x-claude-code-session-id: <UUID v4>
anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advisor-tool-2026-03-01,effort-2025-11-24,extended-cache-ttl-2025-04-11
X-Stainless-Arch: arm64
X-Stainless-Lang: js
X-Stainless-OS: MacOS
X-Stainless-Package-Version: 0.94.0
X-Stainless-Retry-Count: 0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v22.22.2
X-Stainless-Timeout: 600
```

1m profile 可额外出现：

```text
context-1m-2025-08-07
```

兼容层原则：

- 非 native 请求的 canonical headers 由 **server-selected persona builder** 生成。
- 不信任、不透传外部 `User-Agent`、`x-app`、`x-claude-code-*`、`x-stainless-*`、`anthropic-beta`。
- 不保留 stale `Content-Length`；body 改写后由 HTTP stack 重新计算。

### 2.2 body top-level keys 基线

真实字段：

```text
context_management
max_tokens
messages
metadata
model
output_config
stream
system
thinking
tools
```

首发兼容策略：

| 字段 | 真实 CLI 基线 | 兼容层策略 |
|---|---|---|
| model | 用户选择 | 用户选择，经 known/candidate/gray resolver |
| max_tokens | 常见 32000 | 不降低能力；缺省使用 persona 默认；不做 canary 低阈值 clamp |
| stream | true | 保留用户意图；只支持 Anthropic streaming shape |
| tools | CLI 工具集合 / ToolSearch 动态加载 | 用户 Anthropic tools 透传、no-tools truthful、服务端真实 tool-runner、ToolSearch-compatible deferred tools（仅能力真实时） |
| thinking | enabled / budget | 仅在 persona/model 支持且协议可表达时注入；不伪装不可用能力 |
| output_config | effort 等 | 由 persona 生成，用户显式安全字段可保留 |
| context_management | 1m 相关 | 仅 known 1m profile + account capability + model resolver 允许时注入 |
| system | text block array | versioned template builder 构建 |
| metadata | `user_id` JSON string | server-issued session + account identity ref，由 CC Gateway rewrite |

### 2.3 system block shape

真实 CLI 常见 shape：

```text
[0] text: <system-reminder> + project/context reminder
[1] text: <env> + Platform/Shell/OS Version/Working directory/Home directory
[2] text: x-anthropic-billing-header: ...
```

兼容层首发模板必须使用真实字段名：

```text
Block 0:
<system-reminder>
{truthful capability reminder generated from tool mode and client protocol}
</system-reminder>

Block 1:
<env>
Platform: {persona.platform}
Shell: {persona.shell}
OS Version: {persona.os_version}
Working directory: {persona.working_directory}
Home directory: {persona.home_directory}
Current date: {date}
</env>
```

禁止事项：

- 不使用 `Workspace folder` 替代 `Working directory`。
- 不在 no-tools 模式下写“你可以使用这些工具”这种虚假能力提示。
- 不注入 raw billing header placeholder。若历史兼容需要 placeholder，也必须由 CC Gateway signing 前统一 strip，adapter 自身仍不得生成 CCH/billing 值。

### 2.4 metadata.user_id shape

真实 CLI shape：

```json
{
  "user_id": "{\"device_id\":\"...\",\"account_uuid\":\"...\",\"session_id\":\"...\"}"
}
```

兼容层策略：

- `session_id` 由服务端发放 UUID-like ID。
- `device_id` / `account_uuid` 不从用户请求读取；由号池账号 identity record 和 CC Gateway rewrite/sign path 派生。
- 后台、ledger、safe-deliverable 只显示 scoped ref，不显示 raw UUID/email。

### 2.5 CCH / billing / signer 链路

真实链路职责必须拆清：

```text
Sub2API compat adapter
  - 协议转换、body shape、system/env、metadata/session、工具/流式转换
  - strip 外部 billing/CCH/auth/cookie/attestation-like header
  - 不计算 CCH，不生成 billing header，不调用 messages signer
        ↓
CC Gateway rewriter.ts
  - rewriteMessagesBody / rewritePromptText
  - 清理外部 billing material
  - 替换 metadata identity refs / persona env / path
  - canonicalPersonaHeaders 注入规范 headers
        ↓
CC Gateway policy.ts signing path
  - removeExistingBillingMaterial
  - prepend `x-anthropic-billing-header` placeholder
  - compute CCH
  - verifySignedCCH
  - signer evidence gates
        ↓
Upstream send
  - post-sign mutation check
  - no fallback / fail closed
```

关键约束：

- adapter 不生成 CCH，不伪造 billing，不把控制面请求送进 messages signer。
- 外部传入的 `x-anthropic-billing-header`、`CCH marker`、`cc_version=`、billing block 必须被视为 untrusted billing input；在进入 signing 前由 adapter/rewriter/policy 层层 strip 或 fail closed。
- verifier 必须在上游发送前通过。
- signing 后禁止任何 body mutation。
- fallback、sign-strip fallback、verifier fail 都是 P0 hard block / quarantine 触发条件。

---

## 3. ClientType 与信任边界

### 3.1 ClientType 枚举

```go
type ClientType string

const (
    ClientTypeClaudeCodeNative     ClientType = "claude_code_native"
    ClientTypeClaudeCodeCompat     ClientType = "claude_code_compat"
    ClientTypeAnthropicSDK         ClientType = "anthropic_sdk"
    ClientTypeAnthropicGeneric     ClientType = "anthropic_generic"
    ClientTypeUnsupportedProtocol  ClientType = "unsupported_protocol"
    ClientTypeUntrustedSpoof       ClientType = "untrusted_spoof"
)
```

### 3.2 attestation-first 检测顺序

外部请求头不可作为 native 身份依据。检测顺序必须是：

```text
0. Entry strip/reject:
   - 删除或拒绝外部传入的 internal attestation-like headers。
   - 包括 x-zhumeng-guard-attestation、x-cc-gateway-persona-source、x-internal-* 等。

1. Trusted ingress validation:
   - 只有来自受信本机 guard / 受信 runtime route 的请求，且 attestation HMAC / nonce / timestamp / route binding 校验通过，才能进入 ClaudeCodeNative 候选。

2. Native shape verification:
   - 在 attestation 通过后，再验证 UA、x-app、session-id、system shape、beta、body keys。
   - shape 不完整则降级为 quarantined native drift，不继承动态 persona。

3. Spoof detection:
   - 如果外部请求伪造 claude-cli UA、x-app: cli、x-claude-code-session-id、x-stainless-*、Claude Code beta，但没有可信 attestation，则标记 ClientTypeUntrustedSpoof。
   - 可按配置 fail closed，或进入 ClaudeCodeCompat(server-selected)；绝不能识别为 ClaudeCodeNative。

4. Protocol detection:
   - `/v1/messages` 或 `/v1/messages?beta=true` with Anthropic SDK/generic Anthropic body and no attestation → AnthropicSDK / AnthropicGeneric / ClaudeCodeCompat(server-selected)
   - `/v1/chat/completions`、`/v1/responses`、OpenAI-compatible body shape → ClientTypeUnsupportedProtocol，返回 safe error；本计划不做协议转换
   - 其他白名单 control-plane path → 按 §8 control-plane tier 处理
   - 其他未知路径 → fail closed / quarantine
```

### 3.3 Persona 信任边界

- `ClaudeCodeNative` 才能走 dynamic persona resolver。
- `ClaudeCodeCompat` 只能使用 server-selected persona。
- `UntrustedSpoof` 不得继承用户 header、beta、session、stainless、system 中的 persona 信息。
- server-selected persona 必须来自 CC Gateway persona registry，而不是用户字段。

---

## 4. Anthropic-only Protocol Contract

### 4.1 endpoint matrix

本计划只暴露 Anthropic messages 协议。OpenAI-compatible 协议不进入本适配器。

| Inbound endpoint | Client family | 首发支持 | 转换目标 | 备注 |
|---|---|---:|---|---|
| `POST /v1/messages` | Anthropic SDK / generic Anthropic | 是 | `/v1/messages?beta=true` | Sub2API 接收入站标准路由；转发 CC Gateway 前规范化为 beta route |
| `POST /v1/messages?beta=true` | Anthropic beta / Claude-like | 是 | `/v1/messages?beta=true` | 外部 beta 不可信，由 server-selected persona 重建 |
| `POST /v1/messages/count_tokens` | Claude count_tokens | 首发禁用或 native-only | 无 | 非 native 不主动模拟；显式调用返回 safe error / stub |
| `POST /v1/chat/completions` | OpenAI-compatible | 否 | 无 | 返回 404/400 safe error；不做协议转换 |
| `POST /v1/responses` | OpenAI Responses | 否 | 无 | 返回 404/400 safe error；不做协议转换 |
| control-plane paths | telemetry/eval/bootstrap/MCP | 首发 suppress/stub/shadow | 无 | doc 38 / §8 分层灰度 |


### 4.1.1 Route normalization to CC Gateway

当前 CC Gateway shared-pool policy 只允许 messages beta route 进入正式签名链路。为兼容标准 Anthropic SDK，同时避免扩展 CC Gateway 裸路由导致策略分叉，本计划采用固定归一化：

```text
Inbound:  POST /v1/messages
Adapter:  validate Anthropic messages body + rebuild canonical beta/header/session
Outbound to CC Gateway: POST /v1/messages?beta=true
```

约束：

- 入站裸 `/v1/messages` 可以被接受，但只作为 Anthropic protocol compatibility；不能因此继承外部 beta/header/persona。
- 发往 CC Gateway 前必须统一为 `/v1/messages?beta=true`，由 server-selected persona 生成 `anthropic-beta`。
- 不要求 CC Gateway policy 直接支持裸 `/v1/messages`；如果未来要支持，必须单独修改 route policy、signing tests 和 fail-closed tests。
- AuditSummary 必须记录 `inbound_route=/v1/messages` 与 `cc_gateway_route=/v1/messages?beta=true`，避免排障时误判。


### 4.2 Anthropic `/v1/messages` high-fidelity normalize

输入必须已经是 Anthropic messages shape。适配器只做高保真 Claude Code-compatible normalize：

- `model`：按 known/candidate/gray resolver 校验，不做 OpenAI model mapping。
- `messages`：保留用户 Anthropic messages/content/tool_result 语义和顺序；作为 outbound business payload 瞬时经过服务器和上游，但不得进入日志、ledger、safe-deliverable 或后台响应。
- `system`：外部 system 不可信任为 Claude Code system；adapter 将用户 system 合并到 user-system extension block，并由 versioned Claude Code template 生成 env/system-reminder。
- `tools`：只接受 Anthropic tools schema；OpenAI `functions/tools` shape 直接 safe error。用户 tools 可 pass-through，但 system prompt 不能虚假宣称本机 Claude Code 文件/终端/MCP 工具。
- `thinking` / `output_config` / `context_management`：仅在 persona/model/account capability 允许时保留或规范化；不继承 canary hard cap。
- `metadata`：外部 metadata 不作为身份来源；由 server-issued session 和 selected pool account identity ref 重建。
- `stream`：只支持 Anthropic streaming shape；响应也保持 Anthropic shape。
- `anthropic-beta` / `x-app` / stainless / session headers：由 canonical persona builder 生成；外部值 strip/reject。

### 4.3 Fidelity levels

为了实现“99%+ 像真实 Claude Code 请求”，但不混淆来源，compat 分层如下：

| Level | 名称 | 目标 | 允许动作 | 标记 |
|---|---|---|---|---|
| L0 | Protocol / route miss | route/client type 不满足 compat contract | fail closed / no compat classification | `L0` |
| L1 | Auditable compat candidate | Anthropic messages route 与 client type 正确，但 denominator checks 未全通过 | 不得宣称高保真；仅用于测试降级 | `L1` |
| L2 | Server-filled high-fidelity compat | route、server-filled shape、audit、能力保留、安全摘要 denominator 全通过 | server-selected persona + CC Gateway rewrite/sign；不冒充 native | `client_type=claude_code_compat`、`server_filled_shape=true`、`capability_backed=false` |
| L3 | Capability-backed / native path | 有真实 tool runner、逐梦 Agent + loopback guard 或 native attestation 支撑 | 后续阶段；不得由普通 compat adapter 伪造 | `capability_backed=true` 或 `claude_code_native` |

本轮服务端 compat adapter 的实现目标是 L2：high-fidelity server-filled compatibility，且 `capability_backed=false`。完整 native parity 与 capability-backed ToolSearch 留给逐梦 Agent + loopback guard，不由普通非 CLI compat 请求升级。

### 4.4 ToolSearch / deferred tools compatibility

45 号文档确认：Claude Code CLI 在 custom `ANTHROPIC_BASE_URL` 下默认可能关闭 ToolSearch / `tool_reference` / `defer_loading`。对 compat adapter 的影响：

- Anthropic-format 非 CLI 客户端如果没有真实 tool runner，不得由服务端伪造 ToolSearch / MCP / deferred tools。
- 如果用户传入普通 Anthropic tools，当前实现走 `truthful_pass_through`，但这不是 Claude Code ToolSearch native parity。
- 如果我们后续实现服务端真实 tool-runner 或逐梦 Agent 托管工具执行器，可进入 capability-backed ToolSearch：允许生成 ToolSearch-compatible deferred tools，但必须真实可执行、可审计、可回滚。
- 外部传入 `tool_reference`、ToolSearch beta、`defer_loading`、`eager_input_streaming` 不得让请求升级为 native；当前实现选择 strip with audit，并记录 `tool_reference_present` / `defer_loading_present` / `eager_input_streaming_present` / `tool_search_mode=strip_with_audit` / `capability_backed=false`。
- `ENABLE_TOOL_SEARCH=auto/true` 是逐梦 Agent native path 的本机 profile 问题，不是 compat adapter 直接信任的输入。

### 4.5 Response and stream contract

- 对 Anthropic inbound，response 保持 Anthropic messages / Anthropic stream shape。
- 不输出 OpenAI chat/responses shape。
- Upstream 400/401/403/429/5xx 转为 Anthropic-compatible safe error。
- 不返回 raw upstream body 中的 token、account、email、raw CCH、proxy 信息。
- `401/403` 对正式号池账号触发 quarantine service；对用户侧 API key 返回 safe error。
- `missing_account_identity`、`egress_proxy_failure`、`raw_capture_missing`、`fallback_detected` 都返回 safe error code，并写 risk_event。

### 4.6 fixtures and acceptance

首发必须有固定 fixture：

- Anthropic SDK `/v1/messages` normalize to CC Gateway `/v1/messages?beta=true`。
- Generic Anthropic HTTP `/v1/messages` normalize to CC Gateway `/v1/messages?beta=true`。
- Spoofed Claude headers fail closed / compat(server-selected)。
- OpenAI `/v1/chat/completions` 和 `/v1/responses` fail closed，不进入 adapter。
- Anthropic tools pass-through truthful mode。
- no-tools truthful mode。
- server-filled shape audit fixture：确认 `server_filled_shape=true`、`persona_source=server_selected`、`client_type=claude_code_compat`。
- ToolSearch/deferred tools fixture：无真实 runner 时 strip with audit 且不得伪造；有 runner profile 时必须 capability-backed。
- Upstream 400/401/403/429/5xx safe-error mapping fixture，验证不泄露 raw upstream body / token / account / proxy / CCH。

### 4.7 Implemented server-side contract (2026-06-07)

本轮实现已经把 §4 的协议 contract 固化到 Sub2API 与 CC Gateway 测试中：

- `POST /v1/messages` 与 `POST /v1/messages?beta=true` 接收 Anthropic Messages JSON；入站裸路由仅是外部 Anthropic protocol compatibility。
- 发往 CC Gateway 前统一归一化为 `/v1/messages?beta=true`，并通过 audit headers 记录 `inbound_route` 与 `cc_gateway_route`。
- `POST /v1/chat/completions`、`POST /v1/responses` 以及 OpenAI-shaped body 打到 `/v1/messages` 均 fail closed，返回 safe error，不进入 CC Gateway signer。
- 外部 `anthropic-beta`、`x-app`、`x-claude-code-*`、`x-stainless-*`、`x-sub2api-*`、`x-cc-*`、auth/cookie/proxy/billing/CCH-like headers 都不作为 persona/native 证据。
- Compat 请求强制 `client_type=claude_code_compat`；即使伪造 Claude Code UA/body metadata，也会清除 native bit 与 Claude Code version。
- Ops request 与 upstream error context 对 compat 请求只保存 safe summary：counts、presence flags、route/audit fields；不保存 raw messages/system/metadata/tool schema/content。

当前实现文件：

```text
backend/internal/service/claude_code_compat_protocol.go
backend/internal/service/claude_code_compat_shape.go
backend/internal/service/claude_code_compat_shape_healthcheck.go
backend/internal/handler/gateway_handler.go
backend/internal/handler/gateway_helper.go
backend/internal/handler/ops_error_logger.go
backend/internal/service/ops_upstream_context.go
backend/internal/service/gateway_service.go
/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts
```

当前测试/fixture：

```text
backend/internal/service/claude_code_compat_protocol_test.go
backend/internal/server/routes/claude_code_compat_protocol_routes_test.go
backend/internal/service/claude_code_compat_shape_test.go
backend/internal/service/claude_code_compat_shape_healthcheck_test.go
backend/internal/service/testdata/claude_code_compat/*.json
backend/internal/service/gateway_cc_gateway_boundary_test.go
backend/internal/handler/gateway_helper_hotpath_test.go
backend/internal/handler/ops_error_logger_test.go
/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts
```


---

## 5. ClaudeCodeCompatAdapter 设计

### 5.1 文件位置

```text
backend/internal/service/claude_code_compat_protocol.go
backend/internal/service/claude_code_compat_shape.go
backend/internal/service/claude_code_compat_shape_healthcheck.go
backend/internal/handler/gateway_handler.go
backend/internal/service/gateway_service.go
```

### 5.2 输入输出

```go
type CompatAdapterInput struct {
    ClientType    ClientType
    InboundPath   string
    Body          []byte
    Headers       http.Header
    Persona       *PersonaProfile   // server-selected only for compat
    Account       *AccountRecord
    SessionID     string            // server-issued UUID
    Model         string
}

type CompatAdapterOutput struct {
    RewrittenBody   []byte
    InjectedHeaders http.Header
    Warnings        []string
    AuditSummary    CompatAuditSummary
}
```

`AuditSummary` 只允许字段名、bucket、shape、scoped refs：

```text
inbound_route
cc_gateway_route
route
client_type
protocol_family
model_bucket
persona_id
beta_profile_id
session_shape
body_keys
tools_count_bucket
thinking_shape
stream_enabled
max_tokens_bucket
unsupported_feature_bucket
custom_base_url_delta_ref
server_filled_shape
server_filled_fields
persona_source
compat_fidelity_level
tool_search_mode
tool_reference_present
defer_loading_present
eager_input_streaming_present
capability_backed
```

### 5.3 body/system/metadata 归一化顺序

```text
1. Parse Anthropic `/v1/messages` body into neutral message graph; reject OpenAI-compatible protocol shapes.
2. Validate model by known/candidate/gray resolver.
3. Build normalized Anthropic messages without changing user intent.
4. Build truthful system blocks from versioned template:
   - system-reminder based on actual tool mode;
   - env block with Platform/Shell/OS Version/Working directory/Home directory;
   - optional user system extension block.
5. Build metadata.user_id with server-issued session and placeholder/ref identity.
6. Select tool mode and normalize tools.
7. Preserve or inject thinking/output_config/context_management only when persona/account/model allow.
8. Build headers from canonical persona builder.
9. Strip all external auth/billing/persona/attestation-like headers.
10. Remove Content-Length and hop-by-hop headers; HTTP stack recalculates.
11. Return body to CC Gateway rewriter/signing chain.
```

### 5.4 tool modes

| Mode | 适用场景 | 行为 |
|---|---|---|
| `truthful_pass_through` | 用户显式传入普通 Anthropic tools | 保留语义和 Anthropic schema；不额外宣称本地 CLI/ToolSearch/MCP 工具 |
| `not_present` | 普通聊天客户端无工具能力 | `tools=[]`，system reminder 不说可调用工具 |
| `strip_with_audit` | 外部传入 native-only ToolSearch / `tool_reference` / `defer_loading` / `eager_input_streaming` | 深度移除 native-only markers；保留普通 JSON Schema property names；记录 audit |
| `capability_backed` | 服务端/逐梦 Agent 有真实 deferred tool runner | 未来可生成 ToolSearch-compatible deferred tools；必须 capability-backed，不得伪造 |

禁止：

- 无工具客户端不应注入假的 Claude Code 文件/终端工具集合。
- 不得让上游以为可以调用本服务无法执行的工具。
- 不得把外部伪造的 ToolSearch / `tool_reference` / `defer_loading` 当作 native 证据。
- 不得因为安全而限制真实 Claude Code Native 的 tools 能力；本模式只作用于 compat 客户端。

### 5.5 thinking / 1m / max_tokens

- Native Claude Code：不削弱 thinking、1m、tools、stream、`max_tokens=32000`。
- Compat 客户端：
  - 如果用户显式请求 thinking 且 persona/model 支持，则保留或规范化。
  - 如果用户协议无法表达 thinking，adapter 可以按 persona 默认注入，但要在 audit 中标记 `thinking_injected_by_persona`。
  - 1m 只在 known 1m model + account capability + policy 开启时注入 `context-1m`；短请求健康检查不强制 1m。
  - 不引入 canary envelope 的低阈值；生产只依赖 session budget observe-only + P0 hard block。


### 5.6 Custom Base URL delta and server-filled shape

45 号文档说明：真实 Claude Code CLI 在 custom `ANTHROPIC_BASE_URL` 下会发生 ToolSearch、FGTS、control-plane 等能力差异。对本 compat adapter 的约束：

- compat adapter 可以补齐 Claude Code-like headers/system/metadata/beta/body keys，目标是 high-fidelity Claude Code-compatible shape。
- 所有服务端补齐字段必须进入 `server_filled_fields` audit；不得假装这些字段来自真实本机 Claude Code CLI。
- 若补齐 ToolSearch/`defer_loading`/`tool_reference`/`eager_input_streaming` 等 advanced fields，必须满足 `capability_backed=true`，并有 localhost shape healthcheck 和 kill switch。
- 若字段来自真实逐梦 Agent + loopback guard 的 native path，则应走 `ClaudeCodeNative` attestation，不走 compat server-filled path。
- first-party-host 等价接管实验不得在 compat adapter 内实现；只能在逐梦 Agent 隔离配置、official CONNECT fail-closed、control-plane route policy 完备后进行。

---

---

## 6. Persona / Model Resolver

### 6.1 server-selected persona

非 native 请求使用：

```text
persona_source = server_selected
profile_id = production default or account-bound profile
```

不能从用户 headers 推断 CLI 版本。

### 6.2 模型 resolver

禁止 `strings.HasPrefix(model, "claude-")` 就自动 1m。改为分层：

```text
known_allow:
  - 已验证 Sonnet / Opus / Haiku Claude Code 形态模型
  - 已在 persona registry 中声明 beta/context/tool/thinking 能力

candidate_allow:
  - 未来 Sonnet/Opus minor/new version
  - 只允许低风险灰度、强审计、kill-switch
  - 不机械阻断，但也不盲目信任

deny_or_manual_review:
  - 非 Claude family
  - capability 不明且请求 1m/thinking/tools 高风险组合
```

输出必须包含：

```text
model_bucket
model_resolution_tier
persona_profile_id
context_1m_decision
kill_switch_state
```

### 6.3 Opus / future 模型

- Opus 4.6 / 4.7 当前必须被视为已知 Claude Code 能力模型，不得被 `allowed_models=sonnet-only` 误挡。
- Sonnet/Opus 4.8 等未来模型进入 candidate/gray，不直接硬挡，也不直接 full production。
- 任何模型策略只影响调度和 persona 选择，不修改用户请求内容。

---

## 7. 新号硬门禁 / Session Budget 集成

### 7.1 新号状态机

沿用 doc 40：

```text
imported → refreshed → runtime_registered → healthcheck_passed → warming → production → quarantined
```

Compat 流量允许策略：

- `warming`：可参与低权重、normal-only 兼容流量；不允许 aggressive；不优先高成本/高频工具任务。
- `production`：允许按正式 normal/aggressive 策略，但 compat 客户端首发默认仍只使用 normal，aggressive 需单独灰度开关。
- `imported/refreshed/runtime_registered/healthcheck_passed/quarantined`：不可调度。

这与现有状态机一致，不再错误要求“只有 production 才可调度”。

### 7.2 Session Budget

- Compat session 使用独立 `session_scoped_hmac`，不与 native CLI session 混用。
- Phase 1 observe-only；不引入 20 tools / 30 messages 等拍脑袋硬阈值。
- 仅 P0 明显异常 hard block：verifier fail、fallback、proxy mismatch、401/403、risk text、sensitive leak、unsafe control-plane upload。
- 记录 model/tool/thinking/max_tokens/context/window buckets，不记录 raw body/prompt。

### 7.3 Quarantine 接入点

必须触发隔离：

```text
missing_account_identity
egress_proxy_failure
401 invalid_auth
403 forbidden
KYC / unusual activity / account on hold / risk text
proxy mismatch
direct fallback
sign-to-strip fallback
verifier fail
raw token / raw body / raw prompt / raw CCH persistence or leak risk
control-plane unsafe upload
```

---

## 8. 控制面与 synthetic telemetry

### 8.1 首发策略

- 非 Claude Code 客户端没有真实控制面行为。
- 首发不伪造、不 raw 上传 telemetry/eval，也不把所有控制面一刀切真实上游。
- 控制面 path template 对 compat session 默认先进入 safe intent / `suppress` / `stub`，再按 8.4 分层进入真实 fetch、cache、synthetic 或 quarantine。
- 仅记录 shadow intent：route bucket、client_type、session ref、event type bucket、是否应上传、为什么未上传。

### 8.2 后续 synthetic telemetry

后续按 doc 38：

1. shadow-only：生成但不上传，对比真实 CLI V1/V2 采集分布。
2. 灰度上传：小流量、可回滚、强审计。
3. 全量策略：仍不得上传 raw telemetry，不得复用 messages CCH signing。

### 8.3 count_tokens

首发策略：

- 非 native `/v1/messages/count_tokens` 默认禁用、stub 或明确 safe error。
- 不由 Sub2API 主动注入 count_tokens。
- 如果后续开启，必须单独 route policy、单独签名/安全边界；不得复用 messages billing/CCH signer。

### 8.4 基于 V1/V2 Lab capture 的控制面分层接管

本机 V1/V2 Claude Code Lab capture 已经足够支撑 **控制面路径分类、字段形状审计和 safe intent 中心化接管**，不能再把所有控制面笼统描述成“数据不足所以全部 suppress”。已观察到的稳定路径包括：

- `POST /api/event_logging/v2/batch`：高频 telemetry batch；
- `POST /api/eval/sdk-zAZezfDKGoZuXXKe`：eval / feature evaluation 类 POST；
- `GET /api/claude_cli/bootstrap`：bootstrap / feature bootstrap；
- `GET /api/claude_code_penguin_mode`：feature/readiness 查询；
- `GET /api/claude_code/organizations/{org}`：账号/组织动态路径；
- `GET /mcp-registry/v0/servers`：MCP registry 查询；
- `POST /v1/messages/count_tokens`：message-adjacent token count；
- `POST /v1/messages?beta=true`：主 messages 链路。

capture 的 deep 摘要已经记录 path template、method、header names、auth shape、body length bucket、schema summary、JSON 字段树、telemetry event names、redaction proof 和 V2 process netwatch。该证据足以将 compat/native control-plane 接管拆成以下层级：

| 层级 | 路径/类别 | 生产接管策略 | 首发默认 |
|---|---|---|---|
| L0 主链路 | `/v1/messages?beta=true` / normalized `/v1/messages` | 进入 CC Gateway sign-primary；保留 1m/tools/thinking/context_management/stream/max_tokens | 已支持 |
| L1 公共/低风险 GET | `/mcp-registry/*`，无账号私密字段的 registry 查询 | public cached fetch；response schema allowlist；异常熔断 | 可进入灰度 |
| L2 账号绑定 GET | `/api/claude_cli/bootstrap`、settings、feature flags、`/api/claude_code_*` | selected pool account identity 重建；account-scoped cache；schema allowlist；不得使用本机 token/org/user id | safe intent + 灰度 |
| L3 账号/用户私有状态 | `/v1/mcp_servers*`、可能含私有配置的 MCP 结果 | account/user partition cache；禁止跨用户污染；敏感字段 schema quarantine | safe intent + 后续灰度 |
| L4 message-adjacent count | `/v1/messages/count_tokens` | 单独 route policy、单独安全边界；不得复用 messages billing/CCH signer；不得记录 raw prompt/body | 首发禁用/stub/safe error |
| L5 高风险 POST | `/api/event_logging/v2/batch`、`/api/eval/*` | raw body 永不上传；先 safe intent；后续只允许 sanitized synthetic/aggregate；按 event family 单独 canary | suppress/204 + shadow |
| L6 unknown/drift | 新路径、动态路径漂移、未知字段组合 | fail closed / quarantine；只上传脱敏 intent 供审查 | quarantine |

因此，首发“不真实上传 telemetry/eval/count_tokens”不是因为控制面完全未知，而是因为不同层级的风险不同：

- telemetry/eval 已掌握路径和 schema/event-name 摘要，但 raw event 可能包含 prompt、路径、工具参数、命令、错误栈、环境、device/session/account 标识；只能从 safe intent 重建最小 synthetic event。
- bootstrap/settings/feature flags/MCP registry 属于可更早灰度的 GET/control-plane fetch，但必须使用 selected pool account、canonical persona、schema allowlist、cache isolation 和熔断。
- `/v1/messages/count_tokens` 虽然形态接近 messages，但 body 可能含 prompt/messages，因此必须独立于 telemetry 和 messages signer 处理。
- org/account 动态路径不能复用本机路径参数，必须由选中号池账号 metadata 重建。

后续实施时，doc 35 的“两段式上传模型”应成为 control-plane 接管基线：

```text
local guard / compat adapter
  -> normalized safe intent envelope
  -> Sub2API control-plane router
  -> selected pool account / cache / synthetic / suppress / quarantine decision
  -> optional CC Gateway control-plane adapter
  -> upstream fetch/upload only for allowlisted tier
```

验收要求：

1. V1/V2 capture 中已知 path template 必须全部有 tier、默认动作、灰度条件和熔断条件。
2. `bootstrap` / `MCP registry` / `feature flags` 的真实 fetch 进入灰度前，必须有 localhost replay、A/B shape diff、schema allowlist fixture 和单路径 canary。
3. `event_logging` / `eval` 只允许 safe intent + shadow synthetic builder；任何 raw body、raw query value、plain hash、长期 deterministic digest 都是 P0 block。
4. `count_tokens` 必须有独立 fixture，验证不会进入 messages CCH/billing signer，也不会持久化 raw prompt/body。
5. V2 netwatch 若发现 guard bypass，相关 capture 不得作为“完全覆盖”证据，必须先复盘绕过目标。

---

## 9. CC Gateway 集成点

### 9.1 rewriter 兼容

CC Gateway `rewriter.ts` 负责：

- `rewriteMessagesBody`：metadata identity refs、system/billing material 清理；
- `rewritePromptText`：env/path/cc_version 文本 rewrite；
- `canonicalPersonaHeaders`：persona-specific headers；
- stripping untrusted billing/header material。

### 9.2 policy signing 兼容

CC Gateway `policy.ts` 负责：

- `removeExistingBillingMaterial`；
- prepend billing placeholder；
- compute CCH；
- `verifySignedCCH`；
- signer evidence gates。

Compat adapter 的 output 必须满足：

- 没有外部 billing/CCH；
- body 可被 rewriter 正常处理；
- signing 前后有 verifier 和 post-sign mutation guard；
- fail closed，不 fallback 到直接 Anthropic。

### 9.3 internal marker

允许 Sub2API → CC Gateway 内部 header：

```text
x-cc-gateway-persona-source: server-selected
x-zhumeng-client-type: claude_code_compat
```

约束：

- 只能在内网/受信链路生成。
- 入口层剥离外部同名 header。
- 发往上游前必须删除。
- 日志只记录 header name / marker bucket，不记录敏感值。

---

## 10. 实施计划

### Phase 1：检测与协议骨架

新增：

```text
backend/internal/service/claude_code_compat_protocol.go
backend/internal/service/claude_code_compat_shape.go
backend/internal/service/claude_code_compat_shape_healthcheck.go
backend/internal/handler/gateway_handler.go
backend/internal/service/gateway_service.go
```

实现：

- attestation-first detector；
- spoof fail-closed / compat(server-selected)；
- Anthropic-only endpoint matrix；
- OpenAI-compatible endpoint fail-closed；
- Anthropic messages normalize。

### Phase 2：系统形态与 CC Gateway 集成

实现：

- versioned system template builder；
- canonical headers；
- metadata/session builder；
- tool modes；
- no stale Content-Length；
- CC Gateway marker + strip before upstream。

### Phase 3：High-fidelity capability-backed shape

实现：

- Anthropic stream shape preservation；
- Anthropic tools truthful pass-through；
- server-filled shape audit；
- ToolSearch/deferred tools capability-backed fixtures；
- unsupported OpenAI-compatible protocol safe errors。

### Phase 4：预算/账号/风险事件

实现：

- warming/production eligibility；
- session budget observe-only ledger；
- quarantine triggers；
- dashboard safe fields。

### Phase 5：shadow telemetry 准备

实现：

- compat session shadow intent ledger；
- 与 V1/V2 Claude Code lab capture 分布对比；
- 不上传 raw telemetry。

---

## 11. 测试计划

### 11.1 Unit tests

- `claude_code_client_detector_test.go`
  - trusted attestation 才能 native；
  - spoofed `claude-cli` / `x-app` / `x-claude-code-session-id` 不得 native；
  - external attestation-like headers 被 strip/reject。
- `claude_code_protocol_adapter_test.go`
  - Anthropic `/v1/messages` accepted；
  - OpenAI `/v1/chat/completions` fail closed；
  - OpenAI `/v1/responses` fail closed；
  - unsupported protocol safe error。
- `claude_code_compat_adapter_test.go`
  - system 使用 Working directory/Home directory/OS Version；
  - no-tools truthful 不宣称工具；
  - pass-through Anthropic tools 不注入假工具；
  - ToolSearch/deferred tools 无真实 runner 时不得伪造；
  - server-filled shape audit 字段完整；
  - thinking/1m/max_tokens 不继承 canary hard cap；
  - external billing/CCH/auth/cookie stripped。
- `persona_model_resolver_test.go`
  - Opus 4.6/4.7 allowed；
  - future Sonnet/Opus candidate/gray；
  - unknown non-Claude model fail closed；
  - no blanket `claude-*` 1m。
- `account_quarantine_service_test.go`
  - 401/403/egress/fallback/verifier/missing identity 隔离；
  - risk_event 脱敏。

### 11.2 Integration tests

- Anthropic SDK request → adapter → CC Gateway → localhost mock。
- Generic Anthropic HTTP request → adapter → CC Gateway → localhost mock。
- OpenAI-compatible request → safe error，不进入 CC Gateway messages signer。
- Spoofed Claude headers → fail closed or server-selected compat，不走 dynamic persona。
- CC Gateway raw capture shape audit：只读、脱敏摘要，不保存 raw body/prompt。
- verifier/post-sign mutation/fallback fail closed。
- warming account low-weight normal-only；production account policy 生效。

### 11.3 Commands

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./internal/service ./internal/handler ./internal/server/routes -run 'ClaudeCode|Compat|ClientType|NonClaudeCode|Persona|Adapter|FormalPool|Budget|Quarantine|DTO' -count=1 -timeout=240s
```

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
PYTHONPATH=. python3 -m unittest discover -s tools/tests -v
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
```

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
```

---

## 12. 风险与缓解

| 风险 | 级别 | 缓解 |
|---|---|---|
| 非 Claude Code 纯 messages 缺控制面，长期分布异常 | P1 | doc 38 shadow-only → 灰度 telemetry；首发不上传 raw |
| 伪造 Claude Code header 诱导 native persona | P0 | attestation-first；spoof fail closed；external internal headers strip |
| adapter 误生成 CCH/billing | P0 | 明确禁止；只由 CC Gateway policy.ts signing path 处理；测试覆盖 |
| OpenAI-compatible 协议误进入本 adapter | P1 | endpoint fail-closed；unsupported protocol safe error fixture |
| no-tools 客户端被注入假工具 | P1 | tool mode 分层；truthful system template；ToolSearch 必须 capability-backed |
| future model 被机械阻断 | P1 | candidate/gray + kill-switch；Opus/Sonnet 不 sonnet-only |
| canary budget 误用于 production | P0 | doc 39 production observe-only；测试确保不继承 canary envelope |
| 新号未过健康检查进入生产 | P0 | doc 40 状态机；warming/production eligibility gate |
| raw body/prompt 被日志持久化 | P0 | sensitive scan + log redaction + safe deliverable contract |

---

## 13. Acceptance

实施完成必须满足：

1. Anthropic SDK `/v1/messages` 可 normalized 到 CC Gateway `/v1/messages?beta=true`。
2. Generic Anthropic HTTP `/v1/messages` 可 normalized 到 CC Gateway `/v1/messages?beta=true`。
3. OpenAI-compatible `/v1/chat/completions` 和 `/v1/responses` fail closed，不进入 messages signer。
4. 外部伪造 Claude Code headers 不会进入 native / dynamic persona。
5. Adapter 不生成 CCH/billing；CC Gateway policy signer 负责 CCH/verifier。
6. 不持久化 raw token、raw prompt、raw body、raw telemetry、raw CCH、email、账号/组织 UUID、proxy credential。
7. 不限制 native Claude Code 的 1m/tools/thinking/stream/Opus/Sonnet/max_tokens。
8. Compat no-tools 模式不宣称工具能力；ToolSearch/deferred tools 必须 capability-backed。
9. Server-filled shape 必须审计：`server_filled_shape=true`、`persona_source=server_selected`、`client_type=claude_code_compat`。
10. Opus 4.6/4.7 放行；future Sonnet/Opus 走 candidate/gray，不机械阻断。
11. 新号只有 warming/production 才可接 compat 流量；warming 低权重 normal-only。
12. 401/403/egress/fallback/verifier/missing identity 自动 quarantine + risk_event。
13. count_tokens 和 synthetic telemetry 首发不真实上传；后续必须单独设计/灰度。

---

## 14. 附录：材料引用速查

| 材料 | 路径 | 用途 |
|---|---|---|
| Claude Code 逆向源码 | `reference-projects/agent-frameworks/claude_code_src/` | system/env/控制面源码线索 |
| 本机 V1/V2 采集 | `~/.zhumeng/claude-code-lab/captures/` | native 请求分布、直连检测 |
| 字段审计 | `docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/` | headers/body keys/session shape |
| highmax 200 | `docs/anti-ban/captures/real-cli-through-highmax-200-2026-05-24/` | max_tokens/thinking/tool evidence |
| CCH 算法 | `docs/anti-ban/cch-algorithm.md` | messages-only signing 边界 |
| CC Gateway rewriter | `cc-gateway/src/rewriter.ts` | body/prompt rewrite |
| CC Gateway policy | `cc-gateway/src/policy.ts` | signing/CCH/verifier |
| Dynamic persona | `docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md` | trust boundary / future model |
| Session budget | `docs/anti-ban/39-formal-pool-session-budget-strategy.md` | observe-only / production budget |
| New account gates | `docs/anti-ban/40-formal-pool-new-account-hard-gates.md` | warming/production/quarantine |
| Synthetic telemetry | `docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md` | shadow-only 后续路径 |


## 11. Implementation status and operations memo (2026-06-07)

### 11.1 Current implemented checkpoints

| Checkpoint | Status | Commit evidence |
|---|---|---|
| 0 baseline audit | done | `2372bf586` |
| 1 Anthropic-only protocol gate | done | `4decdaebf` |
| 2 route normalization + audit | done | Sub2API `877b909a1`, CC Gateway `2ca327f` |
| 3 Claude-Code-compatible shape normalize | done | Sub2API `72732f114`, CC Gateway `4d4b867` |
| 4 ToolSearch/deferred/capability policy | done | Sub2API `ff2db30ce`, CC Gateway `3880f55` |
| 5 shape healthcheck fixtures | done | Sub2API `da0d416ad` |

### 11.2 Operator-visible invariants

- Non-Claude-Code clients must use Anthropic `/v1/messages`. OpenAI-compatible protocols are not accepted by this adapter.
- `/v1/messages -> /v1/messages?beta=true` is an internal normalization boundary, not an instruction to trust external beta/persona headers.
- Compat traffic is high-fidelity `claude_code_compat`, not `claude_code_native`.
- `server_filled_shape` and `server_filled_fields` are mandatory audit evidence, not native attestation.
- Ordinary Anthropic tools may pass through truthfully; native-only ToolSearch/deferred markers are stripped with audit unless a future real runner makes them capability-backed.
- The adapter does not upload synthetic telemetry, does not perform real canaries, does not send count_tokens on behalf of clients, and does not call Anthropic/Claude hosts outside the existing approved CC Gateway data plane.
- The adapter does not clamp 1m context, tools, thinking, streaming, Opus/Sonnet model families, or `max_tokens=32000` for real Claude Code/native flows.
- Persistent logs, ops error records, raw capture safe summaries, fixtures, and docs must not contain raw token, raw prompt, raw body, raw telemetry, raw CCH, email, account/org UUID, or proxy credential.

### 11.3 Required verification commands

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./internal/service ./internal/handler ./internal/server/routes -run 'Compat|ClaudeCode|CCGateway|Messages|Anthropic|Route|Protocol|Shape|Gateway|ControlPlane|Session|Account|Tool|Deferred|Capability|Healthcheck|Fixture|Fidelity' -count=1 -timeout=240s

go test ./internal/service ./internal/handler ./internal/server/routes -count=1 -timeout=300s

cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
PYTHONPATH=. python3 -m unittest discover -s tools/tests -v
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100

cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
```

These commands use local mocks/fixtures only for this adapter work. Do not run real Anthropic/Claude canaries unless separately approved.
