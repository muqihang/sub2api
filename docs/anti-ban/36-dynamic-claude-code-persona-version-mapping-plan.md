# 动态 Claude Code Persona / 版本映射实施计划

日期：2026-05-24  
状态：B2 已实施并通过 Checkpoint 7 复审；作为动态 Persona / Model Resolver 基线
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

## 1. 目标

Claude Code CLI 自动更新很快，正式号池不能长期硬编码 `2.1.150`。本计划目标是让 Sub2API + CC Gateway 在安全边界内动态继承真实 Claude Code CLI 的版本、beta、headers、session 和能力形态，同时防止普通客户端伪造 Claude Code persona。

核心目标：

```text
可信 Claude Code CLI-through 请求：优先继承真实客户端 persona；
已知版本：使用 profile registry；
未知小版本：兼容通过 + 审计 + 灰度预算；
未知大版本或字段漂移：隔离灰度或 quarantine；
明显伪造/矛盾：fail closed；
永远不因版本映射削掉 Claude Code 1m-capable model family、thinking、tools、context_management。
```

## 2. 当前问题

当前成功 runtime 已验证：

- `claude_code_2_1_150_subscription_1m`；
- `env.version=2.1.150`；
- `User-Agent=claude-cli/2.1.150 (external, sdk-cli)`；
- 1m context enabled；
- 30 tools / 32k max_tokens / thinking / context_management 可用。

但如果 Claude Code CLI 更新到 `2.1.151`、`2.1.152` 或更高版本，继续硬编码 2.1.150 会产生两类风险：

1. 上游看到的 persona/header/beta 与真实 CLI 形态偏离；
2. 我们为了安全而“未知就拦”会影响正式用户可用性。

因此需要动态版本映射。

## 3. 信任边界

### 3.1 可信来源

只有以下来源允许触发 dynamic persona：

- 本机 redacting forwarder / control-plane guard；
- guard attestation 通过 server-side 强验证：mTLS、HMAC/JWS 或等价内部信任机制；
- attestation 必须覆盖 timestamp、nonce、method、path_template、body_attestation_digest_or_omission、session_scoped_hmac_or_opaque_id、policy_version，并具备短重放窗口；对 telemetry/eval/raw prompt/raw body 禁止 digest 覆盖原始字节，必须使用 digest_omitted_reason + body_length_bucket + schema_summary，并由 attestation 覆盖该 omission decision；
- 外部客户端提交的同名 attestation/header 必须在入口层剥离或拒绝；
- 已启用 CLI-through 模式的 Sub2API 专用入口；
- 已删除本机 Authorization/x-api-key/cookie；
- 已注入 Sub2API 入口认证；
- route、body、headers 通过 Claude Code shape validator；
- 账号选择、scope、proxy bucket、session budget 通过。

### 3.2 不可信来源

以下不能直接信任 header：

- 普通 OpenAI-compatible API 调用；
- 非 CLI-through route；
- 直接打 Sub2API 且只伪造 `User-Agent: claude-cli/...` 的请求；
- 带本机 token/cookie 的请求；
- body/header shape 与 Claude Code 不一致；
- unknown client 没有 guard attestation。

不可信来源只能使用 server-selected profile，不能动态继承客户端 persona。

## 4. Persona 输入字段

可信 CLI-through 可读取并传递：

- `User-Agent`：解析 `claude-cli/<version>` 或兼容 `claude-code/<version>`；
- `anthropic-beta`：tokenized beta list；
- `anthropic-version`；
- `X-Claude-Code-Session-Id`；
- `x-app`；
- `X-Stainless-*`；
- body top-level keys；
- model；
- max_tokens；
- tools_count；
- thinking/context_management/output_config shape；
- stream。

禁止作为 persona 输入：

- Authorization 值；
- x-api-key 值；
- cookie；
- raw prompt；
- raw telemetry body；
- 本机 email/account/org UUID；
- proxy credential。

## 5. Version Resolver 策略

### 5.1 Version family

把版本分为：

```text
major.minor.patch
```

示例：`2.1.150`。

策略：

| 情况 | 处理 |
|---|---|
| exact known profile | 使用 registry 中精确 profile |
| same minor newer patch，如 2.1.151 | dynamic-compatible，通过但标记 `observed_minor_drift` |
| same major newer minor，如 2.2.x | compatibility tier，低预算灰度 + 强审计 |
| major 变化，如 3.x | quarantine 或人工批准灰度 |
| version 解析失败 | fail closed for dynamic persona，回退 server profile 仅限非 CLI-through普通路径 |
| UA 与 beta/body 矛盾 | fail closed |

注意：正式用户不能因为小版本自动更新就大面积不可用。小版本漂移默认兼容，但必须审计和可熔断。

### 5.2 Beta Resolver

beta 处理原则：

1. 已知 profile 的 beta 按 registry 生成；
2. 可信 CLI-through 的 beta 可在 registry/candidate allowlist 约束下继承；
3. 必须保留 Claude Code 1m-capable model family：`context-1m-2025-08-07`；
4. unknown beta token 不盲目透传也不盲目删除；
5. unknown beta token 必须满足 strict syntax、candidate_beta_allowlist、version/profile 绑定、风险 denylist/conflict 检查、localhost replay 或等价 fixture 证明，否则 quarantine / localhost-only / 使用 registry profile。

unknown beta token 处理：

| 条件 | 处理 |
|---|---|
| trusted same-minor drift + token 在 server-maintained candidate_beta_allowlist 中 + 绑定 version_family/profile + per-token kill switch/预算/审计/熔断 + 不在 denylist/conflict/risk list + localhost replay 或等价 fixture 已证明不会改变禁止字段或触发 risk response | pass-through + audit |
| token 属于已知危险/冲突列表 | drop/fail closed，按策略配置 |
| token 导致 upstream 400/403/429/risk | profile-level 熔断 |
| untrusted client 提供 unknown beta | 不继承，使用 server profile |

这解决“未知就不传不合适”的问题：可信小版本漂移不硬挡，但 unknown beta 不能仅凭格式正常 blind pass-through；必须进入 candidate allowlist、审计、预算和熔断体系。

### 5.3 Header Resolver

对于可信 CLI-through：

- `User-Agent`：优先真实 CLI version，格式规范化为 `claude-cli/<version> (external, sdk-cli)`；
- `X-Stainless-*`：优先真实 CLI 值，但必须同时 allowlist 字段名和值 schema；unknown field 或 unknown value category 默认 drop 或 quarantine，不能原样继承；
- `x-app`：必须是 `cli` 或 allowlisted Claude Code shape；
- session id：UUID-like 保留，否则生成 UUID-like 并同步 metadata；
- `anthropic-version`：allowlist，默认 `2023-06-01`。

对于不可信来源：

- 忽略客户端 persona header；
- 使用 selected server profile；
- 不允许伪造 Claude Code route。


### 5.4 Model Resolver

模型处理不能长期固定在某一个版本，也不能无条件放行任意字符串。策略：

| 情况 | 处理 |
|---|---|
| registry exact known model，例如 `claude-sonnet-4-6`、`claude-opus-4-7` | 正常放行 |
| trusted CLI-through 观测到同一 Claude Code 模型族的新版本，例如未来 `claude-sonnet-4-8` / `claude-opus-4-8` | 进入 candidate_model_allowlist 灰度；限制只能是流量、并发、账号预算、灰度比例，不能削 1m/tools/thinking/context_management/stream/max_tokens 能力 |
| trusted CLI-through 观测到未知但语法合法的 Claude model | localhost replay + candidate review；默认 quarantine 或灰度账号池，不 broad block 全量用户 |
| untrusted client 提供未知 model | 不继承，使用 server policy 或 fail closed |
| model 与 beta/persona/body shape 冲突 | fail closed |

实现要求：

- candidate model 必须有 per-model kill switch、审计、熔断和回滚；
- 不能把未来 Sonnet/Opus 新版本机械挡死；
- 也不能让普通 API 客户端通过伪造 model/UA 绕过策略；
- model resolver 输出必须进入 cache key、session/account budget 和 safe audit。

## 6. Profile Registry

新增 profile registry，至少包含：

```yaml
profiles:
  claude_code_2_1_150_subscription_1m:
    version: 2.1.150
    version_family: 2.1
    trust_level: verified_real_cli
    beta:
      - claude-code-20250219
      - oauth-2025-04-20
      - context-1m-2025-08-07
      - interleaved-thinking-2025-05-14
      - context-management-2025-06-27
      - prompt-caching-scope-2026-01-05
      - advisor-tool-2026-03-01
      - effort-2025-11-24
      - extended-cache-ttl-2025-04-11
    capabilities:
      sonnet_4_6_1m: true
      max_tokens: 32000
      tools_max_observed: 30
      tools_budget: 40
      thinking: true
      context_management: true
      stream: true
```

registry 字段：

- version；
- version_family；
- beta tokens；
- User-Agent template；
- stainless shape；
- capability envelope；
- trust level；
- first_observed_at；
- last_verified_at；
- source evidence path；
- kill switch；
- response/failure counters。

## 7. Sub2API 职责

Sub2API 增加：

1. `ClaudeCodeClientEvidence`：从 trusted guard headers/metadata 中提取安全字段；
2. shape validator：确认请求确实像 Claude Code CLI；
3. account selector：按用户/session/账号预算选择号池账号；
4. version resolver pre-decision：生成 `x-cc-cli-version-observed`、`x-cc-beta-observed-hmac`、`x-cc-persona-trust`；
5. dynamic persona gate：只有 trusted CLI-through 才传给 CC Gateway；
6. safe audit：只记录 version、server-scoped opaque id / scoped keyed HMAC、field set、decision，不记录 raw token/body。

Sub2API 不做最终 persona 重写，不生成 CCH。

## 8. CC Gateway 职责

CC Gateway 增加：

1. profile registry loader；
2. dynamic persona resolver；
3. beta resolver；
4. header resolver；
5. CCH/env version binding；
6. verifier 扩展：确认 final upstream UA/beta/cc_version/session 与 resolver decision 一致；
7. fail-closed 规则：resolver 失败不得 fallback 到 legacy/direct/sign-to-strip；
8. raw capture 不保存 raw request/response/body/prompt/telemetry，只保存安全证据、server-scoped opaque id / scoped keyed HMAC、长度 bucket、字段集合、schema 摘要、状态和决策，不落 raw prompt/body。

CC Gateway 输出 final upstream：

- `User-Agent`；
- `anthropic-beta`；
- `X-Claude-Code-Session-Id`；
- `X-Stainless-*`；
- CCH `cc_version`；
- metadata session alignment。

## 9. Decision states

```text
verified_exact
observed_minor_drift
compatibility_tier
quarantine_version
untrusted_spoof
unsafe_conflict
```

行为：

| state | 上游放行 | budget | audit | 自动 profile |
|---|---:|---:|---:|---:|
| verified_exact | 是 | 正常 | 正常 | 已存在 |
| observed_minor_drift | 是 | 轻度限制 | 强 | 可自动创建候选 |
| compatibility_tier | 灰度 | 限制 | 强 | 候选，需复核 |
| quarantine_version | 否或仅 localhost | 无 | 强 | 否 |
| untrusted_spoof | 否 | 无 | 安全事件 | 否 |
| unsafe_conflict | 否 | 无 | 安全事件 | 否 |

## 10. 安全冲突规则

以下必须 fail closed：

- 本机 Authorization/x-api-key/cookie 未被删除；
- UA 是 Claude Code，但 route/body 不符合 Claude Code shape；
- session header 与 metadata 严重矛盾且无法修正；
- beta 中出现被禁 token；
- profile 要求 1m，但 final beta 丢失 `context-1m-2025-08-07`；
- CCH `cc_version` 与 resolver version 不一致；
- post-sign mutation 发现 persona 被改写；
- dynamic resolver 失败后试图走 legacy fallback。



### 10.1 摘要与标识防关联规则

所有 `*_hash`、cache key hash、audit hash、beta/profile/session/account/user partition 摘要均必须是 server-scoped opaque id 或 scoped keyed HMAC；禁止 plain hash / SHA / MD5 / 长期 deterministic hash。HMAC key 必须按 environment、tenant/session、path_template、purpose、cache_scope、key_version/rotation period 分区；不得跨用户、跨账号、跨日期或跨 purpose 稳定关联。telemetry/eval/raw prompt/raw body 不得计算 body digest，必须使用 digest_omitted_reason + body_length_bucket + schema_summary。

## 11. 实施任务

### Task A：Profile registry

- 新增 registry schema；
- 写入 `claude_code_2_1_150_subscription_1m`；
- 支持 exact/compatible/quarantine；
- 测试 unknown minor、unknown major、bad version。

### Task B：Sub2API client evidence and guard attestation

- guard -> Sub2API 增加 safe observed fields；
- guard 生成 server-verifiable attestation，使用 mTLS、HMAC/JWS 或等价内部信任机制；
- attestation 覆盖 timestamp、nonce、method、path_template、body_attestation_digest_or_omission、session_scoped_hmac_or_opaque_id、policy_version，并限制重放窗口；对 telemetry/eval/raw prompt/raw body 禁止 digest 覆盖原始字节，必须使用 digest_omitted_reason + body_length_bucket + schema_summary；
- Sub2API 入口剥离或拒绝外部客户端提交的同名 attestation/header；
- Sub2API 验证 trusted route；
- 不可信 route 忽略 observed persona；
- 测试伪造 headers 和伪造 attestation 不生效。

### Task C：CC Gateway dynamic resolver

- profile + observed fields 合并；
- final UA/beta/stainless/session/CCH version 统一；
- verifier 检查一致性；
- 测试 2.1.151 小版本漂移通过。

### Task D：Audit and kill switches

- 按 state 输出 safe audit；
- 按 version/profile/beta token/account 熔断；
- 不输出 raw token/body/prompt。

### Task E：localhost-only + canary readiness

- fixture 模拟 2.1.150 exact；
- fixture 模拟 2.1.151 minor drift；
- fixture 模拟 unknown beta；不在 candidate_beta_allowlist 时必须 quarantine / localhost-only / 使用 registry profile，不能 pass-through；
- fixture 模拟 spoof；
- localhost mock 验证 final upstream headers/CCH；
- 真实 canary 需用户单独批准。

## 12. 测试矩阵

### 单元

- parse UA version；
- beta tokenization；
- profile lookup；
- minor drift decision；
- major drift quarantine；
- spoof fail closed；
- 1m token required；
- verifier catches version mismatch。

### 集成

- local guard -> Sub2API -> CC Gateway -> localhost mock；
- trusted CLI-through 2.1.150 exact；
- trusted CLI-through 2.1.151 observed minor drift；
- untrusted direct API spoof；
- control-plane upload cache key includes version/profile；
- session budget 不因版本漂移失效。

### 真实验证

每次新 version family 首次真实上游必须：

1. localhost-only replay；
2. single-account canary；
3. no retry；
4. 不保存 server-side raw request/response/body/prompt/telemetry；只保留 scoped keyed HMAC（不得覆盖 telemetry/eval/raw prompt/raw body 原始字节）、长度 bucket、字段集合、schema 摘要、状态码、resolver/verifier 决策、熔断结果和 sensitive scan 结果；
5. safe field audit；
6. profile registry 更新；
7. kill switch ready。

## 13. 与正式号池控制面上传的关系

动态 persona 输出必须进入控制面 cache key：

```text
account_scoped_opaque_id_or_hmac + path_template + beta_profile_scoped_hmac_or_opaque_id + cli_version_family + policy_version + response_schema_version + key_version
```

否则不同 Claude Code 版本的 bootstrap/settings/feature flag 可能互相污染。

控制面真实上传也必须使用 dynamic persona resolver 生成的 headers，而不是沿用硬编码 2.1.150。

## 14. P0 / P1 / Unknown

### P0

- 普通客户端伪造 Claude Code header 被信任；
- unknown version 直接全局放量且无熔断；
- 1m context 或未来 Claude Code 1m-capable model family 在 production profile 中丢失；
- CCH cc_version 与 final UA 不一致；
- dynamic resolver 失败后 fallback 到 legacy/direct；
- raw token/body/prompt 进入 audit。

### P1

- minor drift 只放行但不产生审计；
- beta unknown token 无统计；
- cache key 不含 version/profile；
- profile registry 无 kill switch；
- profile 更新需要手工改多处配置。

### Unknown

- Claude Code 未来版本是否改变 UA 格式；
- 新 beta token 是否需要配套 body 字段；
- bootstrap/settings 是否会返回 version-specific feature flags；
- 未来 Sonnet/Opus profile 是否需要不同 capability envelope。

## 15. 结论

动态 persona 不是无条件相信客户端 header，而是在可信 CLI-through 链路里把真实 Claude Code 版本和能力形态变成可审计、可熔断、可灰度的 resolver 决策。

生产策略：

```text
可信小版本漂移：兼容传 + 审计；
未知大变化：隔离灰度；
伪造/矛盾：fail closed；
1m/tools/thinking/context_management：不因产品化配置被削掉。
```
