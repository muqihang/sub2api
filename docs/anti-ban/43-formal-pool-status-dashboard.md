# 正式号池实时状态看板

## 1. 目的

正式号池实时状态看板用于让运营快速看清所有 Anthropic OAuth / Setup Token 正式号池账号的当前状态、5 小时窗口、RPM、并发、会话和下一步建议。

它和原来的账号列表是两个独立功能：

- 原账号列表：继续用于账号管理、编辑、诊断、上号和修复；本次只调整布局密度，避免表格被挤压。
- 号池实时看板：只读全屏弹窗，用于总览状态和发现异常，不负责创建、修复或修改账号。

## 2. 安全边界

看板是只读功能：

- 不发真实 Anthropic 请求。
- 不执行 directed 健康检查。
- 不修改账号状态。
- 不修改代理、凭证、调度权重或生产数据。
- 不替代诊断/修复面板；发现问题后仍需回到账号级诊断面板操作。

看板允许显示运营可识别的账号名称，包括普通运营邮箱形式的账号名；但仍禁止展示：

- raw token / cookie / setup token
- access token / refresh token
- Authorization / x-api-key 值
- raw prompt / raw body / raw telemetry / raw CCH
- 账号 UUID / 组织 UUID
- 代理地址中的用户名、密码或完整代理凭据
- 任何看起来像密钥、长 token、原始登录态或 user:pass@host 的字段

如果账号名看起来不安全，后端或前端必须回退为 `账号 #<id>`。

## 3. 入口与刷新

后台账号页面提供按钮：

```text
号池实时看板
```

点击后打开独立全屏弹窗。弹窗打开期间默认每 5 秒自动刷新；关闭后停止刷新，并取消仍在进行的前端请求。手动刷新只重新拉取看板数据，不触发真实上游请求。

## 4. 数据来源

看板后端接口：

```text
GET /api/v1/admin/formal-pool/status-dashboard
```

接口必须返回所有 Anthropic OAuth / Setup Token 正式号池账号，不受账号列表当前分页影响。

状态由后端根据账号持久化字段和运行时计数统一分类，前端只负责展示，不重新发明业务状态。运行时数据包括：

- 5 小时窗口已用量、上限、剩余、利用率、恢复时间和状态。
- 当前 RPM 与 RPM 上限。
- 当前并发与并发上限。
- 活跃会话数与会话上限。
- 最近请求提示。
- 最近安全失败分类和失败桶。
- 后端给出的下一步建议。

如果运行时计数读取不到，看板必须显示“数据不足”，不能猜测为正常。

## 5. 状态优先级

看板按以下顺序判断状态。前面的状态优先级更高：

| 优先级 | 状态 key | 中文显示 | 含义 |
| --- | --- | --- | --- |
| 1 | `inactive` | 已停用 | 账号已停用，不参与调度 |
| 2 | `manual_risk` | 需人工介入 | 存在账号安全、认证、权限或门禁信号，需要人工查看具体失败分类 |
| 3 | `rate_limited` | 限流冷却中 | 命中 429、5 小时/7 天/长上下文额度窗口或本地限流冷却 |
| 4 | `quarantined` | 已隔离 | 账号处于隔离状态，先看隔离原因 |
| 5 | `error` | 错误 | 非限流、非隔离的错误状态 |
| 6 | `not_schedulable` | 不可调度 | 调度门禁未打开，需看 gate 原因 |
| 7 | `evidence_missing` | 证据不足 | 缺少运行时注册或健康检查证据 |
| 8 | `data_missing` | 数据不足 | 运行时计数读取不到，不能判断正常 |
| 9 | `warming` | 预热中 | 低权重、normal profile 可调度 |
| 10 | `production` | 生产中 | 证据完整且处于生产阶段 |
| 11 | `normal` | 正常 | 兼容历史健康账号的正常展示 |

注意：`missing_account_identity`、`missing_egress_bucket` 这类运行时身份/出口映射问题，不等同于上游账号被封。界面应显示“需人工介入”或“缺少账号运行时身份映射”，并引导去运行时注册/映射，而不是简单写成“账号风险”。

## 6. 429 / 限流展示

429 相关状态必须显示为“限流冷却中”或“等待限流恢复”，不能显示成普通隔离或账号风险。

常见字段解释：

| 字段/桶 | 中文含义 | 操作建议 |
| --- | --- | --- |
| `status_429` | 429 / 上游限流 | 等待窗口恢复，避免重复健康检查 |
| `5h` | 5 小时窗口已满 | 等 5 小时窗口恢复 |
| `7d` | 7 天窗口已满 | 等 7 天窗口恢复或换已验证账号 |
| `both` | 5 小时和 7 天窗口均满 | 等两个窗口恢复 |
| `long_context_usage_credits` | 长上下文额度不足 | 等额度恢复；不要削弱用户长上下文能力 |
| `pass_through + no_reset + missing` | 上游没有给恢复时间，仅透传错误 | 不进入本地限流冷却，不把账号误判为限流账号 |

## 7. 401 / 403 / 账号风险展示

看板需要把这些信号优先显示为“需人工介入”，并在诊断面板里展示更具体的中文解释：

| 信号 | 说明 | 下一步 |
| --- | --- | --- |
| `status_401` / `invalid_auth` / `refresh_required` | 认证失败或 access token 过期 | 先走受保护的刷新；失败后替换 Setup Token 或重新 OAuth 授权 |
| `status_403` / `forbidden` | 上游拒绝访问或权限不满足 | 人工确认订阅、地区、组织和权限 |
| `hold` / `account_on_hold` | 上游账号暂停 | 登录上游网页确认账号状态 |
| `kyc` / `verification_required` | 需要账号验证 | 按上游页面完成验证 |
| `unusual_activity` / `risk_text` | 异常活动或风险文本 | 保持隔离，人工判断 |

这些场景不应通过连续健康检查或刷新循环处理。

## 8. 主要卡片解释

| 卡片 | 含义 |
| --- | --- |
| 可正常调度 | 当前实际可调度账号数 |
| 预热中 | warming 阶段账号数 |
| 生产中 | production 阶段账号数 |
| 限流冷却 | rate_limited 状态账号数 |
| 需人工介入 | manual_risk 状态账号数 |
| 错误/隔离 | error 与 quarantined 状态账号数之和 |
| 已停用 | inactive 状态账号数 |
| 不可调度 | not_schedulable 状态账号数 |
| 证据不足 | evidence_missing 状态账号数 |
| 数据不足 | data_missing 状态账号数 |
| 当前总 RPM | 所有可读账号的当前 RPM / RPM 上限 |
| 5 小时总体余量 | 5 小时窗口剩余比例 |

## 9. 表格字段解释

| 字段 | 含义 |
| --- | --- |
| 账号 | 安全账号显示名和账号类型 |
| 状态 | 后端分类后的当前运营状态 |
| 阶段 | imported / refreshed / runtime_registered / healthcheck_passed / warming / production / quarantined 等生命周期阶段 |
| 5 小时限额 | 当前 5 小时窗口使用情况和恢复时间 |
| RPM 实况 | 当前 RPM 与上限 |
| 并发 | 当前并发与上限 |
| 会话 | 活跃会话数与上限 |
| 最近请求 | 最近成功或最近请求时间提示 |
| 建议动作 | 后端给出的下一步只读建议 |

## 10. 操作员使用方法

1. 先打开账号页面，点击“号池实时看板”。
2. 看总览卡片：优先关注“需人工介入”、“限流冷却”、“证据不足”、“数据不足”。
3. 用筛选按钮查看具体账号。
4. 对于“限流冷却”：等待恢复，不连续点击健康检查。
5. 对于“证据不足”：回到账号级诊断面板，先运行时注册/映射，再按需健康检查。
6. 对于“需人工介入”：打开账号级诊断面板，看具体失败分类；如是 403/hold/KYC，不要自动恢复。
7. 对于健康的 production：继续观测，不需要例行健康检查。

## 11. 与账号列表的关系

账号列表仍是管理入口：创建、编辑、测试、诊断、上号、恢复都在账号列表或账号级诊断面板完成。

实时看板只解决“看得清楚”的问题，不解决“直接操作”的问题。这样可以避免把只读监控和有副作用的修复动作混在一起。

## 12. 验收要求

- 账号列表不再被过度挤压，并保持原管理行为。
- 看板独立全屏打开。
- 打开时自动刷新，关闭时停止刷新。
- 后端返回所有正式号池账号，不受分页影响。
- 状态由后端统一分类。
- 429 显示为限流/等待恢复。
- 401/403/hold/KYC/risk 显示为需人工介入。
- `missing_account_identity` / `missing_egress_bucket` 显示为运行时证据链问题，不误导成账号被封。
- 缺少证据或运行数据时不显示正常。
- 看板只读，不发真实上游请求。
- 敏感扫描为 0 findings。

建议验证范围：

```bash
cd backend
go test ./internal/service ./internal/handler ./internal/server/routes -run 'FormalPool|StatusDashboard|Account|RateLimit|DTO' -count=1 -timeout=240s

cd ../frontend
npm run test:run -- FormalPoolStatusDashboardModal formalPoolStatusDashboard AccountsView.bulkEdit
npm run typecheck

cd ..
python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
python3 tools/safe_deliverable_sensitive_scan.py --root docs/anti-ban/43-formal-pool-status-dashboard.md --max-findings 100
git diff --check
```

通过上述验证只表示本地实现和文档满足看板验收；不代表可以部署生产，不代表可以发真实 directed 健康检查，也不代表可以修改生产账号状态。生产部署、真实健康检查和生产数据变更仍需单独明确批准。
