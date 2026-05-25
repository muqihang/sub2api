# 正式号池控制面上传策略

日期：2026-05-24  
状态：B2 已实施并通过 Checkpoint 7 复审；作为正式号池控制面上传策略基线
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

## 1. 目标

本策略把此前“控制面分类接管”升级为“正式号池控制面上传策略”。正式号池不能长期把 Claude Code CLI 的控制面请求全部本地拦截；上游控制面可能参与能力发现、功能开关、MCP、账号设置、风控和遥测。我们的生产目标是：

```text
所有控制面请求都进入统一接管、脱敏、审计、策略决策；
安全 GET/公共查询按选中号池账号或公共缓存上传；
高风险 POST 不 raw 上传，改为字段级脱敏、聚合、合成或继续 suppress；
未知或漂移路径进入 quarantine，不自动真实上游；
永远不上传用户本机 Claude Code token、cookie、raw prompt、raw body、raw telemetry body。
```

这不是“全拦”，也不是“全放”。它是生产号池下的分级上传和可回滚策略。

## 2. 已有证据边界

我们已经通过真实 Claude Code CLI-through 成功请求证明主链路可行：

- `claude-sonnet-4-6` / Opus 4.x Claude Code models；
- `max_tokens=32000`；
- 30 tools 级别能力；
- `thinking` / `context_management` / `stream` 保留；
- CC Gateway sign-primary、verifier、post-sign mutation、no fallback；
- proxy egress 已验证；
- 控制面请求可被本地 guard 分类接管。

safe deliverable 只使用脱敏字段、scoped keyed HMAC、长度 bucket、路径模板、schema 摘要、状态和计数。后续正式策略和 canary 验证不得生成、复制、依赖或交付 raw 调试包；如历史临时 raw 调试包存在，也不得作为实现输入或审查证据。排查只能使用 synthetic fixture、脱敏字段集合、长度、schema 摘要、scoped keyed HMAC 和状态结果。

所有正文、prompt、telemetry/eval、query 动态值相关摘要默认不得使用 plain hash / SHA / MD5 / 长期 deterministic hash。如确需去重，只能使用 scoped keyed HMAC，且 key 必须按 environment、tenant/session、path_template、purpose、日期分区并定期轮换；不得跨用户、跨账号、跨日期稳定关联。

## 3. 核心安全不变量

1. **本机凭证不出本机**：用户本机 Claude Code 的 `Authorization`、`x-api-key`、cookie、proxy credential 永不上传到 Sub2API、CC Gateway 或上游。
2. **所有控制面先转 intent**：local guard 对控制面请求生成 normalized intent envelope；Sub2API 不接收 raw CLI control-plane HTTP 请求。
3. **上游身份来自号池账号**：凡需真实上游的控制面，必须绑定已选中的号池账号、账号 server-scoped opaque id / scoped keyed HMAC、proxy bucket、scope、冷却和熔断状态。
4. **raw POST body 默认禁止**：telemetry/eval/raw event body 不进入 Sub2API，不进入 CC Gateway，不进入 Anthropic。
5. **账号/用户隔离**：账号级缓存、组织级缓存、用户会话级缓存必须分层；一个用户的本机状态不得污染另一个用户。
6. **小步开放**：每个 path template 单独 localhost replay、A/B、单路径 canary、复盘后才能开放。
7. **可熔断**：任一路径出现 400/401/403/429、风险文本、响应 schema 漂移、请求量异常，按账号/路径/profile 立即熔断。
8. **不复用 CCH 语义**：CCH 只属于 `/v1/messages` final-output 签名。控制面上传可由 CC Gateway 规范 header/persona，但不生成 messages CCH。
9. **Claude Code 1m-capable model family 不限制**：正式 profile 必须保留 `context-1m-2025-08-07` 及真实 Claude Code 富能力。
10. **摘要标识必须防关联**：所有 `*_hash`、cache key hash、audit hash、beta/profile/session/account/user partition 摘要均必须是 server-scoped opaque id 或 scoped keyed HMAC；禁止 plain hash / SHA / MD5 / 长期 deterministic hash。HMAC key 必须按 environment、tenant/session、path_template、purpose、cache_scope、key_version/rotation period 分区；不得跨用户、跨账号、跨日期或跨 purpose 稳定关联。telemetry/eval/raw prompt/raw body 不得计算 body digest，必须使用 digest_omitted_reason + body_length_bucket + schema_summary。


## 4. 两段式上传模型

### 4.1 Intent 上传：所有控制面都要进中心化决策

本机 guard 对每一条控制面请求生成 safe intent，并上传到 Sub2API control-plane router：

```text
local Claude Code CLI control-plane request
 -> local guard 删除本机认证和敏感字段
 -> normalized control-plane intent envelope
 -> Sub2API control-plane router
 -> policy/cache/quarantine/upstream decision
```

intent 允许字段：

- method；
- path_template；
- normalized_query allowlist 摘要或 scoped keyed HMAC；
- header_names；
- auth_shape，例如 `Authorization: Bearer`，不含值；
- body_length_bucket；
- body_scoped_keyed_hmac，仅在需要去重时使用，且 key 必须按环境/日期/路径/账号分区轮换；high-risk POST telemetry/eval 默认不上传 full-body deterministic hash；
- body_schema_summary，仅字段集合和类型摘要；
- cli_version，仅解析后的 semver / version family；
- beta_profile_scoped_hmac 或 server-scoped opaque profile id，以及 allowlisted beta token set；
- session_scoped_hmac 或 server-scoped opaque session id；
- persona header 摘要：`x-app` enum、allowlisted `X-Stainless-*` 字段名和值类别，禁止原样透传 unknown headers；
- classification；
- policy_version、strategy_version、response_schema_version；
- server-issued route_context_id 或 tenant/session handle；不得包含、暗示或由客户端指定 selected pool account，账号选择只能在 Sub2API 服务端完成；
- redaction_proof / sensitive_scan 结果。

禁止字段：

- raw Authorization / x-api-key / cookie；
- raw body / raw telemetry body / raw prompt；
- 本机 account/org/user UUID 或 email；
- 本机文件路径、环境变量、堆栈、项目内容；
- proxy credential；
- 未经 allowlist 的 POST body 字段。

### 4.2 上游上传：按风险分级

Sub2API 收到 intent 后才决定是否真实上游：

```text
intent accepted
 -> select pool account
 -> check scope / budget / cooldown
 -> cache lookup
 -> optional CC Gateway control-plane adapter
 -> upstream fetch/upload or local sanitized response
```

“所有控制面都上传”的生产含义是：**全部上传 intent 进入中心策略**；但并不意味着全部 raw 请求都转发到 Anthropic。高风险路径必须转换为安全等价物。

## 5. 决策矩阵

| 类别 | 典型路径 | 生产目标 | 上游身份 | body 策略 | response 策略 | 默认阶段 |
|---|---|---|---|---|---|---|
| Main messages | `POST /v1/messages?beta=true` | 正常放行 | selected_pool_account | raw messages body 仅瞬时处理，不落盘 | 原响应流式返回 | 已验证，进入 session budget |
| Bootstrap | `GET /api/claude_cli/bootstrap` | account-scoped cached fetch | selected_pool_account | none | schema allowlist + cache | B2 首批 |
| Account settings | `GET /api/oauth/account/settings` | account-scoped cached fetch | selected_pool_account | none | schema allowlist；禁止 raw account/org/user 泄露 | B2 首批 |
| Feature flags | `GET /api/claude_code_*` | account-scoped cached fetch | selected_pool_account | none | schema allowlist + version/profile 绑定 | B2 首批 |
| MCP servers | `GET /v1/mcp_servers` | account/user 隔离 cached fetch | selected_pool_account | none | schema allowlist；区分账号级和用户分区 | B2 次批 |
| MCP registry | `GET /mcp-registry/*` | public cached fetch | none/public egress | none | public raw allowlist + cache | B2 首批 |
| Org/referral | `/api/oauth/organizations/<id>/...` | 高风险 account-scoped fetch | selected_pool_account | none | 动态 org id 由 selected account metadata 重建 | B3，单独批准 |
| Event logging | `POST /api/event_logging/*` | sanitized synthetic telemetry 或继续 suppress | selected_pool_account | 禁止 raw；只允许合成最小事件 | suppress/204 或上传 synthetic ack | B3，默认关闭 |
| Eval | `POST /api/eval/*` | 默认 suppress；如需上传需单独模型 | selected_pool_account | 禁止 raw | suppress/204 | B3+，默认关闭 |
| Unknown | 任意新路径 | quarantine | none | forbidden | fail closed | 永久默认 |

## 6. 路径级策略

### 6.1 `/v1/messages`

- 进入现有 Sub2API -> CC Gateway sign-primary 主链路；
- 不改写真实 CLI body，除非 CC Gateway final-output/persona/CCH 策略要求；
- 保留 1m context、thinking、tools、context_management、stream；
- 生产使用 session budget，不再使用 canary 的 max_messages=1；
- raw body/prompt 不进入 safe deliverable。

### 6.2 Bootstrap / Settings / Feature flags

生产首批开放为 account-scoped cached fetch：

- method/path/query 可按 allowlist 近似原样；
- Authorization 使用选中号池账号；
- User-Agent/beta/persona 使用动态 persona resolver 输出；
- 动态 account/org/user path segment 不使用本机值，必须由 selected account metadata 重建；
- response 经过 schema allowlist 后返回给本机 CLI；
- 按账号 server-scoped opaque id / scoped keyed HMAC、CLI version、beta profile、path template、policy version 缓存；
- 401/403/risk text 立即熔断该账号该路径。

### 6.3 MCP servers / MCP registry

- registry 默认 public cached fetch，不带认证；
- MCP servers 可能包含账号级或用户级私有状态，默认 account/user 分区缓存；
- 不把用户 A 的 MCP 响应返回给用户 B；
- 若 response schema 中出现未允许的账号、邮箱、org、token、server credential 字段，quarantine。

### 6.4 Telemetry / Eval

这类不能 raw 上传。生产策略：

1. local guard 上传 safe intent 到 Sub2API；
2. Sub2API 记录账号级统计：classification、path_template、body_length_bucket、schema_summary、status、frequency；high-risk POST telemetry/eval 不得记录 raw body、raw query value、plain hash、deterministic body hash 或长期 HMAC。确需去重时，只能对安全字段组合 path_template + length_bucket + schema_summary + day + account scoped opaque id/HMAC 生成短期 scoped keyed HMAC，不得包含 raw body bytes 或原始字段值；
3. 默认本地返回 204；
4. 如以后证明上游需要活动信号，由 CC Gateway 生成 sanitized synthetic telemetry：
   - 不含 prompt；
   - 不含 raw body；
   - 不含本机路径；
   - 不含环境变量；
   - 不含本机 UUID/email/token；
   - 仅含账号级、版本级、路径模板级最小事件；
   - 有全局开关和账号级熔断。

raw CLI telemetry/eval body 永不进入 Sub2API、CC Gateway 或上游。

### 6.5 Unknown / Drift

- local guard 先本地 fail closed；
- 上传 safe intent 到 quarantine；
- 后台聚合 path template 和 header names；
- 人工/自动审查后才能进入 fixture replay；
- 不因单个用户新版本就全局放开。

## 7. Cache 与隔离

缓存 key 必须至少包含：

```text
cache_key_scoped_hmac(
  account_scoped_opaque_id_or_hmac,
  path_template,
  normalized_query_allowlist_summary_or_scoped_hmac,
  claude_code_version_family,
  beta_profile_scoped_hmac_or_opaque_id,
  cache_scope,
  policy_version,
  strategy_version,
  response_schema_version,
  user_partition_scoped_hmac_when_required,
  key_version
)
```

缓存 scope：

- `public`：公共 registry；
- `account`：bootstrap、feature flag、账号设置中已证明不含用户私有字段的响应；
- `org`：必须由 selected account org metadata 派生；
- `user-session`：只给同一用户/session 使用；
- `none`：不缓存。

`user_partition_scoped_hmac` 在 `cache_scope=user-session`、`cache_scope=org`，或 response 可能含私有状态时强制存在；缺失则禁止缓存并进入 quarantine。account scope 只允许已由 response schema 证明不含用户私有字段的响应。

禁止：

- account A cache 给 account B；
- user A private cache 给 user B；
- 401/403/risk text 后使用 stale fallback；
- 未经 schema allowlist 的 raw response 直接返回。

## 8. 熔断与回滚

每个控制面策略必须支持：

- path-level kill switch；
- account-level kill switch；
- beta/profile-level kill switch；
- version-family kill switch；
- upstream status code threshold；
- risk text detector；
- response schema drift detector；
- request volume/rate detector。

默认动作：

- GET cacheable path：仅在网络超时、5xx 或已明确标记为 stale-safe 的非安全事件下退回 stale-safe cache/stub；遇到 401/403/429、risk text、schema drift、账号冷却或熔断信号时不得 stale fallback，必须 quarantine 或熔断；
- POST telemetry/eval：退回 suppress；
- high-risk path：quarantine；
- messages：不因控制面失败 fallback 到 legacy/direct/sign-to-strip。

`stale-safe` 必须是 per-path 显式配置，包含 max_staleness_seconds、cache_scope、schema_version、response_schema_allowlist 和 private-field scan PASS。account settings、org/referral、MCP private/user-session 响应默认不得 stale fallback，除非单独批准。 stale 只能来自 same cache key 的 last-known-good 2xx schema-allowlisted response，并在响应/audit 中标记 `served_stale=true` 和 reason。

## 9. 实施分期

### B2-P0：控制面 intent 中心化

- local guard 对所有控制面生成 intent；
- Sub2API 新增 control-plane router；
- 所有 intent 入脱敏审计，不 raw 上传；
- unknown quarantine；
- 生产 session budget 不杀 CLI。

### B2-P1：首批安全 GET 上传

开放：

- bootstrap；
- account settings；
- feature flags；
- MCP registry public fetch。

要求：schema allowlist、账号身份替换、cache、熔断、localhost replay、单路径 canary。

### B2-P2：MCP servers 账号/用户隔离

- account/user 分区缓存；
- response schema allowlist；
- private 字段检测；
- 多用户隔离测试。

### B3：高风险控制面

- org/referral/eligibility 单独批准；
- telemetry synthetic 设计；
- eval 默认继续 suppress。

## 10. 测试与验收

### 单元测试

- route -> classification；
- raw credential/body forbidden；
- intent schema strict；
- cache key isolation；
- response schema allowlist；
- unknown quarantine；
- kill switch。

### 集成测试

- CLI -> guard -> Sub2API control-plane router；
- control-plane 不进入 messages route；
- selected account auth 替换；
- proxy bucket 一致；
- cache hit/miss；
- 401/403/429/risk text 熔断；
- 多用户同账号隔离。

### 真实 canary

每条控制面 path 单独批准：

1. localhost replay；
2. A/B diff；
3. 单账号、单路径、单请求真实 canary；
4. 不保存 server-side raw request/response/body/prompt/telemetry；真实 canary 也只能保留 scoped keyed HMAC（不得覆盖 telemetry/eval/raw prompt/raw body 原始字节）、长度 bucket、字段集合、schema 摘要、状态码、resolver/verifier 决策、熔断结果和 sensitive scan 结果；
5. safe deliverable 只含脱敏摘要；
6. 复盘通过后小流量灰度。

## 11. P0 / P1 / P2

### P0

- 本机 Claude token 上传服务器或上游；
- raw telemetry/eval body 上传；
- unknown path 自动真实上游；
- 控制面失败触发 messages fallback；
- 用户 A 私有控制面响应返回给用户 B；
- 账号 A 的控制面 cache 用于账号 B；
- raw response 未经 schema allowlist 返回。

### P1

- B2 首批 GET 未配置 TTL/熔断；
- dynamic persona 与控制面 cache key 未绑定；
- response schema 漂移只报警不熔断；
- safe report 缺少控制面上传决策摘要；
- kill switch 只能全局关闭，不能按 path/account/profile 关闭。

### P2

- telemetry synthetic 长期未上线导致上游活动信号偏弱；
- MCP registry cache 更新不及时；
- 部分 feature flag schema 仍需人工标注。

## 12. 结论

正式号池策略不是“继续 stub 一切”，而是：

```text
所有控制面 -> safe intent 上传到 Sub2API；
安全 GET/public 查询 -> 选中号池账号或公共缓存真实上传；
高风险 POST -> 不 raw 上传，改 synthetic/aggregate/suppress；
未知漂移 -> quarantine；
主消息 -> CC Gateway sign-primary，不受控制面失败污染。
```

这样既能最大化保持 Claude Code 真实能力，又能避免多用户、多账号号池中的 token、隐私、缓存和风控污染。
