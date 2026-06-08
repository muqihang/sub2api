# 正式号池 Session / Account / User Budget 策略设计

日期：2026-05-24
状态：联合验收 Phase 0 修复稿；只允许进入 Phase 0 固化，不允许直接 Phase 1+ 生产实施
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`
关联组件：Sub2API、CC Gateway、local Claude Code guard、control-plane intent、dynamic persona resolver、synthetic telemetry

## 0. 执行结论

正式号池不能沿用 canary 阶段的 `max_messages=1` hard gate。`max_messages=1` 只适合“单条真实上游验证”：它证明了 Claude Code CLI-through、CC Gateway sign-primary、verifier、post-sign mutation、no fallback、proxy egress 和富能力请求形态可以成功通过，但它会直接破坏真实 Claude Code 的正常多轮、工具循环、thinking、stream、context management 和长上下文使用。

正式策略应改为：

```text
默认 observe-only + P0 明显异常 hard block + 多维 soft budget + account health 联动 + 灰度/回滚/kill switch。
```

Budget 是安全气囊，不是能力阉割。生产默认不应硬性限制正常 Claude Code 能力；只能硬拦 verifier 失败、post-sign mutation、fallback、persona/CCH/proxy/route 错配、风控文本、401/403/KYC、429 后无冷却重试、明显工具死循环、重试风暴、不可信客户端伪造 Claude Code headers、raw token/body/prompt/CCH 落盘风险等明确异常。

## 1. 已有证据边界

### 1.1 真实 CLI-through 能力证据

脱敏 field audit 显示已成功通过一条真实 Claude Code CLI-through `/v1/messages?beta=true` 请求：

- upstream status：200；
- model：`claude-sonnet-4-6`；
- `max_tokens=32000`；
- tools=30；
- `thinking` present；
- `context_management` present；
- `stream=true`；
- body top-level keys 包含 `context_management`、`max_tokens`、`messages`、`metadata`、`model`、`output_config`、`stream`、`system`、`thinking`、`tools`；
- persona：`claude-cli/2.1.150 (external, sdk-cli)`、`anthropic-version=2023-06-01`、`x-app=cli`、Stainless headers present；
- verifier PASS；
- post-sign mutation PASS；
- fallback false；
- rate-limit headers 可见，后续 account budget 必须消费这些脱敏状态。

这些事实说明：生产 budget 不能把 30 tools、thinking、context management、stream、`max_tokens=32000`、Sonnet/Opus Claude Code 模型机械视为异常。

### 1.2 Canary hard gate 的适用范围

`docs/anti-ban/30-*` 和 runtime productization 已把 `max_messages=1` 定义为 canary-only 控制：

- 单条真实请求验证；
- 第一条完成后停止 CLI；
- no retry / no concurrency / no fallback；
- 成本包络门禁；
- local guard / CC Gateway / Sub2API 的安全链路验证。

正式号池必须允许多轮 conversation、工具调用/工具结果循环、thinking、多次 stream chunk、长时间 session 和正常 retry。把 canary hard gate 直接迁移到生产会导致正常 Claude Code 基本不可用。

### 1.3 当前代码风险

早期 `tools/cli_session_budget.py` prototype 曾偏向 canary/fixture（例如低 messages/rich/thinking 数字），早期 runtime productization 也曾把 production-session 误接到 canary 风格产物。Phase 0 修订后，production-session 必须是 observe-only、显式 production upstream mode，且不能继承 canary envelope/body cap。CC Gateway 的 `canary-cost-gate.ts` 仍明确保持 canary-only。

这些数字只能作为测试夹具或 canary guardrail，不能作为正式生产默认 hard limit。正式生产需要 observe-only 真实数据校准后才可灰度提升为 soft/hard budget。

## 2. 为什么需要 session/account/user budget

正式号池需要 budget，并不是为了限制 Claude Code 能力，而是为了保护多用户共享正式账号池：

1. **替代 canary hard gate**：从 `max_messages=1` 切换到 session-aware、account-aware、user-aware 的风险控制。
2. **允许真实 Claude Code**：保留多轮 messages、工具循环、thinking、1m context、Opus/Sonnet、高 `max_tokens=32000`、stream 和 context management。
3. **控异常和成本**：识别死循环、重试风暴、异常 body/tool_result、无人值守长循环、跨账号扩散、高成本模型异常集中使用。
4. **保护账号池**：处理账号级 429 冷却、401/403/KYC/unusual activity/risk text、refresh 状态、proxy 出口一致性、account quarantine。
5. **保护多用户隔离**：避免用户 A 的异常使用拖垮用户 B 共用的账号，避免一个用户横向扩散消耗多个账号。
6. **支撑控制面生产化**：把 bootstrap/settings/MCP/registry/event_logging/eval/synthetic telemetry 的节奏纳入同一账号健康和风险体系。

## 3. 不能拍脑袋设置低阈值

以下数字不得作为生产默认 hard limit：

```text
最多 20 次工具调用
最多 30 条 messages
最多 10 次 thinking
```

原因：

- 已有真实成功请求包含 30 tools；“20 tools”会误拦已验证的真实能力。
- Claude Code 正常执行复杂项目可能超过 30 条 messages 或多轮 tool_use/tool_result。
- thinking 的出现次数与任务复杂度、模型行为和 CLI 版本有关，不能没有观测就硬拦。
- `context_management`、1m context、大项目上下文和 `max_tokens=32000` 是正式能力，不是异常。

生产默认必须是：

- observe-only 首发；
- 只对 P0 明显异常 hard block；
- 高水位异常熔断，而非低阈值能力阉割；
- account health、proxy health、user risk、session risk 多维联动；
- 所有阈值灰度、可回滚、有 kill switch；
- 阈值来自真实运营数据、safe fixtures、localhost-only replay 和灰度结果。

## 4. Budget 维度

### 4.1 User budget

用户维度用于保护共享池公平性和防滥用，不直接削弱单个合法 session 的能力。

观测指标：

- 单用户并发 active sessions；
- 单用户短时间请求频率、stream open 数、inflight requests；
- 单用户跨账号扩散：单位时间触达账号数、proxy buckets 数、route contexts 数；
- 单用户异常失败率：400/401/403/429/5xx、verifier fail、persona mismatch、intent invalid；
- 单用户高成本模型使用：Opus、thinking model、1m context、`max_tokens=32000`、长 stream、超大 body/tool_result；
- 单用户 retry 行为：429 后是否等待 cooldown、同一 payload shape 是否重复风暴；
- 单用户 control-plane intent 频率和 unknown path 频率。

默认行为：observe-only。超出软水位时排队、降低并发、延迟调度、要求复用现有 session/account；只有明显异常并发、重试风暴、伪造 headers、raw sensitive 尝试落盘等才 hard block。

### 4.2 Session budget

Session 维度用于判断“一个 Claude Code 会话是否进入异常自动循环”。

观测指标：

- messages 序列长度、role 序列、assistant/tool/user 转换；
- tool_use / tool_result 轮次、连续工具轮次、同一 tool name/category 重复、tool_result 是否缺失或重复；
- thinking 出现次数、thinking shape、thinking budget tokens bucket；
- `context_management` 是否出现、context clear/summarize 行为；
- body size bucket、system bytes bucket、messages content blocks bucket；
- tool definitions size、tool_result size bucket、tool_result blocks count；
- stream enabled、stream duration bucket、stream disconnect/reconnect；
- session wall-clock 时长、idle time、无人输入持续自动循环时长；
- retry 行为：同 session 同 route_context 下重复失败、429 后立即重试；
- model/max_tokens/output_config/model capability envelope；
- risk score 及触发原因。

默认不 hard block 正常多轮、正常工具多次调用、thinking、context management、stream、1m context、Opus、`max_tokens=32000`。只在明显工具死循环、超大异常 body/tool_result、长时间无人输入自动循环、重试风暴等场景升级。

### 4.3 Account budget

账号维度是正式号池安全核心。

观测指标：

- 每个 Claude 账号 inflight messages、open streams、control-plane inflight；
- 账号级 429 次数、rate-limit header bucket、reset 时间、5h/7d utilization bucket；
- 必须摄入真实响应中已观察到的 rate-limit header 名称，且只存 bucket/enum：`anthropic-ratelimit-unified-status`、`anthropic-ratelimit-unified-5h-status`、`anthropic-ratelimit-unified-5h-reset`、`anthropic-ratelimit-unified-5h-utilization`、`anthropic-ratelimit-unified-7d-status`、`anthropic-ratelimit-unified-7d-reset`、`anthropic-ratelimit-unified-7d-utilization`、`anthropic-ratelimit-unified-representative-claim`、`anthropic-ratelimit-unified-fallback-percentage`、`anthropic-ratelimit-unified-reset`、`anthropic-ratelimit-unified-overage-disabled-reason`、`anthropic-ratelimit-unified-overage-status`；
- 401/403、refresh 失败、token lifecycle、setup-token/OAuth 状态；
- unusual activity / KYC / risk text detector；
- proxy 出口一致性：账号绑定 egress bucket、出口 IP/ASN/geolocation drift、proxy credential 是否错配；
- account/proxy binding mismatch、route_context mismatch；
- account quarantine/cooldown 状态；
- 同账号多用户并发公平性和 blast radius；
- control-plane path 429/403/risk/schema drift。

默认行为：429 进入 cooldown；high utilization / reset 临近先进入 soft cooldown、降并发、排队或换账号，不直接 hard block；401/403/risk/KYC/unusual activity 进入 quarantine 或人工复盘；proxy/account mismatch hard block；refresh 状态异常暂停账号调度。

Multi-user fairness rule：

- 同一账号被多个用户共享时，inflight quota 按 active user 和成本权重做 weighted-fair 分配，避免单用户占满账号；
- 账号 utilization 接近高水位时，优先让高成本或高失败率用户排队/降并发，而不是直接 hard block 正常用户；
- session 迁移到健康账号只允许在 user/persona/profile 兼容、没有正在进行的 stream、没有连续 tool loop 的安全点发生；
- 账号级 risk text、KYC、unusual activity 或 401/403 出现时整账号 quarantine，所有共享用户收到脱敏 quarantine reason，不允许只挡某一个用户而继续消耗高风险账号。

### 4.4 Model budget

Model 维度不用于机械禁止高能力模型，而用于高成本和新模型灰度。

必须覆盖：

- `claude-sonnet-4-6`；
- `claude-opus-4-6` / `claude-opus-4-6-thinking`；
- `claude-opus-4-7` / `claude-opus-4-7-thinking`；
- 未来 Sonnet/Opus 新版本；
- thinking 模型；
- 1m context profile / 1m capable capability flag；
- `max_tokens=32000`；
- 高成本模型预算。

策略：

- 已知 Claude Code model family 正常放行；
- future trusted CLI-through 新模型进入 candidate_model_allowlist、localhost replay、灰度账号池、per-model kill switch、审计增强；
- 限制只能是流量、并发、账号预算、灰度比例和告警，不能删除 1m/tools/thinking/context_management/stream/max_tokens 能力；
- 必须区分 `context_1m_capable`（账号/profile/model 具备 1m 能力）与 `observed_context_1m_token_present`（单次请求 beta 中实际出现 1m token）；真实成功请求可以是前者可用但后者为 false；
- untrusted client 伪造 model/UA/beta fail closed。

### 4.5 Control-plane budget

控制面 budget 管 bootstrap/settings/MCP/registry/event_logging/eval/synthetic telemetry 的上传频率、缓存和熔断。

覆盖：

- bootstrap；
- account settings；
- MCP servers；
- MCP registry；
- event_logging；
- eval；
- synthetic telemetry；
- control-plane intent 上传频率、attestation、cache 命中、quarantine。

策略：

- 所有控制面先转 safe intent；
- safe GET/public query 可 account-scoped/public cached fetch；
- high-risk POST telemetry/eval 禁止 raw body，上游只允许 synthetic 或 suppress；
- unknown/drift path quarantine；
- 任一路径 400/401/403/429/risk/schema drift 按 path/account/profile 熔断；
- synthetic telemetry 使用独立 control-plane 子预算：`synthetic_telemetry_inflight`、`synthetic_telemetry_failure_window`、`synthetic_telemetry_event_family`；
- synthetic telemetry 失败只熔断对应 path/account/event family，不污染 main messages 的 session/account risk，除非响应中出现 risk/KYC/unusual activity 等账号级风险；
- control-plane 失败不得 fallback 到 messages/direct/legacy/sign-to-strip。


## 4.6 Pool Utilization Strategy：normal / aggressive 额度消耗策略

正式号池不是低利用率保守池。无论 normal 还是 aggressive，目标都是在安全边界内尽量充分使用订阅额度；区别只在目标消耗曲线、调度权重和追赶/刹车强度。两类策略共用同一套 P0 hard block、安全脱敏、persona/model resolver、control-plane policy、proxy/account binding、429 cooldown 和 quarantine 规则。aggressive 不能降低安全标准，只能提高合规流量调度优先级和目标利用率。

### 4.6.1 两类 pool profile

| Profile | 目标 | 默认风险姿态 | 调度倾向 | 禁止行为 |
|---|---|---|---|---|
| `normal` | 7 天内消耗周额度的 90%-100%，可留少量安全余量 | health-first + smooth burn | 平滑分配真实用户流量；避免 5h 窗口尖峰；优先账号长期稳定 | 不低利用率闲置；不撞 429；不空跑烧额度 |
| `aggressive` | 3 天内消耗周额度的 95%-100%，尽量充分最大化利用 | high-utilization + strict safety | 更积极承接真实用户任务、高成本模型、长上下文和并发；按目标曲线追赶 | 不绕过 cooldown；不重试风暴；不伪造请求；不降低 P0 安全线 |

### 4.6.2 调度依据：以真实 utilization header 为主

公开 API 价格只能作为预估参考，不能作为正式订阅桶唯一依据。正式调度必须优先消费上游响应中可见的统一额度状态，并只以 bucket/enum/比例摘要进入 ledger：

```text
anthropic-ratelimit-unified-5h-status
anthropic-ratelimit-unified-5h-utilization
anthropic-ratelimit-unified-5h-reset
anthropic-ratelimit-unified-7d-status
anthropic-ratelimit-unified-7d-utilization
anthropic-ratelimit-unified-7d-reset
anthropic-ratelimit-unified-overage-status
anthropic-ratelimit-unified-overage-disabled-reason
```

调度器用这些字段判断账号是 behind target、on track、ahead target、cooldown、quarantine，而不是用 raw cost 或 raw response body。若 header 缺失或解析异常，只能降级为 observe-only / conservative scheduling，不得盲目 aggressive。

公开 API 单价在 Phase 1 不是前置依赖，也不得作为正式订阅桶的主调度依据。它只可在后续作为辅助权重，用来解释 Opus、thinking、1m context、长输出等请求相对更“重”；最终调度仍以 5h / 7d utilization 显示条、reset、overage/risk 状态和账号健康为准。

### 4.6.3 目标消耗曲线

定义：

```text
W = 账号一周可用额度，实际以 7d utilization header 为准
D = 目标消耗天数
T = 目标消耗比例
N5h = D * 24 / 5
target_5h_share_of_week = T / N5h
```

Profile 目标：

| Profile | D | T | 约等于每 5h 应消耗周额度 | 说明 |
|---|---:|---:|---:|---|
| `normal` | 7 天 | 90%-100% | 2.7%-3.0% | 平滑用完或接近用完一周额度 |
| `aggressive` | 3 天 | 95%-100% | 6.6%-6.9% | 3 天内尽量最大化用完周额度，但仍不撞风险线 |

该曲线是调度目标，不是硬打满命令。若 5h 窗口、账号健康、429 cooldown、risk text 或 proxy 状态不允许，安全状态优先于 burn target。

### 4.6.4 Ledger 必须新增的观测字段

`account_ledger` / `pool_ledger` 必须支持：

```text
pool_profile: normal|aggressive
target_window_days
target_weekly_utilization_range
target_5h_share_of_week_bucket
observed_5h_utilization_bucket
observed_7d_utilization_bucket
5h_reset_bucket
7d_reset_bucket
burn_curve_position: behind|on_track|ahead|unknown
burn_rate_error_bucket
catch_up_allowed boolean
slow_down_required boolean
account_weight_bucket
cooldown_state
quarantine_state
last_rate_limit_header_seen_at_bucket
```

这些字段只记录比例、bucket、enum、时间 bucket 和 scoped refs，不记录 raw token、raw body、raw prompt、raw CCH、email、account UUID、proxy credential 或 raw response body。

### 4.6.5 Catch-up / slow-down 策略

当账号低于目标曲线：

- `normal`：适度提高调度权重，优先分配普通真实用户任务，避免短时尖峰；
- `aggressive`：更积极提高调度权重，优先承接真实高成本任务、Opus、thinking、1m context、长任务和更多并发；
- 两者都可以从过载账号迁移新 session 到 behind target 的健康账号；
- 两者都不得制造无用户需求的 synthetic messages 或空跑请求来烧额度。

当账号高于目标曲线或接近 5h/7d 高水位：

- 降低 account weight；
- 新 session 排队或分配给其他账号；
- 等待 reset；
- 对 429 或 high utilization 进入 cooldown；
- 只在安全点迁移 session，不中断正在进行的 stream / tool loop。

### 4.6.6 与 Session Budget 的关系

Pool utilization strategy 不直接改写 Claude Code 请求，不删除能力，不静默降级模型。它只影响：

- account selector 权重；
- per-account 并发；
- 排队优先级；
- cooldown 时长；
- 新 session 放到哪个账号；
- control-plane / synthetic telemetry 的节奏预算。

它不得影响：

- `messages` body；
- tools 定义；
- `thinking`；
- `context_management`；
- `stream`；
- `max_tokens=32000`；
- 1m context；
- Sonnet / Opus / future trusted model 的能力集合。

### 4.6.7 P0 安全红线，normal/aggressive 共用

以下情况无论 profile 如何都必须 hard block / quarantine：

- verifier fail；
- post-sign mutation fail；
- direct / legacy / sign-to-strip fallback；
- persona/model/beta/session resolver mismatch；
- account/proxy/egress bucket mismatch；
- 401/403/KYC/unusual activity/risk text；
- 429 后无 cooldown 继续重试；
- control-plane attestation fail / nonce replay；
- unknown control-plane path/schema drift；
- raw token/body/prompt/CCH/telemetry/email/account UUID/proxy credential 试图落盘或进入 safe deliverable；
- 伪造 Claude Code headers/model/beta/session 的不可信客户端。

### 4.6.8 实施阶段要求

Phase 1 observe-only 必须先记录 normal/aggressive 两类 profile 的目标曲线与实际曲线，但不得执行 burn-rate hard enforcement。Phase 2 只加入 P0 hard block。Phase 3 才允许 soft budget 调整 account weight、queue、cooldown 和 catch-up/slow-down。任何从 soft 策略升级为 hard policy 的阈值，都必须有真实运营数据、误伤评估、kill switch 和发布审批。

## 5. 必须 hard block 的 P0 异常

以下事件必须 hard block 或 quarantine，不进入正常 soft budget：

### 5.1 签名、persona、route、trust 边界

- CCH verifier fail；
- post-sign mutation fail；
- direct fallback / legacy fallback / sign-to-strip fallback；
- persona mismatch：UA/beta/session/model/profile 与 resolver decision 不一致；
- CCH mismatch、billing marker 错配；
- route 不合法，尤其非 `POST /v1/messages?beta=true` 伪装为 messages；
- control-plane path 误调用 messages CCH；
- messages 路径外的请求若携带 CCH/billing marker，外部输入默认 reject；仅内部兼容/迁移路径允许 strip + audit；
- control-plane attestation fail、nonce replay、signature mismatch、timestamp invalid；
- external spoofed intent；
- 不可信客户端伪造 Claude Code headers / model / beta / session。

Trust gate 依赖项：trusted CLI-through dynamic persona 与 spoof 判定依赖 guard attestation checkpoint 完成。attestation 未就绪时，不允许把外部 UA/header 当作可信 Claude Code persona；生产 trusted route 必须 quarantine / fail closed，不能默认信任，也不能把缺失 attestation 的请求放进动态 persona 灰度。

### 5.2 凭证和隐私泄漏

- raw token、Authorization、x-api-key、cookie、proxy credential 试图上传、落盘或输出；
- raw body、raw prompt、raw telemetry、raw CCH 试图落盘或进入 safe deliverable；
- plain SHA/MD5/deterministic body hash 用于 production ledger；
- account UUID、email、org UUID、proxy credential 明文进入 audit/deliverable；
- fallback intent 比主路径更宽松。

### 5.3 账号池和上游风险

- 出口 IP / egress bucket 错；
- account/proxy 绑定错；
- selected account 与 route_context 不一致；
- 401/403/KYC/unusual activity/risk text；
- 429 后持续无冷却重试；
- refresh 状态恶化仍继续调度；
- account health quarantine 后继续使用。

### 5.4 明显异常行为

- 明显工具死循环：同一工具/同一安全摘要的 tool_result 无用户输入持续重复、无状态进展、持续失败；
- 明显重试风暴：短时间同一 user/session/account route 重复失败且无 backoff；
- 单用户异常并发；
- 单账号异常并发；
- 超大异常 body/tool_result，超过已观测正常能力多个数量级且无 1m/context 解释；
- 长时间自动循环无人输入且持续消耗高成本模型/工具。

## 6. 不能默认 hard block 的正常能力

以下不得作为生产默认 hard block 条件：

- 正常多轮 messages；
- 正常工具调用很多次；
- 正常 tool_result；
- thinking；
- context_management；
- stream；
- 1m context；
- `max_tokens=32000`；
- Opus；
- future Sonnet/Opus 新版本；
- 大但合理的项目上下文；
- assistant/tool/user 的正常交替；
- 长 stream 和长 session，只要有人类输入/状态进展/账号健康正常。

这些只能 observe、soft budget、account-health 联动、灰度或告警；除非有明确异常证据，不得 hard block。

## 7. Observe-only 首发策略

### 7.1 默认模式

首发生产策略：

```yaml
budget_mode: observe_only
hard_block_profile: p0_only
normal_capability_policy: allow
threshold_source: operational_data_required
kill_switches:
  global_budget_enforcement: false
  per_user_hard_limits: false
  per_session_hard_limits: false
  per_model_hard_limits: false
```

默认只硬拦 P0 异常，所有正常能力只记录脱敏指标。

### 7.2 记录指标

记录多维指标：

- user ledger：并发、频率、失败率、模型成本、跨账号扩散；
- session ledger：messages/tool/thinking/context/body/stream/retry/session duration；
- account ledger：inflight、429 cooldown、401/403/risk、refresh、proxy、quarantine；
- model ledger：model family、thinking、1m、max_tokens bucket、candidate model；
- control-plane ledger：path_template、classification、safe intent status、cache/quarantine/synthetic decision。

### 7.3 脱敏规则

禁止记录：

- raw session；
- raw prompt；
- raw body；
- raw token；
- raw CCH；
- raw telemetry；
- email；
- account/org/user UUID；
- proxy credential；
- plain SHA/MD5 deterministic body hash。

允许记录：

- scoped opaque id；
- scoped keyed HMAC，带 `key_id` / `scope` / `version` / rotation；
- length bucket；
- enum 化字段集合；
- schema summary；
- status bucket；
- model family；
- client UA family、CLI version family、Stainless lang/runtime/os/arch 等 enum 化 persona 字段；
- boolean capability flags；
- risk score bucket 与 risk reason enum。

## 8. Soft budget 策略

Soft budget 是生产常态动作，不直接失败用户请求。

### 8.1 调度动作

- **排队**：user/session/account 达到软水位时进入 FIFO/priority queue；
- **降并发**：降低该 user 或 account 的 inflight session/message/stream；
- **换账号**：在不破坏 session affinity 和账号健康前提下切换到健康账号；
- **账号冷却**：429 或高 utilization 时按 reset header/指数退避冷却；
- **延迟重试**：对可重试 5xx/network error 做 jitter backoff；
- **session risk score**：累计异常行为分数，用于 soft->hard；
- **account quarantine**：风险账号停止新调度，已有 stream 按安全策略处理；
- **用户限速**：对异常用户限制新 session 或高成本模型并发；
- **管理员告警**：P1/P2 风险事件进入 dashboard / alert。

### 8.2 Soft budget 不允许做的事

- 不删除 thinking/context_management/stream；
- 不把 Opus 静默降级为 Sonnet；
- 不把 `max_tokens=32000` 静默降小；
- 不改写用户 messages/tool_result；
- 不因 control-plane GET 失败 fallback 到 legacy/direct；
- 不把账号 A 的 cache/telemetry/session 给账号 B 或用户 B。

## 9. Hard budget：soft 何时升级 hard

以下从 soft 升级 hard：

- 连续 429 且客户端/调度器不遵守 cooldown；
- risk text / unusual activity / KYC；
- 401/403 或 refresh 状态异常；
- verifier fail；
- fallback 出现；
- persona mismatch；
- CCH/billing marker mismatch；
- account/proxy/egress binding mismatch；
- account health 明显恶化且仍持续消耗；
- utilization 高水位、reset 临近、overage disabled、fallback percentage 异常等 rate-limit 软信号先触发 soft cooldown / 降并发 / 排队；只有持续违反 cooldown 或账号健康继续恶化时才升级 hard；
- 工具死循环达到“无用户输入 + 同工具/结果摘要重复 + 无状态进展 + 持续失败/高成本”的组合条件；
- 超大异常 body/tool_result，且不符合正常 1m/project context 形态；
- 长时间自动循环无人输入：必须由组合 detector 判定，至少同时满足无人类输入/确认、连续 assistant->tool 自动推进、重复工具或重复结果摘要、无状态进展、持续失败或高成本消耗、超过 session wall-clock/idle soft 水位；
- untrusted spoof；
- raw sensitive 持久化风险。

Progress detector 必须基于安全摘要，不得读取或保存 raw prompt/body/tool_result。建议的 `progress_signal_set`：`distinct_tool_use_args_summary_count`、`distinct_tool_result_schema_summary_count`、`assistant_text_bytes_delta_bucket`、`user_input_count`、`state_transition_count`。只有连续多个轮次这些信号为 0、重复或无变化，且同时有持续失败/高成本/无人输入，才能把工具循环升级为 hard。

Hard budget 的返回应使用安全错误码和 reason enum，不输出 raw request/body/prompt/token/CCH。

## 10. 数据模型

### 10.1 `session_ledger`

```text
session_ref scoped HMAC/opaque id
user_ref scoped HMAC/opaque id
route_context_ref opaque id
account_ref opaque id
model_family enum
policy_version
strategy_version
created_at / last_seen_at / closed_at
messages_count_bucket
assistant_messages_count_bucket
tool_use_count_bucket
tool_result_count_bucket
consecutive_tool_rounds_bucket
thinking_count_bucket
context_management_count_bucket
stream_count_bucket
body_bytes_total_bucket
max_body_bytes_bucket
tool_result_bytes_total_bucket
max_tool_result_bytes_bucket
session_duration_bucket
idle_duration_bucket
retry_count_bucket
progress_signal_set buckets
risk_score_bucket low|medium|high|critical
risk_reasons enum[]
state observe|queued|cooling|blocked|closed
```

不存 raw session id、raw prompt、raw messages、raw tool_result、raw body、raw CCH。

### 10.2 `account_ledger`

```text
account_ref opaque/scoped HMAC
egress_bucket_ref opaque/scoped HMAC
proxy_ref opaque/scoped HMAC
policy_version
strategy_version
status active|cooldown|quarantine|disabled
inflight_messages
inflight_streams
active_sessions_bucket
rate_limit_status_bucket
rate_limit_5h_status_bucket
rate_limit_5h_utilization_bucket
rate_limit_5h_reset_bucket
rate_limit_7d_status_bucket
rate_limit_7d_utilization_bucket
rate_limit_7d_reset_bucket
rate_limit_representative_claim_bucket
rate_limit_fallback_percentage_bucket
rate_limit_overage_disabled_reason_enum
rate_limit_overage_status_enum
reset_at_bucket
429_count_window
401_403_count_window
risk_text_count_window
refresh_status healthy|refreshing|expiring_soon|expired|locked|kyc_required|unusual_activity
last_health_event_at
quarantine_record_ref nullable
cooldown_record_ref nullable
```

不存 account UUID、email、raw token、proxy credential。

### 10.3 `user_ledger`

```text
user_ref opaque/scoped HMAC
policy_version
strategy_version
active_sessions_count
inflight_messages_count
request_rate_bucket
high_cost_model_rate_bucket
account_spread_bucket
failure_rate_bucket
retry_rate_bucket
risk_score_bucket low|medium|high|critical
risk_reasons enum[] day-rotated scope
soft_limiter_state
hard_block_until nullable
```

### 10.4 `model_ledger`

```text
model_family enum/string allowlisted category
policy_version
strategy_version
resolver_decision known|candidate|quarantine|untrusted_block
capability_flags thinking|context_1m_capable|context_management|stream|max_tokens_32000
observed_context_1m_token_present boolean
usage_count_bucket
cost_bucket
failure_rate_bucket
candidate_replay_proof_ref nullable
kill_switch_state
```

Future model 不得因字符串未知就 broad hard block；trusted CLI-through candidate 进入灰度，untrusted spoof fail closed。

### 10.5 `control_plane_ledger`

```text
intent_ref opaque/scoped HMAC
session_ref scoped HMAC/opaque id
account_ref opaque/scoped HMAC nullable
path_template enum/template
classification enum
method enum
policy_version
strategy_version
body_length_bucket
body_omitted_reason
schema_summary enum-only
attestation_status pass|fail
policy_decision suppress|stub|cache|upstream|synthetic|quarantine
cache_scope public|account|user-session|none
status_bucket
risk_reasons enum[]
```

### 10.6 `risk_event`

```text
risk_event_ref opaque id
severity P0|P1|P2|Unknown
scope user|session|account|model|control-plane|proxy|global
reason enum
first_seen_at
last_seen_at
counter
sample_refs opaque[]
action observe|queue|cooldown|quarantine|hard_block|alert
```

### 10.7 `quarantine_record`

```text
quarantine_ref opaque id
scope account|path|profile|model|user|session
reason enum
trigger_event_ref
created_at
expires_at nullable
manual_review_status pending|approved|rejected
kill_switch_ref nullable
```

### 10.8 `cooldown_record`

```text
cooldown_ref opaque id
scope account|path|user|model
reason 429|high_utilization|retry_storm|health_degraded
start_at
not_before_at
backoff_strategy enum
reset_source header|policy|manual
```

### 10.9 `audit_event`

```text
audit_ref opaque id
event_type enum
actor system|admin|scheduler|guard|gateway
scope refs only
policy_version
strategy_version
config_fingerprint scoped HMAC
redaction_proof
sensitive_scan_result
```

禁止 plain SHA/MD5 deterministic body hash。所有跨请求关联必须使用 scoped opaque id 或 scoped keyed HMAC，HMAC scope 至少包含 environment、tenant/user/session/account/path/purpose/key_version/rotation period，不能跨用户、跨账号、跨日期、跨 purpose 稳定关联。

## 11. 与 B2 / B3 的关系

### 11.1 正式号池控制面上传策略

Session budget 是 `35-formal-pool-control-plane-upload-strategy.md` 的调度和熔断层：

- main messages 进入 session/account/user/model budget；
- control-plane intent 进入 control-plane ledger；
- GET path 的 cache/upstream 频率受 account/path budget 管；
- telemetry/eval synthetic/suppress 频率受 control-plane + account budget 管；
- 401/403/429/risk/schema drift 通过 account/path quarantine 回传给 selector。

### 11.2 动态 Persona / Model Resolver

Budget 消费 resolver 输出：

- profile id / version family / beta profile ref；
- model resolver decision；
- trusted vs untrusted；
- candidate model/beta 灰度状态；
- capability flags。

Budget 不修改 resolver 输出的能力字段；只对并发、流量、账号使用和灰度比例做限制。

### 11.3 Synthetic telemetry

Synthetic telemetry 的节奏应由 session/account/control-plane ledger 生成：

- 从 accepted messages safe summary 生成 lifecycle event；
- 不从 raw telemetry body 重写；
- telemetry failure 不触发 messages fallback；
- synthetic telemetry 消费独立 control-plane 子预算，不占 main messages inflight；
- synthetic telemetry failure 只熔断 telemetry path/account/event family，不提高 messages session/account risk；
- risk/KYC/unusual activity 触发 account quarantine。


### 11.8 B2 最终接口对齐表

| B2 组件 / 接口 | Budget 消费的输入 | Budget 输出 / 使用方式 | Enforcement 默认 | Redaction policy | Fallback / quarantine | 验收用例 |
|---|---|---|---|---|---|---|
| Local guard safe intent | `method`、`path_template`、`normalized_query`、`classification`、`body_length_bucket`、`schema_summary`、`body_omitted_reason`、`digest_omitted_reason` | 写入 `control_plane_ledger`，供 path/account/profile 预算和 cache/quarantine 使用 | observe-only；unknown path P0 quarantine | 禁 raw query/body/prompt/token；只允许 enum/bucket/scoped ref | validation 失败 quarantine，不回退到 plain hash fallback | safe intent unknown field / plain hash / raw body fixture 必须失败 |
| Sub2API `ControlPlaneIntent` router | strict allowlist intent、loopback/auth、stripped external spoof headers | 产生 policy decision：`suppress` / `stub` / `cache` / `upstream-dry-run` / `quarantine` | 不真实上游；Phase 0/1 只 dry-run | audit 只记录 refs、path template、reason enum | missing auth、forged headers、unknown path quarantine | `gateway_control_plane_intent_test.go` missing/forged/replay tests |
| Guard attestation | HMAC/JWS payload：nonce、timestamp、method、path_template、session_ref、body omission decision、policy/strategy version | `attestation_status` 写入 control-plane budget；trusted persona 依赖 attestation PASS | missing/invalid/replay hard block | payload 禁 raw body/query/session/account；digest omitted reason 优先 | nonce replay、clock skew、signature mismatch fail closed | `control_plane_attestation_test.go` replay/rotation/skew tests |
| Session mapper | raw local session、server user scope、account ref、device/account identity inputs | 输出 server-issued UUID-like `session_id` 与 `session_ref`，供 session ledger 与 final headers/body 一致性使用 | mapping failure 不进入 trusted session budget | raw session 不落 audit；`session_ref` scoped HMAC | invalid/missing session id hard block 或 quarantine | `claude_code_session_mapper_test.go` UUID-like/ref/no raw tests |
| Persona resolver | UA/version/beta/x-app/Stainless/trusted client decision/profile id | budget 只消费 resolver decision 与 capability flags，不改写能力 | trusted exact/minor drift observe/gray；unknown major quarantine | 只存 version family/profile/capability enum | mismatch fail closed；unknown beta 需 allowlist/proof/kill switch | persona resolver exact/minor/unknown/candidate tests |
| Model resolver | requested model、known family、candidate allowlist、replay proof、kill switch、audit budget | `model_ledger` 记录 known/candidate/quarantine；限制只作用于流量/并发/灰度比例 | known Sonnet/Opus allow；future trusted candidate gray | 不存 user prompt/body；只存 model family/decision | untrusted spoof reject；candidate 缺 proof/kill switch quarantine | Opus 4.6/4.7、future 4.8 no capability downgrade tests |
| CC Gateway verifier / post-sign mutation | verifier result、final UA/beta/session/model/CCH version、post-sign mutation status | P0 hard block signal，写入 session/account risk event | verifier PASS 才允许；fail hard block | 不输出 raw CCH；只存 pass/fail/reason enum | verifier fail、post-sign mutation fail hard block | verifier fail/no fallback regression tests |
| CC Gateway fallback boundary | direct/legacy/sign-to-strip fallback flags | fallback false 是生产不变量；任何 true 进入 P0 | no fallback | audit 只存 fallback enum | direct/legacy/sign-to-strip fail closed | no direct/legacy/sign-strip fallback tests |
| CC Gateway rate-limit summary | 429 status、unified rate-limit header names、5h/7d utilization/reset/overage buckets | 写入 `account_ledger`，触发 cooldown/queue/soft budget | soft cooldown；违反 cooldown 才 hard | 只存 bucket/enum/reset bucket，不存 raw account | 429 后无冷却重试 hard block；account cooldown | 429 cooldown/retry storm tests |
| CC Gateway risk summary | 401/403/KYC/unusual activity/risk text enum | 写入 `account_ledger` 与 `risk_event` | immediate quarantine | 不保存 raw response body；只存 risk enum/status bucket | account quarantine，不污染其他账号 | risk text/KYC quarantine tests |
| Cache / quarantine / kill switch | path_template、normalized_query、account/user/session partition、persona/profile、model/version、schema version | 控制 control-plane GET cache、path/account/profile kill switches、budget decision | cache observe/dry-run；kill switch hard | cache key scoped HMAC；禁止 public cache 泄漏 account/user scope | unknown query/path/schema drift quarantine | account A/B cache isolation、kill switch tests |
| Budget ledger | session/user/account/model/control-plane safe summaries | observe-only metrics、soft budget、P0 hard block、dashboard/audit | Phase 1 observe-only；Phase 2 P0 only | scoped opaque id/HMAC；length bucket；enum；schema summary | ledger error fail safe：不扩大权限、不降级能力 | no raw sensitive scan、capability non-regression tests |

上述表格是实施入口契约。任何执行代理不得用散落文档推断更宽松字段；字段不在表内或对应 schema allowlist 内时，必须 quarantine 或进入显式设计评审。

### 11.4 CC Gateway sign-primary

CC Gateway 继续负责 `/v1/messages` final-output/signing/verifier/post-sign mutation/no fallback。Budget 在进入 CC Gateway 前做 route/account/user/session 调度，CC Gateway 返回 verifier/fallback/rate-limit/risk 摘要给 ledger。

### 11.5 Sub2API account selector

Account selector 使用 account ledger。账号选择只能由服务端完成，客户端不得指定账号；`route_context_id` 只能作为服务端签发的路由上下文，不得让 user ledger 写入或覆盖 selected account。

- active/cooldown/quarantine；
- egress bucket consistency；
- per-account concurrency；
- per-user account spread；
- model/profile eligibility；
- refresh state。

### 11.6 Proxy / egress bucket

Budget 强制 account_ref 与 egress_bucket_ref 绑定，proxy 出口漂移是 P0 hard block/quarantine。不得使用 legacy fallback bucket 绕过 CC Gateway shared-pool routing。

### 11.7 Raw capture / safe deliverable

本设计只允许 safe deliverable：长度 bucket、字段集合、schema summary、status bucket、verifier/fallback 结果、scoped ids、risk reason。真实 raw capture 如在受控调试环境瞬时存在，不能作为生产 ledger 输入，不能进入文档交付，不能被普通测试依赖。

## 12. 分阶段实施计划

### Phase 0：文档与测试夹具

- 不真实请求；
- 不访问真实上游；
- 基于脱敏 field audit 构造 fixtures：30 tools、thinking、context_management、stream、`max_tokens=32000`、Sonnet/Opus、future model candidate；
- 补 `tools/tests/test_cli_session_budget.py`：证明正常富能力不被 hard block；
- 补 CC Gateway canary envelope 测试：证明 canary gate 不被误用于 production hard cap；
- sensitive scan：禁止 raw prompt/body/token/CCH/plain hash；
- Phase 0 exit gate：`tools/cli_session_budget.py` 默认必须是 observe-only 或显式传入 policy，不得内置生产 hard ceiling；
- Phase 0 exit gate：`tools/cli_runtime_productization.py` 的 `production-session` manifest 必须使用显式 `upstream_mode=production` 与生产开关，不能继承 `real-canary`、`ALLOW_REAL_ANTHROPIC_CANARY`、`real_canary_user_approved`、`canary_cost_envelope` 或低 body cap；
- Phase 0 exit gate：CC Gateway canary cost envelope 必须保持 canary-only，生产模式不得意外加载 2048/32KB/3 tools 等默认值；
- Phase 0 exit gate：`config.example.yaml` 中身份/账号/proxy 示例必须使用 scoped opaque id 或 scoped keyed HMAC 语义，不再示范 plain deterministic digest placeholder；
- 新增 `real_capture_replay_baseline` 安全 fixture：只包含 route、persona enum、Stainless 字段名、body keys、长度 bucket、tools_count、system block count、thinking/output_config/metadata schema summary、context-1m token absent/present boolean，不包含 raw body/prompt/CCH。


### 12.1 Phase 0 修复入口

联合验收发现的 Phase 0 P1 修复项必须在进入 Phase 1 前清零：

1. `tools/cli_session_budget.py` 默认必须是 observe-only / limits disabled；显式 policy 才能 hard block。
2. `tools/cli_control_plane_guard.py` 不得默认启用 session budget enforcement；必须通过显式 `--enforce-session-budget` 或等价生产策略开关启用。
3. runtime productization 的 `production-session` manifest 不得包含 `20/8/2MB` 等低硬阈值，不得使用 `real-canary` upstream mode，不得要求 `ALLOW_REAL_ANTHROPIC_CANARY`；生产默认只声明 observe-only。
4. CC Gateway canary cost envelope 与 shared-pool 默认 body cap 必须有测试证明不会被 production budget 继承为 hard cap。
5. Sub2API -> CC Gateway formal shared-pool 不得发送 raw email/account UUID/org UUID 身份 header；legacy/send-time-only raw identity 必须被 scanner/test 覆盖。
6. legacy telemetry raw body rewriter 必须与 shared-pool synthetic telemetry 隔离；synthetic path 只能来自 safe summary。

### Phase 1：observe-only ledger

- 新增 user/session/account/model/control-plane/pool utilization ledgers；
- 默认只记录脱敏指标；
- 记录 `normal` / `aggressive` profile 的 target burn curve、actual utilization、behind/on-track/ahead 状态；
- P0 之外不 hard block；
- 输出 dashboard/debug safe summary；
- 最小 stream lifecycle hooks：SSE final event、HTTP body close、connection abort、deadline timeout 都必须触发 ledger 关账；
- 支持 fake Redis/DB 和 in-memory backend。

### Phase 2：P0 hard block

实现硬拦：

- verifier/fallback/post-sign mutation；
- persona/CCH/profile mismatch；
- route/proxy/account binding mismatch；
- raw sensitive persistence/output；
- control-plane attestation fail；
- untrusted spoof；
- 401/403/KYC/risk text；
- 429 后无冷却重试；
- unknown control-plane path quarantine。

### Phase 3：soft budget

- 账号冷却；
- 降并发；
- 排队；
- 换账号；
- 根据 `normal` / `aggressive` profile 调整 account weight、catch-up 和 slow-down；
- session risk score；
- user limiter；
- admin alert；
- no capability downgrade invariant tests。

### Phase 4：production budget

- Redis/DB 中心化 ledger；
- 多实例一致；
- atomic counters、leases、stream lifecycle tracking；
- counter lease expiry：进程崩溃、stream 中断、连接断开后必须自动回收 inflight 计数；
- stream close reconciliation：正常 close、client disconnect、upstream error、timeout 都必须把 session/account/user ledger 对账到一致状态；
- duplicate event idempotency：重复上报、重放、retry 不得重复扣 budget 或重复 quarantine；
- 灰度开关；
- kill switch；
- 运营 dashboard；
- per-tenant / per-user / per-account / per-model policy registry。

### Phase 5：feedback calibration

- 基于真实运营数据校准阈值；
- 按 P50/P90/P99/P99.9 和 incident review 调整软水位；
- hard limit 只从真实异常证据、灰度结果和 kill-switch 保护下产生；
- threshold promotion gate：任一 soft budget 升级为 hard 前，必须满足足够观察天数、跨账号样本、跨用户会话样本、0 误伤 incident、人工 approval、可回滚 kill switch；
- 定期复盘 future model/beta drift、account health、synthetic telemetry 影响。

Phase exit sign-off：Phase 0->1、Phase 2->3、Phase 4->5 需要安全、运维和产品三方记录化确认；不得把未完成的 P0/P1 exit gate 带入下一阶段。

## 13. 测试计划

### 13.1 Unit tests

- session ledger 只记录 scoped id / bucket / enum，不记录 raw；
- user ledger 并发、频率、跨账号扩散；
- account/pool utilization ledger 5h/7d utilization、target burn curve、behind/on-track/ahead；
- account ledger 429 cooldown、401/403/risk quarantine；
- model ledger known/candidate/untrusted；
- control-plane ledger attestation pass/fail、unknown path quarantine；
- plain SHA/MD5/body_hash/query_hash 字段出现即失败；
- sensitive scan 覆盖 token/body/prompt/CCH/email/account UUID/proxy credential。

### 13.2 Integration tests

- local guard -> Sub2API -> CC Gateway -> localhost mock，全链路 no real upstream；
- CC Gateway sign-primary/verifier/post-sign mutation/no fallback 摘要进入 ledger；
- control-plane safe intent 进入 ledger，raw telemetry 不进入；
- fake Redis/DB ledger，多实例 atomic counter；
- concurrent sessions；
- user A/B isolation；
- account A/B isolation；
- account/proxy mismatch hard block；
- route_context/account mismatch hard block；
- normal/aggressive pool profile observe-only curve 不影响请求 body/capabilities。

### 13.3 Capability non-regression tests

必须证明以下不被 production budget 默认 hard block：

- large but normal body；
- 30+ tools fixture；
- 多轮 messages fixture；
- 多轮 tool_use/tool_result fixture；
- thinking present；
- context_management present；
- stream true；
- 1m context profile；
- `max_tokens=32000`；
- Opus 4.6/4.7；
- future Sonnet/Opus candidate model；
- real_capture_replay_baseline：匹配真实脱敏字段的安全 fixture 必须返回 observe/allowed；
- absent context-1m token + Sonnet 4.6 + `max_tokens=32000` 不被拦；
- context_1m_capable 与 observed_context_1m_token_present 双口径测试。

### 13.4 Risk tests

- 429 cooldown；
- 429 后立即重试 hard block；
- risk text quarantine；
- KYC/unusual activity quarantine；
- tool loop detection；
- retry storm detection；
- long unattended automatic loop；
- super-large anomalous body/tool_result block；
- untrusted spoofed Claude Code headers block；
- verifier fail block；
- fallback block；
- post-sign mutation block；
- raw prompt/body/token/CCH persistence attempt block。

### 13.5 No real upstream tests

CI 默认只允许 localhost/mock：

- 禁止访问 `api.anthropic.com`、`platform.claude.com`、`claude.ai`；
- no real canary；
- no login；
- no token export；
- no raw capture dependency；
- forbidden-network transport：任何测试访问真实 Claude/Anthropic 域名即失败；
- sensitive scan 范围覆盖 ledger 输出、fixture、cache、queue、mock evidence、audit logs。

## 14. Phase 1 implementation file status and remaining work

Sub2API worktree Phase 1 implemented:

- `tools/cli_session_budget.py` and `tools/tests/test_cli_session_budget.py`: observe-only local policy coverage and capability non-regression fixtures;
- `tools/cli_control_plane_guard.py`: production-safe control-plane guard coverage;
- `tools/cli_runtime_productization.py`: production-session/canary separation coverage;
- `backend/internal/service/session_budget.go`: session ledger and safe summary builder;
- `backend/internal/service/account_budget.go`: account utilization ledger and header bucketing;
- `backend/internal/service/pool_utilization_budget.go`: normal/aggressive target burn curve, account weight, catch-up/slow-down;
- `backend/internal/service/user_budget.go`: user ledger buckets;
- `backend/internal/service/risk_event.go`: P0 risk event ledger;
- `backend/internal/service/budget_decision.go`: observe-only decision engine;
- `backend/internal/service/session_budget_observe.go`: GatewayService observe sink integration;
- tests: `session_budget_phase1_test.go`, `budget_decision_test.go`, `gateway_session_budget_integration_test.go`, `pool_profile_strategy_test.go`.

Remaining post-Phase-1 work:

- optional Redis/DB persistence for already-sanitized ledgers;
- admin/dashboard views for redacted ledger, cooldown/quarantine, and kill switches;
- broader model/control-plane budget files only after staging validates Phase 1 observe data.

CC Gateway：

- `src/canary-cost-gate.ts`：保持 canary-only，避免 production 启用为 hard cap；
- `src/config.ts`：新增 production upstream/budget config schema，区分 canary envelope 与 production observe-only；
- `config.example.yaml`：去掉/替换 plain deterministic digest placeholder 示例，改为 scoped opaque/HMAC 语义；
- fixture：新增 real_capture_replay_baseline 安全 fixture、context-1m absent/present 双口径 fixture、stream lifecycle fixture；
- messages handler / audit logger：输出 verifier/fallback/rate-limit/risk safe summary；
- tests：future model candidate、Opus、thinking、1m、large-normal-body not blocked。

## 15. 风险列表

### P0

1. 把 canary `max_messages=1` 或低阈值误用到生产，导致 Claude Code 正常能力被硬拦。
2. raw token/body/prompt/CCH/telemetry/account UUID/email/proxy credential 落盘或进入 safe deliverable。
3. verifier/fallback/persona/CCH/proxy/account binding 错配未 hard block。
4. 401/403/KYC/unusual activity/risk text 未 quarantine。
5. 429 后无冷却持续重试，导致账号池风险扩大。
6. plain SHA/MD5/deterministic body hash 被用作 production ledger 关联键。
7. 不可信客户端伪造 Claude Code headers/model/beta 绕过策略。

### P1

1. 需要中心化 Redis/DB，否则多实例下 account/user/session 并发不一致。
2. tool loop detection 需要基于安全摘要的 progress signal 校准，避免误伤长任务。
3. rate-limit headers 必须进入 account ledger bucket，否则只能等 429 后才保护账号。
4. 必须区分 `context_1m_capable` 与单次请求是否实际携带 context-1m token，否则会把真实成功样本写成错误断言。
5. Phase 1 observe-only 也必须有 stream lifecycle 关账，否则幽灵 inflight 会污染后续阈值校准。
6. Phase 1 实施前仍需把 Phase 0 修复项纳入回归测试集，防止默认 hard cap、raw identity header、legacy telemetry raw rewrite 回归。

### P2

1. 高成本模型成本 dashboard 需要运营数据校准。
2. synthetic telemetry 节奏需要后续 per-event gray rollout。
3. account settings/MCP private cache scope 需要更细 schema allowlist。
4. 管理员告警噪音需要调优。
5. threshold promotion gate 的观察天数、账号数、用户会话数需要由运营侧最终定值。
6. phase exit sign-off matrix 需要接入实际发布流程。

### Unknown

1. 多用户共享同一正式 Claude 账号的长期上游风控信号尚未由生产数据证明。
2. 未来 Claude Code CLI 版本、beta、model family 可能改变工具/telemetry/control-plane shape。
3. 1m context 的真实大项目 body/tool_result 分布需要 observe-only 数据建立 P99/P99.9。
4. Synthetic telemetry 是否足以替代 raw telemetry 需要单事件 canary 和灰度验证。
5. Opus、429、stream 中断、长工具链、大 body 的真实安全样本仍不足；在补齐前只能作为校准材料，不能拿来制定生产硬阈值。

## 16. 是否可以作为正式实施计划

可以作为正式实施计划的设计基线，但必须按 Phase 0 -> Phase 5 分阶段推进。首发只能 observe-only + P0 hard block，不得直接启用拍脑袋的生产 hard thresholds。Phase 0 修复稿已吸收默认 observe-only、显式 enforcement、B2 接口对齐表、production/canary 隔离、raw identity header 边界和 legacy telemetry 隔离要求；进入 Phase 1 前必须通过对应回归测试和复审。

## 17. 敏感信息检查原则

本文档不包含 raw token、Authorization 值、x-api-key、cookie、raw prompt、raw body、raw telemetry、raw CCH、email、account UUID 或 proxy credential。所有身份和关联设计均要求 scoped opaque id 或 scoped keyed HMAC；禁止 plain SHA/MD5 deterministic body hash。

## 13. Phase 1 observe-only implementation status (2026-05-25)

Phase 1 observe-only ledger has been implemented in the Sub2API worktree as a safe telemetry layer. It does **not** change Claude Code request capability and does **not** mutate request or response bodies.

Implemented files:

- `backend/internal/service/session_budget.go`
- `backend/internal/service/account_budget.go`
- `backend/internal/service/user_budget.go`
- `backend/internal/service/pool_utilization_budget.go`
- `backend/internal/service/risk_event.go`
- `backend/internal/service/budget_decision.go`
- `backend/internal/service/session_budget_observe.go`
- tests: `session_budget_phase1_test.go`, `budget_decision_test.go`, `gateway_session_budget_integration_test.go`, `pool_profile_strategy_test.go`

### 13.1 Observe-only default

The default decision mode is `observe_only`. Non-P0 cases produce `observe` or scheduling recommendations only. The ledger records scoped/HMAC refs, buckets, enum status, and shape summaries. It does not store raw prompt, raw body, raw tool input/output, raw CCH, raw token, raw Authorization, raw email, raw UUID, or proxy credentials.

### 13.2 P0 hard-block/quarantine scope

P0 decisions are limited to explicit safety boundary failures such as verifier failure, fallback/sign-strip fallback, proxy mismatch, risk/KYC/unusual-activity text, sensitive leakage risk, unsafe control-plane upload, or identity boundary failure. These produce `p0_block` or `quarantine`; they are not used for normal budget pressure.

### 13.3 Claude Code capability preservation

Phase 1 does not limit:

- 1m context;
- tools or tool loop shape;
- thinking or thinking budget;
- streaming;
- Opus/Sonnet model choice;
- `max_tokens=32000`;
- output config or context management.

The decision summary explicitly records these as `unchanged`.

### 13.4 normal/aggressive scheduling-only strategy

`normal` targets smooth 7-day 90%-100% utilization. `aggressive` targets 3-day 95%-100% utilization. Both profiles only affect scheduling fields such as account weight, queue priority, catch-up/slow-down, and cooldown recommendations. They do not modify the Claude Code request body or reduce request capabilities. Missing, malformed, or non-finite utilization headers fall back to conservative observe-only scheduling.

### 13.5 GatewayService integration

The GatewayService observe path records request and response summaries through an observe sink. It consumes unified 5h/7d utilization headers, status buckets, cooldown signals, and risk events. The strict Claude Code passthrough and CC Gateway boundary tests verify request body preservation and no canary/persona/signing regression.

### 13.6 Phase 2/3 entry conditions

Phase 2/3 must not begin until staging has validated that:

- sensitive scan findings remain zero;
- targeted Go and Python tests pass;
- CC Gateway build and tests pass;
- observe-only ledgers show no raw data;
- P0/P1 review findings are zero;
- any non-observe action remains limited to P0 safety boundaries.
