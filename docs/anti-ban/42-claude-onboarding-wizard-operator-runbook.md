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
7. 后端创建账号：`schedulable=false`、`onboarding_state=pending_acceptance`，写入 CC Gateway/formal-pool 默认 extra。
8. 运行 acceptance。只做本地/mock/readiness 检查，不发真实 messages。
9. acceptance 全 pass 后，管理员手动激活，账号进入 `ready_for_small_flow`。
10. 真实 OAuth 登录和小流量 smoke 必须单独批准。

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
- CC Gateway bucket missing：先在 CC Gateway runtime 准备 bucket/account_ref 映射，再重跑 acceptance。
- ledger/usage warning：默认 observe-only；除非 P0 安全问题，不应 hard block Claude Code 能力。
- activation 失败：账号保持不可调度，不应半写为 ready。

## 禁止事项

不要在向导外手动拼 token、cookie、sessionKey、raw account_ref、raw proxy URL、raw CCH、raw prompt/body。不要把 canary 的 `max_messages=1`、低 max_tokens 或旧 session/account hard budget 带入正式账号。
