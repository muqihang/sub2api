# Claude Code 控制面请求分类接管策略设计

日期：2026-05-23
状态：设计稿，已根据两轮 GPT-5.5 xhigh 审查意见修订，待用户确认
适用范围：本机 Claude Code CLI-through-Sub2API 验证链路，以及后续正式号池的控制面安全策略雏形
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

## 1. 背景与问题

本轮 localhost-only 验证确认：即使将 Claude Code CLI 的 base URL 指向本地 forwarder，CLI 仍会尝试直连 `api.anthropic.com:443`。这些请求不是主消息请求 `/v1/messages`，而是控制面、遥测、MCP、设置、bootstrap、registry 等请求。

已脱敏观察到的类型包括：

- `GET /api/claude_cli/bootstrap?entrypoint=sdk-cli&model=...`
- `GET /v1/mcp_servers?limit=...`
- `GET /api/claude_code_penguin_mode`
- `GET /api/claude_code_grove`
- `GET /api/oauth/account/settings`
- `GET /mcp-registry/v0/servers?...`
- `GET /api/oauth/organizations/<redacted>/referral/eligibility?...`
- `POST /api/event_logging/v2/batch`
- `POST /api/eval/<redacted>`
- 非 Anthropic telemetry CONNECT，例如 Datadog intake

这些请求中有一部分带本机 Claude Code `Authorization: Bearer`，且遥测请求体可能很大。因此，单靠 `ANTHROPIC_BASE_URL` / `CLAUDE_CODE_API_BASE_URL` 不足以保证“只发唯一一条 `/v1/messages`”的安全边界。

## 2. 设计目标

### 2.1 canary / 验证阶段目标

本小节只适用于 B1、localhost-only、mock-only 和单条 canary 验证阶段；不代表正式号池的长期控制面上传策略。

1. 只允许主链路 `POST /v1/messages?beta=true` 进入 Sub2API。
2. 所有控制面请求必须在本机被分类处理，不得带本机 Claude Code token 到服务器。
3. 不保存、不打印、不转发 raw token、Authorization 值、cookie、raw prompt、raw body、raw CCH、账号 UUID、email、proxy credential。
4. 真实 canary 阶段必须只允许第一条 `/v1/messages` 进入真实上游，第一条完成后立即停止 CLI 和临时 runtime。
5. 未知控制面请求必须 fail closed。
6. 分类策略必须可配置、可审计、可测试，不应散落在代码里成为隐式硬编码。

### 2.2 正式号池目标

正式号池不能简单照搬“第一条完成后杀 CLI”。正式号池需要逐步演化为会话级预算和熔断策略：

- 每个用户/session/账号的并发上限；
- 每个 session 的 messages 数量、工具轮次和成本预算；
- 控制面请求的分类策略；
- 账号级冷却和失败熔断；
- 禁止 direct fallback / legacy fallback / sign-to-strip fallback；
- 默认 fail closed，逐类显式放开。

本设计重点先覆盖 canary / localhost-only / 单条真实验证，保留正式号池扩展接口。

## 3. 非目标

1. B1/canary 阶段不伪造、重签、代发 Claude Code 控制面请求到 Anthropic。
2. 不将控制面请求纳入 `/v1/messages` 的 CCH 签名算法。CCH 只适用于最终 `/v1/messages` 请求体/计费相关材料，不适用于 bootstrap、MCP、settings、event logging 等控制面接口。
3. B1/canary 阶段不允许控制面真实上游访问；正式号池上传策略仅作为后续设计预留，必须另行验证和批准。
4. 不修改用户本机 `~/.claude` 登录状态。
5. 不解决正式号池全部会话调度问题；这里只设计可扩展策略边界。

## 4. 当前证据摘要

### 4.1 A/B localhost-only 结论

报告：`/tmp/cli-control-plane-ab-20260523/safe-deliverable/control-plane-guard-report.md`

- block 模式和分类 stub 模式都能让 CLI 产生 `/v1/messages`。
- 两种模式下 `/v1/messages` 高层 body shape 基本一致。
- CLI 可能一次 prompt 产生多条 `/v1/messages`。
- CLI 可能生成低成本首条消息，也可能生成 30 tools / 32k max_tokens 的大包络消息。
- 因此真实 canary 必须有 `max_messages=1` 和成本包络门禁。

### 4.2 全链路 localhost-only 结论

报告：`/tmp/full-chain-controller-20260523/safe-deliverable/full-chain-controller-report.md`

- `Claude Code CLI -> local guard -> Sub2API -> CC Gateway -> localhost mock` 已跑通。
- mock 只收到 1 条 `/v1/messages?beta=true`。
- 首条完成后执行控制器成功停止 CLI。
- 第二条 `/v1/messages` 被 `max_messages=1` 挡在本地。
- 控制面请求被 stub/suppress/block。
- sensitive scan PASS。

## 5. 控制面请求分类模型

本节的“默认策略”指 B1/canary 阶段默认策略。正式号池阶段不得机械套用“全部不上传”，应按第 11 章的上传策略逐路径审查。

建议将请求分为 7 类：

| 类别 | 示例 | 默认策略 | 是否可转发服务器 | 是否可真实上游 | 说明 |
|---|---|---|---|---|---|
| Main messages | `POST /v1/messages?beta=true` | canary 仅第一条 forward | 是，去 Sub2API | 仅单独批准后 | 主链路，进入 CC Gateway sign-primary |
| Telemetry | `POST /api/event_logging/v2/batch` | suppress 204 | 否 | 否 | 可能含大量事件和本机 token，不能代发 |
| Eval | `POST /api/eval/*` | suppress 204 | 否 | 否 | 风险接近 telemetry，路径可能含动态 id |
| MCP account list | `GET /v1/mcp_servers*` | stub empty list | 否 | 默认否 | 可影响工具/MCP，但先本地空列表验证 |
| MCP registry | `GET /mcp-registry/*` | stub empty list | 否 | 默认否 | 通常无 auth，但仍属于控制面 |
| Bootstrap/settings/feature flags | `GET /api/claude_cli/bootstrap*`, `GET /api/oauth/account/settings`, `GET /api/claude_code_*` | stub configured JSON 或 block | 否 | 默认否 | 可能影响模型、beta、工具、功能开关，是重点观察对象 |
| Unknown/direct CONNECT | 未分类路径、非允许域名 CONNECT | fail closed | 否 | 否 | 默认拒绝，记录脱敏摘要 |

## 6. 策略处理原则

### 6.1 主消息路径

只允许：

```text
POST /v1/messages?beta=true
```

处理流程：

1. 删除本机 Claude Code 的认证头：`Authorization`、`x-api-key`、`Cookie`、`Proxy-Authorization`。
2. 注入 Sub2API 入口认证。
3. 尽量保留真实 CLI 生成的安全必要字段：
   - `User-Agent`
   - `anthropic-beta`
   - `anthropic-version`
   - `X-Claude-Code-Session-Id`
   - `x-app`
   - `X-Stainless-*`
   - request body
4. canary 模式下执行：
   - `max_messages=1`
   - 第一条完成后 stop CLI
   - 成本包络门禁
   - no retry / no concurrency / no fallback
5. 正式号池模式下不杀 CLI，而由 session budget / account budget / route policy 控制。

### 6.2 Telemetry / Eval

默认：本地 suppress，返回 204。

原因：

- 带本机 token 风险高；
- body 可能很大；
- 不属于 `/v1/messages` 功能必需路径；
- 不应进入 Sub2API 或 CC Gateway；
- 不应代发到 Anthropic。

后续若要支持正式 telemetry，应单独设计匿名化/聚合/本地丢弃策略，不能转发原始事件。

### 6.3 MCP / Registry

默认：本地 stub 空结果。

建议响应形态：

- MCP servers：`{"data": []}` 或与 CLI 兼容的空列表结构；
- registry：`{"data": [], "servers": []}` 或与 CLI 兼容的空列表结构。

验证重点：

- stub 是否改变最终 `/v1/messages` beta；
- 是否改变 tools_count；
- 是否改变 body keys；
- 是否触发 CLI fallback 或报错；
- 是否减少/增加后续控制面请求。

### 6.4 Bootstrap / Settings / Feature Flags

默认：可配置 stub；证据不足时 block。

这是最敏感的一类，因为它可能影响：

- 模型选择；
- beta profile；
- output_config；
- tools 展开；
- subscription path 行为；
- UI/CLI 功能开关；
- 是否触发 MCP 或 telemetry。

建议分两阶段：

1. canary 阶段：使用最小安全 stub，并通过 localhost A/B 证明不会改变 `/v1/messages` 高层形态。
2. 正式阶段：如果确实需要控制面真实数据，必须改走第 11 章的 control-plane intent + 选中号池账号路径；不得把本机 token 传到服务器，也不得把 B1/canary stub 逻辑误当成永久策略。

### 6.5 Unknown

默认 fail closed，返回 403 或本地安全错误。

记录内容只允许脱敏摘要：

- method；
- path 模板化后的摘要；
- header names；
- auth shape，例如 `Authorization: Bearer`，不含值；
- declared content length；
- 分类 reason；
- 不记录 raw body。

## 7. P1 审查修订：必须澄清的安全边界

本节回应 GPT-5.5 xhigh 审查中的必改项，作为后续实现前的强制约束。

### 7.1 CONNECT 语义

配置中的 `allowed_stub_targets` 只表示“允许本机 guard 终止并本地处理这个 CONNECT 目标”，绝不表示建立真实隧道。

强制规则：

1. `api.anthropic.com:443` 在 canary / localhost-only 阶段只能进入本机 TLS stub 或 block。
2. guard 不得把 CONNECT 字节流透传到真实网络。
3. direct `/v1/messages` via CONNECT 必须 block，即使 path 合法也不能绕过 base-url forwarder。
4. unknown CONNECT target 一律 `block_403`，仅记录脱敏 target 摘要。
5. 如果 TLS stub 失败，必须 fail closed，不能降级为真实 tunnel。

### 7.2 raw body / raw prompt / raw capture 边界

需要区分“瞬时处理”和“持久化留档”：

| 组件 | 是否可瞬时读取 `/v1/messages` body | 是否可持久化 raw body/prompt | 说明 |
|---|---:|---:|---|
| local guard | 是，仅用于转发和脱敏摘要 | 否 | 只保存 body size、keys、schema 摘要和必要时的 scoped keyed HMAC，不保存 prompt 明文；禁止 plain deterministic hash |
| Sub2API | 是，仅用于解析、账号选择、转发 | 否 | 不保存本机 Claude token，不保存 raw prompt |
| CC Gateway localhost preflight | 是，用于 final-output/verifier | 否 | mock 阶段不需要 raw prompt 留档 |
| CC Gateway 真实 canary | 是，仅用于瞬时 final-output/signing/verifier/转发 | 否 | 真实 canary 调试也只允许保存长度 bucket、字段集合、schema 摘要、状态码、verifier/fallback 结果和必要时的 scoped keyed HMAC；不得 server-side 持久化 raw prompt/body；禁止 plain deterministic body hash |
| safe deliverable | 否 | 否 | 只允许字段存在性、长度 bucket、schema 摘要、必要时的 scoped keyed HMAC、计数、PASS/FAIL |

所有 `*_hash`、cache key hash、audit hash、beta/profile/session/account/user partition 摘要均必须是 server-scoped opaque id 或 scoped keyed HMAC；禁止 plain hash / SHA / MD5 / 长期 deterministic hash。HMAC key 必须按 environment、tenant/session、path_template、purpose、cache_scope、key_version/rotation period 分区；不得跨用户、跨账号、跨日期或跨 purpose 稳定关联。telemetry/eval/raw prompt/raw body 不得计算 body digest，必须使用 digest_omitted_reason + body_length_bucket + schema_summary。

因此，“不保存 raw body/prompt”适用于本机 guard、Sub2API、CC Gateway、服务器调试包和 safe deliverable。若需要排查字段形态，只能使用 synthetic fixture、脱敏字段集合、长度 bucket、shape/schema 摘要、必要时的 scoped keyed HMAC、status、verifier 结果；不得保存或输出 raw prompt/body。

### 7.3 配置 schema 必须严格校验

后续实现必须使用正式 schema，而不是任意 dict。最低要求：

- `schema_version` 必填，当前建议 `1`。
- `mode` 枚举：`localhost_preflight`、`canary_single_message`、`session_budgeted`、`block_all_real`。
- `action` 枚举：`forward_messages`、`suppress_204`、`stub_json`、`block_403`、`quarantine_block`。
- 未知字段：启动失败，而不是忽略。
- 无效 regex：启动失败。
- method 规范化为大写；path 必须以 `/` 开头；query 必须精确匹配规范化结果。
- 匹配优先级固定：`messages` > `control_plane` > `unknown`；同组内按配置顺序，但禁止两个规则同时匹配同一路径，除非显式 `priority`。
- stub 响应必须声明 `status`、`content_type`、`body`；默认不得含敏感字段。
- 所有配置加载结果必须输出脱敏 config fingerprint，不能输出 secret。

### 7.4 canary 单条请求的原子边界

`max_messages=1` 的计数点必须是“准备 forward 前原子占用名额”，不是上游成功后才计数。

状态机：

```text
idle -> slot_claimed -> forwarding -> response_completed -> stop_cli_requested -> stopped
                    \-> forward_failed -> stop_cli_requested -> stopped
```

强制规则：

1. 并发双请求只能有一个拿到 slot。
2. slot 一旦占用，无论上游返回 200、400、401、403、429、5xx、timeout、stream 中断，都不得释放给第二条真实请求。
3. 第一条完成或失败后都必须请求停止 CLI。
4. CLI 停止失败时必须 kill 临时 runtime，并在 safe report 标记 `cli_stop_failed`。
5. 禁止自动 retry；禁止 fallback；禁止第二条真实 upstream。
6. stream 响应以 guard 收到 upstream response 完成/错误为停止触发点；真实阶段不等待 CLI 自然退出。

### 7.5 成本包络扩展

canary 成本门禁不能只看 `max_tokens`、body bytes、tools_count。后续实现至少覆盖：

- `max_tokens`；
- raw body bytes；
- `messages[]` 数量；
- content block 数量；
- text content 总字节数；
- `system` 总字节数；
- tool definitions 数量；
- tool definitions 总字节数；
- tool_result / tool_use block 数量；
- `thinking` 是否存在及预算；
- `output_config` shape allowlist；
- stream true/false；
- 首包之后是否出现工具循环。

超限策略：本地 fail closed，返回本地错误，不 forward，不 retry，不降级 synthetic body。

注意：这些阈值是 canary 阶段自定义保护阈值，不是 Claude Code 官方定义。

### 7.6 Bootstrap / settings / MCP stub 的影响验证

每个控制面 stub 必须有 per-path 响应规范和 A/B diff 要求。

最低响应规范：

| 路径类别 | status | content-type | body |
|---|---:|---|---|
| telemetry/eval | 204 | 空 | 空 |
| MCP servers | 200 | application/json | 空 data/list 结构 |
| MCP registry | 200 | application/json | 空 data/list 结构 |
| bootstrap/settings/feature flags | 200 或 403，按策略 | application/json | 最小空对象或固定 fixture |
| unknown | 403 | 空或 application/json error | 不含敏感信息 |

A/B diff 强制字段：

- `/v1/messages` count；
- model；
- User-Agent；
- anthropic-beta；
- 是否包含 `context-1m`；
- body keys；
- body size；
- max_tokens；
- tools_count；
- output_config keys；
- thinking/context_management 是否存在；
- session UUID-like；
- 是否产生额外重试或错误。

如果 stub/block 改变上述关键字段，必须标 Unknown 或 P1，不得进入真实 canary。

### 7.7 必补负向测试

实现阶段必须补：

- B1/canary 下 raw control-plane HTTP request 绝不触达 Sub2API/CC Gateway 的断言；正式号池下 control-plane 只能以 safe intent envelope 进入 Sub2API control-plane router，不能进入 messages route，不能携带本机 token/raw body；
- CONNECT stub 不建立真实隧道；
- direct `/v1/messages` via CONNECT 被 block；
- query 参数顺序、额外参数、大小写、编码变体均 fail closed；
- invalid config 启动失败；
- 并发双 `/v1/messages` 只有一个 slot；
- 上游失败后 no retry/no fallback；
- stream 中断后停止 CLI；
- 日志、异常、临时文件、safe report 全量敏感扫描。

## 8. 可配置策略设计

建议新增一个独立配置文件，例如：

```yaml
schema_version: 1
mode: canary_single_message
summary_path: /tmp/claude-code-guard/summary.jsonl
redaction:
  store_raw_body: false
  store_auth_values: false
  redact_uuid: true
messages:
  allowed_routes:
    - method: POST
      path: /v1/messages
      query: beta=true
  max_messages: 1
  stop_cli_after_first_response: true
  cost_envelope:
    enabled: true
    max_body_bytes: 32768
    max_tokens: 2048
    max_tools: 3
    allow_stream: true
control_plane:
  defaults:
    # B1 只允许 disabled/stub/suppress/block；即使配置 future_upload，也不得真实上传。
    upload_strategy: disabled
    auth_source: none
    cache_scope: none
    body_policy: forbidden
    response_policy: sanitized_schema
    ttl_seconds: 0
    policy_version: 1
    strategy_version: 1
    response_schema_version: 1
    upload_kill_switch: true
  telemetry:
    match:
      - method: POST
        path: /api/event_logging/v2/batch
      - method: POST
        path_prefix: /api/eval/
    action: suppress_204
  mcp:
    match:
      - method: GET
        path: /v1/mcp_servers
      - method: GET
        path_prefix: /mcp-registry/
    action: stub_json
    response:
      data: []
      servers: []
  bootstrap_settings:
    match:
      - method: GET
        path: /api/claude_cli/bootstrap
      - method: GET
        path: /api/oauth/account/settings
      - method: GET
        path_prefix: /api/claude_code_
      - method: GET
        path_regex: '^/api/oauth/organizations/[^/]+/referral/eligibility$'
    action: stub_json
    response: {}
unknown:
  action: block_403
connect:
  allowed_stub_targets:
    - api.anthropic.com:443
  unknown_target_action: block_403
```

说明：

- canary 默认 `max_messages=1`。
- 正式号池可切到 `mode: session_budgeted`，并把 `stop_cli_after_first_response` 关掉。
- 成本阈值是 canary 阶段自定义保护值，不声称是 Claude Code 官方定义。
- 所有 unknown 默认拒绝。配置解析失败、未知字段、重复匹配、非法 action、非法 regex、非法 stub body 都必须启动失败或 fail closed。B1 schema 必须显式定义 `upload_strategy/cache_scope/auth_source/body_policy/response_policy` 等预留字段；否则这些字段也应被视为未知字段并启动失败。

## 9. 执行模式

### 9.1 `localhost_preflight`

用途：本地验证策略是否影响 `/v1/messages` 形态。

特征：

- upstream 只能是 localhost mock；
- 真实 Anthropic 域名禁止；
- 允许多条 messages 进入 mock 用于观察，或按测试目标限制为 1 条；
- 只保存脱敏摘要、长度、字段集合、schema 摘要和 scoped keyed HMAC；不保存 raw request/response/body/prompt/telemetry。

### 9.2 `canary_single_message`

用途：唯一一条真实 canary。

特征：

- 只允许第一条 `/v1/messages`；
- 第一条完成后立即停止 CLI；
- 第二条及以后 fail closed；
- 控制面请求本地分类接管；
- 无 retry、无并发、无工具循环；
- 不保存 raw prompt/body；仅保存长度 bucket、字段集合、schema 摘要、状态码、verifier/fallback 结果和必要时的 scoped keyed HMAC；禁止 plain deterministic body hash；
- safe deliverable 只输出脱敏摘要。

### 9.3 `session_budgeted`

用途：正式号池预留。

特征：

- 不再第一条后杀 CLI；
- 按用户/session/账号维度施加预算；
- 预算项包括 messages 数、并发、工具轮次、body size、max_tokens、失败率、429 冷却；
- 控制面策略仍默认本地接管或明确配置；
- 未知请求 fail closed。

## 10. 与 Sub2API / CC Gateway 的职责边界

### 10.1 Local guard / redacting forwarder

负责：

- 接收真实 CLI 请求；
- 删除本机 Claude Code 认证材料；
- 控制面分类；
- 主消息路径 hard gate；
- 本机 TLS CONNECT stub/block；
- 本地脱敏摘要；
- canary 阶段停止 CLI。

不负责：

- 选择 Sub2API 账号；
- CCH 签名；
- 代理出口选择；
- OAuth refresh；
- 真实上游请求。

### 10.2 Sub2API

负责：

- 入口认证；
- 账号选择；
- canary-only 隔离；
- scope gate；
- proxy fail-closed；
- `/v1/messages` 路由限制；
- 转给 CC Gateway。

不负责：

- 接收本机 Claude Code token；
- 接收 raw 本机控制面 HTTP 请求；
- 在 B1/canary 阶段处理本机控制面真实上传；

正式号池扩展职责：

- 只接收 safe control-plane intent envelope；
- 通过独立 control-plane router 做账号选择、缓存、quarantine 和上传决策；
- 不让 control-plane intent 进入 `/v1/messages` 主链路；
- 不接收 raw body/raw telemetry/raw prompt。

### 10.3 CC Gateway

负责：

- final-output；
- persona/header/body/metadata/beta 处理；
- billing block；
- CCH sign-primary；
- verifier；
- post-sign mutation check；
- no fallback；
- 脱敏 capture：server-scoped opaque id / scoped keyed HMAC、长度 bucket、字段集合、schema 摘要、状态码、verifier/fallback 结果；不得持久化 raw prompt/body。

不负责：

- 本机 CLI 控制面分类；
- 在 B1/canary 阶段代发非 `/v1/messages` 控制面；
- 用 `/v1/messages` CCH 语义处理控制面；
- 伪造 raw 控制面响应。

正式号池扩展职责：

- 可通过独立 control-plane adapter 做 header/persona/version 规范化；
- 可生成 synthetic telemetry 的账号级最小事件；
- 不复用 `/v1/messages` CCH signing；
- control-plane 失败不得 fallback 到 messages/direct/legacy/sign-to-strip。


## 11. 正式号池控制面上传策略

本章已从 canary/B1 的“分类接管预留”升级为生产化方向。详细设计见：

- `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md`
- `docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md`

核心修订：正式号池不能长期把控制面全部 stub/suppress/block；也不能把用户本机 Claude Code 控制面 raw 请求直接转发到上游。正式策略是：

```text
所有控制面请求 -> local guard 生成 normalized intent envelope -> Sub2API 中心化策略决策；
安全 GET / public 查询 -> 使用选中号池账号或公共缓存真实上传；
高风险 POST telemetry/eval -> 禁止 raw 上传，改 synthetic/aggregate/suppress；
未知路径/字段漂移 -> quarantine；
主消息 /v1/messages -> 继续 CC Gateway sign-primary，不被控制面失败污染。
```

### 11.1 生产安全原则

1. **本机凭证永不上传**：用户本机 Claude Code 的 `Authorization`、`x-api-key`、cookie、proxy credential 不得进入 Sub2API、CC Gateway 或上游。
2. **所有控制面都上传 safe intent**：正式号池下，“全部控制面上传”指全部上传脱敏 intent 进入中心策略；不代表全部 raw 上游转发。
3. **上游身份使用号池账号**：需要真实上游的控制面必须使用 Sub2API 选中的号池账号、proxy bucket、scope、冷却和熔断状态。
4. **账号/用户隔离**：账号级、组织级、用户会话级 cache 必须分层；一个用户的私有控制面状态不得污染另一个用户。
5. **高风险 POST 不 raw 上传**：event_logging/eval/raw telemetry body 永不进入 Sub2API/CC Gateway/Anthropic；如未来需要，只上传合成的账号级最小事件。
6. **可熔断/可回滚**：按账号、路径、version、beta profile、response schema、状态码和风险文本熔断。
7. **动态 persona 参与缓存 key**：control-plane cache key 必须包含 Claude Code version family 与 beta profile scoped HMAC / opaque id，避免不同版本响应互相污染。
8. **Sonnet 4.6 1m 不限制**：生产 profile 使用 1m-enabled profile，不因产品化配置削掉 `context-1m-2025-08-07`、thinking、tools、context_management。

### 11.2 两段式上传模型

第一段：local guard 对每个控制面请求生成 normalized intent envelope，并上传到 Sub2API control-plane router。intent 只包含 method、path_template、allowlisted query 摘要或 scoped keyed HMAC、header_names、auth_shape、body_length_bucket、scoped keyed HMAC（仅必要时，且不得覆盖 telemetry/eval/raw prompt/raw body 原始字节）、body_schema_summary、cli_version、beta_profile_scoped_hmac_or_opaque_id、session_scoped_hmac_or_opaque_id、classification、policy_version 和 redaction_proof。

第二段：Sub2API 根据策略决定真实上游、cache、synthetic、suppress 或 quarantine。凡真实上游必须使用选中号池账号身份，必要时经 CC Gateway control-plane adapter 规范化 header/persona/version。

### 11.3 生产目标矩阵

| 类别 | 生产目标 | 是否 raw 上传 |
|---|---|---:|
| `/v1/messages` | session budget 下进入 CC Gateway sign-primary | messages body 瞬时处理，不落盘 |
| bootstrap/settings/feature flags | account-scoped cached fetch/passthrough | 只允许 GET/query/header 低风险字段 |
| MCP registry | public cached fetch | public raw allowlist |
| MCP servers | account/user 隔离 cached fetch | schema allowlist 后返回 |
| org/referral/eligibility | 高风险 account-scoped fetch，单独批准 | 否，动态 org/account 必须重建 |
| event_logging/eval | synthetic/aggregate 或 suppress | 永不 raw 上传 |
| unknown | quarantine | 否 |

### 11.4 与动态 persona 的关系

控制面真实上传不得继续硬编码 2.1.150。动态 persona resolver 必须安全读取可信 CLI-through 的 `User-Agent`、`anthropic-beta`、session、`x-app`、`X-Stainless-*` 和 body shape：

- exact known profile：用 registry；
- trusted same-minor drift：兼容传 + 审计；
- unknown larger drift：隔离灰度；
- spoof/矛盾字段：fail closed；
- 1m/tools/thinking/context_management 不因 profile 产品化被削掉。

### 11.5 分阶段落地

1. **B2-P0**：所有控制面 intent 中心化上传到 Sub2API，仍不 raw 上游。
2. **B2-P1**：bootstrap/settings/feature flags/MCP registry 首批安全 GET 上传，带 cache/schema/熔断。
3. **B2-P2**：MCP servers account/user 隔离缓存。
4. **B3**：org/referral 高风险路径和 telemetry synthetic，单独批准。

任何路径从 stub/suppress 进入真实上游前，必须经过 localhost replay、A/B diff、单路径单账号 canary、脱敏复盘、熔断验证和多用户隔离测试。

## 12. 安全不变量

任何模式下必须保持：

1. 本机 Claude Code token 不保存、不打印、不转发服务器。
2. raw Authorization / x-api-key / cookie 不进入日志。
3. raw body / raw prompt 不进入 safe deliverable。
4. unknown route fail closed。
5. direct Anthropic/Claude 直连不允许绕过 guard。
6. raw CLI event_logging/eval 不进 Sub2API，不进 CC Gateway，不进真实上游；未来 synthetic account-level telemetry 必须是单独模型、默认关闭、单独批准。
7. canary 阶段只允许一条真实 `/v1/messages`。
8. CC Gateway 仍保持 no direct fallback / no legacy fallback / no sign-to-strip fallback。
9. 所有策略变更必须有 localhost-only preflight 和 sensitive scan。

## 13. 测试矩阵

### 13.1 单元测试

- route classifier：所有已知路径分类正确。
- telemetry/eval：返回 204，不 forward。
- MCP/registry：返回 stub JSON，不 forward。
- bootstrap/settings/feature flags：按配置 stub 或 block。
- unknown route：403。
- direct `/v1/messages` via CONNECT：block。
- redaction：Authorization 值、x-api-key 值、cookie 值、UUID、raw body 不落盘。
- `max_messages=1`：第二条消息不进 upstream。
- `canary_single_message`：第一条完成后触发 CLI stop。
- `session_budgeted`：不杀 CLI，但预算超限 fail closed。

### 13.2 localhost-only 集成测试

- CLI -> guard -> Sub2API -> CC Gateway -> localhost mock。
- mock request count 符合模式预期。
- final route 为 `/v1/messages?beta=true`。
- final model 为 `claude-sonnet-4-6`。
- User-Agent / beta / session 保持真实 CLI 形态。
- 包含并保留 `context-1m-2025-08-07`，确保 Sonnet 4.6 1m 上下文不被产品化配置限制。
- verifier PASS。
- post-sign mutation PASS。
- fallback false。
- raw/safe deliverable sensitive scan PASS。

### 13.3 A/B 影响测试

对比：

1. 控制面全部 block。
2. 控制面分类 stub/suppress。
3. 若未来批准，正式号池 control-plane intent 经 Sub2API 选中账号上传；不使用本机直连和本机 token。

对比字段：

- messages count；
- model；
- beta；
- body keys；
- body size；
- max_tokens；
- tools_count；
- output_config keys；
- session UUID-like；
- 是否触发额外错误或重试。

## 14. P0 / P1 / P2 / Unknown

### P0

- 本机 Claude Code token 被转发到服务器或写入日志。
- 控制面请求真实上游访问未获批准。
- raw CLI event_logging/eval 进入 Sub2API 或 CC Gateway。
- unknown route 默认放行。
- canary 阶段第二条 `/v1/messages` 进入真实 upstream。
- direct fallback / legacy fallback / sign-to-strip fallback 出现。

### P1

- 控制面策略硬编码、不可配置。
- bootstrap/settings stub 对 `/v1/messages` 形态影响未持续测试。
- `max_tokens=32000`、1m context、tools/thinking 等富能力缺少 session/account 预算和熔断。
- safe report 未覆盖控制面分类计数。
- B1/canary 规则和正式号池上传规则作用域不清，导致误全拦或误真实上传。
- control-plane intent envelope 缺字段白名单。
- account/org/user 类响应默认 raw-to-client。
- 正式号池缺少 session budget 模式设计。

### P2

- stub JSON 与真实控制面响应形态不完全一致，但当前不影响 `/v1/messages`。
- ResourceWarning 类测试清洁度问题。
- 更多 Claude Code 版本 profile 的自动发现能力。

### Unknown

- 真实 bootstrap/settings 返回内容是否会影响 subscription path 的细节。
- 长期完全 suppress telemetry 是否会影响 CLI 后续行为。
- MCP 空列表对用户复杂工具场景的实际影响。
- Claude Code 后续版本是否新增控制面路径。

## 15. 实施建议

建议分三步实施：

### 15.1 阶段 B1：配置化分类策略

- 将当前 `classify_request()` 迁移为数据驱动策略。
- 支持 YAML/JSON 配置。
- 保留当前默认策略作为内置 safe profile。
- 增加单元测试覆盖每类 action。

### 15.2 阶段 B2：策略影响观测

- localhost-only A/B 自动化脚本纳入 repo 工具目录。
- 输出统一 safe deliverable。
- 每次 Claude Code 版本变化时重新跑 A/B。

### 15.3 阶段 B3：正式号池预留

- 新增 `session_budgeted` 模式配置结构。
- 先只实现 fail-closed 预算骨架，不扩大真实能力。
- 与账号调度、冷却、并发控制对接。

## 16. 下一次真实 CLI-through canary 前置条件

必须全部满足：

1. control-plane guard 使用配置化策略或当前策略已冻结审查。
2. `canary_single_message` controller 通过 localhost-only。
3. `max_messages=1` 通过 localhost-only。
4. 成本包络门禁明确处理 CLI 默认 `max_tokens=32000`，并覆盖 body/messages/content blocks/tools/system/tool_result/thinking/output_config。
5. 控制面 stub/suppress 不改变目标 `/v1/messages` 高层形态，或差异已解释并接受。
6. Sub2API 账号、scope、proxy、canary-only gate 全部通过。
7. CC Gateway sign-primary、verifier、post-sign mutation、no fallback 全部通过。
8. 用户单独明确批准唯一一条真实请求。

## 17. 结论

控制面请求不是 CCH 的一部分，也不应由 CC Gateway 用 `/v1/messages` 的 CCH 语义重新计算后代发。正确方向是：

```text
主消息请求：进入 Sub2API + CC Gateway + sign-primary
控制面请求：先本机分类接管；canary 阶段 stub/suppress/block；正式号池阶段按账号身份、缓存、脱敏、策略分级上传
未知请求：fail closed + quarantine
canary：第一条完成后立即停止 CLI
正式号池：演进为 session budget + control-plane upload policy
```

该策略既能保护本机 Claude Code token，又能维持真实 CLI 生成 `/v1/messages` 的字段形态；同时为正式号池保留“安全控制面上传”的路径，避免长期全拦导致形态偏离，也避免 raw 控制面上传造成多用户、多账号污染。
