# CC Gateway 共享账号池兼容性排查与落地方案

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan checkpoint-by-checkpoint. Each checkpoint must be reviewed before moving on.

**Goal:** 以“共享账号池路线”为准，让 Sub2API 与 CC Gateway 分工清晰、可验证地协作：Sub2API 负责账号选择/调度/凭证/计费/多用户到多账号池的稳定分发，CC Gateway 负责账号级 identity、header/body 归一化、billing/CCH 移除、出口 bucket 与部署边界。两个项目要各展所长、合力收敛共享账号池中的不一致信号。

**Canonical Persona:** 本轮统一对齐 Claude Code `2.1.146`。Sub2API native mimicry 与 CC Gateway canonical persona 都以 `2.1.146` 为唯一目标版本；走 CC Gateway 路径时，Sub2API 的 `2.1.146` helper 不得再次污染 body/header/persona，最终上游 persona 由 CC Gateway 统一输出。

**Risk posture:** 目标是减少共享账号池中的身份、出口、header/body、session、billing/CCH 混杂所造成的不一致和异常风险；这不能保证零风控，也不能替代合规使用、限流、账号隔离和人工 canary。

**Scope:** 本文是部署前兼容性排查与实施计划，不执行真实账号请求，不做 MITM，不改动账号。

**First-wave Scope:** 首轮只完成 Claude / Anthropic 共享账号池闭环。Antigravity 相关代码和文档保留为后续阶段，不进入首轮真实 canary，不作为本轮上线阻断项；但不能让 Antigravity 半开启进入生产路径。

---

## 0. 当前基准与重要修正

### 当前基准

- Sub2API worktree: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
- Sub2API branch: `codex/claude-antiban-implementation`
- CC Gateway: `/Users/muqihang/chelingxi_workspace/cc-gateway`
- CC Gateway branch: `main`
- CC Gateway latest relevant commit: `6a419de Merge branch 'codex/v0.3-integration' into main`
- v0.3 design doc: `/Users/muqihang/chelingxi_workspace/cc-gateway/.claude/worktrees/jovial-brattain-52942d/docs/v0.3.0-design.md`

### v0.3 改造提交痕迹

当前基准路径 `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main` 可以追溯到以下相关提交：

- `212c0ddbb Add sub2api cc-gateway adapter`
- `9b7d6a3a5 Merge branch 'codex/cc-gateway-phase0b-0c-0f' into codex/v0.3-integration`
- `664a2fabb Merge branch 'codex/v0.3-integration' into main`

CC Gateway 当前 main 可以追溯到：

- `2968c6b Implement cc-gateway sub2api phase 0`
- `85b046e Finalize cc-gateway phase 0 docs and defaults`
- `6a419de Merge branch 'codex/v0.3-integration' into main`

### 路径边界硬规则

`/Users/muqihang/chelingxi_workspace/sub2api` 是历史归档/废弃路径，即将移除。后续所有审查、计划、实施、测试、部署文档均不得以该路径作为 Sub2API 当前依据。若任何子代理或脚本引用该路径，其结论必须重新按 `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main` 校准。

### 当前仓库状态注意事项

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main` 当前存在与本方案无关的未提交改动，执行代理不得覆盖或回退它们。
- 当前实施 worktree `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation` 仍有未跟踪文件：`backend/internal/service/local_capture_acceptance_artifact_test.go`。执行时必须保留，除非任务明确要求清理。

### 对子代理反馈的修正

Raman 的部署审查里有一部分证据取自旧路径 `/Users/muqihang/chelingxi_workspace/sub2api` 和 CC Gateway 旧 worktree，因此其中两条对当前基准不准确：

- “Sub2API 没有 cc-gateway adapter”——对当前基准不成立。当前 worktree 已有 `gateway.cc_gateway` 配置和 adapter。
- “CC Gateway sub2api mode 会覆盖成单 OAuth”——对当前 `cc-gateway/main` 不成立。当前 main 已实现 `mode: sub2api` 透明凭证基础。

但 Raman 关于“多 identity、egress bucket、health/verify/capture/canary/rollback 未闭环”的判断仍然成立。

---

## 1. 目标分工

### Sub2API 职责

1. 账号池、分组、计费、调度。
2. OAuth / API key / setup-token 的凭证选择和刷新。
3. 把选中账号凭证透明交给 CC Gateway。
4. 注入内部控制头：
   - `x-cc-gateway-token`
   - `x-cc-account-id`
   - `x-cc-provider`
   - `x-cc-token-type`
   - `x-cc-account-email`
   - `x-cc-account-uuid`
   - `x-cc-organization-uuid`
   - `x-cc-project-id` for Antigravity
   - `x-cc-egress-bucket`
5. 在 CC Gateway 路径下，不再做 Claude Code identity/body/persona 的二次归一化。

### CC Gateway 职责

1. `mode: sub2api` 下只认证 `x-cc-gateway-token`，保留 selected account credential。
2. 按 `provider + account_id` 生成/读取稳定 identity。
3. 按 `x-cc-egress-bucket` 选择出口代理和连接池。
4. 对 Anthropic/Antigravity 使用 provider-specific header allowlist。
5. 统一移除 `billing header / CCH`，不复用用户 CCH，也不启用未验证服务端 CCH。
6. 对 `/v1/messages`、`count_tokens`、event logging、Antigravity `v1internal` 做最小、可测试的归一化。
7. 输出 health/verify/rollback/transport residual risk 资料。

### 两项目合力原则

1. **Sub2API 发挥调度与治理价值**：负责“多账户号池被多用户轮询”时的账号选择、限流、sticky、配额、计费、回滚与运营控制。
2. **CC Gateway 发挥身份与出口价值**：负责把同一个上游账号在网关这一侧表现为长期稳定的同一 persona / identity / egress，而不是被下游多用户请求不断污染。
3. **严格单一 persona 输出层**：共享账号池路径上，最终对上游展示 Claude Code `2.1.146` persona 的唯一组件是 CC Gateway；Sub2API 不再重复做 persona/body mimicry。
4. **目标不是“看起来完全等于真实单用户”**，而是尽可能消除多用户、多账号、多出口、多会话混杂造成的异常不一致信号，让单账号在可控范围内长期稳定运营。
5. **共享池调度原则**：默认一账号一身份一出口；允许多账号共享同一出口 bucket，但连接池仍按账号隔离；禁止多账号共享同一 identity。

### Sub2API 既有 anti-ban / anti-control 硬化的使用原则

本路线不是“启用 CC Gateway 后就把 Sub2API 的防封控硬化全部关掉”，而是要做**去冲突后的叠加使用**。原则如下：

#### A. 必须继续由 Sub2API 保留的能力

1. **账号池调度与 sticky**
   - 同一用户/同一会话尽量稳定命中同一上游账号，避免无意义跨账号漂移。
2. **账号健康与熔断**
   - 401/403/429/异常上游返回后的冷却、摘除、quarantine、重试门控继续保留。
3. **渐进式 canary / fail-closed / 回滚**
   - 未通过本地 capture、字段缺失、bucket 不合法、CC Gateway 不可用时默认 fail closed，而不是静默退回未审查路径。
4. **日志与错误脱敏**
   - OAuth onboarding、错误 body、内部调试输出中的 token/secret/redaction 继续保留并加强。
5. **路由级治理**
   - 哪些路由允许进入 CC Gateway、哪些路由首轮保持关闭，由 Sub2API 明确控制。
6. **请求量治理**
   - 并发、速率、账号使用窗口、预算/配额、运营层限流继续由 Sub2API 控制。

#### B. 在 CC Gateway 路径上必须让位给 CC Gateway 的能力

1. **Claude Code persona 输出**
   - User-Agent、x-stainless-*、session/body persona、messages/count_tokens 终态 persona 由 CC Gateway 统一输出。
2. **身份归一化**
   - `metadata.user_id`、device/account/session 级 identity 改写以 CC Gateway 为准。
3. **billing / CCH 处理**
   - 共享账号池路径上由 CC Gateway 统一 strip；Sub2API 不再生成、不再签名、不再透传用户 CCH 作为共享池默认策略。
   - 更正后的验证结论：`CLAUDE_CODE_ATTRIBUTION_HEADER=0` 的 no-CCH 链路已经按 `16-no-cch-upstream-acceptance-validation.md` 补齐两段证据：Phase A 本地 capture 证明 Claude Code 2.1.146 最终 body/header 无 billing block / `cch=`；Phase B 一次真实 non-MITM upstream 最小 `/v1/messages` 请求正常返回 `OK`，无 400/401/403/KYC/unusual activity/third-party warning。该结论只覆盖最小 messages 场景。
   - 新发现的 CCH 算法已在既有 2.1.145 localhost raw fixture 上离线验证命中，但不改变当前首轮决策：共享账号池默认继续使用 strip；CCH signer 仅作为 offline verifier 与未来手动审批的 opt-in signing mode 候选，详见 `15-cch-algorithm-validation-and-usage-plan.md`。签名不是 strip 失败后的自动兜底；strip 未覆盖或失败时默认 halt / fail closed，不能自动切换到 sign。
4. **最终 header allowlist**
   - 最终发往 Anthropic 的 header 白名单由 CC Gateway 控制。
5. **最终出口策略**
   - 账号级出口 proxy/bucket 由 CC Gateway 解析和执行；Sub2API 只传 bucket name。

#### C. 只在 Sub2API native 路径保留的能力

以下能力在**不走 CC Gateway**时可以继续使用；一旦 `shouldUseCCGatewayAnthropic(account)=true`，就必须禁用或绕过：

1. `applyClaudeCodeOAuthMimicryToBody` 及相关 Claude Code body/persona helper。
2. Sub2API 侧的 `2.1.146` mimicry header 生成。
3. Sub2API 侧 `metadata.user_id` persona 注入。
4. Sub2API 侧 billing/CCH/fingerprint 的任何终态输出。

#### D. 设计目标

- **能叠加的硬化继续叠加**：调度、sticky、熔断、限流、脱敏、回滚、canary 继续由 Sub2API 提供价值。
- **会打架的硬化只保留一个输出层**：persona、identity、最终出口、billing/CCH、最终 header 不允许 Sub2API 与 CC Gateway 双重输出。
- **默认从严**：如果不确定某项硬化是否会和 CC Gateway 冲突，则默认不让它在 CC Gateway 路径生效，先加 capture 和测试再决定。

---


## 1.5 CCH verified, but stronger shared-pool signing still needs full control plane

The newly supplied CCH algorithm has now been offline-verified against available Claude Code raw fixtures. This removes the previous largest unknown around the 5-hex body CCH. However, it does **not** mean the stronger shared-pool signing path is complete. CCH only proves final body integrity for the billing attribution block; the shared-pool risk model still requires these controls to be correct together:

1. account-level identity, not user-level identity leakage;
2. stable device_id / account_uuid / session_id behavior;
3. `metadata.user_id` final rewrite;
4. User-Agent and `x-stainless-*` persona;
5. endpoint-specific beta list;
6. egress IP / proxy bucket and connection-pool isolation;
7. event_logging behavior;
8. count_tokens behavior;
9. final upstream header allowlist;
10. no double body/persona rewrite between Sub2API and CC Gateway.

Therefore the stronger path should be treated as: **CC Gateway as the only final output layer**. If opt-in signing mode is manually approved later, CC Gateway must generate/normalize the billing block, compute the correct `cc_version` suffix, serialize the final body, sign CCH as the last mutation, verify it, then forward. Signing is not an automatic fallback for strip failures. Sub2API must not sign or generate billing blocks on the CC Gateway path.

Additional signing-readiness gate: `25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md` absorbs the 2026-05-21 A/B audits and defines the current blockers for a signing canary. In particular, signing canary is blocked until the 2.1.146 `count_tokens`, OAuth refresh, metadata/session lifecycle, and Linux parity MUST gates are closed, and until Sub2API no longer performs final body/persona generation on CC Gateway-selected messages, count_tokens, chat_completions, and responses routes.

## 2. P0 部署阻断项

### P0-1: CC Gateway 缺多账号 IdentityManager

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts:34-37` 只有单个 `identity.device_id/email`。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts:139-154` 调 `rewriteBody/rewriteHeaders` 只传 `config`，不传 account context。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts:79`、`:219-220` 使用全局 `config.identity`。
- 缺 `src/identity.ts`、`tests/identity.test.ts`、identity volume 和 corrupt/concurrency 测试。

**风险：** 多个上游账号会坍缩成同一个 device/email/env，形成明显账号池关联。

**要求：**

- `x-cc-account-id` 在 `mode: sub2api` 下必须存在。
- identity key: `sha256(provider + ':' + accountId)`。
- 同账号重启稳定，不同账号不同 identity。
- identity 文件不得保存 access token、refresh token、cookie、proxy secret。
- 支持并发首次创建、坏 JSON 隔离、max identities 上限。

---

### P0-2: CC Gateway 缺多出口 Egress Bucket

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts` 无 egress 配置。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy-agent.ts:5-18` 只有全局 env proxy。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts:165-176` 每个请求都用同一个 `getProxyAgent()`。

**风险：** 即使 identity 不同，多个账号仍可能从 CC Gateway 宿主机或同一个全局代理出站。

**要求：**

- `egress.buckets[]` 配置 bucket → proxy URL。
- Sub2API 只传 bucket name，不传 proxy URL。
- unknown/disabled bucket 在 `sub2api` mode 下 fail closed。
- proxy 失败不得静默直连。
- agent cache 默认按 `provider + account_id + bucket` 隔离。

---

### P0-3: CC Gateway header allowlist 未实现

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts:341-375` 是默认透传 + 少量 blacklist。
- 未明确丢弃 `cookie`、未知 `x-*`、remote/session/container headers、`x-client-current-telemetry`、`x-client-last-telemetry` 等。

**风险：** 下游客户端、浏览器、代理或 SDK 的敏感/异常 header 可穿透到上游。

**要求：**

- 改为 provider-specific allowlist。
- `x-cc-*` 永远只在 Sub2API ↔ CC Gateway 内部链路存在，转上游前全部删除。
- Anthropic 与 Antigravity 分开 allowlist，不共用。
- 未明确允许的 header 默认丢弃。

---

### P0-4: Sub2API 启用 CC Gateway 后仍可能提前执行 mimicry body 改写

**现状证据：**

- Native Messages：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_service.go:4443-4488`
  - 在进入 `buildUpstreamRequest` 前已经执行 `rewriteSystemForNonClaudeCode`、`buildOAuthMetadataUserID`、`normalizeClaudeOAuthRequestBody`、tool/cache 改写。
- Native Count Tokens：`/Users/muqihang/.../backend/internal/service/gateway_service.go:8889-8922`、`:8965`
  - 在进入 `buildCountTokensRequest` 前已经执行 OAuth mimicry/metadata/body normalize。
- OpenAI-compatible Chat Completions：`/Users/muqihang/.../backend/internal/service/gateway_forward_as_chat_completions.go:93-107`
  - 在调用 `buildUpstreamRequest(...)` 前可能调用 `applyClaudeCodeOAuthMimicryToBody`。
- OpenAI-compatible Responses：`/Users/muqihang/.../backend/internal/service/gateway_forward_as_responses.go:90-104`
  - 在调用 `buildUpstreamRequest(...)` 前可能调用 `applyClaudeCodeOAuthMimicryToBody`。
- `useCCGateway` 目前主要在 builder 内部生效：
  - `/Users/muqihang/.../backend/internal/service/gateway_service.go:6005-6080`
  - `/Users/muqihang/.../backend/internal/service/gateway_service.go:6113-6168`

**风险：** CC Gateway 路径出现双重 body/persona/identity 改写；Sub2API 的 `2.1.146` mimicry 与 CC Gateway 的 `2.1.146` canonical persona 冲突。Chat Completions / Responses 若生产开放，也会带同样风险，不能只修 native messages/count_tokens。

**要求：**

- 在 Forward/ForwardCountTokens 顶层就判断 `shouldUseCCGatewayAnthropic(account)`。
- Chat Completions / Responses 在 `shouldUseCCGatewayAnthropic(account)` 为 true 时，不得调用 `applyClaudeCodeOAuthMimicryToBody` 或任何 Sub2API Claude Code persona/body helper。
- CC Gateway 路径下 Sub2API 不做 Claude Code mimicry body rewrite。
- Sub2API 只执行必要的协议适配、账号选择、认证头和 `x-cc-*` 注入。
- 加测试断言 CC Gateway 收到的 body 未被 Sub2API mimicry 污染。测试必须覆盖 native messages、count_tokens、`/v1/chat/completions`、`/v1/responses`。
- 若 Chat Completions / Responses 暂不纳入首轮上线，则在完成本项 capture 前必须明确关闭/不路由到 CC Gateway，不能半开启。

---

### P0-5: CC Gateway `2.1.146` persona 输出尚未被具体锁死

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts:367-368` 当前仍输出 `claude-code/${version} (external, cli)` 风格 UA。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/rewriter.test.ts:263-269` 仍围绕旧 UA 断言。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml` 仍残留旧版 `2.1.119` persona 痕迹。
- 当前 `2.1.146` 的 count_tokens / event logging fixture 仍不完整。

**风险：** 即使方案文字改成了 `2.1.146`，执行代理仍可能只改版本号，不补齐 `User-Agent`、`entrypoint`、`x-stainless-*`、beta、session header、body fixture，最终让 CC Gateway 输出半旧半新的 persona。

**要求：**

- 把 CC Gateway `2.1.146` persona 作为显式阻断项，而不是“顺手升级”。
- 至少锁定以下内容：
  - `User-Agent` / `entrypoint`
  - `x-stainless-*`
  - messages beta
  - count_tokens beta
  - `X-Claude-Code-Session-Id` 与 body session 的同步
  - `metadata.user_id` 相关字段
- `config.example.yaml`、tests、fixtures、capture 报告必须一起升级。
- `build_time`、如有必要的运行时字段，必须从本地 `2.1.146` probe/debug 产物锁定，不得沿用 `2.1.119` 旧值。
- 若 `count_tokens` / `event_logging` 真实 2.1.146 fixture 尚未补齐，则必须在首轮范围中标明 residual risk 或默认阻断其进入共享池生产路径。
- 若生产部署输出 Linux canonical persona，则无论首轮使用 strip/no-CCH 还是未来 opt-in signing mode，都必须先完成 Linux-host parity local capture，并把 header/body/env/TLS 摘要纳入验收；否则只能标注 Mac-derived residual risk，禁止生产放量。
- 首轮真实 canary 前必须明确 event_logging route policy：`suppress locally` / `rewrite through CC Gateway` / `forward with allowlist` / `block` 四选一，不允许 accidental passthrough。2.1.146 event schema 动态捕获可作为后续 SHOULD，但路由策略本身是 canary 前 MUST。
- **Checkpoint 1 决策（2026-05-21，本地/静态证据版）**：
  - `POST /api/event_logging/batch`：`suppress locally`（Sub2API 本地 ACK，默认不进共享池 CC Gateway 路径）
  - `POST /api/event_logging/v2/batch`：`suppress locally`
  - 任意未知 `/api/event_logging/*`：`block`
  - 在补齐 `2.1.146` event schema capture 之前，event logging route family 全部 **excluded from first-wave canary**，不得 accidental passthrough

### Deferred-P0: CC Gateway 侧 Antigravity provider 未实现（首轮后置）

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts:22-24` 只有 `providers.anthropic`。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts:214-221` 非 `anthropic` 一律拒绝。
- 缺 `src/rewriter-antigravity.ts`、`tests/rewriter-antigravity.test.ts`。

**风险：** Sub2API 已经会对 Antigravity 注入 `x-cc-provider: antigravity`，但当前 CC Gateway 会拒绝。由于首轮明确只做 Claude / Anthropic，此项不阻断首轮 canary；但生产配置必须保持 `gateway.cc_gateway.providers.antigravity=false`，且任何 Antigravity 路由不得半开启走 CC Gateway。

**要求：**

- 支持 `providers.antigravity.enabled` 与 upstream/daily_upstream 配置。
- 支持 `/v1internal:generateContent`、`/v1internal:streamGenerateContent?alt=sse`。
- 保留 Sub2API 已包装 v1internal body，不在 CC Gateway 内做模型转换。
- SSE 逐字节透传。
- `x-cc-project-id` 与 body project 冲突时 fail closed。

---

### P0-6: 联合 capture 验收不存在

**风险：** 目前已有 Sub2API 本地 capture 主要验证 native strict/mimicry，不覆盖 `client -> sub2api -> cc-gateway -> fake upstream` 四段链路。

**要求：**

首轮 Claude / Anthropic 只覆盖：

- Anthropic OAuth `/v1/messages`
- Anthropic OAuth `/v1/messages/count_tokens`
- Anthropic API-key passthrough
- OpenAI-compatible Anthropic `/v1/chat/completions`、`/v1/responses`（仅当首轮实际开放）
- Antigravity 在首轮只验证 **disabled / fail-closed**，不要求 `/v1internal:streamGenerateContent?alt=sse` 成功链路
- event logging 策略：rewrite / pass / block 必须明确；若首轮无 2.1.146 真实 fixture，则必须写成 residual risk 或默认关闭/阻断
- `x-cc-*` 不泄露上游
- selected token 不被覆盖
- billing/CCH 不出现
- identity per-account 稳定
- egress bucket 命中
- proxy failure 不直连
- CC Gateway rewrite/identity/egress canonicalization 失败时 fail-closed，不带原始未归一化请求继续上游
- CC Gateway control-plane 错误（wrong token/provider disabled/bad bucket/proxy failure）不触发 Sub2API 账号池 failover，不触发 native fallback

**Control-plane error wire contract（必须实现）：**

- CC Gateway 自身控制面错误必须返回可识别标记，例如：
  - response header: `X-CC-Gateway-Error-Kind: control-plane`
  - response body code: `wrong_gateway_token | provider_disabled | route_not_allowlisted | bad_bucket | proxy_failure | rewrite_failed | identity_failed`
- 真正的上游透传错误不得携带 `X-CC-Gateway-Error-Kind: control-plane`。
- Sub2API 必须基于该标记区分“CC Gateway 控制面错误”与“上游账号错误”。
- 带 control-plane 标记的 4xx/5xx：
  - 不触发账号池 failover
  - 不触发账号 quarantine / provider cooldown
  - 不触发 native fallback
  - 默认向调用方显式失败

---

## 3. P1 高优先级项

### P1-1: 所有 Anthropic CC Gateway 路径的账号代理 / TLS profile 需要统一剥离

**现状证据：**

- Native messages：`/Users/muqihang/.../backend/internal/service/gateway_service.go:4550-4557`、`:4577`
- Native count_tokens：`/Users/muqihang/.../backend/internal/service/gateway_service.go:8980`、`:9093`
- API-key passthrough：`/Users/muqihang/.../backend/internal/service/gateway_service.go:5067`、`:9093`
- OpenAI-compatible Chat Completions：`/Users/muqihang/.../backend/internal/service/gateway_forward_as_chat_completions.go:118-133`
- OpenAI-compatible Responses：`/Users/muqihang/.../backend/internal/service/gateway_forward_as_responses.go:115-130`

**要求：**

- 目标为 CC Gateway 时，Sub2API 连接 CC Gateway 的 proxy/TLS profile 必须独立于账号上游 proxy/TLS。
- 出口 proxy/TLS fingerprint 由 CC Gateway 到上游负责，而不是由 Sub2API 连接内网 CC Gateway 时继续带账号画像。
- 如果 `cc_gateway.base_url` 是内网 HTTP，Sub2API 应直连内网 CC Gateway。
- 如果 `cc_gateway.base_url` 是 HTTPS，必须明确：连接 CC Gateway 使用的是内部受控 TLS，而不是账号 TLS profile。

---

### P1-2: 统一对齐到 `2.1.146`，但 persona 输出层必须硬隔离

**现状证据：**

- `/Users/muqihang/.../backend/internal/pkg/claude/constants.go` 当前 anti-ban mimicry 已锁定 `2.1.146`。
- CC Gateway 历史 v0.3 文档与 config.example 仍停留在 `2.1.119`。
- 我们已经围绕 `2.1.145/2.1.146` 做过本地 probe、non-MITM debug、messages beta 修订与差异审计，因此当前共享账号池路线不应再继续保留 `2.1.119` 作为 canonical persona。

**要求：**

- 本轮共享账号池统一对齐到 `2.1.146`。
- `2.1.146` 在 Sub2API native mimicry 路径可继续使用；走 CC Gateway 路径时，Sub2API 不再作为 persona 输出层。
- CC Gateway 负责统一输出 `2.1.146` canonical persona，并补齐对应 fixtures / capture / config example。
- 联合 capture 中验证发到 fake upstream 的 UA/persona 由 CC Gateway 输出 `2.1.146`。
- 后续版本升级不跟随每次 latest 自动漂移；只有完成本地 probe / debug / capture 审计后，才允许升级 canonical persona。

---

### P1-2.5: 缺 per-account allow/deny/canary gate

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/cc_gateway_adapter.go:76-81` 当前只按平台/账号类型/全局 provider 开关判断是否走 CC Gateway。
- `/Users/muqihang/.../deploy/config.example.yaml` 当前主要是全局 `providers.anthropic`。

**风险：** 一旦全局开启 `providers.anthropic=true`，所有符合条件账号都可能被切进 CC Gateway，缺少单账号灰度、禁用、canary 控制。

**要求：**

- 增加 account-level allow/deny/canary gate。
- 最少支持：
  - `extra.cc_gateway_enabled=false`
  - `extra.cc_gateway_canary_only=true`
- 测试覆盖：
  - 单账号禁用时不走 CC Gateway
  - 单账号 canary only 时仅在显式 canary 组/路由下走 CC Gateway
  - 全局 enabled 不应覆盖单账号 deny

### P1-3: metadata/session/account fields 同步不完整

**要求：**

- CC Gateway 使用 account identity 统一改写 `metadata.user_id.device_id`。
- 有真实 email/account_uuid/org_uuid 时按账号一致写入；缺失时不得伪造 `@cc-gateway.local` 到上游。
- `X-Claude-Code-Session-Id` 与 body `metadata.user_id.session_id` 必须同步。
- 多用户共享同一个上游账号时，identity/session 对上游应表现为账号级稳定策略，而不是每个下游用户各自乱入。

---

### P1-3.5: CC Gateway 缺 provider route allowlist

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts:214-224` 当前主要按 provider 校验。
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts:274-279` 当前会把 path 转发到固定 upstream。

**风险：** sub2api mode 下若没有 route allowlist，执行代理可能误把任意路径打到 Anthropic upstream，而不是仅限首轮已审查路径。

**要求：**

- 首轮 Claude / Anthropic 共享池只允许：
  - `/v1/messages`
  - `/v1/messages/count_tokens`
  - `/v1/chat/completions`（若首轮开放）
  - `/v1/responses`（若首轮开放）
- event logging 需单独决策：明确 rewrite / block / deferred。
- 未在 allowlist 的路径默认 fail-closed。

### P1-4: health/verify/rollback 不足

**要求：**

CC Gateway:

- `/_health`: mode, providers, identity storage writable, egress buckets, redacted upstream, auth status。
- `/_verify`: 给定 provider/account/bucket/body 时输出 redacted rewrite preview、identity hash、bucket、masked proxy、final upstream。

Sub2API:

- admin probe 或运维命令可按 account_id 生成发往 CC Gateway 的 redacted request preview。
- 回滚开关明确：global provider disable、single account disable、fail closed。

---

### P1-4.5: `openai_gateway_egress_bucket` legacy fallback 需要显式管控

**现状证据：**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/cc_gateway_adapter.go:88-95` 当前会在 `cc_gateway_egress_bucket` 为空时回退到 `openai_gateway_egress_bucket`。

**风险：** Claude / Anthropic 共享池可能误读到旧 OpenAI bucket，造成出口策略混乱或绕过首轮审查。

**要求：**

- 首轮默认禁止 silent fallback 到 `openai_gateway_egress_bucket`。
- 若迁移期必须兼容，则需要显式迁移开关、capture 验证和风险说明。
- 生产完成迁移后，应只使用 `cc_gateway_egress_bucket`。

### P1-5: 日志脱敏不足

**现状证据：**

- CC Gateway：`/Users/muqihang/chelingxi_workspace/cc-gateway/src/logger.ts:16-26` 直接输出 message/extra。
- CC Gateway：`/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy-agent.ts:15-17` 会打印完整 proxy URL。
- Sub2API：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_service.go:9670-9680` 存在 full body/debug 输出路径。
- Sub2API：`/Users/muqihang/.../backend/internal/service/gateway_service.go:215-223` 已有部分 header 脱敏，但不足以覆盖 capture/debug/local artifacts。

**要求：**

不得进入普通日志、审计日志、health/verify、capture artifact、测试 snapshot：

- `authorization`
- `x-api-key`
- `x-cc-gateway-token`
- Google access token
- refresh token
- cookie
- raw proxy URL
- proxy username/password
- identity JSON 中的上游 token
- 原始 prompt/body 明文（除非明确在 raw、且不提交）
- metadata 中的 account/session/user PII 明文

必须同时给出 CC Gateway 与 Sub2API 两侧的 redaction checklist 与测试。

---

## 4. 共享账号池策略矩阵

| 策略 | Identity Key | Egress | 连接池 | 推荐级别 |
|---|---|---|---|---|
| 一账号一身份一出口 | `provider:accountID` | 独立 bucket | `provider+account+bucket` | 默认生产 |
| 多账号共享出口 | `provider:accountID` | 同 bucket | 仍按账号隔离 | 小规模可信组 |
| 多用户共享一个账号 | `provider:accountID` | 账号绑定 bucket | 账号级连接池 | 可用，但限流/sticky 由 Sub2API 做 |
| 多账号共享同一 identity | 同一个 identity | 任意 | 任意 | 禁止 |
| 多账号经同一全局代理且连接池不隔离 | 不同 identity | 同全局代理 | 全局连接池 | 禁止 |

---

## 5. 实施计划

### Checkpoint 1: CC Gateway 安全边界先补齐

**Files:**

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/security-boundary.test.ts`

**Tasks:**

- [ ] 改为 provider-specific header allowlist。
- [ ] `mode: sub2api` 要求 `x-cc-account-id`。
- [ ] `x-cc-*` 转上游前全部删除。
- [ ] cookie/unknown x-*/proxy auth/forwarded headers 默认丢弃。
- [ ] billing header / CCH 继续 strip。
- [x] 已按 `16-no-cch-upstream-acceptance-validation.md` 验证 no-CCH body 在一个真实 upstream 最小 messages 请求下可正常响应；该证据只覆盖最小 messages 场景。
- [ ] 验证 shared-pool 路径不生成 billing attribution，不生成 CCH，也不透传下游用户 CCH。
- [ ] provider route allowlist：首轮仅允许已审查的 Claude / Anthropic 路径，其他路径 fail-closed。
- [ ] 增加负向测试。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

**Review gate:** 使用当前可用的高强度审查代理或人工 review。审查输出必须逐项覆盖：header allowlist 漏放、allowlist 误杀、credential 泄露、`x-cc-*` 泄露、provider 分叉、billing/CCH strip、回滚/fail-closed 语义。

---

### Checkpoint 1.5: CC Gateway canonical persona 2.1.146 lock

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/helpers.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/rewriter.test.ts`
- Create/update fixtures under `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/fixtures/claude-code/2.1.146/`
- Create/update audit doc: `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/claude-code-persona-audit.md`

**Tasks:**

- [ ] 把 `2.1.146` persona 变成显式实施项，而不是顺手升级版本号。
- [ ] 锁定并测试以下输出：
  - `User-Agent`
  - `entrypoint`
  - `x-stainless-*`
  - messages beta
  - count_tokens beta
  - `X-Claude-Code-Session-Id`
  - body `metadata.user_id` 相关字段
- [ ] `build_time` 与必要 runtime 字段必须从本地 `2.1.146` probe/debug 产物锁定。
- [ ] 若 `count_tokens` / `event_logging` 缺完整 `2.1.146` fixture，则必须显式写为 block / deferred / residual risk。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

---

### Checkpoint 2: CC Gateway IdentityManager

**Files:**

- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/identity.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/identity.test.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`

**Tasks:**

- [ ] 配置 `identity_storage_dir`、`max_identities`、`repair_corrupt_identity`。
- [ ] `getOrCreateIdentity(provider, accountId, accountEmail, accountUuid, orgUuid)`。
- [ ] 同账号稳定，不同账号不同。
- [ ] 并发首次创建只生成一个文件。
- [ ] corrupt JSON fail closed 或显式 repair。
- [ ] identity 不保存 token/proxy/cookie。
- [ ] rewriter 使用 request-scoped identity。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

---

### Checkpoint 3: CC Gateway Egress Bucket

**Files:**

- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/egress.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy-agent.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/egress.test.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-egress.test.ts`

**Tasks:**

- [ ] `egress.buckets[]` 配置。
- [ ] 支持 proxy bucket；`direct` bucket 仅允许显式配置，生产默认 `direct: false`。
- [ ] `direct: true` 必须在本地 capture 报告中显式记录，不得作为 unknown/disabled/proxy-failure 的 fallback。
- [ ] `x-cc-egress-bucket` 解析后立即从上游 header 删除。
- [ ] unknown/disabled bucket fail closed。
- [ ] proxy 失败不得 fallback direct。
- [ ] legacy `openai_gateway_egress_bucket` fallback 默认关闭；若迁移期启用，必须有显式开关和 capture。
- [ ] agent cache key 默认 `provider+account+bucket`。
- [ ] proxy URL 日志脱敏。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

---

### Checkpoint 4: Sub2API CC Gateway 路径边界修正

**Files:**

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_service.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_forward_as_chat_completions.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_forward_as_responses.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/cc_gateway_adapter.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/cc_gateway_adapter_test.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/deploy/config.example.yaml`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/deploy/.env.example`
- New tests as needed under `/Users/muqihang/.../backend/internal/service/`

**Tasks:**

- [ ] 在 Forward 顶层计算 `useCCGateway`，CC Gateway 路径禁用 Sub2API mimicry body rewrite。
- [ ] ForwardCountTokens 同步禁用提前 body rewrite。
- [ ] Chat Completions / Responses 在 `useCCGateway=true` 时禁用 `applyClaudeCodeOAuthMimicryToBody` 和所有 Sub2API Claude Code persona/body helper。
- [ ] 所有 Anthropic CC Gateway 路径（native messages、native count_tokens、API-key passthrough、chat completions、responses）连接 CC Gateway 时，不使用账号上游 proxy/TLS profile。
- [ ] 引入 per-account allow/deny/canary gate：`cc_gateway_enabled` / `cc_gateway_canary_only`。
- [ ] 默认禁止 silent fallback 到 `openai_gateway_egress_bucket`；如迁移期保留，必须有显式开关和 capture。
- [ ] 解析并处理 CC Gateway control-plane error wire contract。
- [ ] 增加 fake cc-gateway/stub 测试：native messages、count_tokens、chat completions、responses 收到的 body 均未被 Sub2API mimicry 污染。
- [ ] 测试断言 CC Gateway 路径没有 Sub2API `2.1.146` persona 产物；最终上游 persona 由 CC Gateway 输出 `2.1.146`。
- [ ] 确认 selected credential 和 `x-cc-*` 注入完整。
- [ ] 验证 CC Gateway control-plane 错误不会触发账号池 failover，也不会 silent fallback 到 native 直连路径。
- [ ] 如果首轮开放 Chat Completions / Responses，必须同步纳入同一组 fake cc-gateway tests；否则路由层面明确关闭。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend

go test ./internal/config -run 'CCGateway' -count=1

go test ./internal/service \
  -run 'CCGateway|StrictPassthrough|AnthropicAPIKeyPassthrough|ForwardCountTokens|ClaudeCodePersona|IdentityService|ChatCompletions|Responses' \
  -count=1

go test ./internal/pkg/antigravity \
  -run 'NewAPIRequest|ForwardBaseURLs|OAuth|Fallback|Stream|V1Internal' \
  -count=1
```

---

### Checkpoint 5: CC Gateway Antigravity Provider（首轮后置，不进入 Claude/Anthropic canary）

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter-antigravity.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/rewriter-antigravity.test.ts`
- Create/modify proxy integration tests.

**首轮状态:** Deferred。只有 Claude / Anthropic 共享账号闭环、本地 capture、真实 canary 均通过后，才进入本 checkpoint。首轮部署必须保持 Antigravity provider disabled。

**Tasks:**

- [ ] `providers.antigravity.enabled/upstream/daily_upstream`。
- [ ] Accept `/v1internal:*` paths only for Antigravity provider。
- [ ] Preserve `?alt=sse`。
- [ ] Preserve selected Google OAuth `authorization`。
- [ ] Validate `x-cc-project-id` against body project when both exist。
- [ ] SSE byte streaming passthrough test。

**Verification:**

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

---

### Checkpoint 6: Observability / Redaction / Rollback

**Files:**

- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/redact.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/logger.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/redaction.test.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/rollback.test.ts`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/v0.3.0-transport-risk-report.md`
- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/v0.3.0-rollback-runbook.md`

**Tasks:**

- [ ] 统一 redaction helper。
- [ ] health/verify 不泄露 token/proxy/cookie。
- [ ] CC Gateway 与 Sub2API 两侧的 log/audit/debug/capture 不泄露 secrets。
- [ ] 回滚开关测试。
- [ ] transport residual risk 报告。

---

### Checkpoint 6.5: 部署边界与回滚语义固化

**Files / Artifacts:**

- Create: `/Users/muqihang/chelingxi_workspace/cc-gateway/docs/v0.3.0-deployment-boundary.md`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/deploy/config.example.yaml`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/deploy/.env.example`
- Add tests as needed under CC Gateway and Sub2API.

**Tasks:**

- [ ] 明确 CC Gateway identity storage 的持久 volume、目录权限、备份/清理策略。
- [ ] 明确 Sub2API ↔ CC Gateway 只能走 loopback / Docker private network / TLS/mTLS/可信反代；跨公网明文 HTTP 禁止。
- [ ] 明确 egress bucket proxy secrets 只存在 CC Gateway 配置/secret manager，不进入 Sub2API extra、请求头、health、verify、capture、日志。
- [ ] 定义单账号 `cc_gateway_enabled=false` 的语义：生产共享池默认应 fail closed 或摘出账号，不得 silent fallback 到未审查 native 直连路径。
- [ ] 定义 provider disabled / bucket disabled / bucket unknown / proxy failure 的语义：默认 fail closed，不回退 direct，不回退 default bucket。
- [ ] 定义全局回滚：只有显式关闭 `gateway.cc_gateway.enabled=false` 且经过人工确认时，才允许回到 Sub2API native 路径。
- [ ] 增加测试或运维 checklist，证明回滚不会绕过 CC Gateway 悄悄使用账号代理/TLS profile。

**Pass criteria:**

- 所有回滚路径都是显式的、可审计的。
- 没有“错误时自动直连官方上游”的 silent fallback。
- proxy secret、token、cookie 不出现在任何文档样例以外的 runtime 输出中。

---

### Checkpoint 7: 联合本地 Capture 验收

**Artifacts directory:**

`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/captures/cc-gateway-shared-pool-local/`

**Scenarios:**

- [ ] Anthropic OAuth messages。
- [ ] Anthropic OAuth count_tokens。
- [ ] Anthropic API-key passthrough messages/count_tokens。
- [ ] OpenAI-compatible Anthropic chat/responses 如生产开放。
- [ ] Antigravity：首轮只验证 provider disabled / fail-closed；不要求 stream SSE 成功链路。
- [ ] bad bucket fail closed。
- [ ] wrong gateway token / provider disabled / route not allowlisted 不触发账号池 failover。
- [ ] wrong gateway token fail closed。
- [ ] two accounts: different identity, different bucket。
- [ ] two accounts: different identity, same bucket but isolated connection cache。

**Pass criteria:**

- `x-cc-*` 不到 fake upstream。
- selected account credential 保留且不被 gateway OAuth 覆盖。
- CC Gateway rewrite/identity/egress canonicalization 失败时 fail-closed。
- billing header / CCH 不出现。
- Sub2API `2.1.146` mimicry 不污染 CC Gateway 路径。
- CC Gateway canonical persona 为 `2.1.146`。
- identity per account 稳定。
- egress bucket 命中。
- proxy failure 不直连。

---

## 6. 部署前配置建议

### Sub2API

```yaml
gateway:
  cc_gateway:
    enabled: true
    base_url: "http://cc-gateway:8443"
    token: "<strong-random-internal-token>"
    timeout_seconds: 600
    default_egress_bucket: default
    providers:
      anthropic: true
      antigravity: false # Antigravity 完成 Checkpoint 5 后再开
```

账号 extra：

```json
{
  "cc_gateway_egress_bucket": "acct-001",
  "cc_gateway_enabled": true,
  "cc_gateway_canary_only": false,
  "account_uuid": "...",
  "organization_uuid": "..."
}
```

> 首轮建议默认显式写入 `cc_gateway_enabled` / `cc_gateway_canary_only`，不要依赖“全局 enabled + 自动全量切流”。

### CC Gateway

```yaml
mode: sub2api
server:
  port: 8443
  # 生产建议 Docker private network / loopback；跨主机必须 TLS/mTLS 或反代 TLS

providers:
  anthropic:
    enabled: true
    upstream: "https://api.anthropic.com"
  antigravity:
    enabled: false
    upstream: "https://cloudcode-pa.googleapis.com"
    daily_upstream: "https://daily-cloudcode-pa.googleapis.com"

auth:
  gateway_token: "<same-as-sub2api>"

identity:
  storage_dir: "/var/lib/cc-gateway/identities"
  max_identities: 1000
  repair_corrupt_identity: false
  canonical_persona:
    claude_code_version: "2.1.146"
    build_time: "<lock-from-local-2.1.146-probe>" # 必须从本地 2.1.146 probe/debug 产物锁定，不允许沿用 2.1.119 旧值

egress:
  reject_unknown_bucket: true
  default_bucket: default
  buckets:
    - name: default
      direct: false
      proxy_url: "http://user:pass@proxy-default:8080"
    - name: acct-001
      direct: false
      proxy_url: "http://user:pass@proxy-acct-001:8080"
```

> 以上是目标配置形态，当前 CC Gateway 需要先按 Checkpoint 2/3 实现后才能使用。

> 注意：目标配置形态中的 `providers.<name>.enabled/upstream`、`identity.canonical_persona` 等字段与 CC Gateway 当前 schema 仍有差异，执行时必须同步修改 `src/config.ts`、`config.example.yaml`、config tests 与 backcompat 测试，不能只改文档。

---

## 7. 真实 canary 进入条件

不得进入真实账号 canary，除非全部满足：

- [ ] Checkpoint 1-4 完成并审查通过。
- [ ] 首轮不需要 Antigravity；确认 `providers.antigravity=false` 且相关路由不走 CC Gateway。若未来需要 Antigravity，必须先完成 Checkpoint 5 并单独审查。
- [ ] Checkpoint 6 redaction/rollback 完成。
- [ ] Checkpoint 7 本地联合 capture PASS。
- [ ] count_tokens / event logging 若无完整 `2.1.146` 真实 fixture，则其首轮生产策略已明确写成 block / deferred / residual risk，而不是默认放行。
- [ ] Sub2API 与 CC Gateway 配置均确认不泄露 raw token/proxy。
- [ ] 账号授权浏览器出口与 token exchange /运行出口策略一致。
- [ ] 回滚命令和开关已演练。
- [ ] Chat Completions / Responses 若未完成联合 capture，生产路由中保持关闭或不走 CC Gateway。
- [ ] `direct` bucket 若存在，已被显式审批并在 capture 报告中记录；默认生产 bucket 均为 proxy bucket。
- [x] no-CCH upstream acceptance 已按 `16-no-cch-upstream-acceptance-validation.md` 完成，结论为 PASS，范围限定为一个 Claude Code 2.1.146 最小 `/v1/messages` 请求；更广流量仍需 canary。

首轮真实 canary：

1. 只用 1 个非主力 OAuth 账号。
2. 一账号一出口 bucket。
3. 只发 1 个短请求。
4. 停止条件：401/403/KYC/unusual activity/third-party warning/异常 429/上游提示代理或自动化风险。

---

## 7.5 Checkpoint 5 gate snapshot (2026-05-21)

| Gate | Status | First-wave decision | Evidence |
|---|---|---|---|
| P0-A count_tokens | DEFER | `/v1/messages/count_tokens` remains blocked/deferred and excluded from first-wave shared-pool canary until a real `2.1.146` dynamic fixture exists. | `captures/real-baseline/2026-05-21-claude-code-2146-count-tokens-local-probe/safe-deliverable/count_tokens_local_probe_summary.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-B refresh | PASS | Refresh behavior is limited to static audit plus service-local mock; no real `platform.claude.com` call was made. | `captures/real-baseline/2026-05-21-claude-code-2146-oauth-refresh-static-and-local-mock-audit/safe-deliverable/oauth_refresh_static_local_mock_summary.md` |
| P0-C metadata/session | DEFER | First-wave scope is limited to observed `--no-session-persistence`, default persistence first turn, `-c/--continue`, `stream-json`, and local retry/error paths; explicit `--resume` / `--session-id` remains excluded until separate local capture exists. | `captures/real-baseline/2026-05-21-claude-code-2146-session-lifecycle-local-probe/safe-deliverable/session_lifecycle_summary.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-D Linux parity | DEFER | Linux shared-pool deployment/persona parity claims remain blocked; no Linux or deployment-like host was available in this checkpoint. | `captures/real-baseline/2026-05-21-claude-code-2146-linux-parity-local-probe/safe-deliverable/linux_parity_summary.md` |
| P0-E event route-family policy | PASS | `POST /api/event_logging/batch` and `/api/event_logging/v2/batch` are suppressed locally; unknown `/api/event_logging/*` is blocked; the route family stays excluded from first-wave canary until schema capture exists. | `14-cc-gateway-shared-pool-compatibility-plan.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-F Sub2API boundary | PASS | Native messages/count_tokens and OpenAI-compatible chat/responses CC Gateway routes skip final mimicry/billing/CCH/proxy-TLS ownership in Sub2API. | `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_cc_gateway_boundary_test.go`; `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_cc_gateway_control_plane_test.go` |
| P0-G CC Gateway final-output boundary | PASS | Per-account identity/egress isolation, strict route/header allowlists, strip verifier, retry contract, and control-plane fail-closed behavior are covered in CC Gateway tests. | `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts`; `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts` |
| P0-H canonical 2.1.146 persona lock | PASS | CC Gateway synthesizes the canonical Claude Code `2.1.146` persona and joint local capture observes it at the localhost upstream. | `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/checkpoint3-remediation.test.ts`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` |
| P0-I CCH and cc_version fixture | PASS | Eight billing-attributed `2.1.146` localhost fixtures matched the 5-hex CCH verifier and 3-hex `cc_version` suffix formula. | `captures/real-baseline/2026-05-21-claude-code-2146-cch-cc-version-local-fixtures/safe-deliverable/cch_cc_version_fixture_summary.md` |
| P0-J API-key passthrough include/block/defer | PASS | Anthropic API-key passthrough `/v1/messages` is included through CC Gateway; API-key `/v1/messages/count_tokens` is explicitly deferred/blocked in first wave. | `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`; `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/proxy-sub2api.test.ts` |
| P0-K joint local capture | PASS | Fifteen local topology scenarios passed with redaction scan clean, no real upstream, no native fallback, and negative cases fail-closed. | `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md`; `captures/real-baseline/2026-05-21-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json` |

**Checkpoint 5 decision:** Do **not** start `27-final-shared-pool-signing-mode-design.md` yet. Remaining P0 `DEFER` items are `P0-A count_tokens`, `P0-C metadata/session`, and `P0-D Linux parity`. Current first-wave strip/no-CCH scope stays messages-only plus Anthropic API-key passthrough messages; count_tokens remains blocked/deferred, explicit resume/session-id lifecycle paths remain excluded, and Linux deployment parity claims remain blocked.

## 8. 最小测试命令清单

### CC Gateway

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm test
npm run build
```

### Sub2API

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend

go test ./internal/config -run 'CCGateway' -count=1

go test ./internal/service \
  -run 'CCGateway|StrictPassthrough|AnthropicAPIKeyPassthrough|ForwardCountTokens|ClaudeCodePersona|IdentityService|ChatCompletions|Responses' \
  -count=1

go test ./internal/pkg/antigravity \
  -run 'NewAPIRequest|ForwardBaseURLs|OAuth|Fallback|Stream|V1Internal' \
  -count=1

go test ./internal/handler ./internal/server/routes \
  -run 'CountTokens|Antigravity|Gateway|ClaudeCode' \
  -count=1
```

发布前再跑更大范围：

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./... -count=1
```

---

## 9. 结论

当前不能直接部署为共享账号池生产路线。准确状态是：

- Sub2API 当前基准已有 CC Gateway adapter 基础和测试。
- CC Gateway 当前 main 已有 `sub2api mode` 透明凭证基础。
- 但要让两个项目真正合力发挥价值、把共享账号池中的混杂信号压到最低，以下关键闭环尚未完成：
  1. per-account identity
  2. per-account/per-bucket egress
  3. strict provider/header/route allowlist
  4. Sub2API CC Gateway 路径禁用本地 mimicry body/persona 改写
  5. control-plane errors fail-closed 且不触发账号池 failover
  6. 2.1.146 persona fixture / count_tokens / event logging / redaction / rollback 闭环
  7. CCH 新算法若要从 offline verifier 进入手动审批的 opt-in signing mode，必须先完成 `15-cch-algorithm-validation-and-usage-plan.md` 中的 V1-V4 以及 `25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md` 的 G1-G5；首轮共享账号池默认仍 strip billing/CCH，且 sign 不是 strip 失败后的自动兜底
- Antigravity 仍需在 CC Gateway 侧实现 provider 支持。

推荐先按 Checkpoint 1-4 完成 Claude/Anthropic 共享账号池闭环，再完成 Checkpoint 6/6.5/7 的本地验收与回滚演练。Antigravity 明确后置，不纳入首轮上线。
