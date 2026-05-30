# 正式号池新账号上线硬门禁

## 1. 目标

新账号不能再依赖人工判断直接进入生产调度。正式号池账号必须经过状态机：导入、刷新、运行时注册、定向健康检查、预热、生产。任何身份、代理、签名、fallback、401/403 或风险文本异常都会让账号自动隔离，并写入脱敏风险事件。

本策略只控制“账号什么时候可以参与调度”和“账号初始权重”。它不修改用户请求体，不降低 Claude Code 能力，不限制 1m context、tools、thinking、stream、Opus/Sonnet 或 `max_tokens=32000`。

## 2. 状态机

| 阶段 | 进入条件 | 可调度 | 说明 |
| --- | --- | --- | --- |
| `imported` | OAuth/setup-token 导入成功 | 否 | 默认 `schedulable=false`、`pool_profile_effective=normal`、`pool_weight_mode=low` |
| `refreshed` | refresh-only 成功 | 否 | 只验证凭证刷新链路，不发 messages |
| `runtime_registered` | CC Gateway runtime 注册成功 | 否 | 确认账号安全引用、代理引用、出口 bucket 已写入 runtime |
| `healthcheck_passed` | 最终定向健康检查 200，且经过 CC Gateway，且 raw capture 存在 | 否 | 200 前不能进入预热或生产；若启用真实 Claude Code 客户端健康检查，则以真实客户端定向检查 200 作为最终准入证据 |
| `warming` | 后台显式 start-warming | 是 | 低权重、normal effective profile，禁止 aggressive 生效 |
| `production` | 后台显式 promote-production，且已处于 warming | 是 | `pool_profile_requested` 才允许变为 effective |
| `quarantined` | 命中硬风险 | 否 | 人工恢复前不可调度 |

老账号如果缺少 `onboarding_stage`，后台显示 `legacy_unknown`，不会自动迁移或降级，避免误伤现有生产账号。

## 3. 账号 extra 字段

正式号池新账号必须维护以下脱敏字段：

- `onboarding_stage`
- `onboarding_stage_updated_at`
- `onboarding_last_check`
- `onboarding_last_check_at`
- `onboarding_last_error_code`
- `onboarding_last_error_bucket`
- `healthcheck_status`
- `healthcheck_last_status_code_bucket`
- `healthcheck_last_raw_ref`
- `cc_gateway_runtime_registered`
- `cc_gateway_runtime_registered_at`
- `warming_started_at`
- `warming_until`
- `pool_profile_requested`
- `pool_profile_effective`
- `pool_weight_mode`
- `risk_event_ref`
- `quarantine_reason`
- `quarantine_at`

这些字段不得包含 raw token、Authorization、x-api-key、email、账号/组织 UUID、raw body、raw prompt、raw telemetry、raw CCH 或 proxy credential。

## 4. 上号默认策略

1. 新账号创建后无论前端传什么，`schedulable=false`。
2. `pool_profile_requested` 可以记录 `normal` 或 `aggressive`。
3. `pool_profile_effective` 初始固定为 `normal`。
4. `pool_weight_mode` 初始固定为 `low`。
5. `cc_gateway_canary_only=false`，但账号仍不可调度；安全性由状态机决定，不由 canary-only 字段伪装。
6. aggressive 只能在 `production` 阶段生效，不能在新账号导入后直接生效。

## 5. 定向健康检查要求

健康检查必须满足：

```text
status=200
cc_gateway_seen=true
raw_capture_present=true
fallback=false
proxy_mismatch=false
risk_text=false
```

没有健康检查 200，不得进入 `warming`。健康检查必须指定账号，不允许调度器自由换号。健康检查期间即使临时授权账号发请求，也不得进入生产调度。

当前实现已经把 acceptance/healthcheck 接口改为必须依赖定向健康检查 runner；runner 不存在时，acceptance fail closed。Sub2API 要求 CC Gateway 返回安全证据头：`X-CC-Gateway-Seen: 1` 与 `X-CC-Gateway-Raw-Capture-Ref: hmac-sha256:<64hex>`。raw capture 未启用或未实际生成时，健康检查不得通过。

## 6. 自动隔离条件

以下情况必须自动隔离账号：

- `missing_account_identity`
- `egress_proxy_failure`
- 401 invalid auth
- 403 forbidden / hold / risk
- KYC / unusual activity / account on hold / risk text
- proxy mismatch
- direct fallback
- sign-to-strip fallback
- verifier fail
- raw token/body/prompt/CCH 泄漏风险
- control-plane unsafe upload

隔离行为：

```text
status=error
schedulable=false
onboarding_stage=quarantined
healthcheck_status=quarantined
quarantine_reason=<safe bucket>
risk_event_ref=<safe ref>
```

同时写入 `risk_event` 脱敏 ledger。ledger 只允许 safe ref / bucket，不允许 raw secret 或 PII。

## 7. 第二号 hold 事故对应防线

第二号异常暴露出的风险点在本策略中分别由以下防线覆盖：

1. 新号不可直接生产：`imported/refreshed/runtime_registered/healthcheck_passed` 都不可调度。
2. runtime mapping 缺失：CC Gateway control-plane 返回 `missing_account_identity` 会隔离。
3. 代理失败：`egress_proxy_failure` 会隔离，不继续调度。
4. 401/403：正式号池 Anthropic OAuth/setup-token 账号会隔离，不再进入 refresh loop 继续打真实请求。
5. 200 健康检查前不可生产：`activate/start-warming` 要求 `healthcheck_passed`。
6. 新号不能立即 aggressive：warming 阶段固定 normal + low weight；production 后才允许 requested profile 生效。

## 8. 后台字段解释

后台账号列表/详情展示：

- `onboarding_stage`：当前状态机阶段。
- `pool_profile_requested`：目标策略，可能是 `normal` 或 `aggressive`。
- `pool_profile_effective`：当前实际生效策略。
- `pool_weight_mode`：当前调度权重模式。
- `healthcheck_status`：健康检查状态。
- `healthcheck_last_status_code_bucket`：健康检查状态码桶。
- `cc_gateway_runtime_registered`：runtime mapping 是否注册。
- `quarantine_reason`：隔离原因桶。
- `risk_event_ref`：脱敏风险事件引用。
- `warming_until`：预热窗口结束时间。
- `production_ready`：是否处于 production。

## 9. 后台动作

新增或规范动作：

```text
POST /api/v1/admin/claude-onboarding/sessions/:id/refresh-only
POST /api/v1/admin/claude-onboarding/sessions/:id/runtime-register
POST /api/v1/admin/claude-onboarding/sessions/:id/healthcheck
POST /api/v1/admin/claude-onboarding/sessions/:id/start-warming
POST /api/v1/admin/claude-onboarding/sessions/:id/promote-production
POST /api/v1/admin/accounts/:id/quarantine
```

约束：

- `healthcheck` 复用 acceptance runner；runner 不可用时 fail closed。
- `start-warming` 只能在 healthcheck passed 后成功。
- `promote-production` 只能从 warming 推进。
- 接口不接受前端传入的 raw token、refresh token、access token、account ref。
- 账号级 `schedulable=true` 与批量 `schedulable=true` 对正式号池账号有硬门禁：只有 `warming` / `production` 阶段允许打开；`imported/refreshed/runtime_registered/healthcheck_passed/quarantined` 会被后端拒绝。
- `POST /api/v1/admin/accounts/:id/quarantine` 只接受安全原因桶/摘要，后端写入 `risk_event_ref`，不会回显 raw secret。

## 10. 上线操作清单

1. 创建或选择代理，确认出口 IP。
2. 创建 onboarding session，记录目标 profile。
3. 通过 setup-token 或 OAuth 导入账号。
4. 确认账号阶段为 `imported` 或 `runtime_registered`，且 `schedulable=false`。
5. 执行 refresh-only；失败则隔离。
6. 执行 runtime-register；失败则隔离。
7. 执行 healthcheck；必须返回 200 且 CC Gateway raw capture 存在。若启用真实 Claude Code 客户端健康检查，则后台生成短期单账号 healthcheck key，由真实 CLI 发起一次低成本请求，最终仍必须 200。
8. 执行 start-warming；进入低权重 normal 预热，并写入 `warming_until`。
9. 观察预热窗口；无 401/403、无代理异常、无风险文本后，执行 promote-production。
10. production 后再允许 requested aggressive 生效。

## 11. 故障恢复

- `quarantined` 账号不得自动恢复。
- 需要人工确认代理、凭证、runtime mapping、健康检查 raw capture 后，再通过独立恢复流程处理。
- 恢复流程不得直接跳到 production，应重新走 refresh-only、runtime-register、healthcheck、warming。

## 12. 敏感字段规则

任何 safe deliverable、后台 DTO、risk event、ledger 和日志摘要不得输出：

- raw token / cookie / setup token
- Authorization / x-api-key 值
- email
- account UUID / org UUID
- raw body / raw prompt / raw telemetry
- raw CCH
- proxy credential

只允许输出 HMAC ref、bucket、布尔值、状态枚举和脱敏摘要。
