# 逐梦 Agent Claude Code 多模型托管运行时设计方案

日期：2026-06-15
状态：DRAFT v1
适用范围：逐梦 Agent V0.1/V1 Claude Code CLI 接管第二阶段
前置依赖：46 号 Claude Code native takeover、CC Gateway formal pool、Sub2API Codex/Claude/DeepSeek/AGNES 网关调优

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
5. 终端体验支持三档：逐梦 Agent 点击启动、`zhumeng-claude` 命令、用户显式同意后的 shell alias/shim `claude`。

本方案不是“粗暴魔改用户 Claude Code”，而是“逐梦托管 Claude Code Runtime + 双通道网关 + 原生能力保真”。

## 1. 目标与非目标

### 1.1 目标

1. 用户通过逐梦 Agent 安装并启动托管 Claude Code Runtime。
2. 用户可在 Claude Code CLI 内部 `/model` 模型选择里看到混合模型列表：Claude、GPT、DeepSeek、后续国产 OpenAI-compatible / Anthropic-compatible 模型。
3. Claude 模型请求保持真实 Claude Code native body，经 guard attestation 进入 Sub2API / CC Gateway / formal pool。
4. GPT、DeepSeek、AGNES 等非 Claude 模型请求走独立 bridge path，不冒充 `claude_code_native`，不污染 Claude 订阅账号池。
5. Subagents / Agent model options / Workflow 默认模型选择要跟当前 provider profile 一致。
6. ToolSearch / `tool_reference` / `defer_loading` 在 custom Base URL 下由逐梦 Agent 显式开启和验证，而不是依赖 Claude Code 默认 first-party host gate。
7. 版本更新可控：首发固定 2.1.175，后续版本进入 candidate profile，通过 shape healthcheck 才放行。
8. 用户可随时回到官方原版 Claude Code。
9. 不读取、不上传、不复制用户默认 `~/.claude` OAuth / cookie / setup token。
10. 所有 patch/overlay 有 hash、manifest、备份、回滚、禁用开关。

### 1.2 非目标

本阶段不做：

- 覆盖 `/opt/homebrew/bin/claude` 或系统全局官方安装；
- 未经用户授权写 shell rc 文件；
- 将 GPT/DeepSeek 请求伪装为 Claude Code native 请求进入 Claude formal pool；
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

Claude Code 内部 `/model` 目标体验：

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
       |     -> DeepSeek OpenAI-compatible upstream
       |
       |-- claude_code_bridge_agnes
             -> AGNES adapter
```

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

继承 46 号设计，但 CP0 必须先修复当前测试失败：native guard plan 没有传 `--native-attestation` 时，转发 headers 缺失：

```text
x-sub2api-client-type: claude_code_native
x-sub2api-guard-attested: true
x-sub2api-native-attestation: ...
x-sub2api-native-signature: ...
```

Guard 负责：

- strip 本机 Authorization / x-api-key / cookie；
- 注入逐梦 entry auth；
- 注入 native attestation；
- control-plane safe intent；
- netwatch bypass 检测；
- summary-only capture。

### 4.3 Provider router layer

根据 Claude Code 请求体 `model` 和当前 session profile 决定目标：

| Model pattern | Route | Client type | 是否可进 Claude formal pool |
|---|---|---|---:|
| `claude-*` / `opus` / `sonnet` / `haiku` | Claude native | `claude_code_native` | 是 |
| `gpt-*` | OpenAI bridge | `claude_code_bridge_openai` | 否 |
| `deepseek-*` | DeepSeek bridge | `claude_code_bridge_deepseek` | 否 |
| `agnes-*` | AGNES bridge | `claude_code_bridge_agnes` | 否 |

硬规则：

- 非 Claude 模型不得带 `claude_code_native`。
- 非 Claude 模型不得走 CC Gateway formal subscription account pool。
- Claude native 与 bridge 的 usage/cost/cache/evidence 必须分开记账。

### 4.4 Bridge layer

Bridge 的职责不是“让模型变成 Claude”，而是提供 Claude Code 可消费的 Anthropic Messages façade：

```text
Anthropic /v1/messages request
  -> normalized internal turn
  -> provider-specific request
  -> provider stream
  -> Anthropic content_block_* stream
  -> Claude Code CLI
```

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
      "description": "Fast low-cost model for coding tasks",
      "capabilities": {
        "tool_use": true,
        "thinking": true,
        "subagents": true,
        "mcp": true,
        "tool_search": "bridge",
        "context_window": 128000,
        "image_input": false
      }
    }
  ]
}
```

### 5.2 Capability truthfulness

不得虚标：

- 不支持 image 的模型，不在 UI 里展示 image-capable；
- 不支持 Claude-style thinking 的模型，标记为 `reasoning_bridge` 或 `hidden_reasoning_unavailable`；
- 不支持 1M 的模型，不显示 1M；
- ToolSearch 若只是 bridge 等价，不得标成 `native`。

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

### 7.1 首发固定推荐版本

首发固定：

```text
@anthropic-ai/claude-code@2.1.175
```

原因：

- 与当前 CCH/persona 算法对齐；
- 46/51/52 号 formal pool 安全材料围绕 2.1.175 建立；
- shape fixture、CCH fingerprint、CC Gateway persona 更可控。

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
- model 属于 Claude family；
- CC Gateway persona/profile known or candidate approved。

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

## 10. Anthropic Messages bridge 设计

### 10.1 入站标准化

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

### 10.2 OpenAI / GPT bridge

转换方向：

```text
Anthropic Messages
  -> OpenAI Responses or Chat
  -> OpenAI stream/tool events
  -> Anthropic content_block stream
```

重点：

- tool_use id 稳定；
- function/tool schema 保真；
- reasoning effort 映射；
- previous_response_id 如可用则内部使用，不暴露给 Claude Code；
- stop/error 事件转为 Claude Code 可理解格式；
- 不让 GPT 走 Computer Use 高保真压缩策略时误影响 Claude native。

### 10.3 DeepSeek bridge

复用 Codex Gateway DeepSeek 经验：

- OpenAI-compatible 特殊字段；
- KV cache 友好 prompt 稳定化；
- tool_search/deferred tools bridge；
- reasoning/thinking 显示策略；
- 失败时保留 session isolation，避免 thought signature 污染。

### 10.4 AGNES bridge

AGNES 目前稳定性较弱，首发可标 beta：

- 不默认放进 Claude Code `/model` 主列表；
- 或放在 Experimental 分组；
- Computer Use/长任务未稳定前不作为推荐模型。

## 11. Subagents / Workflow / MCP 适配

### 11.1 Subagent model options

Patch `getAgentModelOptions()` 或等价入口，使 subagent 可选：

```text
Inherit from parent
Claude Sonnet
Claude Opus
Claude Haiku
GPT Fast
GPT Main
DeepSeek Flash
DeepSeek Pro
```

默认必须是 `inherit`，避免跨 provider 意外走 Claude 号池。

### 11.2 Subagent model resolution

规则：

1. `inherit`：使用父线程当前模型与 provider profile。
2. family alias：在当前 provider 内映射，如 DeepSeek profile 下 `haiku/fast` -> DeepSeek Flash。
3. explicit model：按 catalog route。
4. unknown：fail closed，提示模型不可用。

### 11.3 Workflow / MCP

对 Workflow / MCP 中隐式模型调用：

- 若请求模型为 Claude family，走 native；
- 若当前 session profile 为 non-Claude 且请求 alias 为 fast/simple/haiku，映射到当前 provider fast model；
- 若 workflow 明确要求 Claude-only capability，则 UI 提示 fallback 到 Claude 或禁用该 workflow。

## 12. 本地文件与隐私边界

### 12.1 目录

```text
~/.zhumeng/claude-code/
  runtimes/
  profiles/
  sessions/
  captures/
  shell-integration/
```

### 12.2 禁止读取

- 默认 `~/.claude` OAuth；
- 默认 `~/.claude` cookie/setup token；
- 浏览器 Claude session；
- keychain 中 Claude 官方 token；
- 用户 shell 中已有 `ANTHROPIC_AUTH_TOKEN`。

### 12.3 允许记录

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

### 12.4 禁止记录

- raw prompt；
- raw body；
- raw token；
- raw CCH；
- raw telemetry；
- email；
- account/org/user UUID；
- proxy credential。

## 13. 实施 Checkpoint 计划

### CP0：46 号 native takeover 缺口修复

目标：先把现有 native path 修稳。

任务：

- 修 `build_native_guard_plan()` 缺 `--native-attestation`。
- 跑 Claude Code Python tests。
- 跑 Go native/compat targeted tests。
- 更新 46/47 交界说明。

验收：

- `test_native_guard_forwards_attested_native_markers_without_prompt_leak` PASS。
- `claude_code_native` headers 完整。
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
- 确认 `/model` 展示混合列表。

测试：

- static patch test；
- runtime smoke with `--print` where possible；
- model list capture；
- rollback test。

### CP3：Subagent / Workflow model overlay

任务：

- agent model options；
- `inherit` 默认；
- provider-local fast mapping；
- workflow alias mapping。

测试：

- parent DeepSeek -> subagent inherit stays DeepSeek；
- explicit Claude subagent goes native；
- unknown model fails closed。

### CP4：Provider router and bridge skeleton

任务：

- `claude_code_native` route guard；
- `claude_code_bridge_openai`；
- `claude_code_bridge_deepseek`；
- safe usage/audit split。

测试：

- non-Claude never enters formal pool；
- Claude native still uses CC Gateway；
- bridge stream produces Anthropic-compatible event order。

### CP5：DeepSeek + GPT bridge parity

任务：

- text stream；
- tools；
- reasoning mapping；
- usage/cache；
- error passthrough；
- stop/timeout。

测试：

- Claude Code CLI live smoke；
- ToolSearch fixture；
- subagent smoke；
- cache ratio audit；
- no native contamination。

### CP6：UX / shell integration

任务：

- 逐梦 Agent UI 按钮；
- `zhumeng-claude`；
- optional shell alias；
- uninstall/rollback；
- status/doctor。

测试：

- first install；
- restart；
- alias enable/disable；
- official Claude unaffected。

### CP7：Live matrix and final review

矩阵：

- Claude Opus/Sonnet native；
- GPT main/fast bridge；
- DeepSeek Pro/Flash bridge；
- Subagent；
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
- commit；
- 清理不再使用的后台代理/进程。

## 14. 风险与缓解

| 风险 | 级别 | 缓解 |
|---|---:|---|
| patch 破坏官方 Claude Code | P0 | 只 patch 托管 runtime，不覆盖系统安装 |
| 非 Claude 流量污染 formal pool | P0 | client_type/route hard split，测试证明 |
| CCH 版本漂移 | P0 | 首发 2.1.175 known-good，candidate 需 healthcheck |
| ToolSearch 打开但后端不支持 | P1 | fixed MCP fixture + kill switch |
| `/model` patch 点随版本漂移 | P1 | version manifest + patch point detector + fallback |
| Subagent 默认回到 Claude | P1 | inherit-first + provider-local mapping |
| GPT/DeepSeek tool stream 不兼容 | P1 | bridge fixture + event order tests |
| 用户难以回官方原版 | P1 | 不覆盖系统 claude，提供 explicit official path |
| 法务/授权风险 | P1 | 用户本机下载官方包，本地 overlay，不分发修改后二进制 |
| raw sensitive 泄漏 | P0 | summary-only capture + sensitive scan |

## 15. Acceptance Criteria

完成后必须满足：

1. 用户可通过逐梦 Agent 安装托管 Claude Code Runtime 2.1.175。
2. 用户系统官方 `claude` 未被覆盖。
3. 用户可通过逐梦 Agent 或 `zhumeng-claude` 启动增强 Runtime。
4. 可选 shell integration 能让 `claude` 指向逐梦 wrapper，且可撤销。
5. Claude Code `/model` 可显示 Claude + GPT + DeepSeek 混合模型。
6. Subagent model options 支持 inherit 和 provider-local fast/main 模型。
7. Claude 模型请求进入 `claude_code_native`，带 native attestation，并走 CC Gateway formal pool。
8. GPT/DeepSeek 请求进入 `claude_code_bridge_*`，不得进入 formal pool。
9. ToolSearch 在 managed runtime 下按 profile 显式开启并通过 fixture 验证。
10. Bridge stream 事件可被 Claude Code CLI 正常消费。
11. 不读取/上传默认 `~/.claude` OAuth/cookie/setup token。
12. 不保存 raw prompt/body/token/telemetry/CCH。
13. Unknown Claude Code 版本不自动 patch。
14. Runtime overlay 可回滚。
15. 相关 Python/Go/bridge tests PASS。

## 16. 结论

逐梦 Agent 对 Claude Code CLI 的竞争力不应停留在“改 Base URL 接单一供应商”。真正有壁垒的是：

```text
Claude Code 原生工具壳 + 混合模型列表 + 双通道安全网关 + provider capability truthfulness
```

首发应固定托管 Runtime 2.1.175，与 CCH/CC Gateway 画像对齐；通过逐梦 Agent 管理 runtime、patch、guard、bridge、shell integration。这样既能让用户在 Claude Code CLI 内部 `/model` 混合切换 Claude/GPT/DeepSeek，又能保证 Claude 订阅号池安全，不把非 Claude 流量伪装成 native Claude Code。
