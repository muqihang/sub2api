# CCH 算法验证与使用决策手册

> **For agentic workers:** 本文只定义验证与决策流程。不要直接把 CCH 签名打开到生产共享账号池路径。实施前必须经过本地 fixture、local capture、联合 capture、审查和真实 canary。

**Goal:** 验证新发现的 CCH 5 位签名算法是否与 Claude Code 实际请求一致，并明确验证成功后在 Sub2API + CC Gateway 共享账号池路线中的正确使用边界。

**Current conclusion:** 新文档中的 CCH seed `0x4d659218e32a3268` 已经在既有 Claude Code 2.1.145 localhost raw capture 上离线验证命中；当前 Sub2API legacy signer 使用的 `0x6E52736AC806831E` 不命中这些 fixture。`CLAUDE_CODE_ATTRIBUTION_HEADER=0` 的 no-CCH 链路已按 `16-no-cch-upstream-acceptance-validation.md` 完成 Phase A 本地 body-shape 验证和 Phase B 一次真实 upstream 最小 messages 请求验证，结论 PASS；因此共享账号池首轮可以继续以 `billing/CCH strip` 为默认方向，但范围仍限于最小 messages canary，count_tokens/event_logging/复杂流量需继续验证。

---

## 1. 发现来源

用户提供的算法文档：

`/Users/muqihang/Library/Containers/com.tencent.xinWeChat/Data/Library/Application Support/com.tencent.xinWeChat/2.0b4.0.9/7ecf2a306ac810d8c592f3a6f9f9d9e7/Message/MessageTemp/9e20f478899dc29eb19741386f9343c8/File/cch-algorithm.md`

核心声明：

```text
cch = lower_hex_5( xxh64(body_with_placeholder, 0x4d659218e32a3268) & 0xFFFFF )
```

其中 `body_with_placeholder` 是把 billing header 中 `cch=xxxxx;` 还原为 `cch=00000;` 后的最终 JSON body bytes。

---

## Current verification status

- CCH algorithm offline verification: **PASS**. See `captures/real-baseline/2026-05-21-cch-algorithm-offline-verification.md`.
- Verified against 8 existing Claude Code 2.1.145 raw localhost requests.
- New seed `0x4d659218e32a3268`: 8/8 matched.
- Old Sub2API seed `0x6E52736AC806831E`: 0/8 matched.

## 2. 已完成的离线交叉验证

验证输入来自既有 localhost raw capture，不触达真实 upstream：

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/captures/real-baseline/2026-05-20-non-mitm-prep/20260520T170354Z-local-non-mitm-probe/raw/captured_requests.raw.json`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/captures/real-baseline/2026-05-20-non-mitm-prep/20260520T170245Z-local-non-mitm-probe/raw/captured_requests.raw.json`

验证方法：

1. 读取每条 raw request body。
2. 提取真实 `cch=xxxxx;`。
3. 将 body 中该字段还原为 `cch=00000;`。
4. 分别计算：
   - `xxh64(body, 0x4d659218e32a3268) & 0xFFFFF`
   - `xxh64(body, 0x6E52736AC806831E) & 0xFFFFF`
5. 对比真实 CCH。

结果摘要：

| fixture | requests | seed `0x4d659218e32a3268` | seed `0x6E52736AC806831E` |
| --- | ---: | --- | --- |
| `20260520T170354Z-local-non-mitm-probe` | 4 | 4/4 命中 | 0/4 命中 |
| `20260520T170245Z-local-non-mitm-probe` | 4 | 4/4 命中 | 0/4 命中 |

结论：

- CCH 的 body-level xxh64 算法和 seed `0x4d659218e32a3268` 对 Claude Code 2.1.145 localhost raw fixture 成立。
- 当前 Sub2API `backend/internal/service/gateway_billing_header.go` 中 legacy `cchSeed = 0x6E52736AC806831E` 与这些 fixture 不一致。
- 既有 fixture 中 `cc_entrypoint=sdk-cli`，因此不要照抄外部文档里的 `cc_entrypoint=cli` 作为 2.1.145/2.1.146 目标值；entrypoint 必须以当前版本 capture 为准。
- 当前 CC Gateway `src/rewriter.ts` 中 `computeCCH()` 实际计算的是 `cc_version` 3 位消息指纹，不是 5 位 body CCH；命名容易误导，后续应重命名或注释澄清。
- 截至本项目已检查的 local raw capture，未观察到独立 HTTP `CCH` header；5 位 CCH 出现在 JSON body 的 `x-anthropic-billing-header` system block 中。若未来版本出现真实 HTTP `CCH` header，必须单独验证。

---

## 3. 仍必须补齐的验证

CCH 算法虽然已被 2.1.145 fixture 支持，但还不能直接作为生产开关。必须补齐：

### V1. Claude Code 2.1.146 localhost raw fixture

目的：确认 2.1.146 仍使用同一 CCH seed 和占位符流程。

要求：

- 使用 localhost HTTPS capture server。
- `ANTHROPIC_BASE_URL=https://localhost:<port>`。
- 不使用 MITM。
- 不调用 `api.anthropic.com`。
- 不保存 Authorization / raw token 到 safe deliverable。
- 产出 raw 仅本机临时保留，safe deliverable 只保存摘要。

通过标准：

- 至少 2 个 `/v1/messages?beta=true` 请求。
- 每个请求 `seed 0x4d659218e32a3268` 命中真实 CCH。
- `seed 0x6E52736AC806831E` 不再被当作候选生产 seed。

### V2. cc_version 3 位 fingerprint 同步验证

注意：CCH 和 `cc_version` 后缀不是同一个算法，且后者不能简化为 `sha256(first_user_message + version)`。

- CCH: body-level xxh64 低 20 位，5 hex。
- `cc_version` suffix: 目前从 CC Gateway 0.3 / 旧版逆向线索看，应按 `sha256("59cf53e54c78" + chars + cli_version)[:3]` 计算，其中 `chars` 是从被选中的 first user text 中按 Claude Code/JS 字符串索引取 `[4, 7, 20]` 三个字符，不存在的位置用 `0` 填充。
- 该 suffix 公式仍必须用 2.1.146 raw fixture 单独验证；不要因为 5 位 CCH 命中，就假设 3 位 suffix 也正确。

已有快速验证发现：当第一条 user content 里包含 `<system-reminder>` 时，当前 Sub2API `extractFirstUserText()` 的行为可能与“选择/跳过 system-reminder 后再取 first user text”的规则不一致。若未来要重新生成 billing block，必须先修正并验证 fingerprint，否则 CCH 即使命中也仍可能不一致。

### V3. 服务端重签 local capture

在不触达真实 upstream 的情况下，构造一条服务端重写后的 body：

1. 生成或保留 billing block。
2. 最后一步紧凑 JSON 序列化。
3. 将 `cch=00000;` 作为占位符。
4. 用 `0x4d659218e32a3268` 计算 CCH。
5. 回填。
6. 用独立 verifier 再还原占位符并校验。

通过标准：

- verifier 对最终 body 100% PASS。
- body 改写后不得再发生任何会改变 JSON bytes 的步骤。
- count_tokens、messages、stream/non-stream 路径分别验证。

### V4. 联合链路 capture

只在 V1-V3 都通过后执行：

- Sub2API -> CC Gateway -> local capture upstream。
- 覆盖 strict、mimicry、shared-pool strip、可选 signing fallback。
- 确认共享账号池默认路径仍 strip billing/CCH。
- 签名路径必须是显式 opt-in，不得默认开启。

---

## 4. 验证成功后如何使用

### 4.1 共享账号池默认策略：仍然 strip，不启用 CCH

即使 CCH 验证成功，首轮共享账号池路线仍建议：

- 不透传下游用户 CCH。
- 不默认服务端生成 billing block。
- 不默认服务端签 CCH。
- 继续由 CC Gateway 统一移除 billing attribution header / CCH。

理由：

1. 已证明 `CLAUDE_CODE_ATTRIBUTION_HEADER=0` 在 localhost capture 中确实产生 no-CCH body，并且同一 env 下一次真实 upstream 最小 messages 请求可正常响应。
2. 基于这个最小场景 PASS，strip 比“网关伪造一个 billing block 并重签”更保守；如果后续 count_tokens/event_logging/复杂流量 canary 失败，再转向服务端重签或保留合法 billing block 的方案。
3. 共享账号池的主要风险来自 identity / egress / session / header 混杂；CCH 签名只能解决 billing body integrity，不能解决共享账号池整体画像问题。
4. 一旦服务端生成 billing block，就必须同时保证 cc_version suffix、body final bytes、header order、session、metadata、event logging 等所有相关字段一致，否则反而增加新不一致面。

### 4.2 CCH signer 的正确定位

验证成功后，CCH signer 可作为以下用途：

1. **离线 verifier**：检查 captured body 的 CCH 是否与最终 body bytes 一致。
2. **回归测试工具**：防止未来 body rewrite 在签名后又修改 body。
3. **受控 fallback**：仅在明确要求“保留/生成 billing block”的单账号或实验路径中启用。
4. **诊断工具**：当上游出现 billing/body integrity 类异常时，用于排查是 CCH、cc_version suffix 还是 body mutation 问题。

不应用作：

- 共享账号池默认伪装手段。
- 用用户 A 的 body/header/CCH 固定绑定到账号后服务多个用户。
- 在未完成 2.1.146 fixture 和联合 capture 前直接生产启用。

### 4.3 如果未来要启用 signing fallback，建议边界

必须满足：

- `enable_cch_signing=false` 仍为默认。
- 只能通过账号级或路由级显式开关启用。
- CC Gateway 路径与 Sub2API native 路径只能有一个组件负责最终 body signing。
- signing 必须发生在所有 body rewrite 和 JSON serialization 之后。
- signing 后任何 body mutation 都必须禁止或重新签名。
- 若 verifier fail，必须 fail closed，不能无签名继续发。

建议实现位置：

- Shared-pool CC Gateway path: 如果未来一定要签，优先由 CC Gateway 签，因为它是最终 persona/body/header 输出层。
- Sub2API native path: 只保留 legacy / 单账号 fallback，不参与共享账号池默认路线。

---

## 5. 对现有代码的直接影响

### Sub2API

文件：

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_billing_header.go`
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend/internal/service/gateway_billing_block.go`

应做但不急于生产启用：

1. 把 legacy CCH seed 从 `0x6E52736AC806831E` 改为 `0x4d659218e32a3268`，但保持 deprecated / disabled by default。
2. 增加 fixture 测试：用 2.1.145 raw capture 摘要或脱敏 fixture 验证 seed。
3. 修正或至少测试 `extractFirstUserText()` 对 `<system-reminder>` 的跳过规则。
4. 保持 shared-pool / CC Gateway path 不调用 `signBillingHeaderCCH()`。

### CC Gateway

文件：

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/tests/rewriter.test.ts`

应做：

1. 将 `computeCCH()` 重命名为 `computeCcVersionFingerprint()` 或补充明确注释，避免把 3 位 fingerprint 与 5 位 CCH 混淆。
2. 共享账号池默认继续 strip billing header。
3. 若未来新增 signer，放到独立模块，例如 `src/cch.ts`，并用 fixture 测试隔离。
4. 签名路径必须 opt-in，默认关闭。

---

## 6. 决策结论

当前最佳决策：

1. 承认新 CCH 算法已获得强初步证据支持。
2. 立即补一轮 2.1.146 localhost raw CCH 验证。
3. `16-no-cch-upstream-acceptance-validation.md` 已通过最小 messages 场景；共享账号池首轮默认 strip 可以继续推进到 Sub2API + CC Gateway 联合 capture / canary，但不得外推到 count_tokens/event_logging/复杂流量。
4. 把 CCH signer 作为 verifier / fallback / 测试工具，而不是默认生产伪装手段。
5. 若后续业务强制要求 billing block 存在，再单独立项实现 signer，并经过完整 capture + canary。
