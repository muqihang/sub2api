# Formal Pool 操作员面板运行手册

## 1. 适用范围与安全边界

本手册用于 Formal Pool Anthropic OAuth / setup-token 账号的诊断、刷新、恢复、预热和进入生产。它只指导账号生命周期操作，不允许绕过安全门禁，也不改变 Claude Code 请求能力。

禁止事项：

- 不在没有明确批准时部署生产。
- 不在没有明确批准时发起真实 directed healthcheck。
- 不修改生产数据，除非该操作本身就是已批准的后台账号操作。
- 不泄露 raw token、raw prompt、raw body、raw telemetry、raw CCH、email、账号/组织 UUID、proxy credential、full proxy object；setup-token 只允许通过专用导入/替换 secret-ingress 一次性输入。
- 不限制 tools、thinking、stream、1m/long context、Opus/Sonnet、max_tokens，也不改写用户 request body / response body。

## 2. 生命周期总览

| 阶段 key | 中文显示 | 调度状态 | 操作员下一步 |
| --- | --- | --- | --- |
| `imported` | 已导入，待刷新 | 不可调度 | 刷新登录凭证；随后运行时注册 |
| `refreshed` | 已刷新，待运行时注册 | 不可调度 | 运行时注册/映射 |
| `runtime_registered` | 已完成运行时注册，待健康检查 | 不可调度 | 定向健康检查 |
| `healthcheck_passed` | 健康检查通过，待进入预热 | 不可调度 | 进入预热 |
| `warming` | 预热中，低权重可调度 | 低权重可调度 | 观察预热窗口；证据完整后进入生产 |
| `production` | 生产中，正常调度 | 正常调度 | 无需操作，继续观测 |
| `quarantined` | 已隔离，需要修复 | 不可调度 | 按原因修复；不得直接进入生产 |
| `legacy_unknown` | 历史账号，状态未知 | 不自动迁移 | 只读诊断；不要批量改状态 |

`quarantined` 是异常安全状态，不是正常流水线的一步。`production` 是正常稳态，没有“post-production”阶段。

## 3. 面板动作词汇

| 动作 key | 中文按钮/标签 | 说明 |
| --- | --- | --- |
| `refresh_only` | 刷新登录凭证 | 只刷新 access token，不发 messages |
| `runtime_register` | 运行时注册/映射 | 把安全账号引用、代理引用、出口 bucket 写入 CC Gateway runtime |
| `healthcheck` | 定向健康检查 | 会发起一次极小真实上游请求；仅在需要准入/排障时点击 |
| `start_warming` | 进入预热 | 低权重、normal profile 可调度 |
| `promote_production` | 进入生产 | 只能从 warming 进入；证据不完整则 fail closed |
| `replace_setup_token` | 替换 Setup Token 登录态 | setup-token 的 `invalid_grant` / 登录态失效修复入口 |
| `reauthorize_oauth` | 重新 OAuth 授权 | OAuth 的 `invalid_grant` / refresh token revoked 修复入口 |
| `monitor` | 无需操作，继续观测 | 健康 production 的主状态 |
| `quarantine` | 隔离账号 | 安全停止；不可调度 |
| `swap_proxy` | 更换出口代理 | 换代理后必须重新 runtime-register + healthcheck + warming |

面板可显示英文机器 key，但操作员可见主要文案必须是中文；后端 enum/action key 保持英文以便自动化稳定解析。

## 4. 新 setup-token 上号流程

1. 创建或选择代理，确认出口 IP 与账号地理策略一致。
2. 创建 onboarding session 或导入 setup-token 登录态。
3. 确认新账号为 `imported`，`schedulable=false`，`pool_weight_mode=low`。
4. 点击“刷新登录凭证”。成功后应进入 `refreshed` 或具备 refresh 证据。
5. 点击“运行时注册/映射”。失败时不要继续健康检查，先修复 runtime 证据。
6. 点击“定向健康检查”。点击前必须确认：会发起一次极小真实上游请求。
7. 健康检查通过后点击“进入预热”。
8. 预热期间低权重观测，确认无 401/403、proxy mismatch、fallback、risk text。
9. 满足条件后点击“进入生产”。
10. production 后只做观测；不要例行反复点健康检查。

## 5. 新 OAuth 上号流程

OAuth 与 setup-token 共用同一 Formal Pool gates：refresh、runtime-register、directed healthcheck、warming、production。

1. 完成 OAuth 授权导入。
2. 确认账号不可调度且处于 `imported` / `refreshed` / `runtime_registered` 之一。
3. 若面板建议“刷新登录凭证”，先刷新 OAuth access token。
4. 点击“运行时注册/映射”。
5. 点击“定向健康检查”，并确认真实上游小请求。
6. 健康检查通过后进入预热。
7. 预热观察后进入生产。

OAuth 账号不能用 Setup Token 替换入口修复；`invalid_grant` 时必须重新 OAuth 授权。

## 6. access token 过期与 401 恢复

Formal Pool production 账号遇到 stale access token 时，系统应自动执行一次受并发保护的 refresh-and-retry：

1. 首次业务请求返回 `401 Invalid authentication credentials`。
2. 若账号是 Anthropic OAuth/setup-token 且具备 refresh token，系统只触发一个 refresh。
3. refresh 成功后清理 token cache，并只重试一次原请求。
4. 重试成功：账号保持 production/schedulable，不隔离。
5. refresh 不可用、refresh 失败、重试后仍 401：进入 quarantine。
6. `invalid_grant`：直接视为终止性凭证故障，不重复 refresh，不推荐 healthcheck。

该恢复逻辑不得改写用户请求体、响应体、tools、thinking、stream、model、max_tokens 或 long-context 参数。

## 7. invalid_grant 处理区别

`invalid_grant` 表示 refresh credential / 登录关系已经失效，不是健康检查能解决的问题。

| 账号类型 | 面板推荐 | 操作员动作 |
| --- | --- | --- |
| setup-token | 替换 Setup Token 登录态 | 只在专用 setup-token 替换表单中粘贴新的 `sk-ant-sid` 登录态；系统换取新 inference token；该原始登录态不回显、不进入 DTO/日志/审计/测试快照；随后 runtime-register、healthcheck、warming |
| OAuth | 重新 OAuth 授权 | 重新完成 OAuth 授权；随后 runtime-register、healthcheck、warming |

不要对 `invalid_grant` 账号执行 promote-production。不要反复点 directed healthcheck 试图“刷过”。账号 5 类型事故的结论就是：`invalid_grant` 必须替换登录态或重新授权，而不是生产提升。

## 8. runtime-register

运行时注册/映射必须完成以下证据：

- 安全账号引用，不含账号 UUID / email / raw token。
- 安全代理引用 / 出口 bucket，不含 proxy host credential。
- `cc_gateway_runtime_registered=true`。
- `cc_gateway_runtime_registered_at` 存在。

缺少 runtime 证据时，面板应推荐“运行时注册/映射”，而不是 healthcheck 或 production。

## 9. directed healthcheck

定向健康检查是准入/排障动作，不是日常监控动作。它必须指定账号，并且会发起一次极小真实上游请求。点击前必须确认：

```text
确认继续？此操作会发起一次极小真实上游请求。
```

通过条件：

```text
status=200
cc_gateway_seen=true
raw_capture_present=true
fallback=false
proxy_mismatch=false
risk_text=false
```

以下情况不要点 healthcheck：

- 健康 `production` 账号只是日常观测。
- 已知 `invalid_grant`。
- runtime-register 证据缺失。
- proxy mismatch / proxy failure 尚未处理。
- 账号处于 hard risk / hold / KYC 状态。

## 10. warming

`warming` 是低权重可调度阶段：

- `pool_weight_mode=low`。
- `pool_profile_effective=normal`。
- requested aggressive 不得在 warming 生效。
- 观察期内如果发生 403、hold、risk text、proxy mismatch、fallback、verifier fail，应 hard quarantine。

预热未结束或证据不完整时不要进入 production。

## 11. production

`production` 是正常稳态：

- 账号可正常调度。
- 允许 requested profile 生效。
- 健康 production 不应显示主修复按钮或 directed healthcheck 推荐。
- access token 过期应由自动 refresh/retry 静默处理。
- 操作员主要动作是“无需操作，继续观测”。

只有发生真实故障或明确排障时才使用 directed healthcheck。

## 12. quarantined

隔离后账号不可调度：

```text
status=error
schedulable=false
onboarding_stage=quarantined
healthcheck_status=quarantined
quarantine_reason=<safe bucket>
risk_event_ref=<safe ref>
```

修复原则：

1. 先看 `quarantine_reason` / `failure_code` / recommended action。
2. 凭证过期但 refresh 可用：等待系统 refresh/retry 或执行 refresh-only。
3. `invalid_grant`：setup-token 替换登录态，OAuth 重新授权。
4. proxy 问题：更换出口代理，然后重新 runtime-register + healthcheck。
5. control-plane/runtime 证据缺失：先 runtime-register。
6. 健康检查证据缺失：再 directed healthcheck。
7. 通过后进入 warming，不得直接 production。

恢复后当前 `quarantine_reason` / `quarantine_at` 应清空，但历史 safe ref 可以保留用于审计。

## 13. promote-production

账号级 endpoint：

```text
POST /api/v1/admin/accounts/:id/formal-pool/promote-production
```

要求：

- 只能 `warming -> production`。
- runtime evidence 完整。
- healthcheck evidence 完整。
- 证据不完整时 fail closed。
- 已是健康 production 时返回稳定 no-op 或安全响应，不得乱改证据。
- 操作必须写 audit log：operator、内部数值 account id 或 safe/HMAC account ref、before/after stage、action、reason bucket、success/failure；不得写入账号 UUID、组织 UUID、email、raw token 或 proxy credential。

## 14. proxy swap revalidation

更换出口代理后必须重新验证：

1. 点击“更换出口代理”。
2. 账号退回需要重新验证的状态，通常不再保持 production 证据。
3. 点击“运行时注册/映射”。
4. 点击“定向健康检查”。
5. 通过后进入 warming。
6. 观察后再 promote-production。

不要换代理后直接恢复 production。

## 15. 面板字段说明

| 字段 | 含义 | 安全要求 |
| --- | --- | --- |
| `onboarding_stage` | 生命周期阶段 | 英文机器字段，界面中文显示 |
| `recommended_actions` | 下一步推荐动作 | key 稳定英文，label 可中文化 |
| `failure_origin` | 失败来源：local gate / control plane / upstream / proxy / token exchange | 不含 raw 响应 |
| `failure_code` | 安全失败桶 | 不含 email/UUID/token |
| `healthcheck_status` | 健康检查状态 | 中文摘要展示 |
| `status_code_bucket` | 状态码桶，如 `status_2xx` / `status_401` | 只显示桶 |
| `cc_gateway_runtime_registered` | runtime 证据是否存在 | 布尔值 |
| `raw_capture_ref` | raw capture 的安全 HMAC 引用 | 只允许 safe ref |
| `quarantine_reason` | 当前隔离原因桶 | 恢复后清空当前字段 |
| `risk_event_ref` | 历史风险事件 safe ref | 不含 raw 风险内容 |

## 16. 敏感字段规则

Formal Pool 后端 DTO、前端展示、审计日志、文档和测试不得包含：

- raw token / setup token / cookie / Authorization。
- raw prompt / raw body / raw telemetry / raw CCH。
- email。
- account UUID / org UUID。
- proxy credential。
- full proxy object。
- raw host identity。
- 审计日志中的账号标识只能使用内部数值 id 或 safe/HMAC account ref，不能使用账号/组织 UUID。
- credentials object。

允许展示：

- HMAC ref。
- bucket。
- 布尔值。
- 阶段/动作 enum。
- 脱敏摘要。

## 17. 快速决策表

| 面板状态 | 推荐动作 | 不要做 |
| --- | --- | --- |
| production healthy | 无需操作，继续观测 | 不要例行 healthcheck |
| healthcheck_passed | 进入预热 | 不要直接 production |
| warming | 进入生产 | 不要跳过观察证据 |
| missing runtime | 运行时注册/映射 | 不要 healthcheck |
| missing healthcheck | 定向健康检查 | 不要忘记真实请求确认 |
| setup-token invalid_grant | 替换 Setup Token 登录态 | 不要 OAuth 重授权或重复 healthcheck |
| OAuth invalid_grant | 重新 OAuth 授权 | 不要粘贴 setup-token |
| proxy mismatch/failure | 更换出口代理 | 不要保留旧 production 证据 |
| 403 / hold / risk / KYC | 隔离账号 | 不要 refresh loop |
