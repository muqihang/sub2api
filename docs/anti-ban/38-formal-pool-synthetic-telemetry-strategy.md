# 正式号池 Synthetic Telemetry 策略设计

日期：2026-05-24  
状态：设计方案已审查；实施仍等待 B2 + Session Budget 基线完成后进入 B3/shadow-only
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`  
关联计划：

- `docs/anti-ban/30-claude-code-control-plane-classification-strategy.md`
- `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md`
- `docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md`
- `docs/anti-ban/37-formal-pool-control-plane-and-dynamic-persona-implementation-plan.md`
- `docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/field-audit-report.md`
- `docs/anti-ban/runtime-productization/2026-05-24-cli-through/README.md`

## 1. 结论

正式号池不能长期只 suppress telemetry。上游控制面可能把 telemetry 作为 Claude Code 活动、能力、诊断、功能开关和风控一致性的软信号；长期完全缺失可能让 messages 主链路虽然字段正确，但账号整体行为不像真实 Claude Code。

但 raw telemetry 也不能原样上传。raw telemetry 可能包含本机用户 token、路径、项目、prompt 片段、工具参数、错误栈、环境、账号/组织/session 标识；在多用户共用号池账号时，原样上传会造成隐私泄露和身份错位。

因此生产目标是：

```text
raw telemetry never upload；
all telemetry first becomes safe intent；
only synthetic telemetry may be uploaded；
synthetic telemetry must match real Claude Code schema/semantics/timing as closely as possible；
identity/session/account/persona must be rebuilt from selected pool account；
per-event-type localhost replay -> single-event canary -> gray rollout；
kill switch and quarantine remain default.
```

“无限接近真实”的工程含义是 schema/semantic/timing compatible，而不是复制用户本机 raw telemetry：

- 路径、method、header persona、beta/profile、User-Agent、x-app、Stainless 形态与真实 Claude Code 对齐；
- event batch 外层结构、event_type、event_data 字段集合、字段类型、时间/序列形态与真实 capture/fixture 对齐；
- 与真实 `/v1/messages` 请求的 model、tools、thinking、context_management、stream、status、duration、request lifecycle 对齐；
- 身份、session、device、account、org、环境由号池账号和 server-issued session 重建；
- 不含任何用户本机 raw body/prompt/telemetry/path/token/UUID/email。

## 2. 已有证据边界

### 2.1 已验证的 messages 能力

真实 CLI-through 已成功通过上游 `/v1/messages`：

- model：`claude-sonnet-4-6`；
- `max_tokens=32000`；
- tools=30；
- thinking present；
- context_management present；
- stream=true；
- `User-Agent=claude-cli/2.1.150 (external, sdk-cli)`；
- `anthropic-version=2023-06-01`；
- `x-app=cli`；
- Stainless headers present；
- verifier PASS；
- post-sign mutation PASS；
- fallback false。

这些字段是 synthetic telemetry 生成时的 messages lifecycle 基线，不能被 telemetry 策略削弱。

### 2.2 已观察到的控制面/telemetry 路径

脱敏材料确认 Claude Code CLI 会尝试控制面直连，包括：

- `POST /api/event_logging/v2/batch`；
- `POST /api/eval/*`；
- `GET /api/claude_cli/bootstrap?...`；
- `GET /v1/mcp_servers?...`；
- `GET /mcp-registry/*`；
- `GET /api/oauth/account/settings`；
- `GET /api/claude_code_*`；
- org/referral 类动态账号路径。

其中 telemetry/eval 是 high-risk POST。当前本地 guard / CC Gateway 策略默认 suppress，并已证明这不阻塞单次 `/v1/messages` 200；但正式号池长期运营不能把这个结论外推为“telemetry 永远不重要”。

### 2.3 当前代码线索

CC Gateway 旧 rewriter 中已有 event body rewrite 能力的雏形：

- event_data.device_id / email 可替换为 canonical identity；
- env 可替换为 canonical env；
- process metrics 可重写为合理范围；
- baseUrl/base_url/gateway 可删除；
- additional_metadata 可做有限脱敏。

但这不是生产 synthetic telemetry 策略。原因：

- 它仍以 raw event body 为输入；
- 不能保证 prompt/path/tool args/error stack 全部字段级安全；
- 不满足 safe intent / no raw body / no plain hash 的新边界；
- 正式策略必须从 safe event intent 和 messages safe summary 重建，而不是 raw body rewrite 后上传。

## 3. 非目标

1. 不把 raw telemetry 上传到 Sub2API、CC Gateway 或 Anthropic。  
2. 不把 telemetry 纳入 `/v1/messages` CCH 签名语义。  
3. 不用 synthetic telemetry 绕过上游账号限制或伪造不存在的模型能力。  
4. 不在本设计阶段执行真实 canary。  
5. 不承诺 100% byte-identical raw telemetry；追求的是 schema、语义、身份、时序、persona 的安全等价。

## 4. 核心安全不变量

1. **raw never upload**：raw telemetry/eval body 不出本机 guard，不进 Sub2API/CC Gateway/上游。  
2. **no raw digest**：telemetry/eval 不计算 raw body hash；禁止 plain SHA/MD5/长期 deterministic digest。  
3. **safe intent first**：所有 telemetry 先转为 safe intent / event intent，再由服务器策略决定 suppress、synthetic、quarantine 或 canary。  
4. **identity rebinding**：所有上游可见身份来自 selected pool account、server-issued session、dynamic persona resolver。  
5. **account/user/session isolation**：多用户共用单账号时，telemetry queue、sequence、budget、cache、audit 必须隔离。  
6. **CCH isolation**：control-plane telemetry 不生成、不复用、不校验 messages CCH。  
7. **default off**：synthetic telemetry 默认关闭；必须按 event_type/path/account/profile 灰度开启。  
8. **single-event canary**：每个 event family 第一次真实上传必须单独批准、单账号、单事件、单请求、可熔断。  
9. **capability non-degrade**：telemetry 策略不得削弱 1m/tools/thinking/context_management/stream/max_tokens=32000。  
10. **future model safe gray**：future Sonnet/Opus 新模型不机械阻断，也不盲目信任；进入 candidate/gray/kill-switch 流程。

## 5. 总体架构

```text
Claude Code CLI telemetry/eval
  -> local guard
     - strip local Authorization/cookie/x-api-key
     - classify path
     - parse only schema-level safe facts
     - discard raw body immediately
     - emit telemetry safe intent
  -> Sub2API control-plane router
     - verify guard attestation
     - validate strict intent schema
     - bind selected pool account/session/persona
     - decide suppress / synthetic preview / enqueue / quarantine
  -> telemetry synthesizer
     - build sanitized Claude-Code-shaped event batch
     - bind account identity, server session, dynamic persona
     - enforce event schema registry and rate/budget
  -> optional CC Gateway control-plane adapter
     - normalize headers/persona only
     - no messages CCH
  -> upstream single-event canary or gray upload
```

## 6. 数据模型

### 6.1 Local telemetry safe intent

Local guard 对 telemetry/eval POST 只能产生如下安全字段：

```json
{
  "schema_version": 1,
  "kind": "telemetry_intent",
  "method": "POST",
  "path_template": "/api/event_logging/v2/batch",
  "classification": "telemetry_event_batch",
  "header_names": ["authorization", "anthropic-beta", "user-agent", "x-app"],
  "auth_shape": {"authorization": "Bearer"},
  "body_length_bucket": "128kb_256kb",
  "body_omitted_reason": "raw_telemetry_forbidden",
  "schema_summary": {
    "registry_known_top_level_keys": ["events"],
    "events_count_bucket": "10_50",
    "registry_known_event_type_set": ["ClaudeCodeInternalEvent"],
    "registry_known_event_data_keys": ["event_name", "env", "process", "device_id"],
    "unknown_field_count": 0,
    "unknown_field_categories": [],
    "contains_base64_metadata": true,
    "contains_error_like_fields": false,
    "contains_path_like_fields": false,
    "contains_prompt_like_fields": false
  },
  "cli_observation": {
    "version_family": "2.1.x",
    "entrypoint": "sdk-cli",
    "user_agent_family": "claude-cli",
    "beta_profile_ref": "opaque-or-scoped-hmac"
  },
  "session_ref": "server-scoped-opaque-id",
  "policy_version": 1,
  "strategy_version": 1,
  "redaction_proof": {
    "raw_body_persisted": false,
    "raw_body_digest_computed": false,
    "auth_values_persisted": false
  }
}
```

禁止字段：

- raw event body；
- raw prompt；
- raw path；
- raw stack；
- raw tool args；
- raw account/org/user/session UUID；
- email；
- Authorization/x-api-key/cookie 值；
- query_hash/body_hash/plain sha；
- raw CCH。

`schema_summary` 只能包含 registry-known key enum。若 raw telemetry 出现未知字段名、动态字段名、路径片段、工具名、项目名、错误字段名或任意不在 registry 的 key，不得上传原 key 字符串；只能记录 `unknown_field_count`、`unknown_field_categories`、`quarantine_reason`。unknown nested key 同样适用。

### 6.2 Server synthetic telemetry event

Server 只从安全输入生成 synthetic event：

输入来源：

- selected pool account identity；
- server-issued session；
- dynamic persona resolver；
- safe `/v1/messages` lifecycle summary；
- telemetry safe intent schema_summary；
- policy registry。

示例结构，字段名仅作为设计目标，实际需由 schema registry / captured fixtures 决定：

```json
{
  "events": [
    {
      "event_type": "ClaudeCodeInternalEvent",
      "event_data": {
        "event_name": "message_request_completed",
        "timestamp_ms": 1770000000000,
        "sequence": 42,
        "device_id": "<selected_pool_account_device_id>",
        "session_id": "<server_issued_session_id>",
        "entrypoint": "sdk-cli",
        "cli_version": "2.1.150",
        "version_base": "2.1.150",
        "model": "claude-sonnet-4-6",
        "route_template": "/v1/messages",
        "status_bucket": "2xx",
        "duration_bucket": "1000_3000ms",
        "stream": true,
        "max_tokens_bucket": "32000",
        "tools_count_bucket": "30_40",
        "thinking_enabled": true,
        "context_management_enabled": true,
        "context_1m_profile": true,
        "env": "<canonical_env_object>",
        "process": "<canonical_process_metrics_or_omitted>"
      }
    }
  ]
}
```

注意：

- 不包含 prompt、messages、tool schema、tool args、file path、working directory raw value；
- 不包含本机 email/account UUID/session；
- `email` 默认不上传。synthetic telemetry 永不包含 email 或 account UUID。若未来证明上游强依赖，必须另起 P0 exception 设计，单账号人工批准、单事件 canary，并且不得进入默认生产策略；
- `device_id` 只能来自 selected pool account identity；
- `session_id` 必须与 messages final header / metadata 策略一致。

## 7. Event taxonomy

### 7.1 允许生成的首批 synthetic events

首批只允许与已验证 messages lifecycle 直接相关、可由安全摘要证明的事件：

| Synthetic event | 触发来源 | 必要字段 | 禁止字段 | 默认阶段 |
| --- | --- | --- | --- | --- |
| `message_request_started` | guard slot claimed / Sub2API forward start | model, route, session, persona, stream, tools_count_bucket | prompt/body/tool args | localhost preview |
| `message_request_completed` | upstream final status | status_bucket, duration_bucket, model, tools_count_bucket, thinking/context flags | raw response/body | first canary candidate |
| `message_stream_completed` | stream terminal event | stream=true, duration_bucket, status_bucket | SSE raw payload | localhost preview |
| `control_plane_suppressed` | telemetry/eval suppress | path_template, body_length_bucket, reason | raw telemetry | internal audit only |
| `guard_policy_quarantine` | unknown control-plane | path_template, reason | raw path dynamic id | internal audit only |

### 7.2 暂不生成的 high-risk events

以下事件必须等待更完整 schema capture / fixture review：

- tool execution detail；
- file edit detail；
- shell command detail；
- error stack detail；
- MCP server private configuration；
- eval payload；
- project/git/workspace detailed telemetry；
- user/account/org referral/eligibility details。

这些事件即使 raw telemetry 中出现，也只能进入 safe intent summary，不得 synthetic 上传。

## 8. Field policy matrix

| 字段类别 | 处理策略 | 说明 |
| --- | --- | --- |
| CLI version / version_base | registry / resolver 输出 | 可信 CLI-through exact / minor drift 可继承 |
| User-Agent / x-app / Stainless | dynamic persona resolver 输出 | 不信任外部 spoof |
| model | 来自 accepted messages summary | known models include `claude-sonnet-4-6`, `claude-opus-4-7`, `claude-opus-4-7-thinking`, `claude-opus-4-6`, `claude-opus-4-6-thinking`; future Sonnet/Opus 走 candidate gray |
| max_tokens | bucket or exact allowlisted value | 32000 不削弱 |
| tools_count | bucket；可 exact count | 不上传 tool schemas |
| thinking/context_management/stream | boolean | 不上传 thinking 内容 |
| status | bucket: 2xx/4xx/5xx/risk | 不上传 raw response body |
| duration | bucket + jitter | 防止精确关联 |
| timestamp | server time + jitter policy | 不用本机 raw timestamp |
| sequence | per account/session monotonic | 防跨用户混乱 |
| device_id | selected pool account canonical | 不用本机 device id |
| email / account UUID | forbidden | synthetic telemetry 默认永不包含；如上游强依赖，另起 P0 exception 设计和单账号人工批准 |
| session_id | server-issued UUID-like | 与 final messages policy 一致 |
| env | canonical env | 不含真实 local path/env |
| process | canonical range / omitted | 不保留原始 metrics 精确值 |
| prompt/messages/body/tool args | forbidden | 永不上传 |
| local path/project/git remote | forbidden or coarse enum | 禁止 raw |
| error stack | forbidden; optional error_class enum | 不上传 stack/message |
| raw query/path dynamic id | forbidden | 用 path_template / selected account rebuild |

## 9. Timing / batching / queue strategy

Synthetic telemetry 的“像真实”不只在字段，还在节奏。

### 9.1 Queue scope

必须按以下维度隔离：

- selected pool account；
- server-issued session；
- user partition；
- Claude Code version family；
- model family；
- egress bucket。

### 9.1.1 Atomic lease / dedupe / TTL

Queue implementation must use atomic lease per `(account_ref, user_partition, server_session, event_family)`. Required fields:

- `lease_id` server-generated opaque id；
- `lease_expires_at` short TTL；
- `dedupe_ref` scoped keyed HMAC over safe fields only: account_ref + user_partition + server_session + event_family + sequence + schema_version；
- `sequence` monotonic per server session；
- `user_partition` required even when multiple users share one pool account；
- expired leases are dropped or requeued only within same partition；
- concurrent reorder must not move user A events into user B session/account queue.

### 9.2 Batch rules

默认规则：

- synthetic telemetry 默认关闭；
- localhost preview 只生成但不上传；
- canary 只允许一个 batch、一个 event family；
- gray 阶段每 account/session 有 rate limit；
- batch size、flush interval、jitter 都由 policy 配置；
- 失败不重试到 messages；
- 429/403/risk/schema drift 熔断该 path/account/profile。

### 9.3 Sequence consistency

每个 server session 维护 monotonic event sequence：

```text
session_start? -> message_request_started -> message_request_completed -> optional stream_completed -> control_plane_summary
```

若 CLI 产生多条 messages，sequence 必须与 session budget ledger 对齐。不能让用户 A 的事件插入用户 B 的账号/session 序列。

## 10. Header / route policy

Synthetic telemetry 上传若开启，必须使用 control-plane route policy，不走 messages route。

### 10.1 允许路径

首批候选：

- `POST /api/event_logging/v2/batch`

Legacy 路径 `/api/event_logging/batch` 只用于兼容 fixture，不应作为新生产上传首选。`POST /api/eval/*` 默认仍 suppress。

### 10.2 Headers

上游 headers 由 CC Gateway control-plane adapter / persona resolver 生成：

- Authorization：selected pool account OAuth Bearer；
- User-Agent：dynamic persona resolver；
- anthropic-beta：event path 对应 allowlisted beta set；
- x-app：cli；
- x-service-name：仅在真实 capture/fixture 证明需要且 allowlisted 时加入；
- Stainless：按 path-specific schema；
- 不允许本机 Authorization/cookie/x-api-key/proxy header。

Authorization/proxy/header values are send-time only. They must not be stored in audit logs, mock evidence, safe deliverables, queue payloads, cache entries, fixtures, or test snapshots. Safe outputs may contain header names and auth shape only.

### 10.3 CCH boundary

Telemetry 不生成 billing block，不生成 CCH，不调用 messages signer，不调用 messages verifier。若请求体或 header 出现 CCH/billing marker：

- 外部输入：reject；
- 内部迁移/compat fixture：strip + audit；
- 绝不 sign-to-strip fallback。

## 11. Schema registry

必须建立 telemetry schema registry，来源只能是脱敏 capture / synthetic fixture / reviewed schema，不包含 raw body。

每个 schema 条目包含：

```yaml
schema_version: 1
path_template: /api/event_logging/v2/batch
method: POST
event_family: message_lifecycle
allowed_event_types:
  - ClaudeCodeInternalEvent
allowed_event_names:
  - message_request_started
  - message_request_completed
required_fields:
  - event_type
  - event_data.event_name
  - event_data.timestamp_ms
  - event_data.device_id
  - event_data.session_id
  - event_data.cli_version
field_policies:
  event_data.device_id: selected_pool_account_identity
  event_data.session_id: server_issued_session
  event_data.model: messages_summary_allowlist
  event_data.tools_count_bucket: bucket
  event_data.prompt: forbidden
  event_data.messages: forbidden
  event_data.error_stack: forbidden
response_policy:
  expected_status: [200, 202, 204]
  body_policy: empty_or_schema_allowlist
kill_switch:
  path: true
  account: true
  profile: true
  event_family: true
```

Nested field policy requirements:

- every nested path must be explicit allowlist；
- wildcard field pass-through is forbidden；
- base64/additional_metadata must be decoded only in memory, scanned, converted to allowlisted schema summary, then discarded；
- unknown nested key values and names must not be emitted；
- response header allowlist records names only by default; values require per-header policy；
- response body policy defaults to empty/omitted/schema_summary, never raw body；
- scanner must cover generated payload, queue payload, cache, audit, mock evidence, fixtures, and snapshots.

Unknown event_name / unknown field 默认 quarantine，不自动 pass-through。

## 12. Runtime modes

| Mode | 行为 | 真实上游 | 用途 |
| --- | --- | --- | --- |
| `telemetry_off` | suppress 204 | 否 | 当前默认 |
| `telemetry_shadow_generate` | 只生成安全本地 artifact，不执行 HTTP | 否 | schema/shape 预览 |
| `telemetry_localhost_replay` | 发到 localhost mock HTTP server | 否 | header/body/queue 验证 |
| `telemetry_single_event_canary` | 单账号单事件单 batch | 是，需单独批准 | 首次真实验证 |
| `telemetry_gray` | 小流量灰度 | 是 | 生产试运行 |
| `telemetry_production` | 按账号/session budget 正常上传 | 是 | 最终态 |

任何真实模式必须要求：

- explicit env switch；
- selected account scope check；
- proxy/egress bucket check；
- per-path kill switch；
- sensitive scan；
- no retry unless explicitly configured for telemetry path and bounded；
- failure must not affect messages fallback。

## 13. Response handling

Telemetry 上游响应不得直接影响 messages 主链路。

处理规则：

- 2xx：记录 safe status，更新 path/account health；
- 400：schema mismatch，quarantine event family；
- 401/403：账号/path/profile 熔断，禁止 stale/synthetic retry；
- 429：telemetry path cooling，不影响 messages account unless policy says severe account risk；
- 5xx/timeout：telemetry queue backoff/drop，不重试 messages；
- risk/KYC/unusual activity text：全局停止该账号 telemetry，进入人工复盘。

Response body 只允许 schema summary / status / header names；不保存 raw response body、header values、set-cookie、account id、request id 原值。

## 14. Verification plan

### 14.1 Local fixture tests

- raw telemetry body rejected at Sub2API boundary；
- telemetry safe intent accepts only allowlisted fields；
- raw body digest/hash forbidden；
- schema_summary contains no raw values and no unknown raw key names；
- prompt/path/token/email/account UUID scanner PASS；
- unknown event_name quarantine；
- unknown event_data field quarantine；
- event containing CCH/billing marker reject；
- untrusted spoofed persona rejected。

### 14.2 Synthetic builder tests

- message_request_completed generated from messages safe summary；
- generated body contains selected account device/session, not local device/session；
- no prompt/messages/tool args/tool schemas；
- env canonicalization; process metrics in configured ranges；
- status/duration/max_tokens/tools_count bucket policy；
- sequence monotonic per session；
- user A/B isolation on same pool account；
- Opus 4.7/4.6 and future Sonnet/Opus candidate do not lose capability flags。

### 14.3 Queue/rate tests

- per account/session/user_partition queue isolation；
- atomic lease, lease TTL, dedupe_ref, and concurrent reorder safety；
- batch size limit；
- jitter present but bounded；
- 429 cooldown；
- kill switch by path/account/profile/event_family；
- no fallback into messages/direct/legacy/sign-to-strip。

### 14.4 Localhost-only full chain

```text
synthetic telemetry builder
 -> Sub2API control-plane router
 -> CC Gateway control-plane adapter
 -> localhost mock
```

Assert:

- route exactly `POST /api/event_logging/v2/batch`；
- Authorization presence/auth_shape/account_binding is asserted by the localhost mock at send-time only；mock evidence records only header name、auth shape、selected account ref category、policy decision，never Authorization/proxy/header values；
- User-Agent/beta/x-app/Stainless match resolver decision；
- event schema allowlist PASS；
- sensitive scan PASS across safe deliverable, audit logs, local artifacts, queue payload, cache, fixtures, test snapshots, and mock evidence；
- any mock evidence containing raw Authorization/proxy/header value is a P0 failure；
- no real Anthropic/Claude network；
- messages route unaffected。

### 14.5 Single-event real canary prerequisites

Only after localhost replay PASS and separate user approval:

- one account；
- one event family；
- one batch；
- no retry；
- no raw request/response/body/header values capture；
- no raw response body/header value/cookie/account id capture；
- safe deliverable redacted；
- immediate stop on 400/401/403/429/risk/schema drift；
- post-canary offline review.

## 15. Implementation checkpoints

### T0：Design / schema freeze

- This document reviewed and approved；
- schema registry skeleton written；
- no code uploads real telemetry。

All T0-T3 work must use fake transport / forbidden-network tests. Any real Anthropic/Claude network attempt is a test failure.

### T1：Safe intent extension

- Add telemetry_intent strict schema；
- remove any body_hash/query_hash fallback；
- tests for raw body/digest forbidden。

### T2：Synthetic builder shadow mode

- Build event from messages safe summary；
- write only safe local artifact；
- no upstream。

### T3：Localhost replay mode

- Add CC Gateway control-plane adapter for telemetry headers/persona only；
- send to localhost mock；
- validate body/header shape。

### T4：Single-event canary plan

- Write execution plan only；
- no real request without separate approval。

### T5：Gray production policy

- queue/rate/jitter/kill switch；
- account/session budget；
- response health model。

## 16. P0 / P1 / P2

### P0

- raw telemetry/eval body reaches Sub2API/CC Gateway/upstream；
- raw body digest/plain SHA/MD5 computed；
- 本机 Authorization/cookie/x-api-key reaches server/upstream；
- synthetic telemetry includes prompt/messages/tool args/local path/account UUID/email；
- control-plane telemetry uses messages CCH signer；
- telemetry failure triggers messages fallback；
- user A telemetry appears under user B session/cache/queue；
- raw response body/header values/cookies/account ids are captured；
- future Sonnet/Opus mechanically blocked or capability downgraded。

### P1

- schema registry incomplete for event family；
- event timing too mechanical or globally synchronized；
- response schema drift only logs but does not quarantine；
- missing per-event kill switch；
- telemetry_gray lacks account/session budget；
- safe deliverable scanner does not cover artifacts/cache/snapshots。

### P2

- process metrics ranges not calibrated across Mac/Linux；
- exact event_name coverage incomplete；
- legacy `/api/event_logging/batch` compatibility not fully mapped；
- long-term telemetry usefulness not proven until single-event canary。

## 17. Open questions

1. Which exact event_names does Claude Code 2.1.150 emit for message lifecycle in raw telemetry? Existing safe material shows route/body class but not a finalized event_name allowlist. Need脱敏 schema extraction, not raw export.  
2. Does upstream accept empty/204 telemetry response as normal long-term, or only short-term? Single-event canary can answer later.  
3. Is `x-service-name` required for `event_logging/v2` under current CLI version? Must be confirmed by脱敏 capture/fixture.  
4. Should `email` ever be present? Default answer is no: email/account UUID are forbidden in default synthetic telemetry. Any exception requires separate P0 design and approval.  
5. How should Linux deployment persona affect env/process telemetry? Needs Mac/Linux parity fixture.

## 18. Recommendation

Do not block B2 control-plane / dynamic persona implementation on synthetic telemetry. Treat synthetic telemetry as B3/T-series module:

1. Finish safe intent center and dynamic persona first；
2. Implement telemetry schema registry and shadow generation；
3. Run localhost-only replay；
4. Only then request separate approval for one `POST /api/event_logging/v2/batch` single-event canary。

This gives us the product value we want:上游看到足够正常、稳定、账号一致的 Claude Code control-plane 活动；同时不泄露用户本机隐私，不污染多用户号池，不削弱 Claude Code 能力。
