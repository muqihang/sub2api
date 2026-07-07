# 逐梦 Agent 训练数据飞轮设计方案

> 版本：v0.6 个人自有池/默认聚合/数据再利用与贡献交易修订版
> 日期：2026-06-18（v0.5 → v0.6 补强）
> 适用范围：`sub2api-zhumeng-main/tools/zhumeng-agent`、Codex Gateway、未来逐梦 Agent 桌面客户端
> 文档定位：产品策略 + 数据架构 + 采集设计 + 训练数据出口方案，**不含实现代码**
> 上位策略：数据归属/授权/分级以《逐梦企业学习闭环与数据主权》（[文档2](./zhumeng-enterprise-learning-loop-and-data-sovereignty-design.md)）为准；本文负责采集机制与训练出口的实现。

---

## 0. 结论摘要

逐梦 Agent 若要为未来自有模型训练建立数据壁垒，不能只记录普通聊天日志，而应沉淀真实 Agent 使用过程中的完整执行轨迹：

```text
用户任务
  -> 上下文
  -> 模型选择
  -> Agent 计划
  -> 工具调用
  -> 工具结果
  -> 中间修正
  -> 最终产物
  -> 用户反馈
  -> 任务结果
```

这些轨迹可用于：

1. 评估不同开源模型的 Agent 能力；
2. 训练模型路由器；
3. 训练工具调用能力；
4. 训练 Planner / Executor；
5. 构建 SFT、DPO、轨迹蒸馏、奖励模型和强化学习数据；
6. 支撑未来基于 DeepSeek、Qwen、GLM、Llama 等开源/开放权重模型继续训练出逐梦自有权重。

本方案建议采用“用户授权后尽快采集可训练原文”的产品策略，但必须提供清晰、可撤回、可审计的数据采集开关。默认保留低风险的指标与协议形状采集；可训练原文数据必须经过明确授权。


### 0.1 本次调研补强后的关键判断

基于 Claude Code、OpenAI Agents SDK、OpenTelemetry GenAI、MCP、ReAct、Toolformer、ToolBench、SWE-agent、OpenHands、Agent Lightning、AgentGym、tau-bench 等公开资料，训练最强 Agent 模型所需数据不应只覆盖 prompt、response、tool call，还必须覆盖以下“容易被忽略但训练价值极高”的数据：

1. **Harness / ACI 数据**：Agent 可见的工具界面、命令空间、权限模式、sandbox、工作区、上下文压缩、hook、plugin、skill、MCP server、subagent 结构。SWE-agent 与 OpenHands 的公开研究均表明，Agent-computer interface / harness 设计会显著影响模型行为和任务成功率。
2. **工具决策前数据**：模型提出工具调用但被用户、策略、hook、权限系统拒绝的记录。这类“未执行动作”是偏好学习和安全训练的关键负样本。
3. **工具错误恢复数据**：参数错误、schema validation 失败、命令失败、测试失败、MCP 连接失败、重试与修正过程。MCP 规范明确指出工具执行错误可反馈给模型用于自我纠正。
4. **上下文工程数据**：上下文如何被选择、压缩、丢弃、缓存、复用；compaction 前后 token、摘要质量、cache read/cache creation。Claude Code 与 Anthropic context engineering 资料都显示上下文管理是长任务 Agent 能力的核心。
5. **隐式采纳信号**：很多 CLI/桌面 Agent 没有明确“采纳/不采纳”按钮，因此必须从 git diff、文件最终状态、测试是否通过、用户是否继续编辑、是否复制/提交/PR/回滚等行为推断接受度。
6. **多模型与多轨迹对比**：同一任务下不同开源模型、不同 harness、不同工具描述、不同上下文策略的轨迹差异，是训练 router、reward model 和 teacher trajectory selection 的核心数据。
7. **轨迹级信用分配数据**：未来做 RL 时，必须能把 episode 分解为 state/action/observation/reward/terminal transition。Agent Lightning 等工作强调应将 agent execution 与 training 解耦，并从复杂轨迹中做 credit assignment。
8. **生态资产数据**：plugin、skill、MCP server、tool schema、tool description、AGENTS.md/CLAUDE.md/规则文件、hooks、marketplace 来源、版本、启用方式，这些决定模型“会不会正确使用生态能力”。

因此，本方案从 v0.2 起采用一个更强的定义：

> 可训练数据 = 用户授权内容原文 + Agent 全链路执行轨迹 + Harness/环境/权限/工具生态状态 + 成功/失败/拒绝/回滚/重试/压缩/协作等过程信号。


### 0.2 v0.3 对 Reasoning/CoT 与采集位置的修订

本方案 v0.3 进一步明确两点：

1. **Reasoning/CoT 有高训练价值，不应简单排除。** 对 DeepSeek-R1 等开源/开放权重推理模型，若模型本身公开输出 reasoning/CoT，且部署来源、许可证、服务条款和用户授权均允许，应将其作为 L3+ 高价值数据采集。对于“隐藏 CoT / internal reasoning trace”，也应设计可开关的采集能力，但仅允许在 self-hosted、白名单模型、强授权、强隔离、强审计条件下开启。
2. **sub2api / gateway 侧应成为采集主干，本地逐梦 Agent 负责补全端侧事实。** 当前逐梦 Agent 初期接管 Claude Code CLI、Codex Desktop 等现有 Agent 工具，模型请求经过 sub2api/gateway。因此 prompt、response、tool call 意图、reasoning 字段、usage、provider、stream event 等主干数据应优先在网关侧统一观测；本地工具则补充工具执行结果、用户采纳/回滚、文件 diff、终端输出、权限决策、UI 操作、MCP/plugin/skill/hook 等网关看不到的数据。**注意：网关侧采集不等于业务原文一定出客户边界；企业/私有化部署时 gateway-lite / sub2api 应位于客户边界内，中心侧只能接收合同允许的控制面、脱敏数据或共建贡献。**

因此，v0.3 的架构原则是：

```text
网关侧采模型与协议主干
本地采 Harness 与真实执行结果
端侧先做分级/脱敏/出境决策
Episode Builder 按数据池合并为客户资产或训练样本
```

---

### 0.3 v0.4 修订说明（归属 + 边缘脱敏 + 闭环采集对象）

> v0.4 在 v0.3 基础上做五件事，并严格遵守 v3 brief §11.1“深度不超现实一个版本”：只补近期该落地的采集机制与归属底座，**训练方法级数据只在文末附录登记、不展开**。

#### 0.3.1 归属/授权/分级以文档2为准

数据归属、授权、分级、出境策略统一以《逐梦企业学习闭环与数据主权》（文档2）为准，本文负责实现。落地要求：每条采集事件出生即带不可篡改归属标签——

- `owner`：tenant / user / 逐梦（**默认 tenant 或 user，绝不默认逐梦**）；
- `tier`：L0/L1/L2/L3/L3+；
- `egress_allowed`：是否允许离开客户边界（**企业/私有化档默认 false**）；
- `contribution_consent_id`：若 opt-in 反哺逐梦，引用对价授权。

归属矩阵（默认）：个人/团队默认匿名 L0/L1、内容类 L2/L3 一律 opt-in；企业/私有化业务数据默认不出域，控制面元数据与合同化贡献按最小化原则和出境账本处理。

#### 0.3.2 边缘脱敏架构修正（覆盖 §7.0 与 §8.3）

v0.3 的“服务端优先采集原文 + 服务端 redaction”与“数据不出域”承诺冲突（原文越过客户边界才脱敏）。v0.4 修正为：

```text
端侧先分级 → 端侧脱敏（出境前完成）→ 受控出境 → 服务端二次校验（defense in depth）
```

- **分级与脱敏首道防线在端侧、出境前**；服务端 redaction 降级为二次校验，不得作为唯一防线；
- **fail-closed**：端侧分级器拿不准时往高判（默认当 L2、不出境）；
- 企业/私有化档：L2/L3 原文在本地处理完之前**一个字节不出境**。

#### 0.3.3 出境账本（egress manifest）

新增**客户自持、防篡改、可逐条核对**的出境清单，区别于已有 consent 记录与删除账本：

- 记录到底什么离开了客户边界；企业档业务数据应可证明零出境，控制面元数据和合同化贡献也必须逐条记账（见 v0.5 三池模型）；
- 防篡改（hash 链/签名），验证能力归客户而非逐梦；
- 是“可验证不采集”的承载对象。

#### 0.3.4 闭环采集对象登记（仅登记存在与归属，字段细化留 V1）

为支撑文档2 §4 的企业学习闭环（尤其“工作流盘点/优化面”），登记以下此前缺失的采集对象——**本版只登记其存在与归属，schema 细化留待 V1**：

1. 组织/角色/团队图谱（盘点前提；现仅有 user/tenant hash）；
2. 工作流作为一等对象（重复工作流聚类 → 黄金轨迹/公司 skill）；
3. 成本/ROI/token-maxing 归因；
4. 多人交接（人-人，区别于 §5.13 的 AI subagent）；
5. 企业 system-of-record 数据血缘（从哪读、写到哪）；
6. 自然标注流（人类修正即金标签，推广到所有任务类型，不止 §5.15 的编程 diff）。

#### 0.3.5 采集工程补强（近期）

- **episode 边界切分**：补 episode 起止判定（一个 CLI session 常跨多个不相关任务，切错则下游轨迹作废），不再仅靠时间窗口（§7.0/§8.4）；
- **采集不进热路径**：采集必须异步、有界、降级时绝不阻塞 Agent；
- **schema/template 漂移可比**：同一逻辑工具跨工具/版本变更时保持纵向可比；
- **PIPL 第三方个人信息**：采集内容夹带的第三方个人信息属数据处理者合规义务，区别于 §11.5 的密钥/PII 扫描。

#### 0.3.6 训练方法级数据

RLVR replay、teacher logprobs、多候选采样、PRM、loop 遥测、私有 eval 训练集等，按 v3 §11.1 **只在文末“附录·未来留缝”登记列名，不展开**。

---

### 0.4 v0.5 修订说明（三池模型 + 共建开关 + 分级例子）

v0.5 进一步明确：**采集能力要从现在预埋，逐梦模型反哺要从现在可选择积累，但默认策略必须按客户档、授权和归属隔离。**

#### 0.4.1 三个数据池

所有采集数据进入以下三类之一，禁止混用：

1. **客户自有池**：默认归客户/租户/个人，用于客户自己的工作流盘点、skill 沉淀、私有 eval、私有训练或治理审计。个人用户开启 L2/L3 后默认进入个人自有池（`owner=user` + `data_pool=customer_owned`，local-first）；企业/私有化默认只进入租户自有池，且业务数据默认不出域。
2. **逐梦共建池**：个人/团队/企业在显式 opt-in 或合同约定后，贡献给逐梦用于产品改进、共建模型、行业 benchmark 或模型训练的数据池。企业贡献必须有 `contribution_consent_id` 和对价/用途约定。
3. **公共训练池**：从共建池中再经过授权校验、脱敏/敏感信息扫描、质量筛选、license/teacher_eligible 检查后，才可进入逐梦模型训练。

#### 0.4.2 能力预埋，启用靠开关

反哺逐梦模型不是当前交付承诺，但数据结构、授权口、共建池、导出格式和训练标签必须从第一版预埋；否则等真正训练时再补，会错过长期轨迹积累。

落地原则：

```text
能力默认具备
采集默认分档
贡献默认关
启用靠用户/租户授权、合同和开关
```

#### 0.4.3 企业共享回流

企业默认不共享 L2/L3/L3+ 给逐梦，但可以合同化开启：

- 共享 L0/L1 聚合指标；
- 共享脱敏 L2 工作流样本；
- 共享特定行业 benchmark；
- 共享经审核的 workflow / skill；
- 共同建设行业模型或企业私有模型。

共享数据必须进入逐梦共建池，而不是直接进入公共训练池。

### 0.5 v0.6 修订说明（个人自有池 + 默认聚合 + 数据再利用/贡献交易）

v0.6 进一步补齐两个产品闭环：**个人也拥有自己的数据闭环**，以及**客户留存数据必须能被清洗、利用、贡献和交易**。

#### 0.5.1 个人自有池明确化

客户自有池不仅指企业/团队，也包括个人用户：

- 个人用户开启 L2/L3/L3+ 后，默认进入**个人自有池**，即 `owner=user` 且 `data_pool=customer_owned`，优先 local-first 保存；
- 个人自有池可用于个人自己的工作流复盘、个人 skill、私人 eval、个人模型/LoRA/偏好数据整理；
- 个人贡献给逐梦共建池是独立 opt-in，不因开启个人数据飞轮而自动发生；
- “可携带的个人 AI 工作记忆”作为 V1+ 产品留缝登记，不在本版展开。

#### 0.5.2 人员维度默认聚合，人级分析显式开启

企业确实需要知道哪些团队/岗位/流程 AI 用得好，也可能需要人员维度能力。但默认姿态必须从“默认做人级画像”改为：

```text
默认：团队/角色/项目/工作流聚合分析
显式开启：个人排名、个人失败率、个人 token 异常、个人 skill 贡献等人级分析
```

人级分析需要租户管理员显式开启，并绑定企业制度、员工告知、可见范围、导出权限和当地合规要求；系统不得在默认模式下先做人级绩效画像、再只控制展示。

#### 0.5.3 数据价值再利用机制

客户留存的数据若不能整理，就只是日志。逐梦需要预埋“数据资产工作台”能力，把客户自有池或共建池数据加工成可复用资产：

```text
数据盘点 -> 分级/权属确认 -> 脱敏/去重/去污染 -> episode 修复 -> 质量评分 -> 自然标注 -> workflow/skill 挖掘 -> 私有 eval/训练集/看板/导出
```

企业侧用于流程优化、skill 库、私有 eval、私有训练和 ROI 看板；个人侧用于个人工作流复盘、可携带个人 AI 工作记忆、个人偏好/skill 和可选择贡献包。

#### 0.5.4 数据贡献与交易机制

对不打算自己训练模型、但愿意变现数据的个人/团队/企业，应预埋贡献与交易机制：

- 客户自行选择贡献范围、数据档、用途、期限和对价；
- L0/L1 可做聚合指标/协议形状包，L2 可做强脱敏工作流样本或行业 benchmark，L3/L3+ 原文默认不交易，除非强合同、强授权、强审计；
- 逐梦可以作为收购方购买客户授权数据，也可以未来作为数据交易撮合方；
- 每次贡献/交易都生成贡献合同、数据包快照、出境账本、交易账本和用途限制，贡献数据先进入逐梦共建池，不得直接进入公共训练池。


---

## 1. 背景与目标

### 1.1 背景

当前项目中已经具备逐梦 Agent 的关键基础能力：

- 本地代理：`tools/zhumeng-agent/src/zhumeng_agent/proxy/server.py`
- Codex Desktop Capture：`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`
- 本地 capture 配置：`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_config.py`
- 后端 Codex Gateway Capture：`backend/internal/service/codex_gateway_capture.go`
- 后端 Codex Gateway Capture 配置：`backend/internal/service/codex_gateway_capture_config.go`
- 后端协议形状提取：`backend/internal/service/codex_gateway_capture_shape.go`
- DeepSeek / Anthropic / OpenAI Responses 等 provider adapter 与 gateway 流转能力

现有 capture 体系以 shape-only、诊断、安全脱敏为主。它适合协议排障和兼容性验证，但距离“可用于未来训练 Agent 模型的数据资产”还差四类能力：

1. 用户授权和训练用途标记；
2. 原文级训练数据采集；
3. episode/turn/tool/artifact/feedback/outcome 的统一数据模型；
4. 从 capture 到 eval/SFT/DPO/tool-use/router 数据集的流水线。

### 1.2 目标

本方案目标是设计一套逐梦 Agent 数据飞轮：

- 在用户使用逐梦 Agent 工具时，采集可训练的真实 Agent 执行轨迹；
- 对不同模型型号、provider、adapter、模型许可证和任务结果进行标记；
- 通过授权开关区分匿名指标、脱敏轨迹、原文训练数据；
- 将采集数据沉淀为可查询、可审计、可删除、可导出的数据资产；
- 支持客户自有闭环、客户私有训练和逐梦未来模型反哺，而不是只服务产品日志分析。

范围声明：本文中“训练逐梦自有模型”的数据来源仅限个人/团队显式加入共建计划，或企业/私有化客户通过合同和 `contribution_consent_id` 明确贡献的数据；个人开启 L2/L3 默认只进入个人自有池，企业默认采集数据只服务客户自有闭环，均不默认进入逐梦训练。

### 1.3 非目标

第一阶段不做：

- 直接训练大模型；
- 自动把所有用户数据默认用于训练；
- 隐式采集用户未授权的源代码、文档、邮件、网页内容；
- 未授权、未标记来源、未进入白名单地采集或暴露模型内部推理链；
- 绕过第三方闭源模型或商业 API 的服务条款；
- 在未做租户隔离和删除机制前开放企业级训练数据池。

---

## 2. 核心概念

### 2.1 Agent Episode

Agent Episode 是一次用户目标驱动的完整任务过程。

示例：

- “修复登录接口 500 错误”；
- “帮我整理这份市场调研报告”；
- “基于这些网页写一份竞品分析”；
- “把这个表格做成可视化分析”；
- “在浏览器里完成某个后台配置”。

Episode 可能包含多轮对话、多次模型调用、多次工具调用、多个中间产物和最终结果。

### 2.2 Agent Turn

Turn 是一次模型请求/响应过程。一个 Episode 中可能有多个 Turn。

每个 Turn 需要绑定：

- episode_id；
- turn_id；
- provider；
- model；
- upstream_model；
- adapter；
- request/response；
- token usage；
- status；
- latency。

### 2.3 Agent Step

Step 是 Agent 在执行中的一个可观察动作，例如：

- 生成计划；
- 读取文件；
- 调用 shell；
- 打开网页；
- 修改文档；
- 运行测试；
- 请求用户确认；
- 输出最终答案。

### 2.4 Tool Call / Tool Result

Tool Call 是模型或 Agent 框架触发的工具调用。Tool Result 是工具返回给模型或系统的结果。

这些数据是训练 Agent 能力的核心。

### 2.5 Artifact

Artifact 是任务过程中产生或修改的结果对象，例如：

- 代码 diff；
- 新建文件；
- 文档草稿；
- 邮件草稿；
- 表格；
- 图表；
- 研究报告；
- 浏览器操作结果。

### 2.6 Feedback

Feedback 是用户或自动系统对输出质量的反馈，例如：

- 接受；
- 拒绝；
- 点赞/点踩；
- 重新生成；
- 用户手工编辑；
- 测试通过；
- CI 通过；
- 用户最终发送/提交/部署。

### 2.7 Outcome

Outcome 是 Episode 的最终结果：

- completed；
- failed；
- abandoned；
- partially_completed；
- reverted；
- escalated_to_human。


### 2.8 Harness / ACI

Harness 是 Agent 被包裹和运行的工程系统，包括：

- CLI / Desktop / IDE / Web 宿主；
- 工具注册与调用层；
- MCP server / plugin / skill / subagent；
- sandbox、权限、确认、hook、policy；
- 工作区、文件系统、shell、浏览器、computer-use；
- 上下文构建、压缩、缓存、检索和记忆系统；
- 评测、回放、重试、状态恢复和 trace 系统。

ACI（Agent-Computer Interface）是 Harness 暴露给模型的操作界面。公开研究表明，ACI 会影响 Agent 是否会正确浏览仓库、编辑代码、执行测试和恢复错误。因此必须把 Harness/ACI 当作训练数据的一部分，而不是只记录模型输入输出。

### 2.9 Observation 与 State

为了未来训练工具调用、行为克隆和强化学习，必须把每个 Step 表达为：

```text
state_t -> action_t -> observation_t -> reward_t -> state_{t+1}
```

其中 observation 包括工具结果、错误、测试输出、页面状态、文件状态、用户反馈、权限反馈、hook 反馈等。state 包括任务目标、上下文窗口、可用工具、工作区状态、权限模式、已完成步骤和未解决错误。

### 2.10 Negative Action / Rejected Action

未执行或被拒绝的动作同样重要，包括：

- 用户拒绝工具调用；
- hook 阻止；
- policy 阻止；
- 权限模式阻止；
- schema validation 阻止；
- sandbox 阻止；
- 模型提出高风险命令但未被执行。

这些是训练安全边界、工具选择和偏好模型的关键负样本。


### 2.11 Reasoning / CoT

Reasoning/CoT 在本方案中分为五类，采集策略不同：

1. **Visible plan**：用户可见的计划、步骤、行动摘要。默认按 L2/L3 策略采集。
2. **Visible rationale**：模型公开给用户的工具选择理由、失败恢复理由、自检摘要。默认按 L2/L3 策略采集。
3. **Provider reasoning field**：模型 API 明确返回的 reasoning、reasoning_content、thinking、chain_of_thought 等字段。若 provider/model/source 允许，按 L3+ 策略采集。
4. **Self-hosted internal trace**：逐梦自托管开源模型时，由推理服务、解码器、instrumentation 或模型运行时暴露的中间推理 trace。仅允许白名单模型 + 强授权 + 强隔离开启。
5. **Unauthorized hidden CoT**：闭源模型、第三方服务或未授权运行时未公开但试图绕过获取的隐藏推理链。本方案禁止采集。

Reasoning/CoT 对训练价值很高，但也可能包含敏感上下文、错误推理、噪声、版权/服务条款风险。因此必须单独标记 `reasoning_source`、`reasoning_visibility`、`reasoning_policy`、`reasoning_trainable` 和 `reasoning_quality_score`。

---

## 3. 数据采集分层

逐梦 Agent 应同时支持四档数据模式。

### 3.1 L0：匿名产品指标层

默认可开启，可关闭。

采集内容：

- 模型使用量；
- 任务类型；
- 请求量；
- token 用量；
- 成功率；
- 延迟；
- 错误率；
- 工具调用次数；
- 工具失败率；
- 客户端版本；
- provider/adapter 维度统计。

不采集：

- 用户原始 prompt；
- 模型原文输出；
- 工具输入输出原文；
- 文件内容；
- 浏览器页面内容；
- 终端原文输出。

用途：

- 产品分析；
- 运营报表；
- 模型成本分析；
- 模型路由初步统计。

### 3.2 L1：协议形状层

默认可开启，可关闭。

采集内容：

- request shape；
- response shape；
- tool schema；
- 字段名；
- 字段长度；
- hash；
- stream event 类型；
- error class；
- terminal status；
- call_id/thread_id/session_id 的 HMAC hash。

不采集：

- 可还原原文的 prompt/response/tool output；
- token、cookie、API key；
- 本地绝对路径原文；
- 代码、网页、文档、邮件原文。

用途：

- 协议兼容；
- 诊断排障；
- 模型流式输出一致性验证；
- 低风险 eval 统计。

当前项目已有较多 L1 基础，应保留并扩展 episode/turn 关联字段。

### 3.3 L2：脱敏语义轨迹层

用户明确授权后开启。

采集内容：

- 脱敏后的用户任务；
- 脱敏后的模型输出；
- 脱敏后的工具输入输出；
- 上下文摘要；
- 产物摘要；
- 用户反馈；
- 任务结果；
- 自动分类标签；
- 质量评分。

用途：

- eval 数据集；
- 模型路由器训练；
- 任务分类器训练；
- 偏好数据初筛；
- 失败模式分析。

### 3.4 L3：可训练原文轨迹层

强授权开启，默认关闭，但产品上可以明确引导用户加入“逐梦模型共建计划”。

采集内容：

- 用户原始任务；
- 用户后续追问；
- 模型完整输出；
- 可见计划；
- 工具调用输入原文；
- 工具调用输出原文；
- 文件片段；
- 代码 diff；
- 终端命令与输出；
- 浏览器页面内容；
- 文档/邮件/表格产物；
- 用户修改前后对比；
- 最终采用版本。

用途：

- SFT；
- DPO；
- reward model；
- tool-use trajectory training；
- Agent 行为克隆；
- 开源模型行为蒸馏；
- 继续训练数据池。

L3 必须满足：

- 明确授权；
- 可撤回；
- 可删除；
- 可审计；
- 租户隔离；
- 加密存储；
- 来源和许可证标记；
- 敏感内容扫描；
- 训练用途标记。


### 3.5 L3+：Reasoning/CoT 高敏训练层

L3+ 是 L3 的子集，专门用于 reasoning/CoT、internal trace、verifier trace 等高敏过程数据。

开启条件：

- 用户或租户明确开启 `capture_reasoning`；
- 单独开启 `capture_hidden_cot` 才允许采集隐藏/内部推理链；
- 模型在 `reasoning_teacher_whitelist` 中；
- provider/source 明确允许输出用于训练；
- self-hosted 或合同明确允许的模型优先；
- 数据独立加密存储；
- 数据进入训练前必须经过质量筛选和敏感信息扫描；
- UI 明确提示“可能包含模型推理过程和用户上下文”。

采集内容：

- visible plan；
- visible rationale；
- provider reasoning field；
- self-hosted internal reasoning trace；
- reasoning token count；
- reasoning summary；
- verifier/reward/self-check trace；
- reasoning 与最终答案/工具调用/任务结果的对齐关系。

禁止：

- 通过破解、绕过、调试闭源客户端等方式获取未授权隐藏 CoT；
- 将第三方服务不允许训练的 reasoning 输出进入训练池；
- 未经授权把 L3+ 与普通 L3 混合导出。

### 3.6 L0/L1/L2/L3/L3+ 分级例子

分级器按“是否包含客户 know-how、是否可还原内容、是否含原文/推理过程”判断，而不是只按是否有 PII 判断。实现时应至少覆盖以下例子：

| 示例 | 建议分级 | 说明 |
|---|---|---|
| 本月请求量、平均延迟、总 token | L0 | 聚合指标，不含具体业务内容 |
| tool name = `shell_exec` | L1 | 协议/工具形状，不含业务内容 |
| tool schema = `{cmd: string}` | L1 | 工具接口形状，不含具体命令 |
| request body 字段名、长度、hash | L1 | 不可还原内容时为协议形状 |
| 命令原文 `pytest tests/payment/refund_test.py` | L2 或 L3 | 暴露业务域和排查路径；若保存原文通常按 L3 |
| 脱敏任务摘要“排查支付退款失败” | L2 | 无 PII 但含业务 know-how |
| 脱敏后的工具轨迹“先查账单再回滚订单状态” | L2 | 包含企业流程判断 |
| 完整 prompt / response | L3 | 可训练原文 |
| 代码 diff、终端输出、浏览器页面内容 | L3 | 原文或可还原工作成果 |
| DeepSeek-R1 reasoning field | L3+ | 推理过程，高敏训练数据 |
| self-hosted internal CoT trace | L3+ | 仅白名单 + 强授权 + 强隔离 |
| 企业高频 workflow 模板 | L2/L3 | 摘要为 L2，完整模板/skill 为 L3，默认归客户 |

判定拿不准时 fail-closed：往更高档判，默认不出境。

### 3.7 企业闭环采集对象实现登记

对齐文档2 §4，本文只登记以下对象的存在、归属和用途，不在本版展开完整 schema：

| 对象 | 默认归属 | 近期用途 | 展开时机 |
|---|---|---|---|
| 组织/角色/团队图谱 | 租户 | 工作流盘点、人员维度 AI 采用度分析 | 企业治理模块设计时 |
| 人员维度 AI 采用度 | 租户（默认聚合）/用户（显式开启） | 默认识别团队/角色/流程熟练度；人级排名、失败率、token 异常、skill 贡献需租户显式开启 | 租户策略、员工告知与可见范围确定后 |
| 工作流一等对象 | 租户 | 重复任务聚类、黄金轨迹、workflow 资产 | 工作流盘点面进入 V1 设计时 |
| Skill / Workflow 资产 | 租户/团队/用户 | 老师傅经验沉淀、候选 skill 生成、企业 skill 库 | skill 审核/发布流进入设计时 |
| 成本/ROI/token-maxing | 租户 | 管理层看板、模型路由优化 | ROI dashboard 进入设计时 |
| 人-人交接 | 租户 | 起草-审核-批准等多人流程优化 | 协作工作流进入设计时 |
| system-of-record 血缘 | 租户 | 追踪 Jira/Wiki/CRM 等系统读写 | 企业连接器进入设计时 |
| 自然标注流 | 租户/用户 | 人类修正作为金标签，覆盖所有任务类型 | 标注与训练出口进入设计时 |

这些对象可进入客户自有池；个人用户的数据默认进入个人自有池，企业用户的数据默认进入租户自有池。是否贡献给逐梦共建池必须走 `contribution_consent_id`。人员维度对象默认只生成团队/角色/项目/工作流聚合视图；人级画像、排名、绩效相关分析必须作为租户显式开启的独立模式。

### 3.8 数据价值再利用与贡献交易对象登记

为支撑“客户留存后可清洗利用”和“客户可选择贡献/交易”，需登记以下对象。本版只定对象与用途，不展开完整 schema：

| 对象 | 默认归属 | 用途 | 展开时机 |
|---|---|---|---|
| data_inventory | 用户/租户 | 盘点有哪些 episode、工具轨迹、artifact、workflow、skill 可用 | 数据资产工作台 V1 |
| curation_job | 用户/租户 | 脱敏、去重、质量评分、episode 修复、污染检测 | 清洗流水线设计时 |
| dataset_package | 用户/租户 | 将数据打包成 eval、SFT、DPO、workflow、skill 或 benchmark 包 | 导出/交易设计时 |
| contribution_offer | 用户/租户 | 客户选择贡献范围、档级、用途、期限、价格/权益 | 共建/交易入口设计时 |
| transaction_ledger | 用户/租户/逐梦 | 记录购买、授权、用途限制、出境、撤回和收益结算 | 数据交易 V1+ |

客户自有池中的数据包只能由客户自己复用或导出；进入逐梦共建池必须经过贡献授权；进入公共训练池必须再次经过授权、脱敏、质量、license 和 teacher_eligible 检查。

---

## 4. 用户授权与开关设计

### 4.1 产品默认策略

策略必须区分“客户自有采集”和“贡献逐梦共建”。

个人/团队默认：

```text
L0 匿名指标：默认开启，可关闭
L1 协议形状：默认开启，可关闭
L2 脱敏语义：默认关闭，明确 opt-in；个人开启后默认进入个人自有池
L3 原文轨迹：默认关闭，强 opt-in；个人开启后默认进入个人自有池
L3+ Reasoning/CoT：默认关闭，单独强 opt-in
贡献逐梦共建池：默认关闭，单独 opt-in
数据贡献/交易：默认关闭，单独 opt-in
```

企业/私有化默认：

```text
业务数据：客户自有池，默认不出域
L0/L1 出境：默认关闭，可由合同/租户策略开启
L2/L3/L3+ 出境：默认关闭，必须合同 + 租户授权 + 出境账本
贡献逐梦共建池：默认关闭，必须合同化 opt-in
```

但逐梦可以在个人/团队产品中主动引导用户加入：

> 逐梦模型共建计划：允许逐梦使用你的 Agent 使用轨迹改进未来模型，并以额度、折扣或共建权益作为对价。

### 4.2 总开关

设置页应拆成两个总开关，避免把“客户自有闭环”和“贡献逐梦训练”混为一谈：

```text
[ ] 启用本地/个人/租户 AI 数据飞轮，用于我的工作流优化、评测和私有资产沉淀
[ ] 加入逐梦模型共建计划，允许将授权数据贡献给逐梦改进产品和模型
```

关闭共建计划后：

- 数据仍可进入个人/租户自有池；
- 不上传或不贡献 L2/L3/L3+ 到逐梦共建池；
- 可继续保留必要的本地运行日志；
- 可保留计费/安全/风控所需的最小控制面记录；
- UI 明确说明关闭后的影响。

### 4.3 数据模式选择

设置页提供四档模式，并显示数据进入哪个池：

```text
数据模式：
- 不上传任何使用数据
- 仅保留/上传匿名产品指标
- 保留/上传脱敏 Agent 轨迹
- 保留/上传可用于训练的原文 Agent 轨迹
```

其中“上传”受客户档、`egress_allowed`、出境账本和共建计划控制；企业/私有化档默认只在客户边界内保留。

### 4.4 子开关

当用户选择 L3/L3+ 时，应展示细分开关：

```text
[ ] 采集用户任务和模型回复
[ ] 采集工具调用输入输出
[ ] 采集代码 diff 和测试结果
[ ] 采集终端命令输出
[ ] 采集浏览器页面内容
[ ] 采集文档/表格/邮件产物
[ ] 采集用户修改前后对比
[ ] 采集 provider reasoning 字段
[ ] 采集 hidden/internal CoT（仅白名单模型）
[ ] 允许用于客户自有训练/评测
[ ] 允许贡献给逐梦模型共建计划
[ ] 允许用于人工审核标注
```

建议默认：

- 对客户自有池：可按租户/用户授权默认采集已开启类别；个人用户开启 L2/L3 后默认 local-first 进入个人自有池；
- 对逐梦共建池：未加入共建计划前不默认勾选；加入后的个人/团队可在共建计划范围内默认勾选“允许用于模型训练”；
- 对企业/私有化：贡献逐梦共建、人工审核、L3+ 均不得默认勾选，必须合同/租户策略单独开启。

建议默认不勾选或单独确认：

- 浏览器页面内容；
- 邮件内容；
- 企业文档原文；
- hidden/internal CoT；
- 人工审核标注；
- 贡献给逐梦共建池；
- 数据贡献/交易。

### 4.5 范围开关

采集范围应支持：

```text
- 仅当前任务
- 当前项目/工作区
- 当前应用
- 所有工作区
```

编程 Agent 场景中，推荐默认绑定当前工作区；通用办公场景中，推荐默认绑定当前任务。

### 4.6 临时隐私模式

必须提供快速入口：

```text
暂停本次任务数据采集
```

暂停后：

- 不采集 L2/L3；
- L0/L1 可按用户全局设置处理；
- 当前 episode 标记为 `privacy_mode=true`；
- UI 明确显示当前为隐私模式。

### 4.7 授权记录

每次授权应保存：

- consent_id；
- user_id_hash；
- tenant_id_hash；
- device_id_hash；
- scope；
- allowed_data_classes；
- allowed_uses；
- granted_at；
- revoked_at；
- policy_version；
- client_version；
- server_origin。

每条训练数据必须引用对应 consent_id 或授权快照。

---

## 5. 具体采集内容

### 5.1 模型身份与许可证数据

每个 Turn 必须记录：

- provider；
- provider_type：self_hosted / third_party_api / managed_gateway；
- model；
- upstream_model；
- display_model；
- base_model_family；
- model_version；
- adapter；
- adapter_version；
- deployment_region；
- context_window；
- supports_tools；
- supports_vision；
- supports_reasoning；
- supports_computer_use；
- license_profile；
- output_training_allowed；
- teacher_eligible。

说明：

- self-hosted DeepSeek/Qwen/GLM/Llama 等通常更适合作为 teacher 轨迹来源；
- 第三方 API 即使提供开源模型，也可能有平台条款限制；
- 必须把“模型开源许可证”和“调用平台服务条款”分开标记。

### 5.2 用户任务数据

按授权级别采集：

- 原始任务；
- 脱敏任务；
- 任务摘要；
- 任务语言；
- 任务类型；
- 用户显式约束；
- 是否要求联网；
- 是否要求改文件；
- 是否要求运行命令；
- 是否涉及敏感领域；
- 任务复杂度；
- 用户期望输出形式。

建议任务类型枚举：

```text
coding
research
writing
spreadsheet
browser_operation
email
calendar
document_editing
data_analysis
planning
customer_support
sales
legal
finance
general
unknown
```

### 5.3 上下文数据

采集：

- 当前应用：codex_desktop / cli / future_desktop；
- 工作区 hash；
- 项目类型；
- 编程语言；
- 框架；
- 文件树摘要；
- 被读取文件路径 hash；
- 被读取文件内容原文或摘要，按授权决定；
- RAG 命中文档；
- 用户上传文件；
- 浏览器页面域名、标题、URL hash；
- 可用工具列表；
- 系统 prompt 版本；
- developer instructions 版本；
- Agent 产品版本；
- tool schema 版本。

禁止默认采集：

- API key；
- access token；
- cookie；
- refresh token；
- 私钥；
- 明文密码；
- 未授权的完整绝对路径；
- 未授权的企业文档原文。

### 5.4 Agent 计划数据

采集：

- 用户可见计划；
- 结构化 plan steps；
- step status；
- plan revision；
- 是否请求用户确认；
- 用户是否批准；
- 哪一步失败；
- 是否进行了自我检查。

本节只描述 Agent 计划数据，不直接承载 hidden/internal CoT。Reasoning/CoT 另见 §5.17 与 L3+ 策略。

计划数据中禁止采集：

- provider 明确禁止保存的 reasoning 内容；
- 无法合规使用的 hidden chain-of-thought；
- 未授权或未进入白名单的内部推理链。

建议将普通计划数据建模为“可见计划 + 动作摘要”；若采集 provider reasoning field 或 self-hosted internal trace，应写入 `reasoning_trace`，并受 L3+ 开关控制。

### 5.5 工具调用数据

每次工具调用采集：

- tool_name；
- tool_namespace；
- tool_schema_hash；
- call_id_hash；
- item_id_hash；
- step_index；
- arguments_shape；
- arguments_raw，按授权决定；
- result_shape；
- result_raw，按授权决定；
- duration_ms；
- status；
- error_class；
- error_message，按授权与脱敏策略决定；
- retry_count；
- sent_back_to_model；
- high_risk_action；
- user_confirmed；
- pass_fail_rule；
- degraded_reason。

工具类型建议枚举：

```text
file_read
file_write
shell
browser
search
http
mcp
computer_use
spreadsheet
document
image
calendar
email
git
test_runner
unknown
```

### 5.6 产物数据

采集：

- artifact_id；
- artifact_type；
- path_hash；
- before_hash；
- after_hash；
- diff；
- content_raw，按授权决定；
- content_summary；
- created/modified/deleted 标记；
- user_finalized；
- copied/saved/submitted/sent/deployed 标记。

注意：根据本地安全策略和用户信任要求，采集系统自身不应执行删除操作；这里只记录 Agent 产品或目标应用中已经发生的用户任务结果。

### 5.7 用户反馈数据

采集：

- accept；
- reject；
- thumbs_up；
- thumbs_down；
- regenerate；
- stop_generation；
- user_edit；
- rollback；
- followup_request；
- copied；
- saved；
- submitted；
- sent；
- deployed。

重点采集 preference pair：

```text
assistant_output_before
user_edit_after
final_accepted_output
```

### 5.8 自动结果数据

编程场景：

- tests_run；
- tests_passed；
- lint_passed；
- build_passed；
- typecheck_passed；
- CI status；
- PR status；
- diff applied；
- user reverted。

办公/通用场景：

- 文档是否保存；
- 邮件是否发送；
- 日程是否创建；
- 表格公式是否可计算；
- 浏览器任务是否到达目标状态；
- 研究报告引用是否有效；
- 用户是否再次返工。


### 5.9 Harness / ACI 状态数据

为训练强 Agent，必须采集每个 episode 执行时的 Harness 状态：

- host_app：codex_desktop / codex_cli / zhumeng_desktop / openhands / opencode / other；
- host_version；
- entrypoint：desktop / cli / sdk / ide / browser；
- sandbox_mode；
- approval_policy；
- permission_mode；
- network_access；
- filesystem_scope；
- cwd_hash；
- workspace_kind；
- worktree_mode；
- available_tools；
- enabled_tools；
- disabled_tools；
- tool_description_hash；
- tool_schema_hash；
- system_prompt_version；
- developer_instructions_hash；
- AGENTS/CLAUDE/规则文件 hash 与来源；
- plugin inventory；
- skill inventory；
- MCP server inventory；
- hook inventory；
- subagent inventory；
- safe mode 是否开启。

训练价值：同一个模型在不同 Harness 下表现可能完全不同。若不记录 Harness，就无法判断能力差距来自模型、工具、上下文、权限还是产品工程。

### 5.10 权限、拒绝与安全决策数据

除了执行成功的工具调用，还要采集工具调用被决策系统处理的全过程：

- proposed_tool_name；
- proposed_arguments_shape/raw；
- risk_class：read_only / write / destructive / network / credential / external_side_effect；
- permission_decision：accept / reject / abort / auto_allow / auto_deny；
- decision_source：user / config / enterprise_policy / hook / sandbox / system_guardrail；
- decision_latency_ms；
- user_choice_scope：once / session / always / never；
- rewritten_input；
- denial_reason_class；
- blocked_on_user_duration_ms；
- later_equivalent_action_executed；
- rejection_followed_by_success。

训练价值：这是训练“什么时候不该调用工具”“什么时候该请求确认”“如何修改参数后重试”的安全数据。

### 5.11 Hook / Guardrail / Policy 数据

采集：

- hook_event：PreToolUse / PostToolUse / PermissionRequest / ContextBuild / ResponseComplete；
- hook_name_hash；
- hook_source：user / project / local / enterprise_policy / plugin；
- matcher；
- hook_input_shape/raw；
- hook_output_shape/raw；
- num_hooks；
- num_success；
- num_blocking；
- num_non_blocking_error；
- num_cancelled；
- total_duration_ms；
- guardrail_name；
- guardrail_result；
- policy_id；
- policy_version。

训练价值：这些数据可训练模型遵守企业规则、项目规则和安全策略，也可用于分析 Harness 规则是否提高或降低任务成功率。

### 5.12 上下文工程与记忆数据

采集：

- context_builder_version；
- context_sources：user_prompt / files / search / memory / AGENTS.md / MCP resource / prior_summary / tool_result；
- selected_context_items；
- dropped_context_items；
- selection_reason_summary；
- context_token_count；
- prompt_cache_key_hash；
- cache_read_tokens；
- cache_creation_tokens；
- compaction_trigger：auto / manual / context_limit / user_command；
- pre_compaction_tokens；
- post_compaction_tokens；
- compaction_summary_raw，按授权决定；
- compaction_quality_score；
- memory_read/write/update/delete；
- memory_scope：session / workspace / user / org；
- retrieval query、top_k、scores、rerank_result。

训练价值：长任务 Agent 的关键不是只会回答，而是会选择、压缩、保留和恢复上下文。

### 5.13 Subagent / Multi-agent 协作数据

采集：

- parent_agent_id_hash；
- agent_id_hash；
- subagent_type；
- delegation_reason；
- delegated_task；
- subagent_tools；
- subagent_context_budget；
- spawn_time；
- join_time；
- result_summary；
- result_used_by_parent；
- parallelism_degree；
- handoff_source；
- handoff_target；
- handoff_payload；
- subagent_failure_reason。

训练价值：多 Agent 系统的能力来自任务分解、并行搜索、结果合成与冲突处理。未来训练 planner/router 时这些字段非常关键。

### 5.14 UI / 人机协作行为数据

很多 CLI 或桌面 Agent 没有明确“采纳”按钮，因此要采集隐式行为信号：

- user_idle_time；
- user_active_time；
- user_interrupt；
- stop_generation；
- regenerate；
- copy_output；
- paste_output；
- save_file；
- open_file_after_agent_edit；
- user_manual_edit_after_agent；
- user_reverts_agent_change；
- user_runs_tests_after_agent；
- user_commits_after_agent；
- user_creates_pr_after_agent；
- user_deploys_after_agent；
- session_quality_survey；
- explicit_rating；
- natural_language_correction。

训练价值：当产品没有 accept/reject UI 时，这些是最重要的“隐式采纳/不采纳”信号。

### 5.15 编程 Agent 专项数据

编程场景需要额外采集：

- repo_profile：语言、框架、包管理器、测试框架；
- issue/bug/task 原文；
- failing_test_before；
- passing_test_after；
- baseline_command；
- reproduction_command；
- edited_files；
- read_files；
- created_files；
- deleted_files，仅记录事件，不自动执行删除；
- patch_raw；
- patch_applied；
- patch_rejected；
- merge_conflict；
- static_analysis_result；
- lint_result；
- typecheck_result；
- coverage_delta；
- dependency_change；
- git_status_before/after；
- commit_created；
- PR_created；
- PR_review_comments；
- CI_result；
- post_agent_human_fix_diff。

训练价值：SWE-bench 类任务的核心 reward 是补丁是否真正解决问题。用户是否最终提交、测试是否通过、人类后续如何修补，都是比“模型答案看起来好”更强的监督信号。

### 5.16 浏览器 / Computer-use / 办公 Agent 专项数据

通用工作场景需要采集：

- browser_url_domain；
- page_title；
- DOM/action target selector hash；
- screenshot hash / image embedding，按授权决定；
- click/type/scroll/navigation sequence；
- form_fill_fields；
- validation_error；
- final_page_state；
- download/upload event；
- document_section_changed；
- spreadsheet_formula_before/after；
- email_draft_before/after；
- calendar_event_created；
- user_sent_or_cancelled；
- external_side_effect_confirmed。

训练价值：这些数据训练的是“真实软件操作能力”，尤其是 UI grounding、长流程恢复、表单错误修正和外部副作用安全控制。

### 5.17 模型行为逻辑、Reasoning/CoT 与解码过程数据

采集模型行为逻辑时，应把 reasoning 作为独立数据类别，而不是简单并入 response。

基础字段：

- visible_plan；
- action_rationale_summary；
- uncertainty_signal；
- self_check_result；
- verifier_result；
- tool_choice_reason_summary；
- alternative_tools_considered；
- stop_reason；
- finish_reason；
- refusal_reason_class；
- max_tokens_hit；
- response_has_tool_call；
- reasoning_effort；
- reasoning_tokens；
- temperature/top_p；
- retries；
- time_to_first_token_ms；
- stream_interruption_reason。

Reasoning/CoT 字段：

- reasoning_source：visible_plan / provider_reasoning_field / self_hosted_internal_trace / verifier_trace；
- reasoning_visibility：user_visible / api_returned / internal_instrumented / hidden；
- reasoning_policy：not_captured / summary_only / raw_allowed / raw_training_allowed；
- reasoning_raw，按 L3+ 授权决定；
- reasoning_summary；
- reasoning_hash；
- reasoning_chars；
- reasoning_tokens；
- reasoning_language；
- reasoning_quality_score；
- reasoning_final_answer_alignment；
- reasoning_tool_alignment；
- reasoning_trainable；
- hidden_cot_capture_enabled；
- hidden_cot_capture_reason；
- hidden_cot_source；
- hidden_cot_license_profile。

训练价值：这些字段可帮助区分“模型不会”“工具不好”“上下文不足”“推理过程错误”“解码参数不合适”“被安全策略拦截”等不同失败来源。对于 DeepSeek-R1 这类公开 reasoning 能力强的模型，其 reasoning 数据还是蒸馏、过程监督、verifier 训练和 RL credit assignment 的核心数据。

安全边界：

- 对 self-hosted 开源/开放权重模型，可在白名单、强授权、强隔离下开启 hidden/internal CoT 采集；
- 对第三方服务模型，必须以服务条款和合同为准；
- 对闭源模型或未授权来源，禁止绕过式获取 hidden CoT；
- 所有 reasoning 数据必须单独标记来源、可训练性和许可证。

---

## 6. 统一数据 Schema 草案

### 6.0 全事件共用归属/授权字段

以下字段是所有事件的最小公共头，不因具体事件类型省略。后续 §6.1-§6.18 的 JSON 示例为便于阅读只展开业务字段；实际落库、导出和路由必须带上这些字段。

```json
{
  "owner": "tenant",
  "tier": "L3",
  "data_pool": "customer_owned",
  "egress_allowed": false,
  "consent_id": "consent_...",
  "contribution_consent_id": null,
  "tenant_policy_version": "tenant-policy-2026-06-16",
  "capture_boundary": "tenant_side",
  "trainable": false,
  "teacher_eligible": false,
  "retention_policy_id": "retention_..."
}
```

字段含义：

- `owner`：`tenant` / `user` / `zhumeng`，默认只能是 tenant 或 user；
- `tier`：L0/L1/L2/L3/L3+；
- `data_pool`：`customer_owned` / `zhumeng_co_build` / `public_training`；其中 `owner=user` + `data_pool=customer_owned` 表示个人自有池；
- `egress_allowed`：是否允许离开客户边界，企业/私有化业务数据默认 false；
- `contribution_consent_id`：只有进入逐梦共建池时才允许非空；
- `capture_boundary`：`local_device` / `tenant_side` / `zhumeng_center`，用于证明 raw 是否越过客户边界；
- `trainable`：对客户自有池表示“可用于个人/客户自有训练/评测”，对逐梦公共训练必须再满足共建授权、license 和 teacher 白名单；
- `teacher_eligible`：只表示模型输出来源可作为 teacher 候选，不覆盖用户/租户授权。

### 6.1 agent_episode

```json
{
  "schema_version": 1,
  "event_type": "agent_episode",
  "episode_id": "uuid",
  "tenant_id_hash": "hmac-sha256:...",
  "user_id_hash": "hmac-sha256:...",
  "device_id_hash": "hmac-sha256:...",
  "client": "codex_desktop",
  "agent_product": "zhumeng_agent",
  "agent_version": "0.1.0",
  "workspace_id_hash": "hmac-sha256:...",
  "owner": "tenant",
  "tier": "L3",
  "data_pool": "customer_owned",
  "egress_allowed": false,
  "contribution_consent_id": null,
  "capture_boundary": "tenant_side",
  "started_at": "2026-06-14T00:00:00Z",
  "ended_at": "2026-06-14T00:05:00Z",
  "task_type": "coding",
  "task_summary": "fix login endpoint failure",
  "capture_mode": "raw_training",
  "consent_id": "consent_...",
  "consent_scope": ["analytics", "eval", "training"],
  "sensitivity": "internal",
  "trainable": true,
  "privacy_mode": false,
  "outcome": "completed"
}
```

### 6.2 agent_turn

```json
{
  "schema_version": 1,
  "event_type": "agent_turn",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "request_id_hash": "hmac-sha256:...",
  "model_provider": "deepseek",
  "provider_type": "self_hosted",
  "model": "deepseek-r1",
  "upstream_model": "deepseek-reasoner",
  "base_model_family": "deepseek",
  "adapter": "deepseek",
  "license_profile": "open_weight_allowed",
  "teacher_eligible": true,
  "request_policy": "raw_allowed",
  "response_policy": "raw_allowed",
  "request_shape_hash": "sha256:...",
  "response_shape_hash": "sha256:...",
  "input_tokens": 1000,
  "output_tokens": 800,
  "latency_ms": 12345,
  "status": "completed"
}
```

### 6.3 agent_message

```json
{
  "schema_version": 1,
  "event_type": "agent_message",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "role": "user",
  "content_policy": "raw_allowed",
  "content_raw": "用户原始任务文本",
  "content_redacted": "脱敏后的任务文本",
  "content_hash": "sha256:...",
  "language": "zh-CN"
}
```

### 6.4 agent_plan

```json
{
  "schema_version": 1,
  "event_type": "agent_plan",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "plan_id": "uuid",
  "plan_policy": "raw_allowed",
  "steps": [
    {"index": 1, "summary": "inspect error logs", "status": "completed"},
    {"index": 2, "summary": "edit handler", "status": "completed"},
    {"index": 3, "summary": "run tests", "status": "completed"}
  ],
  "user_approved": true,
  "revision": 1
}
```

### 6.5 tool_call

```json
{
  "schema_version": 1,
  "event_type": "tool_call",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "step_index": 3,
  "tool_name": "shell_exec",
  "tool_namespace": "shell",
  "tool_type": "shell",
  "tool_schema_hash": "sha256:...",
  "call_id_hash": "hmac-sha256:...",
  "item_id_hash": "hmac-sha256:...",
  "arguments_policy": "raw_allowed",
  "arguments_shape": {},
  "arguments_raw": {"cmd": "pytest tests/test_login.py"},
  "high_risk_action": false,
  "user_confirmed": false
}
```

### 6.6 tool_result

```json
{
  "schema_version": 1,
  "event_type": "tool_result",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "call_id_hash": "hmac-sha256:...",
  "result_policy": "raw_allowed",
  "result_shape": {},
  "result_raw": "3 passed in 1.42s",
  "duration_ms": 1420,
  "status": "success",
  "sent_back_to_model": true
}
```

### 6.7 artifact

```json
{
  "schema_version": 1,
  "event_type": "artifact",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "artifact_id": "uuid",
  "artifact_type": "code_diff",
  "path_hash": "hmac-sha256:...",
  "content_policy": "raw_allowed",
  "diff_raw": "...",
  "before_hash": "sha256:...",
  "after_hash": "sha256:...",
  "user_finalized": true
}
```

### 6.8 feedback

```json
{
  "schema_version": 1,
  "event_type": "feedback",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "feedback_type": "user_edit",
  "target": "assistant_output",
  "before_hash": "sha256:...",
  "after_hash": "sha256:...",
  "before_policy": "raw_allowed",
  "after_policy": "raw_allowed",
  "before_raw": "模型原始输出",
  "after_raw": "用户修改后输出",
  "edit_distance_ratio": 0.27,
  "accepted": true
}
```

### 6.9 outcome

```json
{
  "schema_version": 1,
  "event_type": "outcome",
  "episode_id": "uuid",
  "outcome": "completed",
  "quality_score": 0.92,
  "automatic_signals": {
    "tests_passed": true,
    "build_passed": true,
    "user_accepted": true
  },
  "failure_reason": ""
}
```


### 6.10 harness_context

```json
{
  "schema_version": 1,
  "event_type": "harness_context",
  "episode_id": "uuid",
  "host_app": "codex_desktop",
  "host_version": "...",
  "entrypoint": "desktop",
  "sandbox_mode": "workspace_write",
  "approval_policy": "on_request",
  "permission_mode": "default",
  "available_tools_hash": "sha256:...",
  "system_prompt_version": "...",
  "developer_instructions_hash": "sha256:...",
  "plugin_inventory": [{"plugin_id_hash":"...","scope":"user","has_mcp":true,"has_hooks":false}],
  "skill_inventory": [{"skill_name_hash":"...","source":"plugin"}],
  "mcp_inventory": [{"server_id_hash":"...","transport":"stdio","tool_count":12}],
  "hook_inventory": [{"hook_event":"PreToolUse","hook_source":"project"}]
}
```

### 6.11 permission_decision

```json
{
  "schema_version": 1,
  "event_type": "permission_decision",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "tool_call_id_hash": "hmac-sha256:...",
  "tool_name": "Write",
  "risk_class": "write",
  "decision": "reject",
  "decision_source": "user_reject",
  "decision_latency_ms": 3240,
  "proposed_arguments_policy": "raw_allowed",
  "proposed_arguments_raw": {},
  "denial_reason_class": "user_not_ready",
  "later_equivalent_action_executed": false
}
```

### 6.12 context_event

```json
{
  "schema_version": 1,
  "event_type": "context_event",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "context_builder_version": "...",
  "operation": "compaction",
  "trigger": "auto",
  "pre_tokens": 180000,
  "post_tokens": 42000,
  "cache_read_tokens": 12000,
  "cache_creation_tokens": 3000,
  "selected_context_items": [{"source":"file","path_hash":"...","tokens":800}],
  "dropped_context_items": [{"source":"tool_result","reason":"low_relevance"}],
  "summary_policy": "raw_allowed"
}
```

### 6.13 subagent_event

```json
{
  "schema_version": 1,
  "event_type": "subagent_event",
  "episode_id": "uuid",
  "parent_agent_id_hash": "...",
  "agent_id_hash": "...",
  "subagent_type": "code_reviewer",
  "event": "spawned",
  "delegation_reason": "parallel_review",
  "delegated_task_policy": "raw_allowed",
  "delegated_task_raw": "review the patch for regressions",
  "result_used_by_parent": true
}
```

### 6.14 implicit_feedback

```json
{
  "schema_version": 1,
  "event_type": "implicit_feedback",
  "episode_id": "uuid",
  "target_artifact_id": "uuid",
  "signals": {
    "copied": true,
    "saved": true,
    "tests_rerun_by_user": true,
    "committed_after_agent": true,
    "reverted_after_agent": false,
    "manual_edit_after_agent": true
  },
  "inferred_acceptance": "accepted_with_minor_edits",
  "confidence": 0.83
}
```

### 6.15 rl_transition

```json
{
  "schema_version": 1,
  "event_type": "rl_transition",
  "episode_id": "uuid",
  "transition_id": "uuid",
  "state_ref": "state_snapshot_hash",
  "action_ref": "tool_call_or_message_hash",
  "observation_ref": "tool_result_or_user_feedback_hash",
  "reward": 0.4,
  "reward_components": {
    "tool_success": 0.1,
    "progress": 0.2,
    "user_acceptance": 0.1
  },
  "terminal": false,
  "credit_assignment_method": "heuristic_v1"
}
```


### 6.16 reasoning_trace

```json
{
  "schema_version": 1,
  "event_type": "reasoning_trace",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "model_provider": "deepseek",
  "model": "deepseek-r1",
  "provider_type": "self_hosted",
  "reasoning_source": "provider_reasoning_field",
  "reasoning_visibility": "api_returned",
  "reasoning_policy": "raw_training_allowed",
  "reasoning_trainable": true,
  "hidden_cot_capture_enabled": false,
  "reasoning_raw": "...",
  "reasoning_summary": "...",
  "reasoning_hash": "sha256:...",
  "reasoning_tokens": 4096,
  "reasoning_quality_score": 0.87,
  "reasoning_final_answer_alignment": "consistent",
  "reasoning_tool_alignment": "supports_tool_sequence",
  "license_profile": "open_weight_allowed",
  "consent_id": "consent_..."
}
```

### 6.17 server_gateway_trace

```json
{
  "schema_version": 1,
  "event_type": "server_gateway_trace",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "gateway_trace_id": "uuid",
  "request_path": "/codex/v1/responses",
  "provider": "deepseek",
  "model": "deepseek-r1",
  "upstream_model": "deepseek-reasoner",
  "adapter": "deepseek",
  "request_policy": "raw_training_allowed",
  "response_policy": "raw_training_allowed",
  "captured_on_server": true,
  "has_prompt_raw": true,
  "has_response_raw": true,
  "has_reasoning_raw": true,
  "has_tool_call_intent": true,
  "usage": {"input_tokens": 1000, "output_tokens": 800, "reasoning_tokens": 4000},
  "latency_ms": 12345,
  "status": "completed"
}
```

### 6.18 local_execution_trace

```json
{
  "schema_version": 1,
  "event_type": "local_execution_trace",
  "episode_id": "uuid",
  "turn_id": "uuid",
  "local_trace_id": "uuid",
  "captured_on_client": true,
  "has_tool_execution_result": true,
  "has_user_acceptance_signal": true,
  "has_file_diff": true,
  "has_terminal_output": true,
  "has_permission_decision": true,
  "has_ui_event": true,
  "linked_gateway_trace_id": "uuid"
}
```

---

## 7. 当前项目落点

### 7.0 混合采集架构：服务端主干 + 本地补全

当前逐梦 Agent 初期形态是接管市面上已有 Agent 工具，例如 Claude Code CLI、Codex Desktop 等；用户在这些工具里请求模型时，流量经过 sub2api Gateway。因此采集架构应优先利用网关：

```text
Claude Code CLI / Codex Desktop / 其他 Agent 工具
        |
        | 模型请求
        v
本地逐梦 Agent / Local Proxy
        |
        | trace headers + 本地端侧事件
        v
sub2api Gateway
        |
        | prompt / response / reasoning / tool-call-intent / usage / provider
        v
客户侧/授权训练数据事件总线
        ^
        |
本地逐梦 Agent 上传 tool execution / user adoption / file diff / terminal / UI / permission / MCP-plugin-skill-hook
```

> ⚠️ v0.4/v0.5 修正（见 §0.3.2 与 §0.4）：下列“服务端优先采集原文”已修正为**端侧先分级脱敏、出境前完成；服务端仅二次校验**。企业/私有化档业务数据默认不出域；控制面元数据和合同化贡献必须受出境账本、归属标签和三池模型约束。下文保留作为“服务端能看到的事实源”清单。

网关侧可采集（受部署边界、授权、分级和出境账本约束）：

- prompt raw / redacted / shape（raw 仅限客户边界内、个人/团队强授权，或已合同化贡献）；
- system/developer instructions（按 L2/L3 分级，不默认出境）；
- request body（raw 仅限客户边界内、个人/团队强授权，或已合同化贡献）；
- response raw / redacted / shape（raw 仅限客户边界内、个人/团队强授权，或已合同化贡献）；
- provider reasoning field / visible reasoning；
- tool call intent；
- function call 参数；
- model/provider/upstream_model/adapter；
- token usage、reasoning_tokens、cache usage；
- stream event；
- refusal/error/finish reason；
- provider failover/retry/cost/latency。

本地补充采集：

- tool 是否实际执行；
- tool output 原文/shape；
- shell/browser/file/computer-use 真实结果；
- 用户是否批准或拒绝；
- 用户是否采纳、编辑、回滚；
- 文件 diff、测试结果、git status；
- plugin/skill/MCP/hook/subagent 状态；
- sandbox/permission/approval 决策；
- UI 行为、复制、保存、提交、PR；
- 浏览器和办公软件最终状态。

合并规则：

- 所有请求都必须有 `episode_id`、`turn_id`、`gateway_trace_id`；
- 本地事件必须带 `local_trace_id` 并尽量回填 `gateway_trace_id`；
- 无法实时关联时，Episode Builder 使用时间窗口、model、request hash、tool_call_id、response_id_hash 做延迟关联；
- 网关侧事实优先用于模型输入输出，本地事实优先用于执行结果和用户行为；
- 若网关部署在逐梦中心侧，只能接收已经端侧分级/脱敏且 `egress_allowed=true` 的事件；企业业务 raw 不得越过客户边界。

### 7.1 本地代理层

文件：

`tools/zhumeng-agent/src/zhumeng_agent/proxy/server.py`

职责：

- 生成或透传 episode_id；
- 为每次 `/v1/responses` 生成 turn_id；
- 记录本地请求开始/结束；
- 标记 agent_version、runtime_signature、device_id、managed_session_id；
- 根据本地授权配置决定是否保存 raw；
- 给上游请求增加非敏感 trace headers；
- 失败时本地加密缓存待上传数据；
- 网络恢复后补传。

建议新增逻辑：

- `DataCaptureConfig`：读取本地授权与采集模式；
- `EpisodeContext`：管理 episode_id、turn_id、workspace_id_hash；
- `TrainingTraceWriter`：按 L0/L1/L2/L3 输出本地 JSONL；
- `TrainingTraceUploader`：按 `egress_allowed` 与数据池策略批量写入客户侧或授权后端 ingest endpoint。

### 7.2 Codex Desktop Capture 层

文件：

`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_injector.py`

职责：

- 记录 Codex Desktop runtime 侧事件；
- tool lifecycle；
- app_server_frame；
- model_picker；
- subagent_registration；
- deferred_tool_search。

建议扩展：

- 在所有 capture event 中加入 episode_id / turn_id；
- 采集用户可见 accept/reject/edit/copy/save 等 UI 事件；
- 采集 artifact 创建/修改事件；
- 采集 high-risk confirmation 事件；
- 在 L3 授权下允许保存 raw tool input/output；
- 保留当前 shape-only 作为 L1 默认路径。

### 7.3 本地 capture 配置层

文件：

`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/capture_config.py`

现有 `raw_payloads` 与 unlock env 更偏开发诊断用途。生产训练数据采集不应直接复用该语义。

建议新增正式配置：

```json
{
  "data_capture": {
    "enabled": true,
    "mode": "raw_training",
    "scope": "workspace",
    "allow_training": true,
    "allow_human_review": false,
    "capture_prompts": true,
    "capture_model_outputs": true,
    "capture_tool_io": true,
    "capture_code_diff": true,
    "capture_terminal_output": true,
    "capture_browser_content": false,
    "capture_artifacts": true,
    "capture_reasoning": true,
    "capture_provider_reasoning_field": true,
    "capture_hidden_cot": false,
    "reasoning_mode": "provider_field_and_visible_plan",
    "reasoning_teacher_whitelist": ["self_hosted:deepseek-r1"],
    "data_pool": "customer_owned",
    "egress_allowed": false,
    "contribution_consent_id": null,
    "capture_boundary": "tenant_side",
    "pause_current_episode": false
  }
}
```

### 7.4 后端 / 客户侧 Gateway Capture 层

文件：

`backend/internal/service/codex_gateway_capture.go`

职责：

- 记录服务端事实源；
- 绑定 provider/model/upstream_model；
- 记录 upstream request/response/stream；
- 在部署边界和授权允许时采集 prompt/response/reasoning/tool-call-intent 原文；企业/私有化 raw 只允许落在客户边界内，出境只允许 redacted/shape 或合同化贡献；
- 汇总 trace report。

建议扩展：

- 引入 episode aggregate；
- 引入 consent filter；
- 将 capture trace 转换为 agent_turn、server_gateway_trace、tool_call_intent、reasoning_trace、outcome；
- 保存 trainability tags；
- 记录 provider/model/license/teacher_eligible；
- 记录 quality score；
- 输出数据到训练数据流水线。

### 7.5 客户侧 / 后端 ingest endpoint

建议新增 endpoint；企业/私有化部署时优先落在客户侧，逐梦中心侧只接收控制面、脱敏事件或合同化贡献：

```text
POST /api/v1/agent-data/events
POST /api/v1/agent-data/batches
POST /api/v1/agent-data/consents
DELETE /api/v1/agent-data/consents/:id
POST /api/v1/agent-data/deletion-requests
```

鉴权：

- 必须来自已授权 managed device；
- 必须绑定用户/租户；
- 必须校验 `owner/tier/data_pool/egress_allowed/contribution_consent_id`；
- 批量上传限制大小；
- 服务端重复去重；
- 服务端重新校验 consent；
- 对企业业务 raw，中心侧 ingest 必须 fail-closed 拒收。

---

## 8. 数据流水线

### 8.1 总流程

```text
local events
  -> tier classification / edge redaction / egress decision
  -> local encryption / buffering
  -> tenant-side or allowed server ingest
  -> consent validation
  -> tenant isolation
  -> server-side redaction / PII second check
  -> source/license tagging
  -> episode builder
  -> quality scoring
  -> dataset router
  -> analytics / eval / SFT / DPO / tool-use / router datasets
```

### 8.2 Ingest 层

职责：

- 接收 JSONL batch；
- 校验 schema_version；
- 校验 consent_id；
- 校验 device_id；
- 去重；
- 按部署边界写入 raw event store / redacted event store：企业 raw 默认只在客户自有池内，中心侧不得写入未授权业务 raw；
- 异步进入处理队列。

### 8.3 Redaction / PII 层

> ⚠️ v0.4 修正（见 §0.3.2）：脱敏/分级**首道防线在端侧、出境前**；本节服务端 redaction 降级为二次校验（defense in depth），不得作为唯一防线。另需补 PIPL 第三方个人信息处理（见 §0.3.5）。

职责：

- 检测 token、secret、password、private key；
- 检测邮箱、电话、身份证、地址等 PII；
- 检测商业敏感词；
- 根据 tenant policy 决定保留、脱敏、拒收或进入人工审核。

### 8.4 Episode Builder

职责：

- 将 turn、tool、artifact、feedback、outcome 聚合成 episode；
- 补齐缺失字段；
- 关联服务端 gateway trace 和桌面端 capture trace；
- 生成 episode summary；
- 标记任务类型和难度。

### 8.5 Quality Scoring

建议评分维度：

- 任务是否完成；
- 用户是否接受；
- 是否有用户编辑；
- 编辑距离；
- 工具调用是否成功；
- 是否有失败恢复；
- 编程任务测试是否通过；
- 输出是否过短/过长；
- 是否存在敏感信息；
- 是否存在 hallucination 风险；
- teacher 模型是否 eligible。

### 8.6 Dataset Router

Dataset Router 必须先按数据池路由，再按训练用途路由：客户自有池只能服务客户自己的 analytics/eval/私有训练；逐梦共建池需再次做授权、脱敏、license、teacher_eligible 检查；公共训练池只能从合格共建数据中派生。

根据标签路由到不同数据集：

| 数据集 | 输入条件 | 用途 |
|---|---|---|
| analytics | L0/L1 | 产品指标 |
| eval | L1/L2/L3 | 评测集 |
| router | L0/L1/L2 | 模型路由训练 |
| sft | 高质量 L3 | 指令微调 |
| dpo | 有 before/after 或 accept/reject | 偏好训练 |
| tool_use | 有完整工具轨迹 | 工具调用训练 |
| failure_recovery | 有失败-恢复链路 | 纠错训练 |
| reward | 有 outcome/automatic signals | 奖励模型 |
| distillation | teacher_eligible=true | 行为蒸馏 |


### 8.7 轨迹到训练样本的转换

每个 episode 应至少生成五类派生样本：

1. **Behavior cloning sample**：`state -> next_action`，用于训练模型在给定上下文下选择下一步动作。
2. **Tool parameterization sample**：`task + tool_schema + context -> tool_arguments`，用于训练参数填写。
3. **Recovery sample**：`failed_action + error_observation -> corrected_action`，用于训练错误恢复。
4. **Preference sample**：`rejected_or_unaccepted_output` vs `accepted_or_user_edited_output`，用于 DPO/reward model。
5. **RL transition sample**：`state, action, observation, reward, next_state`，用于后续强化学习。

### 8.8 隐式反馈推断器

由于 Codex Desktop、Claude Code CLI、OpenHands/opencode 类产品未必有明确“采纳”按钮，需要建设隐式反馈推断器：

- 输出后 30 分钟内用户是否修改同一文件；
- 用户修改比例；
- 用户是否运行测试；
- 测试是否通过；
- 用户是否提交 commit；
- 是否创建 PR；
- 是否回滚；
- 是否重新要求 Agent 修同一问题；
- 是否复制/保存/发送最终内容；
- 会话是否以成功语义结束。

推断器输出 `inferred_acceptance` 与 `confidence`，不能替代显式反馈，但可作为训练样本筛选权重。

### 8.9 Harness A/B 数据

为了超越强开源模型，仅训练权重不够，还要优化 harness。建议将以下 A/B 维度纳入数据流水线：

- tool description A/B；
- tool schema strictness A/B；
- permission mode A/B；
- context selection strategy A/B；
- compaction strategy A/B；
- subagent delegation strategy A/B；
- patch apply strategy A/B；
- test command discovery strategy A/B；
- browser/computer-use grounding strategy A/B。

每个 A/B 实验必须记录 `harness_variant_id`，后续用于分析是模型提升还是 harness 提升。

### 8.10 客户自有数据整理/清洗工作台

无论个人还是企业，留存数据都必须经过整理才能产生价值。建议把数据资产工作台做成客户自有池的标准能力：

```text
数据盘点
  -> 权属/授权/分级校验
  -> PII/secret/IP/商业秘密扫描
  -> 脱敏与不可逆摘要
  -> 去重、去模板噪声、去失败污染
  -> episode 边界修复与轨迹补全
  -> 质量评分与自然标注
  -> workflow 聚类 / skill 候选提取
  -> 私有 eval / 私有训练集 / workflow 看板 / 数据包导出
```

企业模式：

- 默认输出团队/角色/项目/工作流聚合分析；
- 支持识别高频低效流程、token-maxing、失败集中点、可沉淀 skill、私有 eval 样本；
- 支持导出到企业私有训练、私有评测、治理审计和 ROI dashboard；
- 人级分析作为独立模式，需租户显式开启并受员工告知、可见范围和导出权限控制。

个人模式：

- 默认 local-first 输出个人工作流复盘、个人偏好、个人 skill、个人可携带 AI 工作记忆；
- 支持把个人自有池整理成个人 eval、个人提示词/skill、个人模型偏好数据；
- 若个人选择贡献，先生成可预览的数据包，再进入贡献授权流程。

### 8.11 数据贡献与交易流水线

贡献/交易不是“上传日志”，而是一个受控的数据包交易流程：

```text
客户选择贡献范围
  -> 生成 dataset_package 预览
  -> 分级/权属/PII/IP/license 检查
  -> 估值与对价建议
  -> 客户确认用途、期限、撤回规则
  -> 签署 contribution_consent / data_purchase_agreement
  -> 写入出境账本与交易账本
  -> 进入逐梦共建池
  -> 再筛选进入公共训练池或行业 benchmark
```

交易档位建议：

| 数据包类型 | 可交易性 | 典型用途 | 关键限制 |
|---|---|---|---|
| L0 聚合指标包 | 高 | 行业趋势、产品改进、benchmark | 不可单用户/单租户可识别 |
| L1 协议形状包 | 高 | 工具 schema、错误类型、路由优化 | hash/聚合，不含内容 |
| L2 脱敏工作流包 | 中 | workflow 学习、行业 benchmark、skill 候选 | 去 PII 不够，还要去商业秘密和第三方个人信息 |
| 经审核 workflow/skill 包 | 中 | 企业/行业 skill 共建 | 需明确 IP 归属和授权范围 |
| L3/L3+ 原文包 | 低，默认不交易 | 特定共建模型/私有训练 | 强合同、强授权、强隔离、强审计 |

逐梦可以作为收购方购买客户授权数据，也可以未来作为撮合方；但任何数据交易都不得绕过客户预览、用途限定、对价确认、出境账本和撤回/删除规则。

---

## 9. 开源模型行为采集策略

### 9.1 Teacher Eligibility

不是所有模型轨迹都应进入蒸馏数据集。

每个模型配置应包含：

```json
{
  "provider": "deepseek",
  "model": "deepseek-r1",
  "provider_type": "self_hosted",
  "license_profile": "open_weight_allowed",
  "output_training_allowed": true,
  "teacher_eligible": true,
  "notes": "self-hosted open-weight model"
}
```

### 9.2 白名单策略

初期建议只将以下来源标记为 teacher_eligible：

- 逐梦自托管的开源/开放权重模型；
- 明确允许输出用于训练的模型服务；
- 用户自带且明确授权的本地模型；
- 逐梦自研模型。

企业/私有化数据默认不进入逐梦 teacher 数据；只有合同化贡献、`contribution_consent_id` 有效、且该条数据进入逐梦共建池后，才可进一步参与 teacher_eligible 判定。

不建议默认标记为 teacher_eligible：

- 闭源商业模型；
- 第三方 API 平台上的不明条款模型；
- 用户未授权的模型输出；
- license_profile 未确认的模型。

### 9.3 多模型对比

为了训练模型路由器和挑选 teacher 数据，可设计 A/B 或 shadow eval：

- 同类任务用不同模型执行；
- 同一任务离线重放给多个开源模型；
- 比较成功率、工具调用次数、耗时、用户编辑距离；
- 将最佳轨迹标记为 preferred。

注意：在线 shadow 不应增加用户成本或泄露敏感数据；建议优先用已授权 eval 数据离线重放。

---

## 10. 训练数据出口格式

### 10.1 SFT JSONL

```json
{"messages":[{"role":"system","content":"..."},{"role":"user","content":"..."},{"role":"assistant","content":"..."}],"metadata":{"episode_id":"...","model":"deepseek-r1","task_type":"coding"}}
```

### 10.2 DPO Pair

```json
{
  "prompt": "用户任务 + 上下文",
  "chosen": "用户最终采用版本",
  "rejected": "模型未采用版本",
  "metadata": {
    "episode_id": "...",
    "feedback_type": "user_edit",
    "edit_distance_ratio": 0.27
  }
}
```

### 10.3 Tool Trajectory

```json
{
  "task": "...",
  "context": "...",
  "steps": [
    {"type":"assistant_plan","content":"..."},
    {"type":"tool_call","tool":"read_file","arguments":{}},
    {"type":"tool_result","content":"..."},
    {"type":"assistant_output","content":"..."}
  ],
  "outcome": "completed",
  "metadata": {
    "model": "deepseek-r1",
    "teacher_eligible": true
  }
}
```

### 10.4 Router Dataset

```json
{
  "task_features": {
    "task_type": "coding",
    "requires_tools": true,
    "context_tokens": 12000,
    "language": "zh-CN"
  },
  "candidate_models": ["deepseek-r1", "qwen3", "glm-5"],
  "selected_model": "deepseek-r1",
  "outcome_scores": {
    "deepseek-r1": 0.91,
    "qwen3": 0.82,
    "glm-5": 0.78
  }
}
```

---

## 11. 存储、安全与合规要求

### 11.1 本地存储

本地缓存要求：

- JSONL 分片；
- 文件权限 0600；
- 可选本地加密；
- 上传成功后按 retention 删除；
- 隐私模式不写 L2/L3；
- 离线时可缓存，超期自动清理。

### 11.2 服务端存储

服务端应分区存储：

```text
raw_event_store        # 企业/私有化 raw 默认只在客户边界内
redacted_event_store
episode_store
dataset_store
consent_store
contribution_consent_store
egress_manifest_store
deletion_ledger
```

L3 原文建议单独加密存储，权限独立。企业/私有化部署中，“服务端存储”优先指客户侧服务端；逐梦中心侧只保存合同允许的控制面、脱敏事件或共建贡献。

### 11.3 租户隔离

企业客户必须支持：

- tenant-level opt-out；
- tenant-level trainable=false；
- tenant-private fine-tuning data；
- 禁止跨租户训练；
- 管理员审计；
- 人员维度 AI 采用度分析的可见范围与导出权限由租户策略控制。

### 11.4 删除与撤回

必须支持：

- 用户撤回授权；
- 删除未来采集；
- 删除历史原文数据；
- 标记已进入训练集的数据批次；
- 对无法物理删除的已训练模型，至少记录训练批次和后续补救策略。

### 11.5 敏感信息处理

必须自动识别并处理：

- API keys；
- OAuth tokens；
- cookies；
- passwords；
- private keys；
- SSH keys；
- payment data；
- medical/legal/financial sensitive data；
- 企业商业秘密；
- 用户通讯录和邮件地址。

---

## 12. 风险与约束

### 12.1 用户信任风险

原文训练数据非常敏感。产品必须明确：

- 收集什么；
- 不收集什么；
- 用来做什么；
- 是否用于训练；
- 是否人工查看；
- 如何关闭；
- 如何删除。

### 12.2 模型许可证风险

开源模型也要区分：

- open-source；
- open-weight；
- research-only；
- commercial allowed；
- redistribution restricted；
- output training allowed；
- third-party API terms restricted。

所有训练数据必须绑定 `license_profile` 和 `teacher_eligible`。

### 12.3 数据质量风险

真实用户数据不是天然高质量训练数据。需要过滤：

- 失败任务；
- 幻觉输出；
- 用户未接受输出；
- 敏感内容；
- 低质量重复任务；
- 污染 eval 的数据；
- 不完整 tool trajectory。

### 12.4 安全风险

尤其要避免：

- 训练数据中泄漏 token；
- 训练模型记住用户代码；
- 跨租户数据污染；
- 用户误以为关闭后仍在上传；
- 调试 raw capture 与正式训练 capture 混用。

---

## 13. 分阶段实施计划

### Phase 1：统一数据模型与授权基础

目标：建立可扩展的数据采集骨架。

工作项：

- 新增 DataCaptureConfig；
- 新增用户授权状态；
- 新增 episode_id / turn_id；
- 扩展本地代理与 capture event；
- 服务端新增 consent store；
- 服务端新增 ingest endpoint 草案；
- 预埋 owner/tier/data_pool/egress_allowed/contribution_consent_id 公共字段；
- 预埋人员维度、workflow、skill 对象登记，不展开 schema；
- L0/L1 数据进入统一 event schema。

验收：

- 用户可在设置中选择数据模式；
- 每条事件都有 consent/capture_mode/trainable；
- 每条事件都有 owner/tier/data_pool/egress_allowed/contribution_consent_id；
- shape-only 不保存原文；
- raw_training 只有授权后生效。

### Phase 2：L3 原文采集 MVP

目标：用户/租户授权后采集可训练原文；企业/私有化 raw 默认只进入客户自有池。

工作项：

- 网关侧在客户边界内或强授权下采集 prompt/response raw；
- 网关侧在客户边界内或强授权下采集 provider reasoning field / tool-call-intent；
- 本地补采 tool input/output raw；
- 采集 code diff/test output；
- 端侧分级、脱敏与出境决策；
- 本地加密缓存；
- 客户侧或授权共建池 L3 存储；
- 出境账本；
- PII/secret 扫描；
- 删除/撤回接口；
- 后台查看采集状态。

验收：

- 授权开启后可生成完整 episode；
- 关闭后不再上传或贡献 L2/L3；
- 企业业务 raw 不出客户边界，出境账本可验；
- secret scanner 能阻断明显 token；
- 用户可请求删除历史 L3 数据。

### Phase 3：训练数据流水线

目标：从事件变成可训练数据集。

工作项：

- episode builder；
- quality scoring；
- dataset router；
- SFT export；
- DPO export；
- tool trajectory export；
- router dataset export；
- teacher_eligible 白名单。

验收：

- 可按模型/任务/质量筛选数据；
- 可导出 HF datasets/JSONL；
- 可生成 DPO pair；
- 可生成 tool-use trajectory。

### Phase 4：模型与产品闭环

目标：让数据反哺模型与 Agent 产品。

工作项：

- 模型路由器训练；
- 开源模型行为蒸馏；
- 失败恢复数据训练；
- 评测集回归；
- 线上灰度；
- 数据质量 dashboard。

验收：

- 路由器提升成功率/成本比；
- 自有模型在目标任务集上超过原基座；
- 失败模式下降；
- 用户接受率提升。

---

## 14. 产品 UI 建议

### 14.1 首次授权弹窗

建议文案：

```text
启用 AI 数据飞轮 / 加入逐梦模型共建计划

你可以只启用本地/租户 AI 数据飞轮，把 Agent 使用轨迹沉淀为自己的工作流、评测和 skill 资产；也可以另行加入逐梦模型共建计划，把授权数据贡献给逐梦改进产品和模型。
这些数据可能包括你的任务、模型回复、工具调用、代码 diff、命令输出和最终产物。
你可以随时暂停、关闭共建、撤回授权或请求删除。

数据模式：
( ) 仅匿名产品指标
( ) 脱敏 Agent 轨迹
( ) 可用于训练的原文 Agent 轨迹
```

### 14.2 设置页

设置页应显示：

- 当前数据模式；
- 当前任务是否采集；
- 已授权范围；
- 数据进入个人/租户自有池、逐梦共建池还是公共训练池；
- 训练用途是否开启；
- 人工审核是否允许；
- 上次上传/出境时间；
- 出境账本入口；
- 本地缓存大小；
- 删除数据入口；
- 人员维度 AI 采用度分析的可见范围；
- skill/workflow 提交、审核和贡献设置；
- 数据资产工作台、清洗任务和数据包导出入口；
- 数据贡献/交易授权、对价和交易账本入口；
- Reasoning/CoT 采集状态；
- hidden CoT 是否开启；
- reasoning teacher 白名单；

### 14.3 任务级提示

当 L3 开启时，任务页应有明显状态：

```text
训练数据采集中 · 可暂停
```

敏感任务建议自动提示：

```text
检测到可能包含密钥/隐私信息，已暂停原文采集，是否继续？
```

---

## 15. 与现有 capture 的关系

当前项目已有 Desktop Capture V2 和 Gateway Capture，默认 shape-only 且 raw payload 需 unlock。这是正确的诊断安全策略。

本方案不建议把现有 `raw_payloads` 诊断开关直接当作正式训练数据采集开关，而应新增正式的 `data_capture` 授权体系。

关系如下：

| 能力 | 现有 capture | 新训练数据飞轮 |
|---|---|---|
| 目标 | 协议诊断、兼容性验证 | 训练数据资产 |
| 默认 | shape-only | L0/L1 默认，L2/L3 授权 |
| raw 开关 | 环境变量 unlock | 用户授权 + 服务端 consent |
| 输出 | trace 文件 | episode/event/dataset |
| 删除 | 本地 retention | 用户撤回 + 删除请求 |
| 训练标签 | 无或不足 | trainable/teacher/license/quality |

---

## 16. 验收清单

### 16.1 数据完整性

- [ ] 每个 episode 有唯一 episode_id；
- [ ] 每个 turn 有唯一 turn_id；
- [ ] 每个 tool_call 可关联 tool_result；
- [ ] 每个 artifact 可关联 episode；
- [ ] 每个 feedback 可关联目标输出；
- [ ] 每个 outcome 可追溯到 episode。

### 16.2 授权与隐私

- [ ] 无授权不采集 L2/L3；
- [ ] L3 必须强授权；
- [ ] 用户可暂停当前任务采集；
- [ ] 用户可撤回授权；
- [ ] 用户可删除历史 L3 数据；
- [ ] 每条数据有 consent_id；
- [ ] 每条数据有 capture_mode；
- [ ] 每条数据有 trainable；
- [ ] 每条数据有 owner/tier/egress_allowed/contribution_consent_id；
- [ ] 企业业务 raw 默认不出域，控制面与贡献数据进入出境账本；
- [ ] 个人/租户自有池、逐梦共建池、公共训练池隔离；
- [ ] 个人 L2/L3 默认进入个人自有池，贡献逐梦单独 opt-in；
- [ ] 人员维度默认聚合，人级分析需租户显式开启；
- [ ] L3+ Reasoning/CoT 有单独授权；
- [ ] hidden CoT 有单独开关和白名单。

### 16.3 安全

- [ ] token/cookie/API key 默认阻断或脱敏；
- [ ] L3 原文加密存储；
- [ ] 跨租户隔离；
- [ ] 本地缓存有 retention；
- [ ] 上传失败不会无限堆积；
- [ ] 诊断 raw capture 不与训练 raw capture 混用。

### 16.4 训练可用性

- [ ] 数据可导出 SFT；
- [ ] 数据可导出 DPO；
- [ ] 数据可导出 tool trajectory；
- [ ] 数据可导出 router dataset；
- [ ] teacher_eligible 可筛选；
- [ ] quality_score 可筛选；
- [ ] eval 数据和训练数据可隔离；
- [ ] reasoning_trace 可按来源/可见性/可训练性筛选；
- [ ] server_gateway_trace 与 local_execution_trace 可关联。

---

## 17. 推荐优先级

最高优先级：

1. 授权体系；
2. owner/tier/egress_allowed/contribution_consent_id 与三池模型；
3. 端侧分级、边缘脱敏、出境账本；
4. episode/turn/tool schema；
5. L3 原文采集开关；
6. L3+ Reasoning/CoT 采集开关；
7. 网关侧 server_gateway_trace 主干采集；
8. consent_id/trainable/capture_mode 标签；
9. secret scanner；
10. 删除/撤回机制。

第二优先级：

1. artifact 和 feedback；
2. outcome 识别；
3. quality scoring；
4. teacher_eligible 模型白名单；
5. dataset export。

第三优先级：

1. 多模型离线重放；
2. router 训练；
3. 自动偏好数据生成；
4. 强化学习 reward 信号；
5. 企业租户私有训练池；
6. 人员维度 AI 采用度看板；
7. Skill / Workflow 资产生命周期闭环；
8. 数据资产工作台与清洗流水线；
9. 数据贡献/交易包与交易账本。

---

## 18. 外部权威资料调研摘要

### 18.1 Claude Code 公开遥测实践

Claude Code 官方文档显示，其 OpenTelemetry 体系覆盖 metrics、events 与 beta traces。对逐梦最有参考价值的公开字段包括：

- session、lines of code、commit、pull request、cost、token、code edit decision、active time；
- prompt.id，用于把一个用户 prompt 触发的 API 请求和工具执行串起来；
- user_prompt、tool_result、api_request、api_error、api_refusal；
- raw API request/response body 的受控采集；
- tool_decision，记录工具是否被 accept/reject 以及来源；
- permission_mode_changed；
- MCP server connection；
- plugin installed/loaded；
- skill activated；
- at mention；
- hook registered/execution start/execution complete；
- compaction；
- feedback survey；
- tracing span hierarchy：interaction -> llm_request/tool/hook -> tool.blocked_on_user/tool.execution。

这说明成熟 Agent 产品会把“工具结果”“权限决策”“hook”“plugin/skill/MCP”“压缩”“反馈 survey”都视为可观测对象。逐梦训练数据飞轮应至少达到这一覆盖面，并在用户授权下增加训练原文与 outcome 聚合。

### 18.2 OpenAI Agents SDK 公开 tracing 实践

OpenAI Agents SDK 官方 tracing 文档显示，Agent run 会被组织为 trace/span，默认覆盖：

- whole runner workflow；
- agent span；
- LLM generation；
- function tool call；
- guardrail；
- handoff；
- audio transcription/speech；
- custom spans；
- trace metadata、group_id、workflow_name；
- sensitive data capture 开关；
- custom trace processors。

逐梦应采用类似 trace/span 思路，但训练数据侧需进一步把 span 派生为 episode/transition/preference 样本。

### 18.3 MCP 规范启示

MCP 官方规范强调 tools 是 model-controlled，并建议 human-in-the-loop、清晰展示工具、敏感操作确认、记录工具使用以便审计。工具定义包含 name、description、inputSchema、outputSchema、annotations、execution，工具结果可能包含文本、图片、音频、resource link、embedded resource、structuredContent。规范还指出工具执行错误应反馈给模型以支持自我纠正。

这对逐梦的要求是：不仅采集 tool call/result，还要采集工具 schema、输出 schema、annotations、execution/taskSupport、错误类型、structuredContent、resource link 和用户确认过程。

### 18.4 论文与开放 Agent 系统启示

- **DeepSeek-R1**：公开 README 说明其通过 RL 探索 CoT，并展示 reasoning patterns 可蒸馏到小模型；其 GitHub 仓库标注 MIT License。但实际采集仍需按部署来源、模型权重许可、API 服务条款和用户授权逐项确认。
- **ReAct**：推理与动作交错的轨迹可用于 fine-tuning，小模型可从成功轨迹中学习任务解决过程。
- **Toolformer**：训练目标应覆盖何时调用工具、调用哪个工具、传什么参数、如何使用工具结果。
- **ToolBench/ToolLLM**：大规模真实 API/tool 数据、自动数据构造和评测对通用 tool-use 能力关键。
- **SWE-agent**：ACI/harness 会显著影响自动软件工程任务表现。
- **CodeAct/OpenHands**：把动作统一为可执行代码/命令，并用 observation 反馈修正，是强 Agent 的重要范式。
- **AgentGym**：通用 Agent 需要多环境探索、高质量轨迹和可扩展 self-evolution 方法。
- **Agent Lightning**：应把 agent execution 与 training 解耦，将复杂轨迹拆成可训练 transition，并做 credit assignment。
- **tau-bench / tau2-bench**：真实 Agent 需要在用户、工具和动态环境中保持一致性、遵守规则并完成最终状态目标。
- **AgentBoard / trajectory-aware benchmarks**：只看最终答案不足，应做过程级、轨迹级分析。

### 18.5 对逐梦方案的直接补强结论

逐梦要追求超越 DeepSeek 甚至接近闭源模型，数据飞轮至少要覆盖：

1. 内容原文；
2. 工具轨迹；
3. 权限与拒绝；
4. Harness/ACI；
5. 上下文工程；
6. 多 Agent 协作；
7. 隐式反馈；
8. 代码执行与测试结果；
9. 浏览器/computer-use 真实状态；
10. 失败恢复；
11. 多模型对比；
12. RL transition 与 credit assignment。

## 19. 参考资料

- Claude Code Monitoring / OpenTelemetry: https://code.claude.com/docs/en/monitoring-usage
- Claude Code Agent SDK Observability: https://code.claude.com/docs/en/agent-sdk/observability
- OpenAI Agents SDK Tracing: https://openai.github.io/openai-agents-python/tracing/
- OpenAI Agents SDK Guide: https://developers.openai.com/api/docs/guides/agents
- OpenTelemetry GenAI Semantic Conventions: https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/
- Model Context Protocol Tools Specification: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- ReAct: https://arxiv.org/abs/2210.03629
- Toolformer: https://arxiv.org/abs/2302.04761
- ToolLLM / ToolBench: https://arxiv.org/abs/2307.16789
- SWE-agent: https://arxiv.org/abs/2405.15793
- CodeAct: https://arxiv.org/abs/2402.01030
- OpenHands: https://arxiv.org/abs/2407.16741
- AgentGym: https://arxiv.org/abs/2406.04151
- Agent Lightning: https://arxiv.org/abs/2508.03680
- tau-bench: https://arxiv.org/abs/2406.12045
- AgentBoard: https://arxiv.org/abs/2401.13178
- DeepSeek-R1 GitHub: https://github.com/deepseek-ai/DeepSeek-R1
- DeepSeek-R1 Paper: https://github.com/deepseek-ai/DeepSeek-R1/blob/main/DeepSeek_R1.pdf
- Anthropic Building Effective Agents: https://www.anthropic.com/research/building-effective-agents
- Anthropic Multi-agent Research System: https://www.anthropic.com/engineering/multi-agent-research-system
- Anthropic Context Engineering: https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents
- Anthropic Writing Tools for Agents: https://www.anthropic.com/engineering/writing-tools-for-agents

## 20. 最终建议

逐梦 Agent 应把“训练数据飞轮”作为底层产品能力，而不是事后补日志；v0.3 起还应把 Reasoning/CoT、server_gateway_trace、local_execution_trace、Harness/ACI、权限拒绝、上下文工程、隐式反馈和 RL transition 作为一等数据对象。

推荐路线：

```text
先建设授权与事件骨架
  -> 预埋 owner/tier/egress_allowed/contribution_consent_id 与三池模型
  -> 网关侧采集模型主干数据，本地侧补全执行与采纳数据
  -> 端侧先分级/脱敏/出境决策，企业业务 raw 默认留在客户边界内
  -> 尽快支持用户授权 L3 原文与 L3+ Reasoning/CoT 采集
  -> 建立 episode 聚合和质量评分
  -> 输出客户自有 eval/工作流/skill 资产，并在共建授权下输出 SFT/DPO/tool-use/router 数据集
  -> 用合格开源模型轨迹与共建池数据训练逐梦自有 Agent 模型
```

这条路线与当前项目已有 Codex Gateway、Desktop Capture、本地代理能力高度匹配，落地成本低于从零搭建，但必须严肃处理用户授权、敏感信息、许可证和删除机制。

核心原则：

> 有授权才采集原文；有白名单才采集 hidden/internal CoT；有标签才进入训练；有质量筛选才导出数据集；有删除机制才建立长期信任。

---

## 21. 附录·未来留缝（训练方法级数据，仅登记不展开）

> 按 v3 brief §11.1“V1 ship 前不写飞轮 RL 方案、深度不超现实一个版本”，以下训练方法级数据需求**仅登记列名**，等 V2+ 法规与进度就绪再设计。登记几乎零成本、堵死将来重构大坑；登记 ≠ 现在设计。

- 每个 turn“模型实际收到的精确渲染输入”（行为克隆保真）；
- 可复现环境快照 / replay bundle（RLVR 可验证奖励前提；编程/沙箱易、实时浏览器难）；
- 同一状态下多候选采样（reward model / DPO 成对样本，需 n>1 主动采样）；
- teacher logprobs / token 分布（自托管独有的蒸馏红利）；
- step 级过程奖励派生（PRM / 过程监督）；
- 检索失败 → 下游失败的因果链；
- 中途纠偏（steering）的轨迹定位；
- chat template / tokenizer / 工具序列化格式元数据；
- 闭环遥测：config/policy 版本向量 → 纵向 outcome 回归（“hill climbing”度量，详见文档2 §7 B 摞）；
- 个人可携带 AI 工作记忆；
- 数据贡献/交易包估值与交易账本。

> 详细动机与归属见文档2 §7；本附录只作为文档1 的实现侧登记。

展开触发条件：只有当个人/团队共建池或企业合同化贡献池累计到足够高质量 episode，且私有 eval、授权撤回、出境账本、数据删除链路均稳定后，才展开训练方法级方案。
