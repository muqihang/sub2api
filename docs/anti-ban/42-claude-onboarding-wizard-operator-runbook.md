# Claude 正式号池上号向导 Runbook

本 runbook 适用于前端 `/admin/claude-onboarding` 的正式号池 Claude 订阅账号上号流程。

## 安全边界

- 不在 acceptance 中发送真实 Anthropic/Claude `/v1/messages`。
- OAuth token、refresh token、cookie、sessionKey、Authorization、x-api-key、代理密码不得出现在前端响应、日志或 safe summary。
- Setup Token、cookie/sessionKey、自定义 base URL、TLS 指纹、metadata 透传、原生 CCH、cache TTL、session masking 不是默认路径。
- 向导不会写入降低 Claude Code 能力的 hard limit：不限制 Sonnet/Opus、1m context、thinking、tools、stream、`max_tokens=32000`、多轮 session。
- normal/aggressive 只表达调度策略/建议；不会绕过 P0、cooldown、quarantine 或 refresh fail-closed。

## 操作流程

1. 选择已有代理或创建新代理。socks5 输入由后端按远程 DNS 语义规范化；失败时 fail closed，不允许 direct fallback。
2. 点击“测试代理”。失败时不得继续 OAuth。
3. 在即将登录 Claude 的同出口浏览器打开 browser-egress-check URL；若自动匹配不可用，完成不可绕过人工 attestation。
4. 选择 `normal` 或 `aggressive`，并选择 Claude Code only / formal pool group。
5. 生成 OAuth URL，在同出口浏览器完成 Claude Code OAuth。
6. 粘贴授权 code；后端执行 exchange-code-and-create，前端不接收 token。
7. 后端先向 CC Gateway runtime 注册 safe account identity + egress bucket 映射；失败则 fail closed，不创建可调度账号。
8. 后端创建账号：`schedulable=false`、`onboarding_state=pending_acceptance`，写入 CC Gateway/formal-pool 默认 extra。
9. 运行 acceptance。只做本地/mock/readiness 检查，不发真实 messages，并确认 `cc_gateway_runtime_registered=true`。
10. acceptance 全 pass 后，管理员手动激活，账号进入 `ready_for_small_flow`。
11. 真实 OAuth 登录和小流量 smoke 必须单独批准。

## 默认账号配置

- `platform=anthropic`
- `type=oauth`
- `status=active`
- `schedulable=false` until manual activation
- `concurrency=10`
- `cc_gateway_enabled=true`
- `cc_gateway_canary_only=false`
- `cc_gateway_routes=native_messages`
- `cc_gateway_egress_bucket_enabled=true`
- server-generated `cc_gateway_egress_bucket`
- server-generated safe `cc_gateway_account_ref`
- `pool_profile=normal|aggressive`
- `oauth_refresh_fail_closed=true`
- `onboarding_state=pending_acceptance -> ready_for_small_flow`

## Acceptance 失败处理

- proxy fail：更换/修复代理后重新测试；不要直连回退。
- browser egress mismatch/expired：用同出口浏览器重新打开 check URL 或重新做 attestation。
- OAuth invalid_grant/scope missing：重新走正式 Claude Code OAuth；不要切 Setup Token/cookie 路径。
- CC Gateway runtime registration failed：检查 Sub2API `gateway.cc_gateway.base_url/token`、CC Gateway `sub2api` mode、代理 URL 规范化和 runtime register 日志；不要手工把 raw token/body/prompt/CCH 写入前端或 safe deliverable。
- CC Gateway bucket/account identity missing：向导正常路径应自动注册；若仍缺失，保持账号不可调度，先修 runtime registration，再重跑 acceptance。
- ledger/usage warning：默认 observe-only；除非 P0 安全问题，不应 hard block Claude Code 能力。
- activation 失败：账号保持不可调度，不应半写为 ready。

## Formal Pool 存量账号手动恢复 SOP

> 如本文与 doc40 的硬门禁描述冲突，以 doc40 为准。恢复流程不得绕过 2xx directed healthcheck、CC Gateway seen、raw capture、no fallback、no proxy mismatch 等门禁。

1. 打开账号列表，过滤或搜索受影响账号。
2. 打开 `Formal Pool 诊断/修复`，先查看账号当前 `onboarding_stage`、Gate 结果、最近失败来源和推荐动作。
3. 确认 failure origin 后按来源处理：
   - `local_gate`：先修复 stage、runtime 注册状态、scope 或配置问题；不要跳过本地门禁。
   - `cc_gateway_control_plane`：先执行 runtime-register，检查 egress bucket 与代理映射，再运行 directed healthcheck。
   - `upstream`：优先尝试 token repair，确认不是本地或 CC Gateway 控制面问题。
   - `proxy`：更换代理后重新验证；proxy swap 后必须有完整 healthcheck evidence 才能进入 warming。若浏览器 egress attestation 尚未实现，作为 P1 follow-up 处理，但不得替代 healthcheck evidence。
   - `token_exchange`：获取新的 `sk-ant-sid` 类型凭据后重试；文档和工单中只能记录字段/类型，不得记录真实值。
4. 先尝试 token repair，再考虑替换账号。
5. 如果 token repair 成功：依次执行 runtime-register -> healthcheck -> start warming。修复账号只能重新进入 warming，不能 direct-promote 到 production。
6. 如果 token repair 失败：替换账号，并让新账号和出口代理重新走完整 onboarding wizard；不要把失败账号手工改成可调度。
7. 任何修复账号都不得 direct-promote 到 production；必须保留健康检查证据，进入 warming 后再按正式流量策略递进。
8. OAuth 存量账号如果不是 `setup-token`，不要走 ST token replacement；应走 refresh/runtime-register/healthcheck 路径。ST repair 仅用于 `setup-token` 账号。

## 禁止事项

不要在向导外手动拼 token、cookie、sessionKey、raw account_ref、raw proxy URL、raw CCH、raw prompt/body。不要把 canary 的 `max_messages=1`、低 max_tokens 或旧 session/account hard budget 带入正式账号。
