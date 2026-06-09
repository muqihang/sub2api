# 逐梦 Agent Claude Code Native 接管 CP7 Operator Runbook

日期：2026-06-09
状态：DRAFT v1
适用阶段：CP7 UI / Runbook / Operator 状态
来源计划：`docs/anti-ban/46-zhumeng-agent-claude-code-native-takeover-plan.md`

## 0. 执行摘要

本 runbook 用于指导逐梦 Agent CLI/UI 操作员在 **Claude Code CLI native takeover** 阶段启动、检查、排障和采集证据。CP7 的目标不是新增服务端兼容逻辑，也不是发起真实上游验证，而是把前序 CP1-CP6 的本机 launcher、loopback guard、process netwatch、capability profile、native attestation 与 mock/localhost shape healthcheck 以可运营状态呈现出来。

CP7 必须展示并解释以下状态：

| 状态 key | 中文含义 | 是否可继续使用 | 操作员动作 |
| --- | --- | --- | --- |
| `ready` | 已准备，可启动接管 | 可以启动 | 执行启动前检查，确认 localhost/mock 边界 |
| `running` | 接管运行中 | 可以继续 | 观察 guard、netwatch、profile 和 mock healthcheck 摘要 |
| `guard_bypass` | 发现疑似绕过本机 guard 的网络目的地 | 不可继续 | 立即停止/隔离当前 session，采集脱敏证据 |
| `profile_mismatch` | 本机 profile 与服务端/healthcheck 期望不一致 | 不可继续 | 停止启动或重启接管模式，刷新 profile 后复查 |
| `toolsearch_degraded` | ToolSearch / `tool_reference` / `defer_loading` 能力降级 | 可诊断，不可默默生产 | 降级到安全 profile，记录风险并等待 CP8 复验 |
| `quarantined` | 已隔离 | 不可继续 | 只允许查看脱敏证据和执行显式修复流程 |

## 1. 硬边界

本 runbook 继承 46 号计划的核心边界，并作为 CP7 操作界面的显式提示文案来源。

### 1.1 本阶段只允许 localhost/mock

- 只允许连接本机 loopback guard、localhost mock Sub2API、localhost mock CC Gateway 或测试替身。
- 不发送真实 Anthropic / Claude 请求。
- 不执行真实 directed healthcheck；新号健康检查属于 40/41 号正式号池流程，不属于 CP7。
- 不把 `native_takeover_staging` 或 `native_takeover_production` 名称误解为真实上游放量；CP7 验收仍是 localhost/mock only。

### 1.2 本机敏感信息禁止触碰

操作员和实现均不得：

- 读取、上传、导出或复制默认 `~/.claude` 中的 OAuth、cookie、setup-token、session 文件。
- 保存 raw token、raw prompt、raw body、raw telemetry、raw CCH、email、account/org UUID、proxy credential。
- 在截图、工单、日志、report、UI toast、测试快照中展示任何原始凭据或用户提示内容。
- 使用用户 shell 继承的 `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_BETAS`、`HTTP_PROXY`、`HTTPS_PROXY` 等旧环境作为可信输入。

### 1.3 本阶段不做的事情

- 不做 GPT / DeepSeek / 多供应商模型注入 Claude Code；该议题属于后续 47 号。
- 不 patch Claude Code CLI 源码，不篡改 messages body。
- 不复用 messages CCH signing 处理 control-plane。
- 不修改 44 号 compat adapter、Codex Gateway、DeepSeek、AGNES、Codex Desktop Gateway 专项逻辑。
- 不用全局系统代理或无提示 MITM。

## 2. 与 44 号 compat 的区分

46 号 native takeover 与 44 号非 Claude Code 客户端 compat adapter 是两条不同路径，CP7 UI 必须清楚显示来源和处理策略。

| 路径 | client_type | 来源证明 | 处理方式 |
| --- | --- | --- | --- |
| 44 号 compat | `claude_code_compat` | 非 Claude Code Anthropic 客户端，由服务端补 shape | 服务端 compat adapter 填充 Claude Code 兼容字段 |
| 46 号 native | `claude_code_native` | 逐梦 Agent 启动真实 Claude Code CLI，并有 guard attestation | 保留 CLI body，经 guard -> Sub2API -> CC Gateway sign-primary |
| 未证明 beta | `untrusted_beta` | 外部请求或缺少 attestation | strip/reject/fail closed |

操作员看到 native session 时，应检查：

- `native_attested=true`。
- `local_session_ref` 为 opaque ref，不含路径、邮箱或账号 UUID。
- `guard_version`、`claude_code_version_family`、`profile_id` 均为安全摘要。
- 没有 “server-filled shape” 提示；如果出现，按 `profile_mismatch` 处理。

## 3. 状态机与 UI 展示要求

### 3.1 状态总览

```text
ready -> running -> ready
ready -> profile_mismatch -> quarantined/manual_fix
running -> guard_bypass -> quarantined
running -> toolsearch_degraded -> ready/profile_refresh
running -> profile_mismatch -> quarantined
quarantined -> manual_fix -> ready
```

UI/CLI 应展示：

- 当前状态 key 与中文解释。
- 是否允许继续启动/继续运行。
- 最近一次状态变化时间。
- safe evidence refs：`local_session_ref`、`guard_summary_ref`、`netwatch_summary_ref`、`profile_ref`、`mock_healthcheck_ref`。
- 下一步推荐动作，且不得自动触发真实上游请求。

UI/CLI 不得展示：raw token、raw prompt、raw body、raw telemetry、raw CCH、email、account/org UUID、proxy credential、完整本机私密路径。

### 3.2 `ready`

定义：本机接管前置条件已通过，但尚未运行 Claude Code CLI session。

必须满足：

- guard 配置指向 `127.0.0.1` 或 `localhost`。
- isolated `CLAUDE_CONFIG_DIR` 已准备，且不是默认 `~/.claude`。
- capability profile 已加载，包含 `profile_id`、`tool_search_mode`、`fgts_mode`、`control_plane_policy_version`、`capture_level`、`netwatch_required`。
- inherited env 已完成 allowlist + scrub。
- mock Sub2API / mock CC Gateway 端点可用，且不是官方 Anthropic/Claude 域名。
- netwatch 可启动，且不会记录 payload/header。

UI 建议文案：

```text
状态：ready / 已准备
说明：Claude Code native 接管前置检查已通过。本阶段仅允许 localhost/mock，不会发送真实 Anthropic/Claude 请求。
下一步：启动接管 session。
```

### 3.3 `running`

定义：Claude Code CLI 已由逐梦 Agent 显式启动，messages 和 control-plane 均经过本机 guard 或被 netwatch 观察。

必须持续展示：

- guard loopback endpoint bucket。
- messages route summary：method、route template、status bucket、body keys、tools_count、messages_count、thinking/context flags。
- control-plane intent summary：bucket、route template、action（suppress/stub/shadow/block）、attestation valid/invalid。
- process netwatch destination bucket：loopback、private、public、anthropic_like、unknown。
- ToolSearch mode 与最近 mock healthcheck 结果。

UI 建议文案：

```text
状态：running / 接管运行中
说明：当前 session 由逐梦 Agent 启动，messages 经本机 guard 转发到 mock 链路；control-plane 仅生成 safe intent 或本地 stub/block。
```

### 3.4 `guard_bypass`

定义：netwatch 或 guard 发现 Claude Code 进程树存在疑似绕过 guard 的目的地，尤其是 Anthropic/Claude 官方域、未知公网 CONNECT、或 messages/control-plane 没有经过 loopback guard。

触发信号：

- `process-netwatch.jsonl` 中出现 `potential_guard_bypass=true`。
- 目的地 bucket 为 `anthropic_like`、`public_unknown` 或未被 profile 允许的 host bucket。
- `/v1/messages` 通过 CONNECT 或非 loopback 路径出现。
- guard 未收到对应 request，但 netwatch 看到外联。

必须动作：

1. fail closed：停止当前 session 或阻止继续启动。
2. 标记 local session risk。
3. 生成脱敏 evidence：destination bucket、process tree safe shape、时间桶、guard route absence summary。
4. UI 状态进入 `guard_bypass`，随后进入 `quarantined`，直到人工确认。
5. 不自动重试真实上游，不切 direct fallback。

UI 建议文案：

```text
状态：guard_bypass / 发现疑似绕过 guard
处理：已安全停止。请导出脱敏证据并检查 base URL、proxy env、guard 端口、control-plane policy。不要重试真实上游。
```

### 3.5 `profile_mismatch`

定义：本机 capability profile、服务端 profile、mock healthcheck profile 或 Claude Code version family 不一致，导致 native path 无法证明。

触发信号：

- `profile_id` 与 mock healthcheck 期望不匹配。
- `claude_code_version_family` 不在 known/candidate/gray/kill switch 允许集。
- `tool_search_mode`、`fgts_mode`、`control_plane_policy_version` 与服务端下发不一致。
- `ANTHROPIC_BASE_URL`、`CLAUDE_CODE_API_BASE_URL`、proxy env 不是由 Agent profile 生成。
- native path 被错误标记为 44 compat server-filled shape。

必须动作：

1. fail closed：不启动或停止当前 session。
2. 清理本次 Agent 生成的临时 env，不读取默认 `~/.claude`。
3. 刷新 mock profile 或切换到 safe fallback profile。
4. 重新运行 localhost-only doctor 和 shape healthcheck。
5. 如果仍不一致，进入 `quarantined`。

UI 建议文案：

```text
状态：profile_mismatch / Profile 不一致
处理：当前 native 证据不足，已阻止继续运行。请刷新 capability profile 并重新执行 localhost-only healthcheck。
```

### 3.6 `toolsearch_degraded`

定义：ToolSearch / `tool_reference` / `defer_loading` 相关能力未达到 profile 预期，但未发现 direct bypass 或敏感泄露。

触发信号：

- ToolSearch fixture 在 mock healthcheck 中失败。
- `tool_reference_present=false` 或 `defer_loading_present=false`，但 profile 期望为 true。
- `ENABLE_TOOL_SEARCH=true` 仅在 healthcheck 通过后才允许；未通过时强行启用。
- 上游替身返回 ToolSearch shape 相关 400 bucket。

必须动作：

1. 不自动补写或伪造 CLI body。
2. 降级到 `tool_search_mode=auto` 或 `standard` safe profile。
3. 记录 safe risk event：版本族、profile ref、fixture ref、字段存在性布尔值。
4. 允许继续进行本机诊断，但不得把该 session 标记为生产可用。
5. 等 CP8 完成 shape healthcheck 后再解除降级。

UI 建议文案：

```text
状态：toolsearch_degraded / ToolSearch 能力降级
处理：已切换到安全 profile。不会改写 Claude Code body，也不会发送真实请求。请查看 mock fixture 和 profile 差异。
```

### 3.7 `quarantined`

定义：session、profile 或账号/出口映射相关证据已被隔离，不能继续运行。

进入条件：

- `guard_bypass`。
- `profile_mismatch` 修复失败。
- unknown control-plane drift。
- verifier fail / fallback / signing strip bucket。
- proxy mismatch / unavailable bucket。
- 401/403/risk/hold bucket。
- sensitive scan 检测到 raw sensitive artifact。

允许动作：

- 查看脱敏 evidence refs。
- 重新执行 localhost-only doctor。
- 修复 profile / guard / mock endpoint 配置。
- 人工确认后重新进入 `ready`。

禁止动作：

- 直接进入 `running`。
- 真实上游重试。
- 手工把 session 标为健康。
- 导出 raw 日志或用户本机 token/cookie/setup-token。

## 4. 启动流程

### 4.1 启动前检查

操作员在 CLI/UI 点击“启动 Claude Code 接管”前，逐梦 Agent 必须完成以下检查：

1. 检测 `claude` 可执行文件与版本族，输出 safe version family。
2. 创建 isolated config 目录，确认不是 `~/.claude`。
3. 构造 clean env：只继承 allowlist，scrub token/cookie/proxy/beta/base-url 等敏感或旧环境。
4. 启动 loopback guard，确认端口绑定在 `127.0.0.1` 或 `localhost`。
5. 设置 `ANTHROPIC_BASE_URL` / `CLAUDE_CODE_API_BASE_URL` 到 guard loopback。
6. 将 proxy env 指向 guard 或按 CP2 设计处理，确认 guard 自身不会 loopback recursion。
7. 启动 process netwatch。
8. 拉取或加载 mock capability profile。
9. 运行 localhost-only doctor。
10. 确认 UI 状态为 `ready`。

### 4.2 启动命令语义

推荐 CLI 语义（示例为操作语义，不要求 CP7 必须实现同名命令）：

```bash
zhumeng-agent claude-code doctor --mode localhost-mock
zhumeng-agent claude-code start --mode localhost-mock --project <workspace-ref>
zhumeng-agent claude-code status --session <local-session-ref>
```

命令输出只能包含 safe refs 和摘要。例如：

```text
state=ready
mode=localhost-mock
guard=loopback
profile_ref=profile:opaque
netwatch=required
upstream=mock
sensitive_boundary=raw_forbidden
```

不得输出真实端口以外的完整代理凭据、token、prompt、body、telemetry 或本机私密路径。端口本身可显示，但建议 UI 用 loopback bucket + session ref 表达。

### 4.3 运行中检查

`running` 状态下，操作员每次刷新只看摘要：

- guard alive：yes/no。
- messages seen：count bucket。
- control-plane intents：bucket/action counts。
- netwatch bypass：yes/no。
- ToolSearch profile：expected/effective。
- mock healthcheck freshness：fresh/stale。
- sensitive scan：pass/fail。

若任一 P0 信号出现，立即 fail closed。

## 5. 故障处理 SOP

### 5.1 Guard 未启动或端口不是 loopback

分类：P0，fail closed。

处理：

1. 不启动 Claude Code CLI。
2. 检查 guard bind host，只允许 loopback。
3. 检查端口冲突并重启 guard。
4. 确认 `ANTHROPIC_BASE_URL` 由 Agent 生成。
5. 重跑 doctor；通过后回到 `ready`。

不要：切到官方 base URL、direct fallback、全局系统代理。

### 5.2 Claude Code 使用了默认 `~/.claude`

分类：P0，fail closed。

处理：

1. 停止当前 session。
2. 确认 isolated `CLAUDE_CONFIG_DIR` 生成逻辑。
3. 确认没有读取、复制、上传默认 `~/.claude` 内容。
4. 重新创建 isolated profile。
5. 重跑 sensitive scan 与 doctor。

### 5.3 发现 direct Anthropic / Claude bypass

分类：P0，状态 `guard_bypass` -> `quarantined`。

处理：

1. 停止 session。
2. 保存 netwatch safe summary：destination bucket、process shape、time bucket。
3. 检查 inherited proxy env、NO_PROXY、hardcoded control-plane path、CONNECT 分类。
4. 修复后只允许 localhost/mock 复测。

不要：为了验证而再发真实请求。

### 5.4 Unknown control-plane drift

分类：P0/P1，默认 fail closed。

处理：

1. guard 对 unknown route block/quarantine。
2. 记录 safe drift summary：method、host bucket、route template、header names、schema summary、auth presence shape。
3. 不上传 raw telemetry/body。
4. 人工审查后才可新增 policy bucket。

### 5.5 Profile mismatch

分类：P1，默认 fail closed。

处理：

1. 停止启动或停止 session。
2. 对比本机 profile ref、server/mock profile ref、healthcheck profile ref。
3. 若 Claude Code 版本族未知，进入 candidate_review 或 kill switch，不自动放行。
4. 切换 safe fallback profile 后重跑 doctor。

### 5.6 ToolSearch degraded

分类：P1，不等同于安全 bypass。

处理：

1. 确认 fixture 是否覆盖 MCP/deferred tools、pending MCP server、disallowed tools、model support。
2. 将 `ENABLE_TOOL_SEARCH=true` 降级为 `auto` 或 `standard`，除非 healthcheck 明确通过。
3. 不 strip `tool_reference` / `defer_loading`；不伪造 body。
4. 在 CP8 中复跑 native ToolSearch fixtures。

### 5.7 Verifier fail / fallback / signing strip

分类：P0，fail closed。

处理：

1. 停止 session。
2. 记录 verifier_summary safe ref。
3. 检查是否误走 44 compat 或 untrusted beta。
4. 检查 CC Gateway mock route、persona/profile resolver、sign-primary mock verifier。
5. 修复前保持 `quarantined`。

### 5.8 401 / 403 / risk / hold / proxy mismatch

分类：P0，fail closed。

CP7 不应发真实上游；若 mock 或测试替身产生这些 bucket，只用于验证状态机行为。

处理：

1. 进入 `quarantined`。
2. 只记录状态桶和 safe refs。
3. 不刷新真实凭据，不运行真实 directed healthcheck。
4. 若该信号来自正式号池流程，应转交 40/41/42 号 SOP，而不是在 CP7 修复。

### 5.9 Sensitive scan fail

分类：P0，fail closed。

处理：

1. 停止 session。
2. 标记本地 capture 不可导出。
3. 定位 raw sensitive artifact 的文件和字段类型；报告中只写字段类型和 safe file ref。
4. 清理策略需遵守本机安全策略；涉及删除文件必须先征得用户确认。
5. 修复后重跑 sensitive scan，PASS 才可回到 `ready`。

## 6. Operator 证据采集

### 6.1 证据目录

生产化建议目录继承 46 号计划：

```text
~/.zhumeng/claude-code-native/
  captures/<YYYYmmdd-HHMMSS>/
    guard-summary.jsonl
    process-netwatch.jsonl
    run-metadata.json
    report.json
    report.md
    README.md
```

CP7 文档和 UI 只引用 opaque evidence ref；不要把本机完整路径作为公开工单字段。

### 6.2 允许采集字段

- route、method、status bucket。
- header names，不含 header values。
- auth presence shape，例如 `authorization_present=true`，不含值。
- body keys、schema tree、类型、长度桶、数组计数。
- model name、max_tokens 数值。
- tools_count、messages_count。
- thinking/context/output_config 是否存在。
- control-plane route template。
- telemetry event name 枚举，不含 raw event body。
- netwatch destination bucket。
- scoped opaque refs。
- guard/control-plane attestation valid/invalid。

### 6.3 禁止采集字段

- raw token / API key / OAuth / cookie / setup-token。
- raw prompt。
- raw request/response body。
- raw telemetry。
- raw CCH。
- email。
- account/org UUID 明文。
- proxy credential。
- 本机文件内容或项目私密路径，除非 bucket/ref 化。

### 6.4 证据包最小清单

操作员提交 CP7 审查时，证据包应包含：

1. `run-metadata` safe summary：mode、state timeline、version family、profile ref。
2. `guard-summary`：messages/control-plane 摘要与 action counts。
3. `process-netwatch`：destination bucket 摘要与 bypass=false/true。
4. `profile-summary`：ToolSearch/FGTS/control-plane policy/capture level。
5. `mock-healthcheck-summary`：fixture refs、freshness、pass/fail buckets。
6. `sensitive-scan-summary`：findings count 与 pass/fail。
7. `operator-actions`：点击/命令动作、时间桶、结果状态。

审查材料不得包含 raw artifacts；如需要定位问题，追加 safe ref 和字段类型，不追加原文。

## 7. Fail-closed 行为矩阵

| 条件 | 状态 | 动作 | 是否允许自动重试 |
| --- | --- | --- | --- |
| guard 未启动或非 loopback | `quarantined` | 阻止启动 | 否 |
| 默认 `~/.claude` 被使用 | `quarantined` | 停止 session | 否 |
| base URL 未指向 guard | `profile_mismatch` | 阻止启动 | 否 |
| Anthropic/Claude direct egress | `guard_bypass` | 停止并隔离 | 否 |
| unknown control-plane drift | `quarantined` | block/quarantine | 否 |
| profile 不受支持 | `profile_mismatch` | 刷新/降级 profile | 仅 localhost/mock doctor |
| verifier fail / fallback | `quarantined` | 停止并记录 safe ref | 否 |
| proxy mismatch / unavailable | `quarantined` | 停止，转正式号池 SOP | 否 |
| 401/403/risk/hold bucket | `quarantined` | 停止，转正式号池 SOP | 否 |
| raw sensitive artifact | `quarantined` | 停止，修复并重扫 | 否 |
| ToolSearch fixture fail | `toolsearch_degraded` | 降级 profile | 仅 localhost/mock fixture |

Fail-closed 的共同要求：

- 停止当前 Claude Code 启动或提示重启接管模式。
- 标记 local session risk。
- 写入 safe risk_event/ref。
- 不自动重试真实上游请求。
- 不 fallback 到 direct official host。

## 8. CP8 验证衔接

CP7 完成后，CP8 应基于本 runbook 的状态和证据包执行最终验证与审查。CP7 不替代 CP8。

CP8 需要重点验证：

- Python tests：Agent launcher/profile/guard/netwatch/capture/toolsearch profile。
- Go targeted tests：native attestation、control-plane intent、compat/native 区分、session/account gates。
- CC Gateway tests：persona/model resolver、sign-primary verifier、raw-safe audit、egress bucket。
- Native shape healthcheck fixtures：no tools、tools + thinking、MCP/deferred tools、ToolSearch、count_tokens、stream、Opus/Sonnet、control-plane safe intent、netwatch。
- Sensitive scan findings=0。
- 审查代理确认：没有真实 Anthropic/Claude 请求，没有 raw sensitive 保存，没有误触 44 compat 或多供应商注入。

CP7 提供给 CP8 的最小交接项：

```text
state_timeline_ref
operator_action_summary_ref
guard_summary_ref
netwatch_summary_ref
profile_summary_ref
mock_healthcheck_summary_ref
sensitive_scan_summary_ref
known_degraded_capabilities
open_risk_events
```

## 9. 操作员检查清单

### 9.1 启动前

- [ ] 已确认本阶段为 localhost/mock only。
- [ ] 没有读取、上传或导出 `~/.claude` OAuth/cookie/setup-token。
- [ ] isolated `CLAUDE_CONFIG_DIR` 已准备。
- [ ] env scrub 已完成。
- [ ] guard 为 loopback。
- [ ] mock endpoints 不是官方 Anthropic/Claude 域。
- [ ] netwatch required 且已启动。
- [ ] capability profile 已加载。
- [ ] UI 状态为 `ready`。

### 9.2 运行中

- [ ] 状态为 `running`。
- [ ] messages 经过 guard。
- [ ] control-plane 只有 safe intent / suppress / stub / shadow / block。
- [ ] netwatch bypass=false。
- [ ] 没有 raw sensitive artifact。
- [ ] ToolSearch 状态与 profile 一致，或已标记 `toolsearch_degraded`。

### 9.3 交付 CP8 前

- [ ] 已导出 safe evidence refs。
- [ ] sensitive scan PASS。
- [ ] 未发真实 Anthropic/Claude 请求。
- [ ] `claude_code_native` 与 `claude_code_compat` 区分清楚。
- [ ] fail-closed 状态均有 operator action summary。
- [ ] open risks 已列入 CP8 审查。

## 10. 禁止事项速查

不要：

- 把本机 `~/.claude` 内容作为接管输入。
- 在日志、文档、截图、工单里粘贴 token、cookie、setup-token、prompt、body、telemetry、CCH、email、account/org UUID、proxy credential。
- 将 `guard_bypass`、`profile_mismatch`、`quarantined` 手工改成 `running`。
- 为了排障切到官方 host 或 direct fallback。
- 把 44 compat adapter 当作 native takeover 的证明。
- 在 CP7 中做 GPT/DeepSeek/多供应商模型注入。
- 用真实上游请求证明 UI 状态。

可以：

- 使用 localhost/mock fixture 验证状态机。
- 导出脱敏 evidence refs。
- 降级 capability profile 进行本机诊断。
- 将无法解决的问题交给 CP8 审查。
