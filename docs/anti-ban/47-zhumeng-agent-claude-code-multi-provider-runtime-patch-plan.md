# 逐梦 Agent Claude Code 多模型托管运行时设计方案

日期：2026-06-15
状态：DRAFT v1
适用范围：逐梦 Agent V0.1/V1 Claude Code CLI 接管第二阶段
前置依赖：46 号 Claude Code native takeover、CC Gateway formal pool、Sub2API Codex/Claude/DeepSeek/AGNES 网关调优
说明：本文继承 46 号文档中预留的“47 号多模型混合注入”方向；若历史材料中出现旧文件名 `47-zhumeng-agent-multi-provider-in-claude-code-plan.md`，以本文为准。
范围说明：本文只定义 Claude Code CLI takeover 的 47 号多模型方案；Codex Desktop takeover 是独立主线，但两者共享云端 gateway/catalog/provider abstraction，47 号不得降低既有 Codex Gateway 能力。
当前代码事实：本文初稿指出 `build_native_guard_plan()` 缺 `--native-attestation`、`start_native_guard()` 只有测试调用者、`cli_control_plane_guard.py` 对 `/v1/messages` 尚无 native/bridge route 分支、后端也未出现 `claude_code_bridge_*` route。因此 47 号实施不是在现成 bridge 地基上加功能，而是要先补 native guard 接入和 per-request routing trust contract。CP0 落地后，native guard 已接入 `zhumeng-claude start` / launcher / desktop 启动流，后续 checkpoint 仍不得在 CP4 routing trust contract 绿之前开启 bridge live path。

## 0. 执行结论

逐梦 Agent 第一版若只做“把 Claude Code 的 Base URL 改成某一个供应商”，产品竞争力不足。市场上已有大量单供应商 router 可以做到这一点，但通常只能把 Claude Code 切成“单一 DeepSeek / 单一 OpenAI-compatible / 单一 Anthropic-compatible”模式，无法在 Claude Code CLI 内部维持原生体验并混合切换 Claude、GPT、DeepSeek 等模型。

本方案的目标是做出有竞争壁垒的 Claude Code CLI 接管：

```text
用户启动 Claude Code
  -> 使用逐梦托管 Claude Code Runtime（首选 2.1.175）
  -> /model 内部直接显示 Claude + GPT + DeepSeek + 后续模型
  -> Claude 模型走真实 Claude Code native 安全通道
  -> 非 Claude 模型走 Claude Code Anthropic Messages bridge
  -> Subagents / Workflow / MCP / ToolSearch 尽量保持 Claude Code 原生工作流
```

推荐路线：

1. 不覆盖用户系统的官方 `claude`。
2. 逐梦 Agent 下载/准备一个 **托管增强 Runtime**，首发固定 `@anthropic-ai/claude-code@2.1.175`，与当前 CCH/persona 算法对齐。
3. 对托管 Runtime 做可审计、可回滚、版本锁定的 model/runtime overlay，而不是直接破坏用户全局安装。
4. 通过 local loopback guard + Sub2API provider router 分离两条通道：
   - `claude_code_native`：真实 Claude Code + Claude 模型 + CC Gateway formal pool。
   - `claude_code_bridge_*`：真实 Claude Code UI/工具壳 + 非 Claude 模型协议转换，不进入 Claude formal pool。
5. 对 DeepSeek/GPT/AGNES/GLM/Kimi 等非 formal-pool Claude 模型，Claude Code CLI 侧必须以 Anthropic Messages 为第一协议面：若上游官方提供 Anthropic-compatible `/v1/messages`，优先直连或最小包装该协议；只有缺失或探测不合格时，才从 OpenAI-compatible/Responses 转成 Anthropic facade。但 **Anthropic 协议兼容不等于 Claude native 安全证明**，不能把非 Claude 的 thinking/reasoning/signature/provider 私有历史原样带回 Claude formal pool。
6. 增加跨供应商会话历史边界：同 provider 内尽量保真，跨 provider 尤其切回 Claude native 时必须清洗、摘要或新开会话，避免 DeepSeek/GPT 的思考块、工具调用历史、assistant replay 污染 Anthropic 上游请求。
7. 终端体验支持三档：逐梦 Agent 点击启动、`zhumeng-claude` 命令、用户显式同意后的 shell alias/shim `claude`。

本方案不是“粗暴魔改用户 Claude Code”，而是“逐梦托管 Claude Code Runtime + 双通道网关 + 原生能力保真”。

## 1. 目标与非目标

### 1.1 目标

1. 用户通过逐梦 Agent 安装并启动托管 Claude Code Runtime。
2. 用户可在 Claude Code CLI 内部 `/model` 模型选择里看到混合模型列表：Claude、GPT、DeepSeek、后续国产 OpenAI-compatible / Anthropic-compatible 模型。
3. Claude 模型请求保持真实 Claude Code native body，经 guard attestation 进入 Sub2API / CC Gateway / formal pool。
4. GPT、DeepSeek、AGNES 等非 Claude 模型请求走独立 bridge path，不冒充 `claude_code_native`，不污染 Claude 订阅账号池。外部上游接入的 Claude-like / Anthropic-compatible 模型也不得默认冒充 formal-pool native，除非满足逐梦 formal pool 的 owner/scope/persona/attestation 全套条件。
5. Subagents / Agent model options / Workflow 默认模型选择要跟当前 provider profile 一致。
6. 在 Claude、DeepSeek、GPT 等模型切换时维护 provider transcript boundary：同 provider 内保真，跨 provider 只传安全可重放的文本/工具结果/摘要。
7. ToolSearch / `tool_reference` / `defer_loading` 在 custom Base URL 下由逐梦 Agent 显式开启和验证，而不是依赖 Claude Code 默认 first-party host gate。
8. 版本更新可控：首发固定 2.1.175，后续版本进入 candidate profile，通过 shape healthcheck 才放行。
9. 用户可随时回到官方原版 Claude Code。
10. 不读取、不上传、不复制用户默认 `~/.claude` OAuth / cookie / setup token。
11. 所有 patch/overlay 有 hash、manifest、备份、回滚、禁用开关。

### 1.2 非目标

本阶段不做：

- 实现本地 provider UI、本地 OAuth 登录、本地 credential vault 或完整 BYOK 管理面；这些属于 Stage 2，本阶段只预留抽象边界。
- 覆盖 `/opt/homebrew/bin/claude` 或系统全局官方安装；
- 未经用户授权写 shell rc 文件；
- 将 GPT/DeepSeek 请求伪装为 Claude Code native 请求进入 Claude formal pool；
- 因为 DeepSeek/GPT 兼容 Anthropic Messages 协议，就把它们的 thinking/reasoning/signature/provider 私有字段当成 Anthropic 原生历史回放；
- 声称非 Claude 模型拥有其并不支持的 Claude 原生能力；
- 未验证版本自动 patch；
- 保存 raw prompt、raw request body、raw token、raw telemetry、raw CCH；
- 真实上传 Claude Code control-plane telemetry；
- Windows 版深度支持，Windows 先保留 packaging 设计，不阻塞 macOS 首发。

## 2. 产品形态

### 2.1 首次安装路径

```text
逐梦 Agent UI
  -> Claude Code CLI 接管
  -> 安装逐梦增强 Runtime
  -> 选择推荐版本 2.1.175
  -> 下载/校验/准备 runtime
  -> 应用 model/runtime overlay
  -> 运行 localhost-only doctor
  -> 启动 Claude Code
```

用户看到的文案应避免“破解/魔改”，使用：

- 逐梦增强 Runtime
- 逐梦托管 Runtime
- Managed Claude Code Runtime
- Native-safe multi-model runtime

### 2.2 后续启动路径

支持三档：

| 启动方式 | 默认 | 说明 |
|---|---:|---|
| 逐梦 Agent 点击启动 | 是 | 最安全，适合普通用户 |
| `zhumeng-claude` | 是 | 开发者命令，不覆盖系统 `claude` |
| `claude` shell alias/shim | 可选 | 用户明确授权后写入 shell integration，可随时撤销 |

推荐 shell 语义：

```bash
zhumeng-agent claude-code install-runtime --version 2.1.175
zhumeng-agent claude-code doctor
zhumeng-agent claude-code start
zhumeng-claude
```

可选 alias：

```bash
alias claude='zhumeng-claude'
```

不得直接覆盖：

```text
/opt/homebrew/bin/claude
/usr/local/bin/claude
```

### 2.3 用户可见模型体验

Claude Code 内部 `/model` 目标体验。以下名称是 Sub2API catalog 的展示示例，不是 patch 脚本里的硬编码常量：

```text
Default (recommended)
Claude Opus 4.8
Claude Opus 4.7
Claude Sonnet 4.6
Claude Haiku 4.5
GPT 5.5
GPT 5.4
DeepSeek V4 Pro
DeepSeek V4 Flash
```

模型显示、描述和 capability badge 应来自 Sub2API model catalog，而不是硬编码散落在 patch 脚本里。

## 3. 现有材料与关键发现

### 3.1 本机 Claude Code 安装形态

当前本机观测：

```text
/opt/homebrew/bin/claude -> ../lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe
@anthropic-ai/claude-code 2.1.177
```

Claude Code 当前 npm 包包含 Bun/compiled binary。真实产品实现不能假设源码文件总是存在，必须准备两类 patch 策略：

1. source-overlay path：针对可解包/源码可映射版本；
2. binary/string/launcher-overlay path：针对只暴露 compiled runtime 的版本，只做可验证的外部配置/启动 overlay，必要时使用托管版本包。

### 3.2 45 号文档的结论

`docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md` 已确认：只改 `ANTHROPIC_BASE_URL` 会让 Claude Code 认为 base host 不是 first-party Anthropic host，从而影响：

- ToolSearch / `tool_reference` / `defer_loading`；
- fine-grained tool streaming / `eager_input_streaming`；
- `x-client-request-id`；
- policy limits；
- remote managed settings；
- settings sync；
- team memory sync；
- model capabilities fetch；
- GrowthBook attributes；
- event logging endpoint。

其中 ToolSearch 是首发必须修复的 P0/P1 能力差异。可通过逐梦 Agent 显式设置并验证：

```text
ENABLE_TOOL_SEARCH=true
```

但 `ENABLE_TOOL_SEARCH=true` 不是完整 native parity 的唯一条件。必须配合 shape healthcheck，确保 Sub2API / CC Gateway / bridge 不删除合法 ToolSearch beta/header/body shape。

### 3.3 模型列表入口

参考源码显示 `/model` 选项主要来自：

```text
src/utils/model/modelOptions.ts
src/utils/model/model.ts
src/utils/model/modelStrings.ts
src/utils/model/modelAllowlist.ts
src/utils/model/aliases.ts
src/utils/model/agent.ts
```

关键入口：

- `getModelOptions()`：模型选择列表；
- `additionalModelOptionsCache`：bootstrap/global config 可追加模型选项；
- `ANTHROPIC_CUSTOM_MODEL_OPTION`：单个自定义模型；
- `ANTHROPIC_DEFAULT_SONNET_MODEL` / `ANTHROPIC_DEFAULT_OPUS_MODEL` / `ANTHROPIC_DEFAULT_HAIKU_MODEL`：家族默认；
- `availableModels`：allowlist；
- `getAgentModelOptions()`：subagent 模型选项；
- `getAgentModel()`：subagent 实际模型解析。

结论：

- 单模型 router 可用 `ANTHROPIC_CUSTOM_MODEL_OPTION` 实现，但只能解决一个模型，不够产品化。
- 混合模型列表需要 overlay `getModelOptions()` / `additionalModelOptionsCache` 或等价 patch 点。
- Subagent 混合模型需要同时处理 `agent.ts` 的 options 和解析逻辑。

### 3.4 当前代码差距核实

当前仓库里 46 号 native guard 基础代码已经存在，但离 47 号可用路径还有以下事实差距：

1. `build_native_guard_plan()` 要求 `attestation_secret`，也会把 secret 注入 env，但命令行没有传 `--native-attestation`；而 `cli_control_plane_guard.py` 里的 `--native-attestation` 是 opt-in flag。因此当前计划点名的 native marker 测试会失败，CP0 必须把它作为红测修复，而不是只做复核。
2. `build_native_guard_plan()` / `start_native_guard()` 当前调用者只有测试；`zhumeng-claude start`、launcher、desktop/proxy 真实启动流还没有接入 native guard。CP0 必须把 native guard 接入 managed runtime 启动链路，否则后续 CP 都是在未运行的 path 上设计。
3. `cli_control_plane_guard.py` 当前 `_forward_messages` 只是在 `native_attestation_enabled` 为真时对所有 `/v1/messages` 统一盖 native attestation，没有按 `body.model`、catalog route 或 signed route hint 区分 native/bridge。
4. 后端当前只有 `claude_code_native` 相关验证与审计，没有 `claude_code_bridge_openai` / `claude_code_bridge_deepseek` / `claude_code_bridge_agnes` route 语义。

这些差距决定了 47 号 P0 顺序必须是：先修 native guard 红测并接入启动流，再建立单进程内 per-request route contract，再做 provider bridge 翻译。

## 4. 总体架构

```text
User / terminal
  -> zhumeng-claude or Zhumeng Agent UI
  -> Managed Claude Code Runtime 2.1.175
  -> isolated CLAUDE_CONFIG_DIR
  -> model/runtime overlay
  -> local loopback guard
  -> Sub2API Claude Code Provider Router
       |-- claude_code_native
       |     -> CC Gateway formal pool
       |     -> Claude upstream / subscription account
       |
       |-- claude_code_bridge_openai
       |     -> Anthropic Messages facade
       |     -> OpenAI Responses / Chat / Codex-compatible upstream
       |
       |-- claude_code_bridge_deepseek
       |     -> Anthropic Messages facade
       |     -> provider-probed DeepSeek transport
       |          A: DeepSeek Anthropic-compatible /v1/messages
       |          B: DeepSeek OpenAI-compatible/Responses fallback
       |
       |-- claude_code_bridge_anthropic_compat
       |     -> Anthropic Messages passthrough/max-capability bridge
       |     -> external Claude-like / Kiro / Anthropic-compatible upstream
       |
       |-- claude_code_bridge_agnes
             -> AGNES adapter
```

单进程路由事实：Claude Code 进程通常只有一个 `ANTHROPIC_BASE_URL` 指向 local guard；用户在 `/model` 切到 DeepSeek/GPT/AGNES 时，请求仍打到同一个 guard。因此 native vs bridge 的逐请求判定不能依赖 base URL，只能依赖 `body.model` 与 overlay/cloud catalog 派生出的 signed route hint，并由 guard/backend 独立交叉校验。

推荐 trust split：managed runtime overlay 负责显示模型、根据签名 catalog 生成当前请求的 route hint；guard 只做验签、核对 `body.model` 与 hint、盖对应 client type/attestation 或 fail closed；cloud backend 作为最终权威重新用服务端 catalog 解析 `body.model` 并判定 model->route->account 绑定。客户端 route hint 只作为一致性/审计/triage 信号，不能作为 native 授权依据。guard 不应持有完整 provider secret，也不应直接按 route 连接多个上游；guard 继续只发往 Sub2API cloud 这个单一 upstream。

### 4.1 Runtime layer

职责：

- 版本安装；
- runtime 校验；
- patch/overlay；
- isolated config；
- shell integration；
- 回滚。

建议目录：

```text
~/.zhumeng/runtimes/claude-code/
  2.1.175/
    upstream/              # 官方包或解包产物
    overlay/               # 逐梦 overlay
    bin/zhumeng-claude-runtime
    manifest.json
    patches.json
    hash.lock
  active -> 2.1.175
```

Manifest 示例：

```json
{
  "runtime": "claude-code",
  "upstream_version": "2.1.175",
  "zhumeng_runtime_version": "0.1.0",
  "source": "npm:@anthropic-ai/claude-code@2.1.175",
  "upstream_hash": "sha256:...",
  "overlay_hash": "sha256:...",
  "patch_points": [
    "model_options",
    "agent_model_options",
    "model_validation",
    "toolsearch_env",
    "guard_env"
  ],
  "cch_profile": "claude_code_2_1_175",
  "status": "ready"
}
```

### 4.2 Guard layer

继承 46 号设计。当前已知 `build_native_guard_plan()` 未向 guard 传 `--native-attestation`，CP0 必须先修复该红测并接入真实启动流，再进入多模型工作。native route 成功时应出现：

```text
x-sub2api-client-type: claude_code_native
x-sub2api-guard-attested: true
x-sub2api-native-attestation: ...
x-sub2api-native-signature: ...
```

Guard 负责：

- strip 本机 Authorization / x-api-key / cookie；
- 注入逐梦 entry auth；
- 按 route 注入 `claude_code_native` attestation 或 `claude_code_bridge_*` client type；
- control-plane safe intent；
- netwatch bypass 检测；
- summary-only capture。

Native attestation 必须是 request-bound 且 freshness-checked。签名 payload 至少绑定：

```text
client_type
route
model_id
provider_owner
credential_scope
gateway_location
runtime_hash
overlay_hash
catalog_version_or_hash
session_ref
request_body_or_shape_hash
nonce
timestamp
```

Guard 和 backend 都必须校验签名、有效期、nonce/anti-replay、request shape hash 与 catalog route 是否一致；任一不一致时 fail closed，不得进入 Claude formal pool。

注意：native attestation 是上游过滤器和路由证据，不是对抗恶意本机用户的最终安全证明。HMAC secret 运行在用户本机，必须按“可被提取”建模；号池安全最终依赖服务端行为预算/异常检测、2.1.175 persona/shape verifier、native/bridge 硬隔离、credential scope/account budget、secret/capability 吊销。

### 4.3 Provider router layer

根据 Sub2API 下发的 signed/catalog route、当前 session profile 和 guard/backend 双校验结果决定目标。下表只是 catalog 中常见模型家族的示例，不是授权逻辑：

| Display/model family 示例 | Catalog route | Client type | 是否可进 Claude formal pool |
|---|---|---|---:|
| catalog-authorized Claude display ids | Claude native | `claude_code_native` | 是 |
| `gpt-*` | OpenAI bridge | `claude_code_bridge_openai` | 否 |
| `deepseek-*` | DeepSeek bridge | `claude_code_bridge_deepseek` | 否 |
| external Claude-like / Kiro / Anthropic-compatible | Anthropic-compatible bridge | `claude_code_bridge_anthropic_compat` | 否 |
| `agnes-*` | AGNES bridge | `claude_code_bridge_agnes` | 否 |

硬规则：

- 非 Claude 模型不得带 `claude_code_native`。
- 非 Claude 模型不得走 CC Gateway formal subscription account pool。
- Claude native 与 bridge 的 usage/cost/cache/evidence 必须分开记账。
- 模型路由不得只靠字符串前缀散判；guard 只能用 Sub2API 下发的 signed catalog / route hint 做本机一致性检查，backend 必须用服务端 catalog 自行推导 route 并裁决授权；prefix 和客户端 route hint 都只能作为诊断/审计信号，不能作为授权依据。
- route decision、runtime hash、overlay hash、catalog version 应进入 native/bridge safe audit summary，便于排查混路由。
- Stage 1 中，Sub2API cloud catalog 是 ProviderRegistry 背后的 route source of truth；它不是永久唯一实现，也不得推动 CP5 提前实现本地 BYOK/OAuth provider registry。
- Catalog trust 必须包含 pinned trust root、签名校验、过期时间、撤销机制、单调版本或 anti-rollback 检查；离线 catalog 只能用于非正式 degraded/diagnostic，不能让 formal-pool eligibility 放行。
- Claude formal pool admission 必须同时满足：`provider_owner=zhumeng_managed`、`credential_scope=formal_pool`、`gateway_location=cloud` 或 approved gateway、服务端 catalog 推导出的 `route=claude_code_native`、native attestation 有效、runtime/overlay/catalog hash 有效。用户自有 Anthropic-compatible/OAuth provider、逐梦云端接入的外部 Claude-like/Kiro/反代 Claude 上游，即使协议和模型名都像 Claude，也必须走 distinct non-formal route，除非明确纳入 formal pool 画像和账号安全体系。

Per-request routing trust contract：

```text
body.model
overlay signed route hint
catalog_version/hash
runtime_hash
overlay_hash
session_ref
client_type
nonce/timestamp
```

规则：

1. overlay 根据当前模型与签名 catalog 为每个 `/v1/messages` 请求注入短期签名 route hint；若某 Claude Code 版本无法安全注入 header，则 guard 可退化为本地 catalog 解析，但必须把该模式标记为 degraded，并增加等价对抗测试。
2. guard 校验 route hint 签名和 freshness，核对 `body.model`、route、client_type、catalog/runtime/overlay hash 一致。
3. hint 为 native 时，guard 才能追加 `claude_code_native` attestation；hint 为 bridge 时，guard 只能追加 `claude_code_bridge_*` client type，绝不能盖 native attestation。
4. unknown model、hint 缺失、hint 与 body 不一致、bridge 模型伪造 native header、body 声称 Claude 但 route=bridge，全部 fail closed。
5. backend 不接受客户端 route hint 作为 native 授权依据；它必须用服务端权威 catalog 按 `body.model` 自行推导 route，并结合 shape healthcheck、persona/profile、account budget、credential scope 与 policy 作最终裁决。客户端 hint 只用于发现进程内 desync、提高误用门槛和审计定位。

### 4.4 Bridge layer

Bridge 的职责不是“让模型变成 Claude”，而是提供 Claude Code 可消费的 Anthropic Messages façade。协议表面必须是可插拔 transport，而不是全局一刀切：

```text
Anthropic /v1/messages request
  -> CanonicalAgentTurn
  -> provider transport selected by probe/catalog
       A: Anthropic-compatible /v1/messages emit
       B: OpenAI-compatible/Responses emit
  -> provider stream
  -> CanonicalAgentTurn delta
  -> Anthropic content_block_* SSE
  -> Claude Code CLI
```

优先策略：如果 DeepSeek/GPT/AGNES/GLM/Kimi 上游原生或兼容 Anthropic Messages 协议，应优先使用该协议形态或最小转换路径，以减少 Claude Code CLI 侧适配误差。但这只提升 bridge 兼容性，不构成 Claude native 安全证明。DeepSeek 默认应走 Anthropic-compatible transport；只有 bridge fixtures 证明该端点在 tool/SSE/reasoning/cache/error 等方面不合格，才回退 OpenAI-compatible/Responses transport 并复用 Codex Gateway adapter 策略。

必须覆盖：

- text block；
- tool_use；
- tool_result；
- stream event order；
- stop_reason；
- usage；
- error passthrough；
- thinking/reasoning 安全映射；
- image/file input；
- cache fields；
- interruption/timeout。

## 5. 模型 catalog 与 capability profile

### 5.1 统一模型定义

Sub2API 应向逐梦 Agent 下发 Claude Code runtime 专用模型 catalog：

```json
{
  "models": [
    {
      "id": "claude-opus-4-8",
      "label": "Claude Opus 4.8",
      "provider": "claude",
      "route": "claude_code_native",
      "runtime_mode": "cloud",
      "provider_owner": "zhumeng_managed",
      "credential_scope": "formal_pool",
      "gateway_location": "cloud",
      "description": "Most capable Claude model",
      "capabilities": {
        "tool_use": true,
        "thinking": true,
        "subagents": true,
        "mcp": true,
        "tool_search": true,
        "context_window": 900000,
        "image_input": true
      }
    },
    {
      "id": "deepseek-v4-flash",
      "label": "DeepSeek V4 Flash",
      "provider": "deepseek",
      "route": "claude_code_bridge_deepseek",
      "runtime_mode": "cloud",
      "provider_owner": "zhumeng_managed",
      "credential_scope": "provider_pool",
      "gateway_location": "cloud",
      "description": "Fast low-cost model for coding tasks",
      "protocol": {
        "protocol_family": "anthropic_messages",
        "preferred_claude_code_protocol": "anthropic_compatible_v1_messages",
        "anthropic_messages_native_or_compatible": true,
        "openai_compatible_fallback": true,
        "probe_required": true,
        "codex_gateway_protocol": "openai_compatible_to_responses"
      },
      "capabilities": {
        "tool_use": true,
        "subagents": true,
        "mcp": true,
        "tool_search": "bridge",
        "claude_style_thinking": false,
        "provider_reasoning": true,
        "reasoning_bridge": true,
        "anthropic_messages_protocol": true,
        "replayable_into_claude_native": false,
        "context_window": 128000,
        "image_input": false
      },
      "transcript_policy": {
        "same_provider": "preserve_provider_native",
        "cross_provider_to_claude": "sanitize_or_summary"
      }
    }
  ]
}
```

### 5.2 Capability truthfulness

不得虚标：

- 不支持 image 的模型，不在 UI 里展示 image-capable；
- 不支持 Claude-style thinking 的模型，不得写成 `thinking=true`；应标记 `claude_style_thinking=false`、`provider_reasoning=true`、`reasoning_bridge=true` 或 `hidden_reasoning_unavailable`；
- 不支持 1M 的模型，不显示 1M；
- ToolSearch 若只是 bridge 等价，不得标成 `native`；
- DeepSeek/GPT 即使兼容 Anthropic Messages 协议，也只能标成 `anthropic_messages_protocol=true` 或 `bridge`，不得标成 `claude_code_native`；
- 非 Claude provider 的 thinking/reasoning/signature 默认不可重放进 Claude native 历史，catalog 必须显式声明 `replayable_into_claude_native=false`。
- Claude Code 路径的 protocol metadata 必须区分 `preferred_claude_code_protocol` 与 Codex Gateway 的 `codex_gateway_protocol`：例如 DeepSeek 在 Codex Desktop/Codex Gateway 里可以继续走 OpenAI-compatible -> Responses；但在 Claude Code CLI 里应优先走官方或探测通过的 Anthropic-compatible `/v1/messages`。
- 对 GLM/Z.AI、Kimi/Moonshot、Qwen、MiniMax 等未来 provider，不得只凭宣传或模型名默认启用 Anthropic-compatible bridge；必须通过 provider probe 确认 `/v1/messages`、tool_use、stream SSE、usage/cache/error 形状，再写入 catalog。

### 5.3 默认模型映射

首发建议：

| 当前 profile | main default | subagent default | simple/fast default |
|---|---|---|---|
| Claude native | Claude Sonnet / Opus | inherit 或 Haiku | Haiku |
| GPT | GPT 5.4/5.5 | inherit 或 GPT fast | GPT fast |
| DeepSeek | DeepSeek V4 Pro | inherit 或 DeepSeek V4 Flash | DeepSeek V4 Flash |
| AGNES | AGNES Pro | inherit 或 AGNES Flash | AGNES Flash |

Subagent 默认策略应优先 `inherit`，避免用户主模型切到 DeepSeek 后，内部子代理仍意外回到 Claude 号池。

## 6. Runtime overlay 设计

### 6.1 Patch 点

首发候选 patch 点：

1. `/model` list：追加 Sub2API catalog models。
2. model validation：允许逐梦 catalog 模型。
3. display name：友好显示 GPT/DeepSeek/Claude。
4. model allowlist：与逐梦 catalog 同步。
5. agent model options：追加 GPT/DeepSeek/inherit。
6. agent model resolve：按 parent profile 继承或映射。
7. ToolSearch env：默认 `ENABLE_TOOL_SEARCH=true`，但受 profile/healthcheck 控制。
8. capability metadata：为非 Claude 模型标注 bridge capability。
9. family/default model dynamic resolver：非 Claude profile 下必须把 `ANTHROPIC_DEFAULT_HAIKU_MODEL` / `ANTHROPIC_DEFAULT_SONNET_MODEL` / `ANTHROPIC_DEFAULT_OPUS_MODEL` 以及等价 fast/compact/title/summary 默认模型解析到当前 provider，避免 Claude Code 后台任务静默走 Claude。静态 env remap 只能作为启动默认值；会话内 `/model` 切换后，后台/快模型/compact/title/summary/probe 必须按 active profile 动态解析，不能依赖“进程启动读一次”的 env。
10. per-request route hint：为每个出站 `/v1/messages` 请求注入短期签名 route hint，供 guard 做一致性检查、供 backend 审计定位；backend 授权必须以服务端 catalog 自行解析 `body.model` 为准。若版本无法安全注入，则必须降级并 fail closed 到 native-only 或外部选择模式。

### 6.1.1 Claude native overlay 安全边界

Runtime overlay 的目标是让 Claude Code CLI 看到更多模型和 provider profile，不是改写 Claude native 请求体。对 `claude_code_native` path 有硬约束：

- 不得改变 Claude 请求的 harness/system block、tools、thinking、context_management、output_config、metadata/session、消息顺序、cache-control 语义；
- 不得为了多模型 UI，把 Claude native 的 body 变成 server-filled 或 bridge-filled shape；
- overlay 只能影响模型列表、显示、allowlist、route metadata、guard/env/profile 选择；
- 每个 overlay 版本都必须通过与未改 2.1.175 native capture 的 shape equality / verifier / CC Gateway signing pipeline；
- 若 runtime hash、overlay hash、catalog hash 任一不匹配，Claude formal pool native path 必须禁用，只允许 degraded/local doctor 或非正式 bridge 测试。

### 6.2 Patch 策略

优先级：

1. **Configuration / bootstrap overlay**：如果 `additionalModelOptionsCache` 可由配置/缓存注入，优先使用，不改 binary。
2. **Runtime wrapper env overlay**：使用 `ANTHROPIC_CUSTOM_MODEL_OPTION` 等 env 补充单模型或 fallback。
3. **Managed source/bundle patch**：仅作用于托管 runtime，不作用于系统全局 runtime。
4. **Binary string patch**：除非经过稳定验证，否则不作为首发默认。

### 6.3 Patch manifest 与回滚

每次 overlay 必须记录：

- upstream version；
- input hash；
- output hash；
- patch points；
- created_at；
- zhumeng-agent version；
- rollback path；
- sensitive scan result。

失败策略：

| 失败点 | 行为 |
|---|---|
| runtime 下载失败 | 提示重试或使用系统 Claude native-only |
| hash 不匹配 | fail closed，不 patch |
| model patch 点缺失 | fallback 到逐梦 Agent 外部选模型，不承诺 `/model` 混合列表 |
| native attestation 缺失 | 不启动 Claude formal pool path |
| ToolSearch healthcheck 失败 | 降级 `ENABLE_TOOL_SEARCH=auto/standard`，UI 显示 degraded |
| bridge stream shape 失败 | 当前 provider 禁用，Claude native 不受影响 |

## 7. 版本策略

### 7.1 首发固定推荐版本与滚动窗口

首发固定：

```text
@anthropic-ai/claude-code@2.1.175
```

原因：

- 与当前 CCH/persona 算法对齐；
- 46/51/52 号 formal pool 安全材料围绕 2.1.175 建立；
- shape fixture、CCH fingerprint、CC Gateway persona 更可控。

但 2.1.175 不能被永久冻结为唯一 persona。Claude Code 用户版本会持续前移，长期落后版本本身也可能成为异常信号。首发后应维护 rolling known-good 窗口：2.1.175 作为初始 canonical，2.1.177 等用户常见版本进入 candidate，经过 7.3 流水线与 51 号 persona gap / 52 号行为预算验证后晋级。

### 7.2 用户本机版本策略

用户本机版本不强行覆盖，按以下状态处理：

| Version status | 行为 |
|---|---|
| `managed_known_good: 2.1.175` | 默认推荐，完整功能 |
| `compatible_candidate: 2.1.177` | 可检测、可 A/B、默认不作为 formal pool canonical |
| `legacy_candidate: 2.1.150` | 仅 native/simple 或提示安装 managed runtime |
| `unknown` | fail closed，不自动 patch |

### 7.3 新版本升级流程

每个新 Claude Code 版本必须通过：

1. static source/binary string scan；
2. `/model` patch point detection；
3. agent model options detection；
4. ToolSearch fixed MCP fixture；
5. native attestation smoke；
6. CC Gateway localhost mock；
7. no raw sensitive scan；
8. one-account canary（如涉及 formal pool，需单独批准）；
9. release profile registry update。

## 8. ToolSearch 与原生能力保真

### 8.1 ToolSearch 首发策略

托管 Runtime 启动 env：

```text
ENABLE_TOOL_SEARCH=true
ANTHROPIC_BASE_URL=http://127.0.0.1:<guard-port>
CLAUDE_CODE_API_BASE_URL=http://127.0.0.1:<guard-port>
```

但 `true` 只允许在 runtime profile 已通过 healthcheck 后启用。否则 fallback：

```text
ENABLE_TOOL_SEARCH=auto
```

### 8.2 必测 fixture

ToolSearch parity 不得用空项目测试，必须有 fixed MCP/deferred-tool fixture：

- 至少一个可 deferred 的 MCP tool；
- ToolSearchTool 未被 disallowed；
- 选 Sonnet/Opus，不用 Haiku；
- pending MCP server fixture；
- 确认 `tool_reference` / `defer_loading` 在 guard、Sub2API、CC Gateway 以及 bridge 中不被误删。
- Bridge profile 下也必须有 fixture：`ENABLE_TOOL_SEARCH=true` 全局开启时，DeepSeek/GPT bridge 要么全量物化 deferred tools，要么提供 tool-search shim，确保 OpenAI-compatible provider 不因无法理解 ToolSearch 原语而断工具调用。

### 8.3 FGTS / eager_input_streaming

首发不强承诺 FGTS native parity。策略：

- Claude native：observe-only + profile registry；
- bridge：按模型能力转换或忽略；
- 不通过欺骗 first-party host 打开所有控制面 gate。

## 9. Claude native 与 bridge 的安全隔离

### 9.1 Native path

只有满足以下条件才进入 `claude_code_native`：

- 逐梦 managed/runtime 或 approved system runtime；
- isolated config；
- loopback guard；
- native attestation；
- netwatch clean；
- model 属于 catalog-authorized Claude route，而不是只靠字符串前缀判断；
- `provider_owner=zhumeng_managed`、`credential_scope=formal_pool`、`gateway_location=cloud/approved`；
- CC Gateway persona/profile known or candidate approved；
- 服务端 per-account 行为预算、异常检测、profile healthcheck 和吊销策略均启用。

### 9.2 Bridge path

非 Claude 模型必须进入 bridge：

```text
x-sub2api-client-type: claude_code_bridge_openai
x-sub2api-client-type: claude_code_bridge_deepseek
x-sub2api-client-type: claude_code_bridge_agnes
```

Bridge path 禁止：

- 使用 Claude formal pool account；
- 使用 native CCH billing block；
- 声称 native Claude Code attestation；
- 复用 messages signer；
- 上传 Claude Code control-plane raw telemetry。

### 9.3 Usage / cache / audit

日志与计费必须分开：

| Field | Native | Bridge |
|---|---|---|
| client_type | `claude_code_native` | `claude_code_bridge_*` |
| account pool | Claude formal pool | provider-specific pool |
| cache metric | Anthropic/Claude cache | provider cache |
| CCH | allowed | forbidden |
| raw body | forbidden | forbidden |
| model catalog | Claude profile | Sub2API provider catalog |

## 10. 跨供应商会话边界与历史清洗

这是本方案新增的 P0 安全边界。主机制不是事后在 Claude native 出口“猜测”历史归属，而是在每个 bridge 边界维护 `ReplaySafeAnthropicTranscript` 不变式：任何进入 Claude Code transcript 的 foreign provider assistant turn，必须已经是 native-replay-safe 的普通文本/工具结果/摘要；任何离开 Claude native 进入非 Claude provider 的历史，必须已剥离 Claude thinking/signature/private metadata。出口 verifier 只做 fail-closed 兜底。

DeepSeek、GPT、AGNES 等模型可以尽量使用或兼容 Anthropic Messages 协议，但 **协议兼容不等于可作为 Anthropic 原生历史回放**。Claude Code CLI 能消费某个 Anthropic-shaped response，只说明 UI/工具壳可兼容；不说明该 response 内的 thinking、tool call、provider metadata、signature 或 assistant replay 可以安全带回 Claude native 上游。

### 10.1 基本原则

1. **同 provider 内保真**：用户在 DeepSeek profile 内连续对话时，尽量保留 DeepSeek 能力、工具调用、reasoning 显示策略和 provider cache 友好结构。清洗不应影响同 provider 的正常能力。
2. **跨 provider 只传安全语义**：从 DeepSeek/GPT/AGNES 切回 Claude native 时，只允许传递普通文本、用户可见结果、工具结果、文件引用摘要和明确的 subagent final answer。
3. **非 Claude 隐式思考不可回放**：DeepSeek/OpenAI/AGNES 的 `reasoning_content`、hidden reasoning、thinking block、signature、provider 私有消息 ID、response ID、cache key、tool runner 内部状态，不得作为 Claude assistant history 原样发送到 Anthropic。
4. **Bridge 入口先净化**：provider->Claude Code 时，bridge 不产出 foreign signed thinking 或 provider-private blocks；Claude->provider 时，bridge 不转发 Claude thinking/signature/private metadata。
5. **不确定就 fail closed**：如果无法证明工具调用成对、历史结构可重放、字段归属清楚，则提示用户开启新 Claude 会话或生成安全摘要后再继续。
6. **确定性与一次冻结**：跨 provider safe summary / safe_tool_result 必须一次生成、冻结并复用；不得每次请求重新让 LLM 现生成、带时间戳、随机排序或产生字段漂移。Claude->provider 的 in-flight 剥离映射也必须确定、幂等，避免每次请求都改变 provider cache 前缀。
7. **后台/compact 跟随 active profile**：标题、compact、summary、probe、fast/simple/haiku 等后台任务必须按当前 active provider profile 解析；非 Claude profile 下不能因为启动时 env 仍指向 Claude 而静默走 formal pool。

### 10.2 Transcript 类型

内部应把会话拆成带归属的 turn，而不是维护一份无归属的混合 history：

```text
ProviderTranscript
  conversation_ref
  active_provider
  turns[]
    provider: claude_native | deepseek_bridge | openai_bridge | agnes_bridge
    model_id
    route
    replay_class: claude_native_replayable | bridge_local_only | summary_only
    user_visible_content
    tool_calls_safe_summary
    provider_private_state_ref (local only, never cross-provider replay)
```

Claude native path 只允许消费：

```text
claude_native_replayable
summary_only
user_visible_content
safe tool_result（默认是语义摘要或重新封装结果，不是外部 provider 原始 tool_result replay）
```

不得消费：

```text
bridge_local_only
provider_private_state_ref
non_claude_thinking_or_reasoning
foreign_signature
foreign_tool_internal_history
```

### 10.3 模型切换策略

| 切换方向 | 默认策略 | 说明 |
|---|---|---|
| Claude -> Claude | 原生保真 | 同一 Claude session 可继续走 native replay |
| DeepSeek -> DeepSeek | bridge 保真 | 不因“清洗”损失 DeepSeek 自身能力 |
| Claude -> DeepSeek | 给 DeepSeek 提供安全上下文 | 可传用户可见历史和必要文件/工具摘要，不传 Claude thinking、Anthropic signature、CCH、message/session/cache/control-plane metadata |
| DeepSeek/GPT -> Claude | 严格清洗或摘要 | 不回放非 Claude thinking/reasoning/signature/tool internals |
| Claude 主控 -> DeepSeek 子代理 -> Claude 主控 | 子代理结果边界 | DeepSeek 子代理返回 final answer/tool_result，Claude 不接收 DeepSeek 内部 assistant replay |
| DeepSeek 主控 -> Claude 子代理 | 显式消耗 Claude 号池 | UI/审计需标记调用 Claude formal pool，并应用 Claude 账号预算 |
| Claude 主控 -> external Claude-compatible 子代理 | bridge 协作 | 若不是 formal pool Claude，按外部 Anthropic-compatible bridge 处理，只返回 safe final answer/tool_result，不共享 formal pool identity |

### 10.4 历史清洗规则

切回 Claude native 前，必须对待发送给 Claude 的历史做 provider-aware verifier。主要清洗发生在 bridge 入口和 runtime transcript 管理阶段；Claude native egress 的职责是强制校验：loopback guard 与 backend/router 在发往 Anthropic/CC Gateway 前必须验证 outgoing `claude_code_native` request 的 turn provenance、replay_class、role/order、tool pairing 与 thinking/signature 字段。出现未知 provenance、foreign provider assistant/tool/thinking block 或 verifier 失败时 fail closed。真实 Claude-origin turn 不得被出口清洗改写字段、顺序或签名内容；foreign turn 只能以安全摘要块进入。

允许保留：

- 普通 `user` 文本；
- 用户可见的 `assistant` final answer；
- 已执行完成且 ID/结构可证明成对的 tool result 摘要；跨 provider 默认不原样 replay 外部 provider 的 tool_use/tool_result turn，`safe_tool_result` 默认表示语义摘要或重新封装结果；只有 verifier 证明满足 Claude native replay 结构时才可保留结构化 replay；
- 文件路径、diff、命令输出等用户可见材料；
- 由逐梦 runtime 生成的安全摘要块，明确标注“来自非 Claude 子代理/模型”。

必须删除或转摘要：

- 非 Claude thinking/reasoning/hidden chain-of-thought；
- Anthropic thinking signature 之外的任何 signature-like 字段；
- OpenAI `previous_response_id`、DeepSeek `reasoning_content`、provider 私有 message id；
- bridge 内部 tool runner 状态、raw provider request/response、raw usage debug；
- 不成对、来源不明、role/order 不满足 Anthropic Messages replay 规则的 tool_use/tool_result；
- 任何可能触发 Anthropic `Invalid signature in thinking block` 的历史块。

跨 provider 到 Claude 时，默认策略是转成安全摘要块、final answer 或 user-visible evidence summary。只有经过 schema、ID、pairing、role-order verifier 的结构，才允许作为可重放 tool result；verifier 失败时不得向 Anthropic 发请求。

同样，Claude native 历史跨出到 DeepSeek/GPT/AGNES 时也要清洗：Claude thinking block、Anthropic signature、Claude 私有 message/session/cache/control-plane metadata、CCH/billing 相关内容都不能传给非 Claude provider。

### 10.5 能力影响与降级策略

清洗边界不应削弱模型在自身 provider 内的能力：

- DeepSeek 连续执行任务时，bridge 可以保留 DeepSeek 所需的上下文和 provider 私有状态。这里的“保留”只指 active local transcript/cache 中按 provider policy 临时保存或引用；audit/log/capture 仍必须 summary-only，不得写入 raw prompt/body/provider response。
- 只有当 DeepSeek/GPT 的结果要进入 Claude native 上游时，才进行“出口清洗”。
- 后台标题、compact、summary、probe 也属于当前 active provider 的能力面。非 Claude profile 下由 DeepSeek/GPT 写出的 compact 摘要是用户可见语义文本，可以继续使用；但它代表“当时 active provider 撰写的记忆”，切回 Claude 时只能作为 safe summary，而不是 Claude native 原始历史。
- 对 Claude 来说，清洗后的内容是用户可见的任务结果或摘要；这会损失一部分非 Claude 内部思考细节，但换来上游安全和可解释性。
- 如果用户需要最大能力保真，可以选择“留在当前 provider 继续”，而不是切回 Claude。
- 跨 provider 清洗会在“缝”处产生一次性 cache 代价，但不应让稳定前缀每次请求都失效。摘要块应尽量插入在最大稳定缓存前缀之后；真实 Claude-origin turn 与 provider-local stable prefix 不得被重新排序或重写。

首发默认策略应是：

```text
same_provider: preserve
cross_provider_to_claude: sanitize_or_summary
unsafe_cross_provider_history: require_new_session_or_user_confirmed_summary
```

### 10.6 缓存确定性要求

来回切换对缓存的影响必须被显式建模：

- 切走超过 provider cache TTL 后再回来，缓存自然冷却，这是物理限制，不应误判为 gateway 回归。
- TTL 内快切时，真实 Claude turn 字节不改写，因此 safe summary 之前的 Claude 前缀应继续命中；summary 之后的新缝可以产生一次性 miss。
- 生产风险是非确定性清洗：如果 summary/剥离每次请求都变，Claude 和 DeepSeek 的 cache 前缀都会持续 miss。实现必须禁止这种漂移。

机制要求：

1. safe summary / safe_tool_result 必须带 stable id、source provider、source turn range、hash，生成后冻结进 runtime transcript；后续请求引用同一份冻结结果。
2. 不允许在冻结摘要中注入当前时间、随机 nonce、请求序号、非稳定排序的文件列表或未排序 JSON。
3. Claude->DeepSeek/GPT 的剥离必须是纯函数：相同 provenance snapshot 生成相同 provider request prefix。
4. cache-control / prompt cache hint 不得因跨 provider UI metadata 漂移而移动。
5. cache ratio 不是单纯运营指标；相关 fixture 未通过不得放行 bridge parity。

### 10.7 测试要求

必须增加 fixture：

1. Claude -> DeepSeek -> Claude：DeepSeek reasoning 不出现在发往 Anthropic 的 body 中。
2. Claude 主控派 DeepSeek 子代理：Claude 只接收 DeepSeek final answer/tool_result，不接收子代理内部 assistant history。
3. Anthropic-compatible is not native-replayable：非 Claude Anthropic-compatible response 可被 CLI 消费，但不能被标记为 `claude_code_native`，也不能作为 Claude native history 原样 replay。
4. 外部伪造 thinking/signature：切回 Claude 前被删除或拦截。
5. Claude -> DeepSeek/GPT：Claude thinking/signature/private metadata 不出现在非 Claude provider 请求中。
6. 不成对或 role/order 异常的 tool_use/tool_result：fail closed，不发 Anthropic。
7. same-provider DeepSeek/GPT 连续会话不触发出口清洗，不损 provider reasoning/tool 能力。
8. 清洗后的 Claude 请求仍通过 CC Gateway verifier、session equality、profile healthcheck。
9. Resume / continue / compact / checkpoint / history replay：重放混合历史时仍不允许 foreign reasoning/signature/tool internals 进入 Anthropic。
10. cache determinism：同一个跨 provider safe summary 被重复请求时 hash 不变；TTL 内 Claude->DeepSeek->Claude 快切只产生缝后的预期 miss，不让缝前 Claude cache 前缀失效。
11. DeepSeek cache bounce：纯 DeepSeek baseline 与 DeepSeek->Claude->DeepSeek 弹跳后的 cache 命中率差异不得超过一次性边界成本；若 DeepSeek Anthropic transport 不支持可观测 cache 字段，必须在 accounting/audit 中标注不可测并用 request prefix hash 代替。

### 10.8 Runtime transcript 对齐合同

Claude Code 自身会维护 transcript，因此逐梦 overlay 不能只在服务端保存一份 ProviderTranscript 后指望出口重建来源。CP3B 必须定义 runtime-side provenance 对齐机制：

- 每个出站请求的 route hint 与当前 transcript provenance snapshot 绑定；
- bridge 返回给 Claude Code 的 assistant content 必须先转为 `ReplaySafeAnthropicTranscript` 允许的 block；
- guard/bridge 对 Claude-origin history 的剥离/转换只能是 in-flight 瞬态处理，绝不回写进 Claude Code 持久化 transcript；
- runtime 本地可维护 provider_private_state_ref，但该 ref 不能进入 Claude Code transcript、audit 或跨 provider replay；
- resume/compact/checkpoint 时必须能重新构造 provenance snapshot，构造失败则要求新会话或用户确认摘要；
- 真实 Claude-origin turn 字节级不改写，避免破坏 Anthropic thinking signature。

## 11. Anthropic Messages bridge 设计

### 11.1 入站标准化

输入：Claude Code 发出的 Anthropic `/v1/messages`。

内部表示：

```text
CanonicalAgentTurn
  model
  provider_profile
  system blocks
  messages
  tools
  tool_choice
  thinking
  stream
  metadata safe summary
```

### 11.2 Claude Code 协议面优先级

Claude Code CLI 发出的原生请求是 Anthropic `/v1/messages`。因此 47 号的 provider bridge 必须优先服务 Claude Code 的 Anthropic Messages 语义，而不是照搬 Codex Gateway 的 OpenAI-compatible -> Responses 路径。

优先级：

1. **官方或探测通过的 Anthropic-compatible `/v1/messages`**：DeepSeek 已有官方 Anthropic API beta 文档；GLM/Z.AI、Kimi/Moonshot 等若官方提供 Claude Code 或 Anthropic-compatible 接入文档，也应优先按此路径接入。
2. **Provider-native Anthropic-like 能力最小包装**：若 provider 不是完整 Anthropic 兼容，但 tool_use/stream/reasoning/cache 形状接近，可做最小包装，并在 catalog 中标明缺口。
3. **OpenAI-compatible/Responses fallback**：只有当 provider 没有可用 Anthropic-compatible surface，或 probe/bridge fixture 证明其 Anthropic-compatible surface 在工具、SSE、reasoning、cache、error、context window 任一关键项不合格时，才走 OpenAI-compatible/Responses -> Anthropic Messages facade。

这与 Codex Gateway 的 DeepSeek 路径不同：Codex Desktop/Codex Gateway 当前可以使用 OpenAI-compatible -> Responses 转换；Claude Code CLI 接入 DeepSeek 时，应默认尝试 DeepSeek 官方 Anthropic-compatible `/v1/messages`，这样更贴近 Claude Code 的工具流、SSE、stop_reason 和消息历史语义。若实测证明 DeepSeek Anthropic-compatible 端点的 cache/tool/reasoning 表现弱于 Codex Gateway adapter fallback，则 catalog 可以把 DeepSeek 的 Claude Code transport 切到 fallback，但必须记录 probe 证据和回退原因。

Transport 选择统一经过 `CanonicalAgentTurn`，避免两套 bridge 逻辑互相漂移：

```text
Claude Code Anthropic /v1/messages
  -> CanonicalAgentTurn
  -> Transport A: Anthropic-compatible emit
     or Transport B: OpenAI-compatible/Responses emit
  -> provider stream
  -> CanonicalAgentTurn delta
  -> Anthropic content_block SSE
```

Per-provider Anthropic-compatible probe matrix：

| Probe item | 必须验证的行为 |
|---|---|
| text | 普通文本流式输出稳定 |
| tool_use pairing | tool_use/tool_result 成对，id 与 index 稳定 |
| SSE event order | 事件类型、顺序、content_block index 符合 Claude Code 可消费形状 |
| stop reason | 工具调用时保持 `stop_reason=tool_use`，不能误成 `end_turn` |
| thinking/reasoning | provider reasoning 可显示但不可 native replay；signature-like 字段不得透传 |
| cache | usage/cache 字段可统计，或至少 request prefix hash 可审计 |
| beta/header | 不支持的 Anthropic beta/header 被过滤或降级，不破坏请求 |
| context/image | context window、image/file input truthfulness 与 catalog 一致 |
| error | error passthrough 可被 Claude Code 恢复或清楚展示 |

### 11.3 既有 Codex Gateway 能力复用原则

47 号 Claude Code bridge 不能用一个降级的 Anthropic façade 替换已有 Codex Gateway 专项能力。需要区分复用边界：现有 Codex Gateway 面向 OpenAI Responses / Codex Desktop 形态，Claude Code bridge 面向 Anthropic Messages façade；可直接复用的是 provider adapter、cache、reasoning、accounting、error、Computer Use policy 等底层能力，不是把 OpenAI Responses 协议表面原样塞进 Anthropic Messages。实现时必须复用并保住现有：

- OpenAI Responses / Chat / previous_response_id / stream / error passthrough 兼容；
- DeepSeek KV cache 友好 prompt 稳定化、reasoning 映射、tool_search/deferred tools、session isolation；
- AGNES 独立 provider 兼容、thinking/tools 修复和 beta 降级策略；
- Computer Use 高保真语义压缩策略的 provider-specific guard，不能误影响 Claude native 或 GPT 原生体验；
- usage/cache/accounting/safe audit 的 provider-specific 统计。

每个 provider bridge 的测试都必须有 no-regression 断言，证明 Claude Code 接管没有把 Codex Gateway 已有调优降级。

### 11.4 OpenAI / GPT bridge

转换方向：

```text
Anthropic Messages
  -> OpenAI Responses or Chat
  -> OpenAI stream/tool events
  -> Anthropic content_block stream
```

重点：

- tool_use id 稳定；
- `input_json_delta` 分片 JSON 重组语义与 Anthropic SSE 一致；
- `stop_reason=tool_use` 保真，不能误写成 `end_turn`；
- function/tool schema 保真；
- reasoning effort 映射；
- previous_response_id 如可用则内部使用，不暴露给 Claude Code；
- stop/error 事件转为 Claude Code 可理解格式；
- 不让 GPT 走 Computer Use 高保真压缩策略时误影响 Claude native。

### 11.5 DeepSeek bridge

DeepSeek 在两个产品面里的最佳协议不同：

- Codex Desktop / Codex Gateway：继续保留已调优的 OpenAI-compatible -> Responses 路径和相关 cache/reasoning/tool 策略。
- Claude Code CLI / Claude Gateway：优先走 DeepSeek 官方或 probe 通过的 Anthropic-compatible `/v1/messages`，只在该路径缺失、不稳定或能力低于 fallback 时，才退到 OpenAI-compatible -> Anthropic facade。

需要复用 Codex Gateway DeepSeek 经验，但复用的是底层策略，不是协议表面：

- OpenAI-compatible 特殊字段；
- KV cache 友好 prompt 稳定化；
- tool_search/deferred tools bridge；
- reasoning/thinking 显示策略；
- 失败时保留 session isolation，避免 thought signature 污染。

对 Anthropic-compatible DeepSeek path 还必须额外验证：

- `/v1/messages` request/response shape；
- streaming SSE event order；
- tool_use/tool_result 与 `stop_reason=tool_use`；
- provider reasoning 与 Claude Code 可见文本的边界；
- 即使 DeepSeek 把 `reasoning_content` 包装成 Anthropic-looking thinking block，也必须按 foreign provider reasoning 处理：剥离、重标或转可见摘要，不得透传 foreign signature，不得写成 Claude native replayable history；
- usage/cache 命中字段是否能映射到 Sub2API accounting；
- error passthrough 是否保持 Claude Code 可恢复。

### 11.6 GLM / Kimi / 其它 Anthropic-compatible 国产模型

GLM/Z.AI、Kimi/Moonshot、Qwen、MiniMax 等后续模型不能通过 scattered hardcode 接入。统一要求：

- catalog 声明 `provider_family`、`protocol_family`、`preferred_claude_code_protocol`、`openai_compatible_fallback`、`probe_required`；
- 若官方提供 Claude Code/Anthropic-compatible 文档，优先实现 Anthropic Messages path；
- 若同时提供 OpenAI-compatible 与 Anthropic-compatible，两条 path 都可保留，但 Claude Code 默认选择 Anthropic-compatible，Codex Gateway 默认选择 Responses/OpenAI-compatible，除非实测证明相反；
- provider probe 必须覆盖 text、tools、stream、error、usage/cache、context window、image/input 限制；
- 所有非 formal-pool Claude provider 默认不可进入 `claude_code_native`。

### 11.7 External Claude-like / Anthropic-compatible bridge

逐梦云端或后续本地 BYOK 可能接入外部 Claude-like 上游，例如 Kiro/反代 Claude、第三方 Anthropic-compatible API、企业自建 Anthropic-compatible provider。这类模型的目标是能力最大化和协议保真，但安全身份必须与 formal pool native 分开：

- route 使用 `claude_code_bridge_anthropic_compat` 或更具体的 provider bridge，不使用 `claude_code_native`；
- `provider_owner` 可为 `zhumeng_managed_external`、`user_local` 或 `enterprise_managed`，但 `credential_scope` 不得是 `formal_pool`；
- 优先 passthrough Anthropic Messages 原生能力：tool_use、tool_result、thinking、image、cache-control、extended context、stream event shape；
- capability truthfulness 由 provider probe/catalog 标注，不能因为模型名是 Claude 就假定支持 official Anthropic 全能力；
- 不进入 CC Gateway formal subscription account pool，不使用 CCH/persona/account identity；
- 可以与 Claude 主控/子代理协作，但跨入 formal-pool Claude native 时仍走 safe final answer / safe tool_result / evidence summary boundary；
- 若后续某外部 Claude-like 上游被纳入 formal pool，必须另走 formal onboarding、capture、shape/persona、budget、account-safety 审核，不能只改 catalog route。

### 11.8 AGNES bridge

AGNES 目前稳定性较弱，首发可标 beta：

- 不默认放进 Claude Code `/model` 主列表；
- 或放在 Experimental 分组；
- Computer Use/长任务未稳定前不作为推荐模型。

## 12. Subagents / Workflow / MCP 适配

### 12.1 Subagent model options

Patch `getAgentModelOptions()` 或等价入口，使 subagent 可选。具体列表来自 ProviderRegistry/catalog，不硬编码；示例：

```text
Inherit from parent
Claude Sonnet
Claude Opus
Claude Haiku
External Claude-compatible
GPT Fast
GPT Main
DeepSeek Flash
DeepSeek Pro
GLM Fast
GLM Pro
```

默认必须是 `inherit`，避免跨 provider 意外走 Claude 号池。若用户显式选择跨 provider 子代理，必须走 transcript boundary：子代理内部历史留在子 provider，只把 final answer / safe tool_result / evidence summary 返回给父代理。Claude Opus 主控 + DeepSeek/GLM/GPT 子代理执行任务是首要产品场景，要求高质量支持，但不得把子代理内部 reasoning、provider-private history 或 raw tool runner state 原样 replay 给父代理。

### 12.2 Subagent model resolution

规则：

1. `inherit`：使用父线程当前模型与 provider profile。
2. family alias：在当前 provider 内映射，如 DeepSeek profile 下 `haiku/fast` -> DeepSeek Flash。
3. explicit model：按 catalog route。
4. unknown：fail closed，提示模型不可用。
5. cross-provider：进入 `summary_only` 或 `safe_tool_result` 边界，不原样重放子 provider assistant history。

### 12.3 Workflow / MCP

对 Workflow / MCP 中隐式模型调用：

- 若请求模型为 Claude family，走 native；
- 若当前 session profile 为 non-Claude 且请求 alias 为 fast/simple/haiku，映射到当前 provider fast model；
- 若 workflow 明确要求 Claude-only capability，则 UI 提示 fallback 到 Claude 或禁用该 workflow。
- 非 Claude profile 下，workflow/subagent 脚本或工具硬编码 Claude model id 时，默认不得静默消耗 Claude formal pool；应先重映射到当前 provider 等价模型，或要求用户显式 opt-in 并在 UI/audit 中标记会消耗 Claude 号池。
- Claude Code 自身的后台/快模型调用也必须纳入映射，包括标题生成、compact/历史摘要、quota 或模型可用性探测、Haiku 级小任务；非 Claude profile 下若无法证明已重映射，native egress 必须为 0 或 fail closed。
- 静态 `ANTHROPIC_DEFAULT_*_MODEL` env 只能作为进程启动默认值，不能作为唯一防线。用户启动在 Claude profile 后通过 `/model` 切到 DeepSeek/GPT 时，后台调用必须在请求时按 active profile 动态解析；否则 compact/title/probe 仍可能读取启动时 Haiku/Sonnet 默认值并静默消耗 formal pool。
- Compact 会重写会话可见历史并自然重置一段 cache。非 Claude profile 下 compact 摘要由当前 provider 撰写；之后切回 Claude 时，该摘要只能作为 safe summary 进入 Claude，而不是 Claude native 原始历史。

## 13. 本地文件与隐私边界

### 13.1 目录

```text
~/.zhumeng/claude-code/
  runtimes/
  profiles/
  sessions/
  captures/
  shell-integration/
```

### 13.2 禁止读取

- 默认 `~/.claude` OAuth；
- 默认 `~/.claude` cookie/setup token；
- 浏览器 Claude session；
- keychain 中 Claude 官方 token；
- 用户 shell 中已有 `ANTHROPIC_AUTH_TOKEN`。

### 13.3 允许记录

- runtime version；
- hash；
- route；
- model id；
- provider profile；
- status bucket；
- token usage；
- cache hit summary；
- tool count；
- safe error class。

### 13.4 禁止记录

本节约束 audit/log/capture/safe deliverable 的长期记录。active local transcript/cache 若为了同 provider 能力保真临时保存 provider 状态，必须遵循最小保留、本地隔离/加密、不得进入 audit、不得跨 provider replay、会话结束可清理的规则。该新增本地敏感数据面必须有 fixture 覆盖：加密落盘或仅内存、session 作用域、退出清理、不会进入 audit/safe deliverable。

- raw prompt；
- raw body；
- raw token；
- raw CCH；
- raw telemetry；
- email；
- account/org/user UUID；
- proxy credential。


## 14. 后续本地化与自定义供应商前向兼容

本方案必须为后续“把 Sub2API 云端网关能力下放到逐梦 Agent 本地”预留清晰边界，但执行顺序必须分两步：**先把云端/服务器侧 Codex Gateway、Claude Code Gateway、provider router、bridge、catalog、cache/accounting、安全审计做到最好、最稳定；再以这套成熟代码为来源下放到逐梦 Agent 本地打包。**

因此，本节是前向兼容约束，不是当前 47 号的立即实施范围。第一版可以继续依赖当前 Sub2API 云端能力，但实现时不得把 Codex Gateway、Claude Gateway、provider catalog、OAuth 登录和 model routing 写成只能云端运行的形态。

### 14.1 两阶段路线与后续目标

阶段一，也就是当前优先级：云端先行。先在 Sub2API 云端把以下能力做完整、做稳定、做高质量：

- Codex Gateway 对 GPT/Claude/DeepSeek/AGNES/后续模型的原生级协议适配；
- Claude Code Gateway 对 Claude native 与非 Claude bridge 的双通道隔离；
- provider catalog、capability profile、model routing、cache/accounting、safe audit；
- ToolSearch、Subagent、Workflow、stream、thinking/reasoning、跨 provider transcript boundary；
- Claude formal pool / CC Gateway 账号安全。

阶段二：本地下放。等云端代码和 live matrix 证明稳定后，再把可复用的 gateway runtime、provider registry、credential store、policy engine 打包到逐梦 Agent 本地，用于 BYOK、自有 OAuth 和企业本地部署。

后续逐梦 Agent 需要支持用户在本地添加自己的模型供应商，并与逐梦托管模型混合使用：

```text
逐梦托管模型
用户自有 OpenAI-compatible provider
用户自有 Anthropic-compatible provider
用户 OpenAI OAuth
用户 Anthropic OAuth
团队/企业本地 provider policy
```

这意味着 Codex Gateway 和 Claude Code Gateway 的关键能力最终都要能在本地运行：

- OpenAI Responses / Chat / OpenAI-compatible provider bridge；
- Anthropic Messages provider bridge；
- Claude Code native / bridge route split；
- model catalog；
- capability profile；
- usage/cache accounting；
- tool / stream / thinking compatibility；
- safe audit；
- provider credentials vault；
- OAuth token storage and refresh。

### 14.2 当前 47 号实现必须遵守的架构约束

即使首发仍调用云端 Sub2API，也必须抽象出以下接口，避免后续重构：

```text
ProviderRegistry
  list_models()
  resolve_route(model_id, session_profile)
  get_capabilities(model_id)

ProviderCredentialStore
  get_api_key_ref(provider_id)
  get_oauth_token_ref(provider_id)
  refresh_oauth(provider_id)
  redact_for_audit()

GatewayRuntime
  mode: cloud | local | hybrid
  forward_anthropic_messages()
  forward_openai_responses()
  forward_openai_chat()
  emit_anthropic_stream()
  emit_openai_stream()

PolicyEngine
  formal_pool_allowed(route, attestation, model_id)
  transcript_replay_allowed(from_provider, to_provider, turn)
  cache_policy(provider_id, model_id)
```

47 号里的 `Sub2API catalog route` 应被理解为 **ProviderRegistry 的一个实现**，不是永久唯一实现。后续本地版可以从：

- 本地用户配置；
- 云端托管 catalog；
- 企业策略下发；
- OAuth 账号能力探测；
- 自定义 provider probe；

合并生成同一个 canonical model catalog。

### 14.3 用户自定义供应商

首批本地 provider 类型只建议支持两类，避免过早发散：

| Provider type | 用户输入 | 本地能力 |
|---|---|---|
| OpenAI-compatible | Base URL + API Key + models/probe | Responses/Chat bridge、Codex Gateway 本地能力复用 |
| Anthropic-compatible | Base URL + API Key + models/probe | Anthropic Messages bridge、Claude Code bridge 复用；默认 external/non-formal，不进入 formal pool |

本地添加 provider 时必须：

1. 不把用户自有 provider 误标为逐梦托管 provider；
2. 不把用户 Anthropic-compatible provider 误标为 `claude_code_native`；
3. 不让用户自有 provider 进入 Claude formal subscription pool；
4. probe 只能做低成本、用户授权的能力探测；
5. API Key/OAuth token 存在本地 vault，默认不上传云端；
6. catalog 中清楚标注 provider owner：`zhumeng_managed` / `user_local` / `enterprise_managed`。

### 14.4 OpenAI OAuth 与 Anthropic OAuth

后续支持用户登录自己的 OpenAI OAuth / Anthropic OAuth 时，安全边界要比 API Key 更严格：

- OAuth token 只进入本地 encrypted vault / keychain；
- 默认不上传逐梦云端；
- refresh token 不写入普通日志、capture、crash report；
- 用户可一键撤销和删除本地 token；
- 每个 OAuth provider 必须有独立 scope、account_id、token_ref；
- OAuth 登录得到的能力只用于该用户自己的 local provider route，不与逐梦托管号池或 formal pool 混用；
- Anthropic OAuth 用户自有账号与逐梦 Claude formal pool 必须完全隔离。

Anthropic OAuth 特别约束：

```text
user_own_anthropic_oauth != zhumeng_formal_pool
```

用户自己的 Anthropic OAuth 可以走本地 Anthropic provider，但不能被 CC Gateway formal pool 的 CCH/persona/signing/account identity 逻辑吞并，也不能反过来把 formal pool 的账号状态暴露给用户本地 provider。

### 14.5 Cloud / Local / Hybrid 三种运行模式

后续逐梦 Agent 应支持三种部署模式：

| Mode | Provider credentials | Gateway logic | 适用场景 |
|---|---|---|---|
| `cloud` | 逐梦云端 | 云端 Sub2API | 当前托管模型、个人轻量用户 |
| `local` | 用户本地 | 逐梦 Agent 本地网关 | 用户 BYOK、自有 OAuth、隐私敏感 |
| `hybrid` | 混合 | 本地优先，云端托管 fallback | 同时使用自有模型和逐梦托管模型 |

47 号实现不得假设只有 `cloud`。所有 route decision 和 model catalog 都应能携带：

```text
runtime_mode
provider_owner
credential_scope
gateway_location
```

### 14.6 与当前计划的关系

当前 47 号仍然先解决 Claude Code CLI 的托管 runtime、多模型 `/model`、native/bridge 双通道和 transcript boundary，并以云端 Sub2API/CC Gateway 的成熟实现作为第一落点。它不是本地化 provider 管理的完整实现，也不应在当前阶段分散精力去重写本地 provider 管理面；但必须做到：

- route/catalog/credential/policy 有清晰接口；
- 云端实现先做成 source of truth，但接口上不把云端 Sub2API 写死为永久唯一实现；
- 不把逐梦托管模型和用户自有模型混同；
- 不把 OAuth token 设计成只能云端保存；
- 不把 Claude formal pool 与用户 Anthropic OAuth 混同；
- 后续下放 Codex Gateway / Claude Gateway 到本地时，不需要推翻 47 号模型列表、provider router、transcript boundary 和 capability profile 设计。

## 15. 实施 Checkpoint 计划

### CP0：46 号 native guard 红测修复与启动流接入

目标：先修复当前已知 native attestation 红测，并把 native guard 真正接入 managed runtime launcher/desktop 启动流；否则后续多模型工作没有运行中的 native path。

任务：

- 修复 `build_native_guard_plan()` 缺少 `--native-attestation` 的回归。
- 将 `build_native_guard_plan()` / `start_native_guard()` 接入 `zhumeng-claude start`、launcher/desktop 的真实 managed runtime 启动链路。
- 跑 Claude Code Python tests，至少覆盖 `tools/zhumeng-agent/tests/test_claude_code_guard.py` 中 native attestation 相关用例，并新增/更新启动流调用测试。
- 跑 Go native/compat targeted tests，至少覆盖 CC Gateway / FormalPool / Gateway / Account / DTO 中与 native route、formal-pool admission、compat body shape 相关的 targeted pattern。
- 更新 46/47 交界说明。

验收：

- `test_native_guard_forwards_attested_native_markers_without_prompt_leak` PASS。
- `build_native_guard_plan()` command includes `--native-attestation`，且移除该 flag 会导致测试失败。
- `claude_code_native` headers 完整。
- native attestation 绑定 route/model/runtime/overlay/catalog/session/body-shape/nonce/timestamp。
- `zhumeng-claude start` 真实路径会启动 loopback guard，并把 Claude Code base URL 指向该 guard。
- 不碰 Codex Gateway / DeepSeek / AGNES 逻辑。

### CP1：Managed Runtime installer 设计与骨架

任务：

- runtime manifest；
- version detector；
- cache path；
- hash lock；
- no-global-overwrite guard；
- rollback metadata。

测试：

- 不写 `/opt/homebrew/bin/claude`；
- 不读默认 `~/.claude`；
- unknown version fail closed。

### CP2：Runtime model overlay proof

任务：

- 探测 2.1.175 patch 点；
- 注入 mixed model options；
- 注入 display labels；
- 注入 model allowlist；
- 确认 `/model` 展示混合列表；
- 探测并实现 per-request route hint 注入 patch 点；若无法安全注入，则记录 degraded 模式并 fail closed。
- CP2 阶段的混合 `/model` 列表只允许用于 overlay proof、`--print`、静态探测和 model list capture。CP4 routing trust contract 绿之前，bridge 模型必须在 live catalog 中 feature-flag off，不能连接到 live formal-pool native path。

测试：

- static patch test；
- runtime smoke with `--print` where possible；
- model list capture；
- live catalog bridge models disabled assertion：CP2 期间 DeepSeek/GPT/AGNES display 可被捕获，但真实 runtime 选择 bridge 模型不得发出 live formal-pool 请求；
- rollback test。

CP2 exit gate：

- 进入 CP3 前，patched managed runtime 必须与未改 2.1.175 的代表性 Claude native request 通过 shape equality。
- verifier 与 CC Gateway signing pipeline 必须通过；失败时禁用 Claude formal pool path，不进入 mixed-provider runtime integration。
- CP4 fail-closed 对抗测试通过前，不得把 mixed `/model` bridge selections 连接到 live Sub2API formal-pool path；若要做 smoke，只能使用 mock/stub upstream 或 native-only degraded 模式。

### CP3：Subagent / Workflow model overlay + transcript boundary

任务：

CP3A：Subagent / Workflow model overlay。

- agent model options；
- `inherit` 默认；
- provider-local fast mapping；
- workflow alias mapping；
- active-profile dynamic resolver：标题、compact、summary、probe、fast/simple/haiku 后台模型在每次请求时按当前 `/model` profile 解析，静态 env 只作为启动默认值。

CP3B：Transcript boundary contract and fixtures。

- cross-provider subagent result boundary；
- provider-aware transcript sanitizer 接口、replay_class 与 fixture 设计；
- `ReplaySafeAnthropicTranscript` 不变式与 bridge 入口双向清洗 fixture；
- resume / continue / compact / checkpoint / history replay fixture 设计；
- 本 checkpoint 只定义 contract 与 fixtures，不依赖真实 bridge 已完成。

测试：

- parent DeepSeek -> subagent inherit stays DeepSeek；
- start Claude -> switch DeepSeek -> trigger title/compact/background fast task：native egress remains 0 unless user explicitly opts into Claude；
- explicit Claude subagent goes native；
- Claude parent -> DeepSeek subagent -> Claude parent 只回传 safe final answer/tool_result；
- unknown model fails closed；
- non-Claude thinking/reasoning/signature 不进入 Claude native replay。

### CP4：Routing trust contract

目标：在接任何真实 bridge 上游前，先让单个 Claude Code 进程内的逐请求 native/bridge 路由变成可验证、可对抗测试的合同。

任务：

- overlay 注入 signed route hint；
- guard 校验 route hint 与 `body.model`、catalog/runtime/overlay hash、session、nonce/timestamp 一致；
- guard 维护 nonce replay cache 与时钟窗口，确保 stale/replayed hint fail closed；
- guard 按 route 追加 `claude_code_native` 或 `claude_code_bridge_*`，unknown fail closed；
- backend 用服务端 catalog 自行解析 `body.model` 并复核 model->route->account binding；客户端 route hint 不参与 native 授权；
- guard 保持单一 Sub2API cloud upstream，不直接按 route 连接多个 provider。

测试：

- body 声称 Claude 但 route=bridge：fail closed；
- bridge 模型伪造 native header/client_type：fail closed；
- unknown model / missing hint / stale catalog / replayed nonce：fail closed；
- native route 才会盖 native attestation，bridge route 绝不盖 native；
- bridge route 正常转发时会盖 `claude_code_bridge_*` 并进入后端 bridge stub，stub 返回固定 Anthropic SSE；不能只测试 reject path。

### CP5：Provider router and bridge skeleton

任务：

- `claude_code_native` route guard；
- `claude_code_bridge_openai`；
- `claude_code_bridge_deepseek`；
- Stage 1：Sub2API cloud catalog 作为 ProviderRegistry 背后的 route source of truth；
- 不在 CP5 实现本地 BYOK/OAuth provider registry；
- runtime/overlay/catalog safe hash 写入 audit summary；
- safe usage/audit split。

测试：

- non-Claude never enters formal pool；
- Claude native still uses CC Gateway；
- bridge stream produces Anthropic-compatible event order；
- bridge tool-use SSE golden diff：以真实 Claude native tool-use SSE 为金样，断言 DeepSeek/GPT bridge 的事件类型、顺序、content_block index、`input_json_delta`、`stop_reason=tool_use` 等价；
- spoofed model id / spoofed client_type 不能升级到 native；
- route decision 与 catalog version 在 safe audit 中可追踪。

CP5 exit gate：

- spoofed model id / client_type / catalog version / runtime hash / overlay hash / route 任一不匹配时都必须 fail closed。
- 上述 fail-closed 测试通过前，不进入 CP6 bridge parity。

### CP6：DeepSeek + GPT bridge parity

任务：

- text stream；
- tools；
- reasoning mapping；
- usage/cache；
- error passthrough；
- stop/timeout；
- Anthropic Messages protocol compatibility 优先路径：DeepSeek/GLM/Kimi 等 provider 若官方或 probe 支持 `/v1/messages`，Claude Code bridge 优先使用该 path；OpenAI-compatible/Responses 只作为 fallback；
- protocol probe：逐 provider 记录 Anthropic-compatible 与 OpenAI-compatible 两条 path 的 text/tools/stream/usage/cache/error 差异，catalog 只能启用实测通过的能力；
- DeepSeek transport selection：默认 Anthropic-compatible transport A；若 tool/SSE/reasoning/cache/error fixture 不通过，回退 OpenAI-compatible/Responses transport B，并记录 fallback reason；
- 接入 CP3 定义的 provider transcript boundary sanitizer 到真实 bridge path。

测试：

- Claude Code CLI live smoke；
- ToolSearch fixture；
- subagent smoke；
- pure DeepSeek profile background fixture：包含标题生成、compact/摘要、后台快模型任务，断言 guard/backend native egress 计数为 0；
- dynamic background remap fixture：启动在 Claude profile，`/model` 切 DeepSeek 后触发标题/compact/后台快模型任务，断言 guard/backend native egress 计数为 0；
- Claude -> DeepSeek -> Claude switch fixture；
- foreign thinking/signature cleaning fixture；
- DeepSeek Anthropic-looking foreign reasoning fixture：DeepSeek Anthropic-compatible transport 返回的 thinking/signature-like block 不得进入 Claude native replay；
- resume/compact/history replay cleaning fixture；
- mid-tool-loop provider switch fixture：Claude tool_use 未配对时切 DeepSeek/GPT，必须先在原 provider 收尾或转安全摘要，不能把残缺历史发给新 provider；
- bridge ToolSearch fixture：DeepSeek/GPT 在 `ENABLE_TOOL_SEARCH=true` 下能物化 deferred tools 或通过 shim 正常工具调用；
- local transcript privacy fixture：本地 provider state 加密/隔离、session scoped、不进 audit、退出可清理；
- Codex Gateway no-regression fixture：DeepSeek cache/reasoning/tool_search、OpenAI Responses/cache、AGNES beta path、Computer Use provider-specific compression 不被降级；
- cache determinism/regression fixture：safe summary hash 冻结，TTL 内快切不破坏稳定前缀；DeepSeek baseline 与 DeepSeek->Claude->DeepSeek 弹跳命中率只允许一次性边界成本；
- cache ratio audit；
- no native contamination。

### CP7：UX / shell integration

任务：

- 逐梦 Agent UI 按钮；
- `zhumeng-claude`；
- optional shell alias；
- uninstall/rollback；
- status/doctor。

说明：rollback 首选 disable/manifest switch，不默认删除文件；涉及删除 runtime/cache/session 等破坏性清理时，必须先获得用户确认。

测试：

- first install；
- restart；
- alias enable/disable；
- official Claude unaffected。

### CP8：Live matrix and final review

矩阵：

- Claude Opus/Sonnet native；
- GPT main/fast bridge；
- DeepSeek Pro/Flash bridge；
- Subagent；
- Claude 主控 -> DeepSeek 子代理 -> Claude 主控；
- Claude -> DeepSeek/GPT -> Claude 手动切换；
- ToolSearch/MCP；
- Workflow；
- long context；
- interruption；
- cache/account audit；
- netwatch bypass。

每个 checkpoint 必须：

- 主控验收；
- 质量审查；
- 测试；
- 测试和审查通过后再 commit；
- 清理不再使用的后台代理/进程。

CP8 外部 live 证据组装流程：

0. 产品验收主路径必须是 Claude Code Runtime -> 逐梦/Sub2API Gateway（例如 `http://127.0.0.1:3012`）-> Sub2API 内部 provider routing。Claude/GPT/DeepSeek/后续模型的官方 API Key、订阅账号或自定义 provider URL 均配置在 Sub2API/逐梦 Agent 的 ProviderRegistry/账号池中，Claude Code Runtime 不直接要求操作者输入或直连 `api.anthropic.com`、`api.openai.com`、`api.deepseek.com` 等官网端点。
1. 使用专用证据目录收集 Sub2API gateway-backed provider provenance，不能指向源码 worktree：
   `zhumeng-agent claude-code live-matrix --collect-sub2api-provenance --run-id <run-id> --output-root <evidence-root>`。
   该命令的 Claude/GPT/DeepSeek probe 均进入同一个 Sub2API `/v1/messages`；Claude 携带 native attestation，GPT/DeepSeek 携带签名 bridge route hint，不得进入 Claude formal-pool native path。
2. 将上一步输出的 `live_provenance` 保存为 JSON，并与已完成的 live matrix scenario 证据组装：
   `zhumeng-agent claude-code live-matrix --assemble-external --evidence <matrix.json> --provenance <provenance.json> --out <external-matrix.json>`。
3. 组装器只绑定 provider provenance 并设置 `mode=external_provider_live_matrix`；它不得把 loopback/mock fixture 提升为 `live_provider_verified=true`，不得生成 scenario artifact。
4. `--collect-sub2api-provenance`、`--collect-provider-provenance`、`--assemble-external`、`--strict-live` 是互斥模式；组装输入若包含 inline headers/body/prompt/token/secret/payload 等敏感或 raw 字段必须 fail closed，不能写出外部矩阵。
5. 严格验收必须再次运行：
   `zhumeng-agent claude-code live-matrix --evidence <external-matrix.json> --strict-live`。
   只有当 Claude/GPT/DeepSeek provider provenance 与全部 CP8 scenario live artifacts 均为同一 `run_id`、hash 校验通过且无敏感内容时，才能进入 `external_live_passed`。Sub2API 模式下若 evidence 出现官方 provider endpoint、route/client_type 不匹配或 bridge 伪造 native，必须 fail closed。
6. `--collect-provider-provenance` 仅保留为隔离实验室/故障定位 fallback，用于官方直连对照；它不是 47 号逐梦版 CP8 产品验收路径。

## 16. 风险与缓解

| 风险 | 级别 | 缓解 |
|---|---:|---|
| patch 破坏官方 Claude Code | P0 | 只 patch 托管 runtime，不覆盖系统安装 |
| 非 Claude 流量污染 formal pool | P0 | client_type/route hard split，测试证明 |
| 跨供应商历史污染 Claude native | P0 | provider transcript boundary，切回 Claude 前清洗/摘要/新会话 |
| CCH 版本漂移 | P0 | 首发 2.1.175 known-good，candidate 需 healthcheck |
| ToolSearch 打开但后端不支持 | P1 | fixed MCP fixture + kill switch |
| `/model` patch 点随版本漂移 | P1 | version manifest + patch point detector + fallback |
| Subagent 默认回到 Claude | P1 | inherit-first + provider-local mapping |
| GPT/DeepSeek tool stream 不兼容 | P1 | bridge fixture + event order tests |
| 清洗过度影响非 Claude 能力 | P1 | 同 provider 内保真，仅跨 provider 出口清洗，必要时给用户选择继续留在当前 provider |
| DeepSeek Anthropic-compatible 端点行为不等价 | P0 | 按 provider probe 选择 transport；默认 A，fixture 失败回退 B，不因“兼容”假设 tool/cache/reasoning 等价 |
| 非确定性摘要打穿 cache | P0 | safe summary 一次冻结、稳定 hash、prefix determinism fixture，把 cache 命中作为正确性约束 |
| 用户难以回官方原版 | P1 | 不覆盖系统 claude，提供 explicit official path |
| 法务/授权风险 | P1 | 用户本机下载官方包，本地 overlay，不分发修改后二进制 |
| raw sensitive 泄漏 | P0 | summary-only capture + sensitive scan |
| attestation secret 被本机提取 | P0 | 把 attestation 降级为过滤器；服务端预算、shape/persona verifier、route/account policy 和吊销为最终防线 |
| 单进程内请求被错误路由 | P0 | per-request signed route hint + body.model 交叉校验；backend 以服务端 catalog 自行裁决，hint 不授权 |
| CP2 混合模型列表早于 CP4 路由合同 | P0 | CP4 绿之前 bridge 模型 live feature-flag off，只做 overlay proof/mock/stub；禁止连接 formal-pool native path |
| Workflow/subagent/后台快模型静默扣 Claude 号池 | P0 | 按 active profile 动态解析，不只靠静态 env；启动 Claude 后切 DeepSeek 的标题/compact/summary/probe fixture 要求 native egress=0 |
| 本地 provider state 形成新隐私面 | P1 | 加密/仅内存、session scope、退出清理、禁止进入 audit/safe deliverable |
| 本地 provider 与云端托管 provider 混路由 | P0 | provider_owner / credential_scope / gateway_location 进入 route decision 与 audit |
| 用户 OAuth token 泄漏或被云端化 | P0 | local encrypted vault/keychain，默认不上传，一键撤销 |
| 用户 Anthropic OAuth 与 formal pool 混用 | P0 | user_own_anthropic_oauth 与 zhumeng_formal_pool route/account identity hard split |

## 17. Acceptance Criteria

### 17.1 47 号 runtime launch acceptance

完成当前 47 号 runtime launch 后必须满足：

1. 用户可通过逐梦 Agent 安装托管 Claude Code Runtime 2.1.175。
2. 用户系统官方 `claude` 未被覆盖。
3. 用户可通过逐梦 Agent 或 `zhumeng-claude` 启动增强 Runtime。
4. 可选 shell integration 能让 `claude` 指向逐梦 wrapper，且可撤销。
5. Claude Code `/model` 可显示 Claude + GPT + DeepSeek 混合模型，AGNES 若未稳定则位于 beta/experimental 或默认隐藏。
6. Subagent model options 支持 inherit 和 provider-local fast/main 模型。
7. Claude 模型请求进入 `claude_code_native`，带 request-bound native attestation，并走 CC Gateway formal pool。
8. GPT/DeepSeek/AGNES 请求进入 `claude_code_bridge_*`，不得进入 formal pool。
9. ToolSearch 在 managed runtime 下按 profile 显式开启并通过 fixture 验证。
10. Bridge stream 事件可被 Claude Code CLI 正常消费。
11. 不读取/上传默认 `~/.claude` OAuth/cookie/setup token。
12. audit/log/capture/safe deliverable 不保存 raw prompt/body/token/telemetry/CCH；active same-provider transient state 只允许为能力连续性最小保留，必须本地隔离/加密，不进入 audit，不跨 provider replay，会话结束可清理。
13. Unknown Claude Code 版本不自动 patch。
14. Runtime overlay 可回滚；破坏性删除需要用户确认。
15. DeepSeek/GPT/AGNES 即使兼容 Anthropic Messages，也不得被标记为 `claude_code_native` 或进入 Claude formal pool。
16. catalog route 与 request model/client_type 不一致时 fail closed；伪造 `claude_code_native`、catalog version/hash、runtime hash、overlay hash、provider_owner、credential_scope、gateway_location、body shape hash 任一不能进入 formal pool。
17. Claude formal pool admission 必须同时满足 approved owner/scope/location、服务端 catalog 权威判定 native route、valid native attestation、known runtime/overlay/catalog hash、CC Gateway persona/profile；客户端 route hint 不得作为 native 授权依据。
18. Claude native overlay 不改变 Claude 请求 body/harness/system/tool/thinking/context_management；shape equality / verifier / CC Gateway signing pipeline 必须通过。
19. Claude -> DeepSeek/GPT/AGNES -> Claude 切换时，非 Claude thinking/reasoning/signature/provider 私有历史不会出现在 Anthropic 上游请求中。
20. Claude -> DeepSeek/GPT/AGNES 时，Claude thinking/signature/private metadata 不会泄露到非 Claude provider。
21. Claude 主控调用 DeepSeek/GPT/AGNES 子代理时，Claude 只接收 safe final answer/tool_result 或摘要，不接收子代理内部 assistant replay。
22. same-provider DeepSeek/GPT/AGNES 连续会话不触发出口清洗，不损 provider reasoning/tool 能力。
23. 清洗后 role/order/tool pairing verifier 失败时不发 Anthropic。
24. Resume / continue / compact / checkpoint / history replay 路径不得把 foreign reasoning/signature/tool internals 送入 Anthropic。
25. Catalog 签名、过期、撤销、anti-rollback、offline fail-closed 规则有测试覆盖。
26. 47 号至少定义或保留 ProviderRegistry / PolicyEngine / GatewayRuntime seams；Stage 1 cloud Sub2API catalog 是 source of truth behind the interface，但不是永久唯一实现。
27. Bridge parity 不降级既有 Codex Gateway DeepSeek/OpenAI Responses/AGNES/Computer Use/usage-cache-accounting 调优，并有 no-regression tests。
28. 单进程 routing trust contract 通过对抗测试：body/model 与 route hint 不一致、bridge 伪造 native、unknown/stale/replayed hint 均 fail closed；backend 必须用服务端 catalog 自行推导 route。
28a. CP4 绿之前，CP2 注入的 mixed `/model` bridge selections 不得连接 live formal-pool native path；bridge 模型在 live catalog 中必须 feature-flag off 或只连 mock/stub upstream。
29. Attestation secret 按本地可提取建模，服务端 budget/persona/shape/account policy/吊销为 formal pool 最终安全依赖。
30. Bridge 入口维护 `ReplaySafeAnthropicTranscript` 不变式，出口 verifier 只做兜底；真实 Claude-origin turn 不被改写。
31. 非 Claude profile 下 workflow/subagent/标题/compact/summary/probe 等硬编码或隐式 Claude model 不得静默消耗 formal pool；pure DeepSeek profile fixture 中 native egress 计数为 0。
32. Bridge tool-use SSE golden diff 覆盖 `input_json_delta`、content_block index 与 `stop_reason=tool_use`。
33. In-flight history stripping 不回写 Claude Code 持久化 transcript；mid-tool-loop provider switch 有 fail-closed 或摘要收尾 fixture。
34. Bridge ToolSearch、local transcript privacy、rolling known-good candidate 晋级均有 fixture 或 release checklist 覆盖。
35. 外部 Claude-like / Anthropic-compatible 上游作为 bridge 时不进入 formal pool，但能最大化保留 Anthropic Messages 能力，并有 capability truthfulness/probe 测试。
36. Claude Code provider bridge 的首选协议由 `preferred_claude_code_protocol` 决定；DeepSeek/GLM/Kimi 等若 Anthropic-compatible path probe 通过，必须优先使用 `/v1/messages`，不得无理由绕回 OpenAI-compatible -> Responses。
37. Codex Gateway 与 Claude Code Gateway 的协议面不得混淆：Codex Desktop 侧可以继续 OpenAI-compatible -> Responses，Claude Code 侧必须以 Anthropic Messages 语义验收。
38. DeepSeek transport 由 probe/fixture 决定：默认 Anthropic-compatible transport A；若 tool/SSE/reasoning/cache/error 不合格，回退 OpenAI-compatible/Responses transport B，并记录 fallback reason。
39. DeepSeek Anthropic-compatible transport 返回的 Anthropic-looking thinking/signature-like block 仍按 foreign reasoning 处理，不能进入 Claude native replay。
40. Cross-provider safe summary / safe_tool_result 必须一次冻结、稳定 hash、幂等复用；缓存回归 fixture 证明 TTL 内快切只产生缝处一次性代价，不持续破坏稳定前缀。
41. 后台模型映射必须按 active profile 动态解析；启动 Claude profile 后切到 DeepSeek 再触发 title/compact/background fast task 时，native egress 仍为 0，除非用户显式 opt-in Claude。
42. 相关 Python/Go/bridge tests PASS。

### 17.2 Stage 2 forward-compat constraints, not 47 runtime launch blockers

以下是后续本地 BYOK/OAuth/自定义供应商阶段的架构约束。47 号必须预留接口和 metadata，不得把云端 Sub2API 写死为永久唯一实现；但除非另开任务，不在当前 runtime launch 中实现本地 provider UI、本地 OAuth 登录或本地 credential vault。未来新增 GLM 5.x、Qwen、Moonshot、MiniMax、更多 DeepSeek 或其它国产 OpenAI-compatible/Anthropic-compatible 模型时，必须通过 ProviderRegistry/capability profile/route policy 接入，而不是新增一套散落的硬编码模型逻辑。

1. 用户自有 provider catalog 必须标注 provider_owner / credential_scope / gateway_location，并与逐梦托管模型分账、分 audit。
2. 用户 OpenAI/Anthropic OAuth token 默认只存本地 encrypted vault/keychain，不进入云端日志或 capture。
3. 用户自有 Anthropic OAuth 与逐梦 formal pool 完全隔离，不能共享 CCH/persona/account identity。
4. ProviderCredentialStore 应优先暴露 opaque token/key refs，而不是 raw token material。
5. Cloud/local/hybrid mode 的 route decision 必须能携带 runtime_mode、provider_owner、credential_scope、gateway_location。
6. 外部 Claude-like / Anthropic-compatible provider 默认是 `claude_code_bridge_anthropic_compat`，不是 `claude_code_native`；只有通过 formal onboarding 才能进入 formal pool。
7. 新增 GLM/其它国产模型时必须声明 provider family、protocol family、capabilities、cache policy、tool policy、transcript replay policy、formal-pool eligibility。

## 18. 结论

逐梦 Agent 对 Claude Code CLI 的竞争力不应停留在“改 Base URL 接单一供应商”。真正有壁垒的是：

```text
Claude Code 原生工具壳 + 混合模型列表 + 双通道安全网关 + provider capability truthfulness
```

首发应固定托管 Runtime 2.1.175，与 CCH/CC Gateway 画像对齐；通过逐梦 Agent 管理 runtime、patch、guard、bridge、shell integration。这样既能让用户在 Claude Code CLI 内部 `/model` 混合切换 Claude/GPT/DeepSeek，又能保证 Claude 订阅号池安全，不把非 Claude 流量伪装成 native Claude Code。

同时必须明确：DeepSeek/GPT 兼容 Anthropic Messages 协议是 bridge 体验增强，不是 Claude native 身份证明。逐梦 Runtime 可以在非 Claude provider 内尽量保真，但切回 Claude formal pool 前必须执行 provider transcript boundary，确保 Anthropic 上游看到的是干净、原生、可解释的 Claude Code native 请求态。

后续把 Codex Gateway / Claude Gateway 下放到逐梦 Agent 本地时，47 号方案应自然演进为 cloud/local/hybrid provider runtime，而不是推翻重写；用户自定义 API Key、OpenAI OAuth、Anthropic OAuth 都应成为 ProviderRegistry/CredentialStore 的本地实现。
