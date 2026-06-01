# 正式号池操作员面板运行手册

## 1. 适用范围与安全边界

本手册用于正式号池 Anthropic OAuth / Setup Token 账号的诊断、刷新、恢复、预热和进入生产。它只指导账号生命周期操作，不允许绕过安全门禁，也不改变 Claude Code 请求能力。

禁止事项：

- 不在没有明确批准时部署生产。
- 不在没有明确批准时发起真实 directed 健康检查。
- 不修改生产数据，除非该操作本身就是已批准的后台账号操作。
- 不泄露 raw token、raw prompt、raw body、raw telemetry、raw CCH、email、账号/组织 UUID、代理凭据、完整代理对象；setup-token 只允许通过专用导入/替换 secret-ingress 一次性输入。
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

`quarantined` 是异常安全状态，不是正常流水线的一步。`production` 是生产稳态，没有“post-production”阶段。

## 3. 面板动作词汇

| 动作 key | 中文按钮/标签 | 说明 |
| --- | --- | --- |
| `refresh_only` | 刷新登录凭证 | 只刷新 access token，不发 messages |
| `runtime_register` | 运行时注册/映射 | 把安全账号引用、代理引用、出口 bucket 写入 CC Gateway runtime |
| `healthcheck` | 定向健康检查 | 会发起一次极小真实上游请求；仅在需要准入/排障时点击 |
| `start_warming` | 进入预热 | 低权重、normal profile 可调度 |
| `promote_production` | 进入生产 | 只能从 warming 进入；证据不完整则 安全失败并停止 |
| `replace_setup_token` | 替换 Setup Token 登录态 | setup-token 的 `invalid_grant` / 登录态失效修复入口 |
| `reauthorize_oauth` | 重新 OAuth 授权 | OAuth 的 `invalid_grant` / refresh token revoked 修复入口 |
| `monitor` | 无需操作，继续观测 | 健康 production 的主状态 |
| `quarantine` | 隔离账号 | 安全停止；不可调度 |
| `swap_proxy` | 更换出口代理 | 换代理后必须重新运行时注册、健康检查和预热 |

面板可显示英文机器 key，但操作员可见主要文案必须是中文；后端 enum/动作 key 保持英文以便自动化稳定解析。

## 4. 新 setup-token 上号流程

1. 创建或选择代理，确认出口 IP 与账号地理策略一致。
2. 创建 onboarding session 或导入 setup-token 登录态。
3. 确认新账号为 `imported`，`schedulable=false`，`pool_weight_mode=low`。
4. 点击“刷新登录凭证”。成功后应进入 `refreshed` 或具备 refresh 证据。
5. 点击“运行时注册/映射”。失败时不要继续健康检查，先修复 runtime 证据。
6. 点击“定向健康检查”。点击前必须确认：会发起一次极小真实上游请求。
7. 健康检查通过后点击“进入预热”。
8. 预热期间低权重观测，确认无 401/403、proxy mismatch、降级旁路、风险文本。
9. 满足条件后点击“进入生产”。
10. production 后只做观测；不要例行反复点健康检查。

## 5. 新 OAuth 上号流程

OAuth 与 Setup Token 共用同一正式号池门禁：刷新凭证、运行时注册、定向健康检查、预热、生产。

1. 完成 OAuth 授权导入。
2. 确认账号不可调度且处于 `imported` / `refreshed` / `runtime_registered` 之一。
3. 若面板建议“刷新登录凭证”，先刷新 OAuth access token。
4. 点击“运行时注册/映射”。
5. 点击“定向健康检查”，并确认真实上游小请求。
6. 健康检查通过后进入预热。
7. 预热观察后进入生产。

OAuth 账号不能用 Setup Token 替换入口修复；`invalid_grant` 时必须重新 OAuth 授权。

## 6. access token 过期与 401 恢复

正式号池生产账号遇到过期 access token 时，系统应自动执行一次受并发保护的刷新并重试：

1. 首次业务请求返回 `401 Invalid authentication credentials`。
2. 若账号是 Anthropic OAuth 或 Setup Token 且具备刷新凭证，系统只触发一次刷新。
3. 刷新成功后清理凭证缓存，并只重试一次原请求。
4. 重试成功：账号保持 生产且可调度，不隔离。
5. 刷新不可用或刷新失败、重试后仍 401：进入 quarantine。
6. `invalid_grant`：直接视为终止性凭证故障，不重复刷新，不推荐 健康检查。

该恢复逻辑不得改写用户请求体、响应体、tools、thinking、stream、model、max_tokens 或 长上下文 参数。

## 7. invalid_grant 处理区别

`invalid_grant` 表示刷新凭证或登录关系已经失效，不是健康检查能解决的问题。

| 账号类型 | 面板推荐 | 操作员动作 |
| --- | --- | --- |
| setup-token | 替换 Setup Token 登录态 | 只在专用 Setup Token 替换表单中粘贴新的 `sk-ant-sid` 登录态；系统换取新的推理访问凭证；该原始登录态不回显、不进入 DTO/日志/审计/测试快照；随后运行时注册、健康检查、预热 |
| OAuth | 重新 OAuth 授权 | 重新完成 OAuth 授权；随后运行时注册、健康检查、预热 |

不要对 `invalid_grant` 账号执行 进入生产。不要反复点定向健康检查试图“刷过”。账号 5 类型事故的结论就是：`invalid_grant` 必须替换登录态或重新授权，而不是生产提升。

## 8. 运行时注册

运行时注册/映射必须完成以下证据：

- 安全账号引用，不含账号 UUID / email / raw token。
- 安全代理引用 / 出口 bucket，不含 proxy host credential。
- `cc_gateway_runtime_registered=true`。
- `cc_gateway_runtime_registered_at` 存在。

缺少运行时证据时，面板应推荐“运行时注册/映射”，而不是健康检查或进入生产。

## 9. directed 健康检查

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

以下情况不要点 健康检查：

- 健康生产账号只是日常观测。
- 已知 `invalid_grant`。
- 运行时注册证据缺失。
- proxy mismatch / proxy failure 尚未处理。
- 账号处于硬风险、上游暂停或账号验证状态。

### 9.1 429 / 限流安全分类

`429` 不是“多点几次就会好”的健康检查失败。面板看到 `status_code_bucket=status_429`、`failure_source=rate_limit_service`、`failure_code=5h/7d/both/long_context_usage_credits` 或类似安全桶时，按下表处理：

| 安全桶/字段 | 中文含义 | 操作员处理 | 不要做 |
| --- | --- | --- | --- |
| `status_429` | 429 / 上游限流 | 停止重复健康检查，查看账号用量窗口和恢复时间；必要时切到另一个已验证生产账号 | 不要刷新循环、不要连续健康检查 |
| `5h` | 5h 用量窗口已满 | 等 5h 窗口恢复；恢复后如仍需准入，再做一次确认后的健康检查 | 不要为了“验证”继续发真实请求 |
| `7d` | 7d 用量窗口已满 | 等 7d 窗口恢复或更换已验证账号 | 不要绕过预热或直接进入生产 |
| `both` | 5h 与 7d 均已满 | 视为高风险限流状态，等待两个窗口恢复 | 不要反复重试 |
| `long_context_usage_credits` | 长上下文额度已满 | 降低排障频率，等长上下文额度恢复；不修改用户请求能力或长上下文配置 | 不要关闭/限制用户长上下文能力来“修复” |

429 处理只允许用安全桶、布尔证据和恢复时间判断；不得展示 raw 响应、raw prompt、raw body 或 token。

### 9.2 健康检查失败处理表

| 面板分类 | 典型安全桶/信号 | 推荐动作 | 健康检查按钮策略 |
| --- | --- | --- | --- |
| 认证失败 | `status_401`、`invalid_auth`、`refresh_required` | 先只刷新凭证一次；刷新关系失效或刷新失败后，Setup Token 账号替换登录态，OAuth 账号重新授权 | 未修复前不要再点 |
| 账号风险，需要人工介入 | `status_403`、`hold`、`risk`、`kyc`、`unusual_activity` | 保持隔离；先登录上游网页查看账号状态，确认订阅、地区、组织权限和账号验证要求 | 不要重复健康检查；不要反复刷新凭证 |
| 代理异常 | `proxy`、`proxy_mismatch`、`egress_proxy_failure` | 更换出口代理；重新运行时注册 | 换代理和注册完成前不要点 |
| CC Gateway 证据缺失 | `cc_gateway_not_seen`、`raw_capture_missing`、`fallback`、`missing_account_identity`、`missing_egress_bucket` | 修复运行时映射、原始捕获、降级旁路或校验器问题 | 证据链修复前不要点 |
| 上游 5xx/临时失败 | `status_5xx`、`healthcheck_failed` 且无硬风险 | 先刷新诊断和观测；确认不是代理/网关/风控后再排障 | 只允许一次明确确认的排障健康检查 |
| 无高风险桶 | 证据完整或只缺最新健康检查证据 | 仅准入/排障时执行 | 按按钮二次确认执行 |

## 10. warming

`warming` 是预热阶段，低权重可调度：

- `pool_weight_mode=low`。
- `pool_profile_effective=normal`。
- requested aggressive 不得在 warming 生效。
- 观察期内如果发生 403、账号暂停、风险文本、代理不匹配、降级旁路或校验失败，应进入硬隔离。

预热未结束或证据不完整时不要进入生产。

## 11. production

`production` 是生产稳态：

- 账号可正常调度。
- 允许 requested profile 生效。
- 健康生产账号不应显示主修复按钮或定向健康检查推荐。
- access token 过期应由自动 refresh/retry 静默处理。
- 操作员主要动作是“无需操作，继续观测”。

只有发生真实故障或明确排障时才使用定向健康检查。

## 12. quarantined

隔离后账号不可调度：

```text
status=error
schedulable=false
onboarding_stage=quarantined
healthcheck_status=quarantined
quarantine_reason=<安全桶>
risk_event_ref=<safe ref>
```

修复原则：

1. 先看 `quarantine_reason` / `failure_code` / recommended 动作。
2. 凭证过期但刷新可用：等待系统自动刷新/重试，或执行“只刷新凭证”。
3. `invalid_grant`：setup-token 替换登录态，OAuth 重新授权。
4. 代理问题：更换出口代理，然后重新运行时注册和健康检查。
5. 控制面/运行时证据缺失：先执行“运行时注册/映射”。
6. 健康检查证据缺失：再执行定向健康检查。
7. 通过后进入预热，不得直接进入生产。

恢复后当前 `quarantine_reason` / `quarantine_at` 应清空，但历史 safe ref 可以保留用于审计。



### 12.1 账号风险提示必须说人话

当面板出现 `403`、账号暂停、异常活动、账号验证或风险提示时，这不是普通限流，也不是简单的 access token 过期。系统应保持隔离，并在面板中明确告诉运营：

- 账号已被上游暂停或限制：先登录上游网页查看账号状态。
- 需要完成账号验证：按上游页面要求处理，不能通过重试恢复。
- 上游返回账号风险提示：保持隔离，等待人工确认。
- 上游拒绝访问：先确认账号订阅、地区、组织和权限状态。

禁止把这些情况写成只有工程师能懂的 `hold/risk/KYC/refresh loop`。技术枚举可以留在安全审计字段里，但默认面板必须优先展示中文解释和下一步动作。

## 13. 进入生产

账号级 endpoint：

```text
POST /api/v1/admin/accounts/:id/formal-pool/promote-production
```

要求：

- 只能 `warming -> production`。
- 运行时证据 完整。
- 健康检查证据完整。
- 证据不完整时 安全失败并停止。
- 已是健康生产状态时返回稳定的无操作或安全响应，不得乱改证据。
- 操作必须写 审计日志：操作员、内部数值 账号 ID 或 安全 HMAC 账号引用、操作前后阶段、动作、原因桶、成功/失败；不得写入账号 UUID、组织 UUID、email、raw token 或 代理凭据。

## 14. proxy swap revalidation

更换出口代理后必须重新验证：

1. 点击“更换出口代理”。
2. 账号退回需要重新验证的状态，通常不再保持 production 证据。
3. 点击“运行时注册/映射”。
4. 点击“定向健康检查”。
5. 通过后进入 warming。
6. 观察后再 进入生产。

不要换代理后直接恢复生产。

## 15. 面板字段说明

| 字段 | 含义 | 安全要求 |
| --- | --- | --- |
| `onboarding_stage` | 生命周期阶段 | 英文机器字段，界面中文显示 |
| `recommended_actions` | 下一步推荐动作 | key 稳定英文，label 可中文化 |
| `failure_origin` | 失败来源：本地门禁 / 控制面 / 上游 / 代理 / 凭证交换 | 不含 raw 响应 |
| `failure_code` | 安全失败桶 | 不含 email/UUID/token |
| `healthcheck_status` | 健康检查状态 | 中文摘要展示 |
| `status_code_bucket` | 状态码桶，如 `status_2xx` / `status_401` | 只显示桶 |
| `cc_gateway_runtime_registered` | 运行时证据是否存在 | 布尔值 |
| `raw_capture_ref` | 原始捕获的安全 HMAC 引用 | 只允许 safe ref |
| `quarantine_reason` | 当前隔离原因桶 | 恢复后清空当前字段 |
| `risk_event_ref` | 历史风险事件安全引用 | 不含 raw 风险内容 |

## 16. 敏感字段规则

正式号池后端 DTO、前端展示、审计日志、文档和测试不得包含：

- 原始 token / Setup Token / cookie / Authorization。
- 原始 prompt / 原始 body / 原始 telemetry / 原始 CCH。
- email。
- account UUID / org UUID。
- 代理凭据。
- 完整代理对象。
- 原始主机身份。
- 审计日志中的账号标识只能使用内部数值 id 或 安全 HMAC 账号引用，不能使用账号/组织 UUID。
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
| 生产健康 | 无需操作，继续观测 | 不要例行健康检查 |
| 健康检查通过 | 进入预热 | 不要直接 production |
| warming | 进入生产 | 不要跳过观察证据 |
| 缺少运行时证据 | 运行时注册/映射 | 不要健康检查 |
| 缺少健康检查证据 | 定向健康检查 | 不要忘记真实请求确认 |
| setup-token invalid_grant | 替换 Setup Token 登录态 | 不要 OAuth 重授权或重复 健康检查 |
| OAuth invalid_grant | 重新 OAuth 授权 | 不要粘贴 setup-token |
| 429 / 5 小时、7 天或长上下文额度 | 等待窗口或额度恢复，必要时换已验证账号 | 不要连续健康检查 或 反复刷新凭证 |
| 代理不匹配或代理失败 | 更换出口代理 | 不要保留旧生产证据 |
| 403 / 账号暂停 / 风险提示 / 账号验证 | 隔离账号 | 不要反复刷新凭证 |
