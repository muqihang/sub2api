# Claude Code 自定义 Base URL 能力差异与逐梦 Agent 接管策略

日期：2026-06-05
状态：DRAFT v1
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

## 1. 目标

本文件专项回答一个生产关键问题：当用户通过环境变量把 Claude Code CLI 的 `ANTHROPIC_BASE_URL` 指向 Sub2API / 第三方网关，而不是官方 `api.anthropic.com` 时，Claude Code CLI 会不会自动改变请求能力形态。

结论：**会。**

这不是单一 ToolSearch 问题，而是一组围绕 first-party host、custom base URL、provider、实验 beta、控制面 endpoint 的行为分叉。若只让用户手工把 Base URL 改成我们的 Sub2API，Claude Code CLI 可能在本机发请求前就降级或关闭部分能力，导致上游看到的 shape 与真实官方 Claude Code CLI 不一致。

本文件目标：

1. 列出已确认的 custom base URL 差异。
2. 标注源码证据与 V1/V2 采集证据。
3. 明确哪些能力应由逐梦 Agent 安全打开，哪些不能盲目打开。
4. 给出 Sub2API / CC Gateway / 本机 guard 的支持要求。
5. 为后续“完整接管 Claude Code CLI”提供工程基线。

## 2. 材料来源

### 2.1 Claude Code 逆向源码

路径：

```text
/Users/muqihang/chelingxi_workspace/reference-projects/agent-frameworks/claude_code_src
```

重点文件：

```text
src/utils/model/providers.ts
src/utils/toolSearch.ts
src/services/api/claude.ts
src/utils/api.ts
src/utils/messages.ts
src/services/api/client.ts
src/services/policyLimits/index.ts
src/services/remoteManagedSettings/syncCache.ts
src/services/settingsSync/index.ts
src/services/teamMemorySync/index.ts
src/utils/model/modelCapabilities.ts
src/services/analytics/firstPartyEventLoggingExporter.ts
src/services/analytics/growthbook.ts
src/utils/betas.ts
src/services/api/bootstrap.ts
src/services/api/logging.ts
src/utils/model/model.ts
src/utils/model/modelOptions.ts
```

### 2.2 本机 V1/V2 Claude Code 隔离采集

路径：

```text
/Users/muqihang/.zhumeng/claude-code-lab/captures
```

重点运行目录包括：

```text
20260529-042841
20260530-025229
20260601-003152
20260601-194006
20260602-223311
```

采集特点：

- `ANTHROPIC_BASE_URL` 指向本机 guard。
- messages 经本机 guard 接入 Sub2API。
- direct Anthropic / Claude control-plane CONNECT 被 stub/block/记录安全摘要。
- V2 增加 process-level network destination watch。
- 不保存 raw token、raw prompt、raw body、raw telemetry、raw CCH。

### 2.3 已有设计和运行材料

```text
docs/anti-ban/30-claude-code-control-plane-classification-strategy.md
docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md
docs/anti-ban/36-dynamic-claude-code-persona-version-mapping-plan.md
docs/anti-ban/37-formal-pool-control-plane-and-dynamic-persona-implementation-plan.md
docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md
docs/anti-ban/39-formal-pool-session-budget-strategy.md
docs/anti-ban/40-claude-code-local-capture-lab.md
docs/anti-ban/40-formal-pool-new-account-hard-gates.md
docs/anti-ban/41-formal-pool-claude-code-shape-healthcheck.md
docs/anti-ban/42-formal-pool-status-dashboard.md
docs/anti-ban/44-non-claude-code-client-compat-adapter-design.md
docs/anti-ban/captures/real-cli-through-capability-field-audit-2026-05-24/field-audit-report.md
docs/anti-ban/captures/real-cli-through-highmax-200-2026-05-24/report.md
docs/anti-ban/runtime-productization/2026-05-24-cli-through/
```

### 2.4 CC Gateway 材料

```text
/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts
/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts
/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-registry.ts
/Users/muqihang/chelingxi_workspace/cc-gateway/src/upstream-safety.ts
```

## 3. Base URL 判定核心

Claude Code 源码中存在 `isFirstPartyAnthropicBaseUrl()`：

```text
未设置 ANTHROPIC_BASE_URL -> true
host == api.anthropic.com -> true
USER_TYPE=ant 且 host == api-staging.anthropic.com -> true
其他 custom host -> false
```

因此，对普通用户而言：

```text
ANTHROPIC_BASE_URL=http://127.0.0.1:<guard-port>
ANTHROPIC_BASE_URL=http://198.12.67.185:18080
ANTHROPIC_BASE_URL=https://sub2api.example.com
```

都会让 Claude Code CLI 认为当前不是官方 first-party Anthropic Base URL。

注意：`getAPIProvider()` 仍可能是 `firstParty`，因为 Bedrock/Vertex/Foundry 是另一组 provider 开关。也就是说 custom Base URL 不是 3P provider，但会触发许多 first-party-host gate。

## 4. 已确认的能力差异

### 4.1 ToolSearch / tool_reference / defer_loading 默认关闭

结论：**已确认，影响重大。**

源码证据：

- `src/utils/toolSearch.ts`
- `src/services/api/claude.ts`
- `src/utils/messages.ts`
- `src/utils/api.ts`

行为：

1. 默认 `ENABLE_TOOL_SEARCH` 未设置时，ToolSearch mode 本来倾向开启。
2. 但当 provider 是 firstParty 且 `ANTHROPIC_BASE_URL` 不是官方 host，`isToolSearchEnabledOptimistic()` 返回 false。
3. `useToolSearch=false` 后：
   - `ToolSearchTool` 从工具列表中过滤；
   - 不添加 ToolSearch beta header；
   - 不给工具 schema 添加 `defer_loading`；
   - 历史消息中的 `tool_reference` blocks 会被 strip；
   - 依赖 `tool_reference` 的动态延迟加载能力消失。

例外与其他关闭条件：

```text
ENABLE_TOOL_SEARCH=true
ENABLE_TOOL_SEARCH=auto
ENABLE_TOOL_SEARCH=auto:N
```

这些显式配置会绕过 custom Base URL 默认关闭逻辑，表示用户/接管工具确认当前网关支持 ToolSearch beta shape。但它们不是万能开关，ToolSearch 仍可能因以下条件关闭：

- selected model 不支持 `tool_reference`，例如当前默认不支持 Haiku 类模型；
- `ToolSearchTool` 不在 tools list 中，或被 disallowedTools 禁用；
- `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1` 强制 `standard` mode；
- `ENABLE_TOOL_SEARCH=auto/auto:N` 时，工具描述规模低于阈值；
- 没有 deferred tools，且没有 pending MCP server，`claude.ts` 会把 `useToolSearch` 重新置 false；
- 历史消息中的 `tool_reference` 只有在 ToolSearch optimistic enabled 时才会保留，否则会被 strip。

风险：

- 用户只改 Base URL 时，MCP/部分 deferred tools 会被全部内联或不可延迟加载。
- 子 agent / MCP / 大量工具场景下能力和真实官方 Claude Code CLI 形态不同。
- `tool_reference` 不出现并不一定是上游或 CC Gateway 问题，可能是本机 CLI 发送前已关闭。

逐梦 Agent 策略：

- 不能让用户手工裸改 Base URL 后自行承担差异。
- 对我们已通过 shape healthcheck 的 runtime，逐梦 Agent 应显式管理 `ENABLE_TOOL_SEARCH`，不能依赖 custom Base URL 下的默认 unset 行为。
- `ENABLE_TOOL_SEARCH=auto` 只能作为 **保守非等价 fallback**：它会绕过 custom host 默认关闭，但低于阈值时仍可能关闭 ToolSearch，不等价官方 unset 时的默认 `tst`/always-defer 行为。
- 若目标是 native parity，应在 fixed MCP/deferred-tool localhost shape healthcheck 通过后，对可信 Claude Code version/profile 使用 `ENABLE_TOOL_SEARCH=true`，并在 audit 中记录 `tool_search_mode=tst_for_native_parity`。
- 必须确保 Sub2API / CC Gateway / upstream signing chain 不删除 `tool_reference`、`defer_loading`、ToolSearch beta。

### 4.2 Fine-grained tool streaming / eager_input_streaming 默认关闭

结论：**已确认，影响工具流式体验。**

源码证据：

- `src/utils/api.ts`

行为：

`eager_input_streaming` 的开启条件包含：

```text
getAPIProvider() == firstParty
isFirstPartyAnthropicBaseUrl() == true
feature tengu_fgts enabled 或 CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING truthy
```

当 Base URL 是 Sub2API / 本机 guard 时，`isFirstPartyAnthropicBaseUrl=false`，即使设置 `CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING`，源码当前分支仍不会加 `eager_input_streaming`，因为它还要求 first-party host。

影响：

- 大工具输入参数时，API 可能缓冲完整参数再输出 delta。
- 首 token / 工具输入 delta 体验可能变慢。
- 朋友反馈“首 token 和总耗时接近”时，除了服务端 flush 问题，也应把此项列为本机 shape 差异候选。

逐梦 Agent 策略：

- 仅设置 env 不一定能打开，因为源码同时检查 official host。
- 不得为了 FGTS 盲目让 Claude Code 认为所有链路都是 first-party host。该做法会同时重新打开 policy limits、remote managed settings、settings sync、team memory、model capabilities 等 first-party-host gates，带来控制面直连和本机 OAuth 泄漏风险。
- 若要做 first-party-host 等价接管实验，必须满足硬门禁：
  - 使用 isolated `CLAUDE_CONFIG_DIR`；
  - 不读取、不导出、不转发本机 Claude OAuth / cookie / setup token；
  - 所有官方域名 CONNECT 由本机 guard 接管，未命中 allowlist 一律 fail closed；
  - per-endpoint route policy 明确 control-plane safe intent：messages、event_logging、bootstrap、policy_limits、settings、team_memory、mcp registry 分开处理；
  - control-plane 不复用 messages CCH signer；
  - 只记录字段名、route、bucket、scoped refs，不保存 raw body/prompt/token/telemetry/CCH。
- 不 patch CLI 的情况下，也可由服务端/CC Gateway 对 safe profile 补齐兼容字段，但这会从“真实 CLI-through body”变成“server-filled shape”，必须单独标记、审计和灰度。
- 首发不要盲目补 `eager_input_streaming`；先做 localhost-only shape healthcheck，确认 CC Gateway 和上游接受。

### 4.3 x-client-request-id 默认不注入

结论：**已确认，影响请求追踪/指纹完整性，不直接影响模型能力。**

源码证据：

- `src/services/api/client.ts`
- `src/services/api/claude.ts`

行为：

只有在：

```text
provider == firstParty
isFirstPartyAnthropicBaseUrl() == true
```

时才注入 client request id。

影响：

- custom Base URL 下该 header 缺失。
- 上游或官方日志关联形态与直连官方 CLI 有差异。
- 我们 CC Gateway 若需要追踪，应使用内部 request ref，但发往上游是否补齐要单独审计。

逐梦 Agent / CC Gateway 策略：

- 本机不一定能自然产生该 header。
- CC Gateway 可以生成内部 request id 用于审计，但不得伪造不属于真实客户端的敏感关联值。
- 若要补上上游 header，应在 persona profile 中显式声明，纳入 field parity audit。

### 4.4 Policy limits 不拉取

结论：**已确认。**

源码证据：

- `src/services/policyLimits/index.ts`

行为：

custom Base URL 用户不会调用：

```text
/api/claude_code/policy_limits
```

影响：

- 组织级策略限制不会通过该路径同步。
- 本机控制面分布减少。
- 若官方控制面对该类请求存在活跃度预期，裸改 Base URL 形态会偏离。

逐梦 Agent 策略：

- 不建议让用户本机直连官方 policy endpoint。
- 应由本机 guard 统一接管/记录 safe intent，并按正式号池控制面上传策略决定 suppress / stub / synthetic。
- 这属于 control-plane 接管，不属于 messages CCH signer。

### 4.5 Remote managed settings 不启用

结论：**已确认。**

源码证据：

- `src/services/remoteManagedSettings/syncCache.ts`

行为：

custom Base URL 时 remote managed settings eligibility 为 false。

影响：

- 远程托管设置不会拉取。
- 相关控制面路径缺失。

逐梦 Agent 策略：

- 首发仍以 suppress/stub 为主。
- 若后续 synthetic telemetry / control-plane upload 灰度，应把该 intent 纳入分类和 shadow-only 对比。

### 4.6 Settings sync 不启用

结论：**已确认。**

源码证据：

- `src/services/settingsSync/index.ts`

行为：

settings sync 要求：

```text
provider == firstParty
isFirstPartyAnthropicBaseUrl() == true
OAuth token with inference scope
```

custom Base URL 下返回 false。

影响：

- 用户设置/记忆文件不会通过官方 settings sync 路径上传/下载。
- 对控制面完整性有影响。

逐梦 Agent 策略：

- 不上传 raw settings。
- 后续若做 synthetic，应只上传 safe intent/shape 或经过正式控制面策略批准的合成事件。

### 4.7 Team memory sync 不启用

结论：**已确认。**

源码证据：

- `src/services/teamMemorySync/index.ts`

行为：

team memory sync 要求：

```text
provider == firstParty
isFirstPartyAnthropicBaseUrl() == true
OAuth token with inference/profile scopes
```

custom Base URL 下不启用。

影响：

- 团队记忆同步控制面缺失。
- 团队/组织账号形态下差异更明显。

逐梦 Agent 策略：

- 这类内容可能包含项目/团队信息，不能 raw 上传。
- 只能进入控制面分类策略，先 shadow-only。

### 4.8 Model capabilities 动态拉取不启用

结论：**已确认，但主要针对 ant/internal 用户。**

源码证据：

- `src/utils/model/modelCapabilities.ts`

行为：

model capabilities fetch 要求：

```text
USER_TYPE == ant
provider == firstParty
isFirstPartyAnthropicBaseUrl() == true
```

custom Base URL 下无法动态读取 capability cache。

影响：

- 模型能力判断更依赖本地静态逻辑。
- future model / beta capability 漂移时风险增大。
- 与我们 dynamic persona / model resolver 计划相关。

逐梦 Agent / 服务端策略：

- 不能依赖用户本机 CLI 自动拿到最新 capability。
- 服务端应维护 persona/model registry、candidate allowlist、gray rollout、kill switch。

### 4.9 GrowthBook attributes 暴露 custom base host

结论：**已确认。**

源码证据：

- `src/services/analytics/growthbook.ts`

行为：

`getApiBaseUrlHost()` 会在 `ANTHROPIC_BASE_URL` 指向非官方 host 时返回该 host，用于 GrowthBook 属性。

影响：

- Claude Code 本机 feature gate 可以知道用户在使用 custom Base URL。
- 未来某些功能可能按 custom base host 分桶或关闭。
- 这是能力漂移的长期风险来源。

逐梦 Agent 策略：

- 记录当前 Claude Code version + custom base delta。
- 不要假设同一版本未来行为不变；V2 采集和 shape healthcheck 要持续保留。

### 4.10 1P event logging 不跟随任意 Base URL

结论：**已确认。**

源码证据：

- `src/services/analytics/firstPartyEventLoggingExporter.ts`

已有材料：

- `docs/anti-ban/captures/2026-05-19-event-logging-live-behavior.md`
- V1/V2 local capture guard summaries

行为：

messages 可以走 `ANTHROPIC_BASE_URL`，但 event logging 默认 endpoint 不是任意 custom Base URL。源码默认 path 是 `/api/event_logging/batch`，除 staging 或 GrowthBook 配置覆盖外，默认仍指向官方 API event endpoint。V1/V2 观测到的 `/api/event_logging/v2/batch` 应理解为 GrowthBook/config/版本路径差异下的控制面形态，不改变“不会简单跟随 custom Base URL”的结论。

V1/V2 证据：

- 本机 lab 用 `ANTHROPIC_BASE_URL=http://127.0.0.1:<guard-port>` 接管 messages。
- guard 仍观测到大量 `api.anthropic.com` control-plane CONNECT / event logging 请求。
- report 中出现 `/api/event_logging/v2/batch`、bootstrap、eval、MCP registry 等控制面 safe summary。

影响：

- 只改 Base URL 无法完整接管控制面。
- 若不通过本机 guard / 逐梦 Agent 接管，控制面可能直连官方，且不经过号池 identity / proxy / safe upload 策略。

逐梦 Agent 策略：

- 必须做进程级网络目的地监控和本机 CONNECT/HTTPS guard。
- control-plane 不能复用 messages CCH signing。
- 首发仍 suppress/stub/shadow-only，后续按 doc 38 灰度。

## 5. 相关但不是 custom Base URL 直接触发的逻辑

### 5.1 firstParty-only betas

`shouldIncludeFirstPartyOnlyBetas()` 当前主要看：

```text
provider == firstParty 或 foundry
且 CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS 未启用
```

它不直接检查 custom Base URL。

影响：

- 只改 Base URL 时，许多 beta header 仍可能保留。
- 但 `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1` 会全局移除若干 beta shape，包含与 ToolSearch/schema 严格性相关的字段。

策略：

- 逐梦 Agent 不应设置 `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1`，除非进入故障降级模式。
- 生产 profile 必须显式声明该变量状态。

### 5.2 1m context

目前没有确认“Base URL 非官方会直接关闭 1m”。

1m 主要取决于：

- model；
- entitlement / plan；
- long context beta；
- account capability；
- usage credits / long context credits；
- CC Gateway persona/model resolver。

策略：

- 不把 1m 和 ToolSearch 混为一谈。
- 不因 custom Base URL 误关 Sonnet/Opus 1m。
- 健康检查短请求不要强制 long context beta；1m 另做专项验证。

### 5.3 thinking / interleaved thinking

目前没有确认“Base URL 非官方直接关闭 thinking”。

thinking 主要取决于：

- modelSupportsISP；
- beta headers；
- provider；
- disable env；
- runtime persona。

策略：

- 不因 custom Base URL 关闭 thinking。
- CC Gateway / Sub2API 不能把 thinking block 当异常拦截。
- 若 compat 或逐梦 Agent 主动注入 thinking，必须基于 model/persona/account capability 和灰度开关。

### 5.4 model defaults / picker 差异

源码中 model defaults 和 model picker 对 provider 有分支，尤其 3P provider 可能默认旧 Sonnet 或不同选项。但 custom Base URL 仍可能是 firstParty provider，因此不能简单套用 3P provider 逻辑。

策略：

- 逐梦 Agent 应显式传递用户选择模型，不依赖本地 picker 推断。
- 服务端 dynamic model resolver 负责 Opus/Sonnet known/candidate/gray，不 mechanical block。

## 6. V1/V2 采集对本结论的支持

V1/V2 safe capture 不能直接输出 raw tool schema 或 raw `tool_reference`，因为安全策略禁止 raw body/prompt 持久化。但它提供了三类间接证据：

1. **messages 与 control-plane 分流存在**：messages 走本机 guard / Sub2API，control-plane 仍出现 api.anthropic.com CONNECT 和 `/api/event_logging/v2/batch` safe summary。
2. **custom Base URL 确认**：lab run metadata 显示 `ANTHROPIC_BASE_URL` 指向本机 guard / Sub2API。
3. **能力 shape 摘要**：report 记录 model、tools_count、max_tokens、thinking/body keys/header names 等脱敏字段，可用于对比不同启动 profile 的 shape 差异。

因此，本文件对 ToolSearch / FGTS / sync/control-plane 的结论主要来自源码，V1/V2 采集用于验证 custom Base URL 接管场景确实发生、控制面没有简单跟随 Base URL，以及 safe shape 审计机制可支持后续 A/B。

## 7. 逐梦 Agent 推荐 capability profile

逐梦 Agent 不应只是设置 URL/API Key。应建立 `claude_code_capability_profile`，由服务端下发并由本机安全执行。

### 7.1 建议环境变量

| 变量 | 默认建议 | 目的 | 风险控制 |
|---|---|---|---|
| `ANTHROPIC_BASE_URL` | 指向本机 guard loopback | 接管 messages | 不直接暴露远端 Sub2API，便于本机控制面接管 |
| `ANTHROPIC_API_KEY` | 逐梦入口 API Key | 用户入口认证 | 不使用本机 Claude token，不上传本机 token |
| `ENABLE_TOOL_SEARCH` | `auto` 首发；验证后可 `true` | 恢复 ToolSearch/tool_reference/defer_loading | 仅对支持 profile 开启；shape healthcheck 必须通过 |
| `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS` | 不设置 / false | 避免误关 beta shapes | 故障降级时才设置 |
| `CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING` | 仅记录，不承诺能打开 | FGTS 显式意图 | 源码仍需 official host；需专项验证 |
| `ANTHROPIC_BETAS` | 不由用户任意填写 | 避免未知 beta 注入 | 由 persona registry / candidate allowlist 管控 |
| proxy/env guard vars | 由逐梦 Agent 管理 | 接管 control-plane 和 direct egress | 禁止用户 raw proxy credential 出现在日志 |

### 7.2 启动模式

```text
裸 Base URL 模式：不推荐生产使用。
逐梦 Agent loopback guard 模式：推荐。
逐梦 Agent + full control-plane capture 模式：用于生产验证和正式运营。
逐梦 Agent + shadow telemetry 模式：后续 B3/B4。
```

### 7.3 逐梦 Agent 必做检查

启动前：

- 识别 Claude Code CLI version。
- 确认 persona/profile 支持当前 version。
- 确认 Sub2API / CC Gateway runtime 支持 ToolSearch shape。
- 确认账号状态至少 warming/production，且 proxy/risk/session budget 正常。
- 确认本机 guard 监听 loopback。

运行中：

- 记录 messages shape summary。
- 记录 control-plane route summary。
- 记录 process netwatch destination bucket。
- 检测 guard bypass。
- 不保存 raw token/prompt/body/telemetry/CCH。

异常时：

- ToolSearch 相关 400：降级 `ENABLE_TOOL_SEARCH=auto` 或 standard，并标记 profile risk。
- `eager_input_streaming` 相关 400：禁用该字段，不影响 ToolSearch。
- direct control-plane bypass：fail closed 或提示用户重启接管模式。
- 401/403/risk/hold：账号隔离，不 retry loop。

## 8. Sub2API / CC Gateway 支持要求

### 8.1 Sub2API

- 接受并保留 native Claude Code 的 ToolSearch beta/header/body shape。
- 不把 `tool_reference`、`defer_loading`、ToolSearchTool 当作异常字段。
- 对 compat 客户端仍按 44 号文档 truthful tool mode，不虚假注入本机工具能力。
- session budget observe-only，不用低阈值拦截工具循环。
- control-plane 按 30/35/38 号策略分类 suppress/stub/shadow。

### 8.2 CC Gateway

- persona registry 中声明 ToolSearch / deferred tools / FGTS 支持状态。
- rewriter 不删除合法 ToolSearch fields。
- policy signer 仅处理 messages CCH，不处理 control-plane。
- shape healthcheck 增加：
  - ToolSearch enabled request；
  - tool_reference history request；
  - defer_loading tool schema；
  - no ToolSearch fallback request；
  - FGTS/eager_input_streaming 请求。

### 8.3 Runtime productization

- production-session profile 不得继承 canary hard gate。
- 新号 healthcheck 默认不要强制 ToolSearch/FGTS/1m；这些作为能力专项检查。
- warming 阶段可低权重 normal-only，但不要削弱 native Claude Code 能力。
- Opus/Sonnet future model 走 candidate/gray，不机械挡。

## 9. 测试计划

### 9.1 Localhost-only A/B shape test

同一 Claude Code version、同一固定 prompt，但不能只用空目录。ToolSearch 只有在存在 deferred/MCP tools 或 pending MCP server 时才会真实保留 ToolSearchTool / `defer_loading`；否则即使 `ENABLE_TOOL_SEARCH=true`，也可能因 no deferred tools 被 `claude.ts` 关闭。

前置 fixture：

```text
- 固定 isolated CLAUDE_CONFIG_DIR；
- 固定 MCP/deferred-tool fixture，至少包含一个可 deferred 的 MCP tool；
- ToolSearchTool 未被 disallowedTools 禁用；
- 选用支持 tool_reference 的 Sonnet/Opus 模型，不用 Haiku 做 ToolSearch parity 测试；
- 构造一条带历史 tool_reference 的 resume/request fixture，用于验证 strip/preserve 行为；
- pending MCP server fixture，用于验证 pending 时 ToolSearch 保留逻辑。
```

A/B 矩阵：

```text
A. official-shape baseline：不发真实上游；使用本机 TLS/DNS/proxy intercept 或 fixture replay 模拟 api.anthropic.com host，并确保未获批准时不访问真实 api.anthropic.com
B. ANTHROPIC_BASE_URL=loopback guard, ENABLE_TOOL_SEARCH unset
C. ANTHROPIC_BASE_URL=loopback guard, ENABLE_TOOL_SEARCH=auto （conservative non-parity fallback）
D. ANTHROPIC_BASE_URL=loopback guard, ENABLE_TOOL_SEARCH=true （native parity candidate，必须 fixed deferred-tool fixture）
E. same as D + attempted FGTS env / server-filled FGTS candidate
F. resume/history request containing tool_reference, compare preserve vs strip behavior
```

对比 safe summary：

```text
headers names
anthropic-beta items
tools_count
deferred_tools_count bucket
mcp_tool_count bucket
pending_mcp_server_present bool
tool_reference_present bool
tool_reference_preserved_in_history bool
defer_loading_present bool
ToolSearchTool_present bool
ToolSearchTool_disallowed bool
tool_search_beta_present bool
eager_input_streaming_present bool
thinking_present
context_management_present
max_tokens bucket
body keys
control-plane routes
process netwatch bypass count
```

### 9.2 CC Gateway localhost mock

- ToolSearch enabled request with fixed deferred MCP tool → CC Gateway → localhost mock，确认 raw capture summary 记录字段名但不保存 raw body。
- ToolSearch disabled request → mock，确认无 tool_reference/defer_loading。
- Resume/history request containing tool_reference → mock，确认 enabled 时 preserve、disabled 时 strip，并只记录 bool/bucket。
- FGTS field request → mock，确认是否被 rewriter/signer 保留或安全剥离。
- Count tokens route：仍按策略 native-only/deferred，不复用 messages signer。

### 9.3 Production safety gate

任何真实上游前必须：

```text
shape healthcheck PASS
safe deliverable scan PASS
runtime profile explicitly supports ToolSearch
account stage warming/production
proxy/egress bucket OK
no direct fallback
verifier PASS
post-sign mutation PASS
no raw sensitive persistence
```

## 10. 风险与决策

| 风险 | 级别 | 说明 | 决策 |
|---|---|---|---|
| 裸改 Base URL 导致 ToolSearch 默认关闭 | P0/P1 | 直接削弱 Claude Code 能力与 shape | 逐梦 Agent 接管并设置 profile |
| ToolSearch 打开但服务端不支持 | P0 | 可能 400 或破坏工具循环 | localhost-only shape healthcheck 先行 |
| FGTS 无法仅靠 env 打开 | P1 | 源码要求 official host | 单独专项；不可盲目承诺 |
| 控制面仍直连官方 | P0 | 账号池身份与安全策略缺失 | 本机 guard / netwatch 必须开启 |
| GrowthBook 因 custom host 改变 future gates | P1 | 版本漂移风险 | 持续 V2 采集 + persona registry |
| synthetic telemetry 过早真实上传 | P0 | raw/identity 风险 | shadow-only → canary → gray |
| 服务端补形态变成非真实 CLI-through | P1 | 审计边界改变 | 必须标记 server-filled fields |

## 11. 当前结论

1. Claude Code CLI 确实会因为 custom `ANTHROPIC_BASE_URL` 改变能力形态。
2. 已确认最关键差异是 ToolSearch/tool_reference/defer_loading 默认关闭。
3. 还确认 FGTS/eager_input_streaming、x-client-request-id、policy limits、remote settings、settings sync、team memory、model capabilities、GrowthBook attributes、event logging endpoint 均存在 custom Base URL 相关差异或控制面分叉。
4. 1m context 与 thinking 暂未发现“custom Base URL 直接关闭”的证据，不能误伤。
5. 完整接管 Claude Code CLI 必须依赖逐梦 Agent + loopback guard + capability profile，而不能只让用户手工改 Base URL。
6. ToolSearch parity 测试必须使用 fixed MCP/deferred-tool fixture；`ENABLE_TOOL_SEARCH=auto` 只能作为保守非等价 fallback，native parity candidate 应在 healthcheck 通过后使用 `true`。
7. 下一步应把本文件转化为逐梦 Agent Claude Code 板块实施计划，并补 localhost-only A/B shape test。
