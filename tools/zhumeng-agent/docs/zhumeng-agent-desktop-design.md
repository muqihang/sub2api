# 逐梦注入工具桌面版设计方案

> 版本：v1 草案  
> 适用：`zhumeng-agent` 桌面客户端 (macOS 优先 / Windows 跟进)  
> 关联仓库：`sub2api-zhumeng-main/tools/zhumeng-agent`  
> 关联网页：逐梦控制台 `/codex` 接入中心  
> 文档定位：产品 + 信息架构 + 交互 + 视觉方向 + 平台/技术选型，**不含实现代码**

---

## 0. 阅读约定

- 文档面向产品、设计、前后端工程、运维、测试。
- 文中"目标应用"统一指被注入的本地客户端，例如 `Codex App`、未来的 `Claude Desktop` 等。
- 文中"本机代理"指 `zhumeng_agent proxy-serve` 在本机监听的转发服务，端口动态分配（示例 `127.0.0.1:50793`）。
- 文中"接入中心"指逐梦后端给出的 Web 入口，例如 `https://zhumeng.example.com/codex`。
- 用户路径形态：**网页发起授权 → 深链拉起本机工具 → 工具完成注入 → 用户在目标应用里使用模型**。

---

## 1. 背景

### 1.1 现状
- 后端：`http://127.0.0.1:18081`（容器 `sub2api-p1_6`），生产环境对应线上服务。
- 工具：`tools/zhumeng-agent` 已具备 Python CLI 与本机代理能力。
  - `zhumeng-agent setup` / `repair codex` / `launch codex` / `proxy-serve` / `doctor` / `logout` 等命令已实现或在规划中。
  - 已支持深链 `zhumeng-agent://setup?...`，包装基础位于 `packaging/macos/Info.plist` 与 `packaging/windows/zhumeng-agent-protocol.reg`。
  - 已实现 `Codex App` 配置写入：`~/.codex/config.toml`、`~/.codex/auth.json`、`~/.codex/zhumeng-codex-models.json`。
  - 已封装 Codex 适配器（`adapters/codex/`）与基础 `BaseAdapter`，初步留下了多目标抽象空间。
- 网页 `/codex` 接入中心：负责创建一次性 `code` 与会话上下文，引导用户"打开本机客户端"。

### 1.2 真实痛点（来自最近排障）
- **代理端口漂移**：本机代理重启后端口变化，`Codex` 仍写着旧端口，结果出现 `stream disconnected before completion: error sending request for url (http://127.0.0.1:50793/v1/responses)`。
- **设备心跳异常**：`/codex` 第三步曾显示 `device_heartbeat 设备已离线，超过 10 分钟未收到心跳`，根因在后端，但桌面侧用户也需要能看到状态。
- **配置文件状态可见性差**：用户不知道 `~/.codex/config.toml` 是否被改写、是否有备份、是否仍指向旧端口。
- **修复路径不直观**：`zhumeng-agent repair codex` 是 CLI，普通用户无法发现。
- **多目标趋势**：内部已经在规划注入 `Claude Desktop` 与未来其它工具，CLI 只能单点接入难以扩展。

### 1.3 战略上下文
来自 `docs/brand/zhumeng-intent-brief.md`：逐梦从"卖电（API token）"升级为"卖电器（增强型 AI 工具）"。
桌面注入工具是"电器接通电网"的最后一公里，**它的体验直接决定首次接入成功率与留存**。这件工具不是中转站的附属，而是品牌承诺"开箱即用"的体感实体。

---

## 2. 目标

### 2.1 产品定位
- 产品名：**逐梦注入工具**（英文：Zhumeng Agent / Zhumeng Connect）。
- 一句话定位：**把逐梦能力一键注入你本机的 AI 客户端，并持续守护连接。**
- **不是** "Codex 注入工具"。`Codex App` 仅是第一个适配目标。
- 工具的人格：可靠的"系统服务面板 + 接入向导"，安静、清晰、可诊断。

### 2.2 业务目标
1. 把现有 CLI 能力包装成普通用户可用的桌面体验，覆盖授权、注入、代理、修复、退出全链路。
2. 抽象出**多目标适配框架**，新增一个目标应用只需要新增一个 Adapter，不动主框架。
3. 把"端口漂移、授权过期、后端不可达、应用未重启"等异常变成**可看见、可一键修复、可脱敏复制诊断**的体验。
4. 给品牌兜底：让用户感觉"这是一个企业级工具"，而不是"开发者临时脚本"。

### 2.3 体验目标
- 从网页点击"打开本机客户端"到目标应用可用：**目标 ≤ 30 秒、p95 ≤ 60 秒、≤ 3 次点击**。
- 出现任何异常时，**首屏能直接看到问题描述与一键修复按钮**，不需要让用户去翻菜单。
- 用户随时关闭主窗口，托盘仍能维持注入与代理（可选）。
- 用户可一键"退出接入"，工具帮其恢复原配置并可选撤销后端设备。

### 2.4 非目标 (Non-Goals)
- 不替代 `Codex App` 等目标应用本身的 UI 与功能。
- 不做逐梦自己的复杂插件市场 / 扩展商店 / 模型微调 / Prompt 编排。**注意：Codex 插件市场可用化是首版 Codex 接入核心能力，不属于本条非目标。**
- 不做企业级多租户管理控制台（保留在 Web 后台）。
- 第一版不做自动更新、不做云端日志聚合、不做 Windows 完整版。
- 不做端到端的"模型代答"或"代理调试器"，桌面工具只做"通路 + 健康"，不参与业务对话。

---

## 3. 用户画像

| 画像 | 描述 | 关键诉求 | 风险 |
|------|------|----------|------|
| 极客开发者 | 熟悉 CLI、熟悉 Codex/Claude/IDE | 想要快、想看日志、想一键诊断 | 不要来回点弹窗 |
| 企业研发 | 团队统一发卡、企业 SSO | 接入要稳、要能看到设备授权状态 | 担心 token、隐私泄露 |
| 学术/学习 | 会用 GUI 但不熟终端 | 想要"打开就能用" | 看到 CLI 报错就放弃 |
| 内部支持/运维 | 帮用户远程排障 | 想要诊断报告可一键复制脱敏 | 担心暴露用户敏感信息 |

设计时优先满足"学术/学习"场景的极简度，再满足"极客 + 运维"的可观测度。两者通过**默认极简 + 可展开高级**来兼顾。

---

## 4. 核心流程

### 4.1 首次接入主路径（Happy Path）

1. 用户在网页 `/codex` 创建接入凭证（一次性 `code`）。
2. 用户点击"打开本机客户端"。
3. 浏览器触发 `zhumeng-agent://setup?code=...&server=...&client=codex&...`。
4. 桌面工具被深链拉起：
   - 若工具已在运行，转入主窗口的"接入向导"。
   - 若未运行，启动后直接进入"接入向导"。
5. 工具显示**接入确认卡**：客户端类型、服务器地址、设备指纹（脱敏）、即将写入的配置摘要、用户主体（如有）。
6. 用户点击"确认接入"。
7. 工具按以下步骤执行（每步带状态条与失败回滚）：
   1. 用 `code` 向后端换取 `device_token`。
   2. 检测目标应用是否已安装（基于 Adapter）。
   3. **先**启动本机代理 `proxy-serve`，拿到实际端口（动态分配避免端口冲突）。
   4. 备份目标应用现有配置（带时间戳，限制份数）。
   5. 写入新的配置：把第 3 步拿到的实际代理 URL 直接落盘，**一次性写好**，避免占位符 → 回写的两阶段竞争。
   6. **启用 Codex 增强项**（仅 `CodexAdapter`）：检测并按需修补 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace`；任何 `app.asar` 修补都先检测状态、备份、校验唯一补丁点与完整性。
   7. 调用一次"健康自检"（loopback 请求 + `/codex/v1/models` 拉取一次 + Codex 增强项状态检查）。
   8. 注册"运行守护"与心跳。
   - 任一步失败：按反向顺序回滚（停止本步及之前的所有副作用），状态机回到 `authorized` 或 `idle`。
8. 工具显示**接入成功页**：连接状态、当前代理端口、模型目录摘要、Codex 插件市场状态（已可用 / 需重启 / 修补失败 / 当前版本不支持）、"打开 Codex App" 按钮、"复制脱敏诊断报告"按钮。
9. 用户点击"打开 Codex App"，工具调用 Adapter 的 launch 能力。
10. 工具最小化到托盘，托盘图标显示"已连接"绿色状态。

### 4.2 异常分支
- **目标应用未安装**：成功页变为"安装指引页"，提供官方下载链接 + 手动配置入口。
- **代理端口冲突**：动态分配端口本就避免此问题；若仍然失败，连续重试 3 次后报错，**不会**留下"代理已起、配置未写"的中间态。
- **后端不可达**：显示"网络异常 / 后端服务不可达"，提供"重试"和"查看诊断"。
- **授权过期**：直接跳"重新授权"二维码 + 网页 deep link 回流。
- **配置写入失败（权限/被占用）**：定位失败文件路径，提供"退出 Codex 后重试"；macOS 不引导提权（`~/.codex` 在用户目录下，不应需要 sudo），Windows 下若极端情况需要写系统目录才提示提权。
- **目标应用版本不兼容**：提示版本范围，给用户兼容版本下载链接。
- **目标应用正在运行**：进入 `app_running_blocking_change`；UI 给出三选项：`稍后我自己重启 Codex` / `退出 Codex 并继续`（用户主动确认） / `中止本次接入`。任何选项均不在未授权情况下强杀进程。
- **Codex 增强项版本不兼容**：只跳过对应修补项并给出"当前版本不支持"状态，不强行 patch `app.asar`。
- **app.asar 修补失败**：配置注入与代理可继续，但成功页必须显示"插件市场修补失败"，并提供"复制诊断 / 一键恢复 / 退出 Codex 后重试"。

### 4.3 长期使用路径
- 用户日常打开 `Codex App`，桌面工具无感知。
- 一旦健康检查失败（端口失联 / 鉴权 401 / 心跳掉线），托盘图标变黄/红，主窗口红条提示，提供"一键修复"。
- 用户主动"退出接入"：工具恢复目标应用原配置，恢复本工具曾修补过的 `app.asar` 备份，停止代理，清除本地状态文件，可选调用后端撤销设备。任一恢复失败都进入可诊断错误态，不能假装退出成功。

---

## 5. 信息架构

```
逐梦注入工具
├── 顶部全局状态条（持续可见）
│   ├── 连接整体状态：正常 / 注意 / 异常 / 未接入
│   ├── 当前账户（脱敏邮箱或租户名）
│   ├── 本机代理：端口 + 状态点
│   └── 操作：刷新 · 一键修复 · 复制诊断 · 设置
├── 左侧导航
│   ├── 概览                （Dashboard）
│   ├── 已接入应用           （Connected Apps）
│   ├── 应用目录             （App Catalog）
│   ├── 诊断与日志           （Diagnostics）
│   ├── 设置                 （Settings）
│   └── 关于                 （About）
└── 右侧内容区
    └── 视当前视图而定
```

### 5.1 概览（Dashboard）
- 一句话状态摘要："1 个应用接入正常，0 个异常"。
- 三张卡片：
  1. **当前连接**：账户、设备名、上次心跳、后端可达性。
  2. **本机代理**：端口、监听地址、CPU/内存占用（轻量）、上次重启时间。
  3. **最近事件**：最近 5 条事件流（接入成功、修复成功、代理重启、授权续期）。
- 主操作按钮：`打开最近接入的应用`、`一键修复全部`、`查看诊断`。

### 5.2 已接入应用（Connected Apps）
- 每个 Adapter 一个卡片：图标、名称、版本、状态徽章、所选模型集摘要、上次健康检查时间。
- 卡片操作：`打开` `重新授权` `修复` `查看详情` `退出接入`。
- 异常时卡片高亮（黄/红边），并把首要错误一行展示。

### 5.3 应用目录（App Catalog）
- 列出所有已注册的 Adapter（已接入与未接入合并展示，状态区分）。
- 每个 Adapter 卡片说明：用途、官方链接、平台支持、是否需要重启目标应用、是否需要管理员权限。
- 第一版只内置 `Codex` 一个；预留 `Claude Desktop`、`Continue.dev`、`Aider` 等的 placeholder（标记"即将支持"）。
- 用户从应用目录"接入"按钮发起的路径：工具打开默认浏览器到 `/codex` 接入中心（携带 client 参数），用户在网页生成 `code` 后由网页拉起深链 `zhumeng-agent://setup?...`，最终复用同一条深链路径，工具内不再有"无 code 接入"分支。

### 5.4 诊断与日志（Diagnostics）
- 顶部：实时健康检查面板（每条检查项独立显示 ✓/✗，附耗时）。
- 中部：日志查看器（按 Adapter / 全局 切换 tab；支持过滤等级）。
- 底部：脱敏诊断报告生成区，按钮 `生成` `复制` `保存为 .txt`。
- 醒目说明："默认不会上传日志。"

### 5.5 设置（Settings）
- 启动：开机自启（v2 引入）、最小化到托盘、关闭主窗口时是否退出代理。
- 代理：动态端口（默认推荐）/ 固定端口（高级）；监听地址固定 `127.0.0.1`；保留备份份数。
- 供应商：见 §6.5；v1 仅展示逐梦托管，自定义供应商入口为占位。
- 模型：主列表显示规则（严格 / 宽松，详见 §6.5.6）。
- 高级：启用 verbose 日志、启用裸协议捕获（受 unlock 限制）、清空状态文件。
- 隐私：诊断脱敏强度（标准 / 严格）、允许遥测（默认关闭）。
- 网络：后端 origin 锁定、HTTP 代理（系统 / 直连）。

### 5.6 关于（About）
- 版本号、更新通道、开源致谢（含 CodexPlusPlus、cc-switch 参考）、隐私声明、服务条款、问题反馈入口。

---

## 6. 多目标适配架构

这是本设计的核心。所有可注入应用都被建模为 `Adapter`，由统一的"注入引擎"调度。

### 6.1 注入引擎（InjectionEngine）
职责：
- 维护已注册 Adapter 列表（来自内置 + 配置）。
- 接管深链 `zhumeng-agent://setup` 的解析与路由。
- 调度 Adapter 的生命周期：`detect → install → authorize → start_proxy → backup_config → write_config/bind_proxy → codex_enhance（仅 CodexAdapter） → health_check → run → repair → uninstall`。顺序必须与 §4.1 一致，避免先写占位配置再回写端口导致端口漂移。
- 维护全局状态机（见第 9 章）与事件总线。
- 把 Adapter 报错翻译成统一错误模型（见第 10 章）。

### 6.2 Adapter 抽象（每个适配器需要回答下列问题）

| 字段 | 含义 | 示例（Codex） |
|------|------|---------------|
| `id` | 唯一标识 | `codex` |
| `display_name` | 中文名 | `Codex App` |
| `vendor` | 来源 | `OpenAI` |
| `icon` | 图标资产 | `assets/adapters/codex.svg` |
| `platforms` | 支持平台 | `[macos, windows]` |
| `min_app_version` | 兼容下限 | `0.x` |
| `detect()` | 检测安装路径与版本 | 扫描 `/Applications/Codex.app`、注册表 |
| `config_targets` | 需要写入的配置文件 | `~/.codex/config.toml` 等 |
| `injection_strategy` | 注入方式 | 配置注入、env 注入、asar patch |
| `proxy_required` | 是否需要本机代理 | `true` |
| `proxy_url_field` | 代理 URL 写到哪个字段 | `[providers.zhumeng].base_url` |
| `deeplink_handlers` | 深链子动作 | `setup`, `reauth`, `open` |
| `health_checks` | 健康检查列表 | 端口可达、`/v1/models` 200、心跳 < 10min |
| `repair_actions` | 修复动作 | 重启代理、回写端口、刷新 token |
| `restart_required_on_change` | 改完是否要重启目标应用 | `true` |
| `uninstall_strategy` | 退出接入恢复策略 | 还原备份、删除注入文件 |
| `tray_quick_actions` | 托盘菜单中暴露的快捷动作 | `打开 Codex` `重启代理` |

### 6.3 当前 Adapter 规划

- `CodexAdapter`（第一版唯一可用）：
  - 写 `config.toml` / `auth.json` / `zhumeng-codex-models.json`。
  - 启动 `proxy-serve`，把端口回写到 `base_url`。
  - 首版主流程启用 Codex 增强项：`model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace`。这些能力不再只是"高级排障"，而是 Codex 接入成功的组成部分。
  - `plugin-auth-gate` 解决 API-key / 逐梦托管配置下 Codex 插件导航和插件页被禁用的问题。
  - `plugin-mention-marketplace` 解决插件提及、插件市场入口等相关可用性问题。
  - `capture` 仍保持高级诊断能力，不进入首版接入主流程。

- `ClaudeDesktopAdapter`（第二版起）：
  - 检测 `Claude.app` 或 Windows 安装路径。
  - 写其配置（具体方式留待后续探针）。
  - 复用 `proxy-serve`，可能需要不同的 base path mapping。

- `CustomAppAdapter`（远期）：
  - 给企业用户/极客用户暴露"自定义注入项"：选定一个本地程序、定义一个配置文件路径、声明字段映射。
  - 用于已知有 OpenAI 兼容入口、但官方未支持的程序。

### 6.4 Adapter 注册与扩展
- 内置 Adapter 随版本发布。
- 远期允许用户从签名清单导入额外 Adapter（受白名单限制，第一版不开放）。
- 任何 Adapter 都可独立通过 CLI 调用（保留底层 CLI 入口，方便排障）。

### 6.4.1 Codex 增强项修补安全模型（首版）

Codex 增强项是 `CodexAdapter` 的主流程能力，目标是让逐梦托管配置下的 Codex App 不只完成模型注入，还能正常进入模型选择和本地插件市场。

首版包含三个修补项：

| 修补项 | 目的 | 失败后的 UI 状态 | 是否影响基础对话 |
|--------|------|------------------|------------------|
| `model-picker` | 让 Codex 模型选择入口正确展示逐梦模型目录 | `模型选择增强失败` | 不阻断基础代理，但影响选模型体验 |
| `plugin-auth-gate` | 解除 API-key / 逐梦托管配置下插件导航、插件页被禁用的问题 | `插件市场需修复` | 不阻断基础代理，但影响插件市场 |
| `plugin-mention-marketplace` | 修复插件提及、插件市场入口相关可用性 | `插件提及需修复` | 不阻断基础代理，但影响插件入口和提及 |

这些修补项可能修改 `Codex.app` 内部的 `app.asar`。任何实现都必须遵守同一安全流程：

1. **修补前状态检测**：识别当前 Codex 版本、`app.asar` hash、已修补 / 未修补 / 不兼容 / 未知。
2. **自动备份**：首次修改前备份原始 `app.asar` 与必要 metadata（版本、hash、时间、修补项列表）。
3. **唯一补丁点校验**：每个修补项必须只命中唯一 patch point；0 个或多个命中都视为不兼容，不强行修补。
4. **完整性校验**：修补前后都记录 hash；修补后的结构检查不通过则立即回滚。
5. **签名 / 重签名策略**：若修补影响 Codex.app 原签名，必须明确提示风险；能安全重签时执行重签，不能重签时以"需用户确认"或"不支持当前版本"呈现，不静默破坏。
6. **重启提示**：修补成功后进入 `restart_required`，明确提示"需要重启 Codex App 后插件市场生效"。
7. **版本不兼容保护**：未知版本、不唯一 patch point、完整性校验失败时不 patch。
8. **一键恢复**：可从详情页、诊断页、退出接入流程恢复到修补前备份。

健康检查必须覆盖：
- 插件市场入口是否可用；
- `plugin-auth-gate` 状态；
- `plugin-mention-marketplace` 状态；
- `model-picker` 状态；
- `app.asar` 修补状态与备份状态。

一键修复必须按 `model-picker → plugin-auth-gate → plugin-mention-marketplace` 顺序逐项检测和修复；若 Codex 正在运行，提示用户退出 Codex 后继续，不静默强杀。

### 6.5 多供应商模型路由（Provider × Model × Adapter）

本节定义"模型来源"这一层。它与 Adapter（目标应用层）正交，**一个 Adapter 可消费多个 Provider 的模型**，同一个 Provider 也可被多个 Adapter 复用。

#### 6.5.1 三层抽象

```
Adapter        要求一组能力 (Requirements)
   |
   |  消费
   v
Model          属于某个 Provider，并继承 Provider 的能力声明 (Capabilities)
   ^
   |  归属
   |
Provider       声明协议 / 能力 / 凭据 / 端点
```

- **Adapter 不直接绑定 Model**。Adapter 只声明"我需要哪些能力"，由路由层匹配 Provider × Model。
- **Model = Provider × ModelId**。同名模型来自不同 Provider 视为不同 Model（例如 `gpt-5-codex@zhumeng` 与 `gpt-5-codex@custom_cloud`）。
- **门禁公式**：`model.in_main_list_for(adapter) := adapter.requirements ⊆ provider.capabilities`。不满足则按"严格模式 / 宽松模式"决定隐藏或角标。

#### 6.5.2 来源标识 (origin)

| origin | 含义 | 路由路径 | 凭据存储 | 诊断脱敏 |
|--------|------|----------|----------|----------|
| `zhumeng` | 逐梦托管供应商（默认） | 本机代理 → 逐梦后端 → 上游 | 由后端持有 | 无额外字段 |
| `custom_local` | 本机服务（如 Ollama / LM Studio） | 本机代理 → `127.0.0.1:<port>` | 一般无密钥 | endpoint 端口保留 |
| `custom_cloud` | 用户自带云上 API | 本机代理 → 直连用户 endpoint | 平台 Keychain | endpoint 域名脱敏，key 全脱敏 |

**关键约束**：`custom_*` 默认走"本机代理直连旁路"，**不**将用户的密钥与请求体上传逐梦后端。这是隐私默认，且写进路由层而非 UI 文案。

#### 6.5.3 Provider 数据结构

| 字段 | 含义 | 示例 |
|------|------|------|
| `id` | 唯一标识 | `zhumeng` / `oa-direct-1` |
| `display_name` | 中文名 | `逐梦托管` / `OpenAI 直连` |
| `origin` | 来源类别 | `zhumeng` / `custom_cloud` |
| `protocol` | 协议适配器 | `zhumeng-codex` / `openai-responses` / `anthropic-messages` / `openai-chat-completions` |
| `endpoint` | 实际地址 | `https://api.openai.com` |
| `auth` | 凭据引用 | `keychain://openai/sk-***` |
| `capabilities` | 能力声明 | 见 6.5.4 |
| `models` | 该 Provider 暴露的模型 | `[gpt-5-codex, gpt-4o, ...]` |
| `pricing` | 模型单价信息 | 见 6.5.10，来自后端数据库 |
| `default_for_adapter` | 默认绑定到哪些 Adapter | `[codex]` |
| `fallback_policy` | 失败时是否回退到 zhumeng | `none` / `to_zhumeng`（默认 `none`） |
| `enabled` | 用户开关 | `true` |

#### 6.5.4 能力矩阵 (Capabilities)

布尔字段：
- `responses`：支持 Responses API 协议（Codex 主路径）
- `chat_completions`：兼容 OpenAI Chat Completions
- `messages`：兼容 Anthropic Messages
- `streaming`：服务端事件流
- `tools` / `parallel_tool_calls`：工具调用 / 并行工具调用
- `vision_input`：图片输入
- `audio_input` / `audio_output`：音频输入 / 输出
- `cache_billing`：缓存计费（Anthropic prompt cache / OpenAI cached input）
- `context_continuation`：多轮上下文续接（thread / conversation）
- `structured_output`：结构化输出 / JSON Schema
- `reasoning_effort`：推理强度参数

数值字段：
- `max_context_tokens`：上下文窗口上限
- `max_output_tokens`：单次输出上限

#### 6.5.5 Adapter 能力要求

| Adapter | 必须 | 推荐 | 影响 |
|---------|------|------|------|
| `CodexAdapter` | `responses`, `streaming`, `tools`, `context_continuation` | `vision_input`, `parallel_tool_calls`, `reasoning_effort`, `cache_billing` | 缺必须能力则不能进入 Codex 主模型列表 |
| `ClaudeDesktopAdapter`（v4） | `messages`, `streaming`, `tools` | `vision_input`, `cache_billing` | 缺必须能力则不能进入 Claude 模型列表 |
| `ChatOnlyAdapter`（通用） | `chat_completions`, `streaming` | `tools` | 仅作为兜底分类 |

#### 6.5.6 主模型列表门禁

- **默认严格模式**：`adapter.requirements ⊄ provider.capabilities` 的模型直接不在主列表显示，仅在"模型目录"页可见，并打灰色标签 `工具能力有限`。
- **宽松模式**（设置里显式开启）：在主列表展示但加角标 `聊天可用，工具能力有限`；用户首次选定时弹一次"我已知风险"确认。
- **禁止做"自动尝试，失败再提示"**：那种路径会在 Codex 内部产生难诊断的协议错误，背离"诊断优先"原则。

#### 6.5.7 路由层（本机代理）

本机代理在 6 层抽象上扮演 Router：

1. 接收来自 Codex App 的请求 `POST /v1/responses`。
2. 取出请求里的 `model`，查询 ModelCatalog → 解出 `provider`。
3. 按 `provider.protocol` 选择 Adapter Plugin（协议层）：
   - `zhumeng-codex` → 转发到逐梦后端 `/codex/v1/...`
   - `openai-responses` → 转发到 `provider.endpoint/v1/responses`
   - `openai-chat-completions` → 协议下变形（Responses → ChatCompletions）后转发
   - `anthropic-messages` → 协议变形（Responses → Messages）后转发
4. 如果 `provider.fallback_policy = to_zhumeng` 且失败，则二次走 zhumeng；否则原样返回错误。
5. 全程**不在桌面工具进程内**持有用户业务 Payload；本机代理才是协议变形点，让 UI 进程的内存与日志最干净。

#### 6.5.8 阶段化交付

- **v1（第一版）**
  - 仅 `zhumeng` 一个 Provider 实际可用。
  - UI、数据结构、Provider 列表项、Model 目录的 `origin`、`capabilities`、`pricing` 字段全部已有。
  - 设置页"供应商"标签下显示 `+ 添加供应商` 入口，点击提示"将在 v0.2 提供"。
  - 后端 `/codex/v1/models` 返回需补 `capabilities` 与 `pricing` 字段（由后端基于真实模型目录与数据库单价填）。
- **v2**
  - 启用"自定义供应商"创建流程，支持 `protocol=openai-responses`。
  - 凭据走 Keychain；端点白名单仅限 https。
  - 路由层接入 `openai-responses` 协议适配器。
- **v3**
  - 增加 `anthropic-messages` 协议适配器（含 Anthropic prompt cache 计费字段）。
- **v4**
  - 增加 `openai-chat-completions` 适配器（用于 DeepSeek 等 OpenAI 兼容直连）。
  - 引入 `ClaudeDesktopAdapter`，开始多 Adapter × 多 Provider 全联通。
  - 提供 Provider 健康检查 / 试连按钮。
- **v5+**
  - 自定义协议（gemini-generate-content、ollama-native 等）。
  - 远程签名 Provider 清单（企业策略下发）。

#### 6.5.9 与现有代码的对齐

- 模型目录文件 `~/.codex/zhumeng-codex-models.json` 在 v1 仍由 `CodexAdapter.config_manager` 负责写入，但内部结构需要扩展：每条 model 增加 `origin`、`provider_id`、`capabilities`、`pricing` 字段。
- 后端 `/codex/v1/models` 推送内容向上述结构对齐；桌面工具按 Provider 分组展示。
- `proxy/upstream.py` 在 v2 起需要支持多 upstream + protocol 适配器；v1 维持单一逐梦上游即可。
- `state.json` 增加顶层字段 `providers: []` 用于持久化用户自定义条目（v1 始终为空数组）。

#### 6.5.10 模型价格与目录展示

模型目录必须展示**完整列表**，并支持固定高度滚动，避免模型数量多时撑爆页面。表头固定，滚动只发生在表体。

搜索 / 筛选能力：
- 搜索：按模型名模糊搜索。
- 供应商筛选：逐梦托管、自定义云端、自定义本机、未来远程签名 Provider 清单。
- 能力筛选：Responses、流式、工具调用、图片输入、缓存计费、上下文续接等。
- 主列表状态筛选：进入 Codex 主模型列表 / 不进入主列表 / 宽松模式可见。
- 可用性筛选：可用 / 受限 / 不兼容 / 价格未配置。

分组：
- `zhumeng`：逐梦托管。
- `custom_cloud`：用户自定义云端供应商（v2 起）。
- `custom_local`：用户自定义本机供应商（v4 起）。
- `remote_signed_provider`：未来远程签名 Provider 清单。

价格数据约束：
- 价格必须来自后端数据库模型单价，或由后端生成的本地 catalog 缓存；**前端不得硬编码模型价格**。
- 如果价格字段缺失，显示 `未配置`，不要臆造。
- DeepSeek / Claude / GPT 等模型是否完全兼容也不能在前端硬编码，必须来自后端返回的 `capabilities` 与实际模型目录。

`pricing` 结构预留：

| 字段 | 含义 |
|------|------|
| `input_price` | 输入 token 单价 |
| `output_price` | 输出 token 单价 |
| `cached_input_price` | 缓存命中输入单价 |
| `cache_write_price` | 缓存写入单价；若后端已有字段名，按现有字段名对齐 |
| `currency` | 币种，例如 `USD` / `CNY` |
| `unit` | 计价单位，例如 `per_1m_tokens` |
| `updated_at` | 价格更新时间 |
| `source` | 数据来源，例如 `database_model_pricing` |

价格悬浮层展示：
- 输入价格；
- 输出价格；
- 缓存命中价格；
- 缓存写入价格；
- 计价单位；
- 数据来源；
- 更新时间。

---

## 7. 桌面端界面设计

### 7.1 设计原则
- "**像系统设置面板，不像营销页**"：克制、安静、信息密度适中。
- "**异常一眼看懂**"：状态色彩与微动效用于状态告警，而不是装饰。
- "**默认极简，进阶可展开**"：日常视图只露出 2~3 个主操作；高级能力放进折叠区。
- "**可达性优先**"：键盘可达、Tab 顺序合理、对比度 ≥ AA。
- "**离线友好**"：所有静态界面在离线时仍可呈现并解释当前异常。

### 7.2 布局骨架

```
┌──────────────────────────────────────────────────────────────────────┐
│ ◉ 已连接   muqihang@zhumeng    Proxy 127.0.0.1:50793   ⟳ 修复 ⋯ 设置 │   ← 顶部全局状态条
├──────────┬───────────────────────────────────────────────────────────┤
│ Overview │   主内容区（按左侧导航切换）                              │
│ Apps     │                                                           │
│ Catalog  │                                                           │
│ Diag.    │                                                           │
│ Settings │                                                           │
│ About    │                                                           │
└──────────┴───────────────────────────────────────────────────────────┘
```

### 7.3 顶部全局状态条
- 左侧：状态点 + 一句话（"已连接" / "正在检查" / "需要修复" / "未接入"）。
- 中部：账户 + 设备名（鼠标悬停展示完整脱敏信息）。
- 右侧：代理端口胶囊、`刷新` `一键修复` `复制诊断` `设置` 图标按钮。
- 出现严重异常时整条变红底白字，并把核心错误浮出（例："本机代理已离线"）。

### 7.4 概览页（Dashboard）

布局：上 1 行三张卡片 + 下 1 行 "最近事件" + 1 行 "快速操作"。

- 卡片 A：**当前连接**
  - 账户脱敏；当前选定后端 origin；上次心跳；token 过期倒计时。
  - 操作：`刷新` `重新授权`。
- 卡片 B：**本机代理**
  - 端口、监听 IP、运行时长、自启状态。
  - 操作：`重启代理` `查看代理日志`。
- 卡片 C：**目标应用**
  - 已接入数量、当前异常数量。
  - 操作：`查看应用列表`。
- 最近事件流：每行 1 条，时间 + 图标 + 一句话；点击展开详情。
- 快速操作：`打开 Codex App` `修复全部` `生成诊断报告` `退出接入`。

### 7.5 已接入应用页
- 卡片网格（小屏自动堆叠为列表）。
- 卡片内容：
  - 应用图标、名称、版本、徽章（绿/黄/红/灰）。
  - 一行子文：`代理 127.0.0.1:50793 · 模型 12 · 上次心跳 12s 前`。
  - 操作按钮：主操作 `打开` + 二级菜单 `重新授权 / 修复 / 高级 / 退出接入`。
- 每张卡片可点击进入详情页。

### 7.6 应用详情页（结构通用）
顶部：状态徽章 + 应用元信息 + 主操作按钮（打开 / 修复 / 退出接入）。

中部分区（每个分区都是独立可观察单元）：
1. **接入状态**：当前 token 摘要（脱敏）、设备名、上次心跳、过期时间。
2. **配置文件**：列出 Adapter 写入的全部路径，每行展示"是否已被本工具管理"、最后修改时间、查看 / 还原备份。
3. **本机代理映射**：本工具代理 → 后端 origin 的拓扑图（一行示意）。
4. **Codex 增强项**：Model Picker、Plugin Auth Gate、Plugin Mention Marketplace、备份状态、是否需要重启。
5. **模型目录**：完整模型列表（固定高度滚动、表头固定、搜索/筛选、价格悬浮）；过期/不可用/受限模型置灰并标注原因。
6. **健康检查**：Adapter 自报告项（端口可达、API 200、模型同步、应用版本、插件市场入口、`app.asar` 修补与备份）。
7. **高级手动修复**：折叠区，保留 Adapter 私有动作（Codex 下放 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace`、`capture`），但不再作为插件市场可用化的唯一入口。

底部：诊断按钮、退出接入按钮（带二次确认）。

#### 7.6.1 Codex 增强项卡片

首版 `CodexAdapter` 详情页必须有一张独立卡片，展示：
- `Model Picker`：已修补 / 未修补 / 不兼容 / 失败。
- `Plugin Auth Gate`：已修补 / 需重启 / 失败。
- `Plugin Mention Marketplace`：已修补 / 需重启 / 失败。
- 备份：可恢复 / 缺失 / 校验失败。
- 插件市场总状态：已可用 / 重启后生效 / 修补失败 / 当前版本不支持。

卡片操作：
- 主操作：`检查并修复 Codex 增强项`。
- 次操作：`查看 app.asar 备份`、`一键恢复增强项`、`复制诊断`。
- 若检测到 Codex 正在运行，主操作变为 `退出 Codex 后继续`，不静默强杀。

#### 7.6.2 模型目录表格

- 固定高度：建议 320-420px，表体滚动，表头固定。
- 搜索框：占位文案 `搜索模型名 / 供应商 / 能力`。
- 筛选器：供应商、能力、主列表状态、可用性。
- 表格列：模型名、供应商、来源、能力、上下文、价格、Codex 主列表状态。
- 价格列默认显示短标签（如 `查看价格` / `未配置`），鼠标悬浮展示价格明细。
- 价格 tooltip 的数据来自 `pricing` 字段；缺失字段显示 `未配置`。

### 7.7 应用目录页
- 卡片：图标 + 名称 + 一句话价值 + 平台徽章 + 状态（已接入 / 可接入 / 即将支持 / 不支持当前平台）。
- 点击可接入卡片 → 工具调起默认浏览器到 `/codex?client=<adapter_id>`；网页生成 `code` 并拉起深链；后续路径与深链路径完全一致。**不存在脱离 code 直接接入的分支**。

### 7.8 接入向导（深链触发或手动触发）
- 半模态窗口（不阻塞主窗口，但需明确确认）。
- 步骤条：`确认 → 检测 → 写入配置 → 启动代理 → 健康检查 → 完成`。
- 每一步都展示 1 行人类语言 + 1 行机器语言（debug 模式可见）。
- 任一失败都允许：`重试 / 跳过 / 中止 / 复制诊断`。

### 7.9 诊断与日志页
- 上：实时检查表（每行：检查项、状态、耗时、详情按钮）。
- 中：日志面板，标签 `[全局] [Codex] [Proxy] [Backend]`，支持级别 `INFO/WARN/ERROR`，支持搜索。
- 下：脱敏报告区。
  - 默认展示"将包含的信息" / "将被脱敏/排除的信息"两栏。
  - 操作：`生成报告` → 弹出预览 → `复制到剪贴板` `另存为`。

### 7.10 设置页
- 分组卡片化：启动 / 代理 / 隐私 / 高级 / 网络。
- 每项设置一行说明，避免黑盒开关。
- 危险开关（如启用裸协议捕获）放在最底部，并要求二次确认。

### 7.11 状态视觉语言
- 状态色：
  - 正常：品牌色绿系（建议 `#16A34A` 暗 `#22C55E`）。
  - 注意/可恢复：琥珀（`#D97706`）。
  - 异常/不可恢复：红（`#DC2626`）。
  - 未接入/灰态：中性灰。
- 状态点尺寸：`6px` 圆点 + 同色 12px halo（轻量光晕，用于顶部状态条与托盘）。
- 状态徽章：圆角胶囊 24px 高，仅文字 + 图标，不使用大色块。
- 异常态文案永远是"问题 + 建议动作"，例如"代理已离线，点击修复"。

### 7.12 浅色 / 深色
- 必须双主题，跟随系统并允许手动切换。
- 深色模式下，背景偏中性深灰（`#0F1115`），不使用纯黑；卡片用 `#171A21`。
- 浅色模式下，背景近白（`#F7F7F8`），卡片用 `#FFFFFF`。
- 边框使用 1px 单色 + 透明度，避免阴影过重。

### 7.13 视觉性格
- 字体：系统优先（macOS `SF Pro Text` / Windows `Segoe UI Variable`）。
- 圆角：8px 卡片 / 6px 按钮 / 999px 状态胶囊。
- 间距系统：4 / 8 / 12 / 16 / 24 / 32。
- 不使用大面积渐变、玻璃拟态、装饰插画。允许在"接入成功"瞬间使用一次轻量动画反馈。
- 图标统一线性风格，2px 描边，端点圆角，色彩跟随状态语义。

### 7.14 关键交互细节
- 顶部状态条点击"代理胶囊"展开 popover：端口、上次重启时间、`重启` `复制 URL` `查看日志`。
- 已接入卡片右键 / `⋯` 菜单与托盘子菜单等价，避免功能埋藏。
- 所有"危险动作"都通过抽屉式确认（描述将发生什么、影响范围、是否可撤销）。
- 全局支持 `⌘K / Ctrl+K` 命令面板：搜"修复"、"打开 codex"、"复制诊断"、"切换深色"。

---

## 8. 托盘设计

托盘是"日常存在感"的载体，决定用户在不打开主窗口的情况下能否处理异常。

### 8.1 托盘图标状态
- 静态：品牌简化标识（与 Dock/任务栏图标一致但单色）。
- 动态：右下角覆盖一个 4~5px 的状态点（绿/黄/红/灰）。
- 严重异常：图标短暂脉冲（≤ 1 秒）一次，避免持续抖动打扰。

### 8.2 托盘菜单（默认）
```
逐梦注入工具
─────────────────
● 已连接 — Codex App
  Proxy 127.0.0.1:50793
─────────────────
打开主窗口
打开 Codex App                ⌘O
─────────────────
重启本机代理
修复全部应用
重新授权…
─────────────────
诊断与日志…
复制脱敏诊断报告
─────────────────
设置…
关于
退出
```

### 8.3 异常态托盘菜单（替换头部）
```
⚠ 需要修复 — 本机代理已离线
[ 一键修复 ]
[ 查看诊断 ]
─────────────────
（其余菜单同默认）
```

### 8.4 托盘行为
- 单击：macOS 弹菜单；Windows 弹菜单（双击打开主窗口可选）。
- 不支持的功能在当前平台直接不显示，避免灰色项噪音。

---

## 9. 状态机

注入引擎维护**全局会话状态**，每个 Adapter 维护**本地会话状态**。两层状态在 UI 上合成"顶部状态条"与"卡片状态"。

### 9.1 全局会话状态
| 状态 | 触发条件 | UI 表现 | 可用操作 |
|------|----------|---------|----------|
| `idle` | 工具刚启动、未接入任何应用 | 顶部灰，概览页空状态 | 浏览目录、扫码接入 |
| `awaiting_web_authorization` | 已通过深链开始流程，等待网页授权回调或 code 兑换 | 顶部黄，"等待授权" | 取消、重试、复制诊断 |
| `authorizing` | 正在用 code 换 token | 顶部黄，进度条 | 取消（终止流程） |
| `authorized` | 拿到 token 但还没有写入任何 Adapter | 顶部黄 | 选择应用注入 |
| `injected_proxy_down` | 已写入配置但代理未就绪（多见于工具重启后） | 顶部黄 | 启动代理 |
| `running` | 至少一个 Adapter 健康 | 顶部绿 | 日常使用 |
| `degraded` | 部分 Adapter 异常 | 顶部黄 | 修复异常项 |
| `error_backend_unreachable` | 后端连续 N 次不可达 | 顶部红 | 重试、切换 origin、查看诊断 |
| `error_token_expired` | token 校验 401 | 顶部红 | 重新授权（拉起 `/codex` 或扫码） |
| `error_device_revoked` | 后端返回 device_revoked | 顶部红 | 重新接入（必须再走一次 code 兑换） |
| `error_proxy_port_lost` | 代理进程退出 / 端口不可达 | 顶部红 | 一键修复（重启代理 + 回写配置） |
| `error_config_write_failed` | 配置文件写入失败（权限 / 被占用） | 顶部红 | 提示退出目标应用后重试 |
| `error_asar_patch_failed` | Codex 增强项修补失败 | 顶部红 | 退出 Codex 后重试 / 恢复备份 |
| `error_restore_failed` | 退出接入恢复配置或 app.asar 失败 | 顶部红 | 查看诊断 / 重试恢复 / 手动恢复指引 |
| `restoring` | 用户点击退出接入 | 顶部灰 | 等待完成 |
| `uninstalled` | 已彻底退出接入 | 顶部灰 | 重新接入入口 |

### 9.2 Adapter 本地状态
| 状态 | 含义 |
|------|------|
| `not_installed` | 目标应用未在本机检测到 |
| `installed` | 已检测到，未注入 |
| `config_pending` | 准备写配置，未完成 |
| `config_written` | 配置写入成功，等代理 |
| `proxy_starting` | 本机代理正在启动 |
| `port_conflict` | 计划端口被占用，需切换 |
| `bound` | 配置 + 代理端口已绑定 |
| `health_ok` | 健康检查全通过 |
| `health_warn` | 部分检查失败但可恢复 |
| `health_fail` | 关键检查失败 |
| `restart_required` | 配置变更后需要重启目标应用 |
| `app_running_blocking_change` | 目标应用正在运行导致写不进配置 |
| `restoring` | 正在还原备份 |
| `restored` | 已恢复成原配置 |

### 9.3 状态合成规则
- 全局状态 = 取严重度最高（`max severity`）：
  - 任一 Adapter 处于红色态 → 全局红。
  - 否则任一 Adapter 处于黄色态 → 全局黄。
  - 全部 Adapter 绿色 + 全局会话非异常 → 全局绿。
  - 全局会话本身的红/黄态（如后端不可达）单独参与合成，不依赖具体 Adapter。
- 顶部状态条以"全局状态"为准；每张应用卡片以"Adapter 本地状态"为准。
- 任何"红色态"必须给出至少一个可点击的"建议动作"。
- 用户主动操作期间（修复中、重启中），顶部条进入"忙碌态"灰底，禁止再次触发同名动作。

### 9.4 关键状态迁移
- `running → degraded`：单次健康检查失败立即降级（黄色），但只在用户已能感知的故障路径上触发；其它后台轻微抖动只记日志。
- `degraded → error_*`：同一类失败连续 2 次，或失败超 30 秒未恢复，升级为红色。
- `degraded → running`：连续 2 次健康检查通过。
- `error_token_expired`：仅由后端 401 触发，本地不私自判断 token 时间过期。
- `restart_required`：仅在 Adapter 显式声明此次写入需要重启时进入；UI 提供"立即重启 Codex" 按钮。
- `error_*` 之间互不直接转移，必须先回到 `degraded` 或 `running` 再触发新的错误，避免红色态之间反复闪烁。

---

## 10. 错误与诊断

### 10.1 统一错误模型
```
ZhumengAgentError {
  code:        // 机器可读，例如 PROXY_PORT_LOST
  severity:    // info / warn / error / critical
  surface:     // adapter / engine / backend / os
  user_title:  // 一句话给用户
  user_hint:   // 给用户的下一步建议
  raw:         // 原始堆栈，仅诊断报告中可见
  fingerprint: // 用于聚合与遥测（脱敏哈希）
}
```

### 10.2 必须翻译的常见错误
| 现象 | 用户文案 | 用户动作 |
|------|----------|----------|
| `error sending request for url (http://127.0.0.1:50793/v1/responses)` | "本机代理端口 50793 已离线，Codex 仍在使用旧端口。" | `[一键修复]` 重启代理并回写配置 |
| `device_heartbeat 设备已离线` | "设备心跳超时，可能是后端没收到请求或网络异常。" | `[查看诊断]` `[重新授权]` |
| `401 token expired` | "授权已过期，需要重新授权。" | `[重新授权]` |
| `device_revoked` | "该设备已在后台被撤销。" | `[重新接入]` |
| `ECONNREFUSED 18081` | "无法连接到逐梦后端。" | `[重试]` `[切换网络]` `[查看诊断]` |
| 写 `config.toml` 失败 | "无法写入 ~/.codex/config.toml，可能是 Codex 仍在运行。" | `[退出 Codex 后重试]` |
| 模型目录拉取失败 | "无法获取模型目录，连接已建立但目录同步失败。" | `[重试]` `[查看诊断]` |
| 端口冲突 | "端口 50793 被其他程序占用，已切换到 51022。" | `[继续]`（自动） |
| Codex 未安装 | "未检测到 Codex App。" | `[官方下载]` `[手动选择路径]` |
| Codex 版本过低 | "当前 Codex 版本可能不兼容。" | `[查看支持版本]` `[继续接入]` |
| `plugin-auth-gate` 未修补 | "Codex 插件导航仍被禁用，需要修补 Plugin Auth Gate。" | `[退出 Codex 后修补]` |
| `plugin-mention-marketplace` 未修补 | "插件提及或插件市场入口不可用，需要修补 Plugin Mention Marketplace。" | `[退出 Codex 后修补]` |
| `app.asar` patch point 不唯一 | "当前 Codex 版本暂不支持自动修补，已跳过以避免破坏应用。" | `[查看诊断]` `[等待适配]` |
| `app.asar` 恢复失败 | "退出接入未完成：Codex 增强项备份恢复失败。" | `[重试恢复]` `[查看备份路径]` `[复制诊断]` |

### 10.3 一键修复策略
- "修复全部"按预设顺序逐项**先诊断再处理**：
  1. 探活后端：`/codex/v1/models` 200。失败 → 不触发后续步骤，提示后端不可达。
  2. 探活代理：监听端口可达。失败 → 重启代理，绑定新端口。
  3. 校验 token：未过期、未撤销。失败 → 提示用户重新授权（不自动续期）。
  4. 校验目标应用配置：`base_url` 端口与代理一致；缺失字段则回写。
  5. 校验 Codex 增强项：检查并修复 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace`；若 Codex 正在运行，提示用户退出 Codex 后继续（**不自动 kill**）。
  6. 校验目标应用进程：必要时提示用户重启目标应用（**不自动 kill**）。
- 每一步执行后立即重新跑一次端到端 loopback；连续两次通过才结束。
- 修复过程实时展示进度日志，任一步失败给出明确动作（不静默吞错）。
- **不在用户没有同意的情况下重启或退出目标应用进程**。
- 频率限制：单次修复 60 秒内，"修复全部"按钮不可重复点击；防止用户反复点击放大问题。

### 10.4 诊断报告
- 内容：版本、平台、Adapter 状态、端口、最近事件、最近 N 行日志、配置文件 hash（不含内容）。
- 自动脱敏：token、device_token、邮箱中段、本机用户名、机器名 → 替换为占位符。
- 输出：纯文本（Markdown）+ 文件名 `zhumeng-agent-diag-YYYYMMDD-HHmm.txt`。
- 行为：默认本地保存 + 复制；只有用户点"上传到逐梦支持"才上传。

---

## 11. 安全与隐私

1. **Token 与凭据**
   - 任何完整 token 不出现在 UI（仅前 4 + 后 4，中间 `…`）。
   - 凭据存储优先用平台密钥环（macOS Keychain / Windows Credential Manager）；状态文件只放非敏感 metadata。
   - 进程内禁止把完整 token 写入日志，CI 测试加 lint 校验。
   - **本机代理与目标应用之间是 HTTP 明文** `127.0.0.1:<dyn>`，仅监听回环口；不暴露公网。任何 `0.0.0.0` 监听是 bug。
   - 自定义供应商凭据（v2+）只能 `keychain://` 引用形式存放在 `state.json`，**禁止**明文写入。

2. **日志**
   - 默认仅本地保存，按大小轮转。
   - "诊断报告"自动脱敏；"上传"为显式动作。
   - 高级"裸协议捕获"仅在解锁环境变量 `ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK=...` 后允许；UI 上单独警示。

3. **写入安全**
   - 写入目标应用配置前必先备份；备份文件命名带时间戳，限制份数。
   - 任意写入失败即回滚到最近一次成功状态。
   - Windows 下避免请求管理员权限；若必须，明确提示原因。

4. **退出接入**
   - 默认动作：恢复 `~/.codex/config.toml`、`~/.codex/auth.json`、`~/.codex/zhumeng-codex-models.json` 备份；若某个文件接入前不存在（例如 `zhumeng-codex-models.json` 由本工具首次创建），退出接入时删除该文件而不是伪造备份。
   - 若本工具修补过 `app.asar`，恢复 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace` 对应备份；若修补过程触发过重签名，退出接入后必须校验恢复后的 `app.asar` hash 与 Codex.app 签名状态。
   - 删除 Adapter 写入的本工具私有文件 → 停止代理 → 清状态文件。
   - `app.asar` 或配置恢复失败时进入 `error_restore_failed`，保留诊断报告和备份路径，不把 UI 显示为"退出成功"。
   - 可选动作：调用后端 `logout --revoke-device` 撤销设备绑定。
   - 用户须二次确认；操作不可撤销时显式说明。

5. **更新与签名**
   - 内测阶段可使用未签名包，但必须在下载页标注安装摩擦与风险（Gatekeeper 强提示、需要右键打开或系统设置允许）。
   - 正式官网发布必须附 SHA256；macOS 必须完成 `Developer ID Application` 签名 + Apple Notarization 公证；若提供 PKG 安装器，必须使用 `Developer ID Installer` 签名；Windows 至少 EV/OV 代码签名。
   - 第二版才引入自动更新通道。

6. **遥测**
   - 默认关闭。开启后仅上报匿名使用计数与错误指纹（不含 raw 错误内容）。
   - 设置页提供完整可读条款链接与"撤回同意"按钮。

---

## 12. 平台策略

### 12.1 Mac 优先（第一版）
- **分发方式**：首版 macOS 不走 Mac App Store；采用官网下载 DMG / PKG 的直接分发方式。
- 深链：`Info.plist` 注册 `zhumeng-agent` URL Scheme（已具备骨架）。
- 路径：状态文件 `~/Library/Application Support/zhumeng-agent/state.json`；目标应用 `/Applications/Codex.app`。
- 检测：`mdfind` / `NSWorkspace` 启动 `Codex.app`；版本读取 `Info.plist`。
- 进程：用 `launchd` 用户级 LaunchAgent 管理"开机自启"和"代理守护"（第二版引入）。
- 正式发布签名：必须 `Developer ID Application` 签名 + Apple Notarization 公证；若提供安装器，则安装器必须使用 `Developer ID Installer` 签名。
- 发布页：标明版本、架构（Apple Silicon / Intel / Universal）、SHA256 校验值、发布日期、最低系统版本。
- 防火墙：仅监听 `127.0.0.1`，避免触发"接受传入连接"弹窗。

首版不走 Mac App Store 的原因：
- 工具需要写入 `~/.codex` 配置。
- 工具需要注册 `zhumeng-agent://` 深链。
- 工具需要运行本机代理。
- 工具可能修补 `Codex.app` 本地资源（`app.asar`）以启用模型选择与插件市场。
- 这些能力与 Mac App Store 沙盒、审核路径不匹配，首版不应为了上架而牺牲工具可用性。

### 12.1.1 macOS 分发阶段

| 阶段 | 分发方式 | 签名 / 公证 | 用户体验 | 风险与备注 |
|------|----------|-------------|----------|------------|
| 内测 | 内部链接 / 手动传包 | 未签名或临时签名 | 需要右键打开或系统设置允许 | Gatekeeper 强提示；仅限内部可信用户 |
| 小范围灰度 | 官网隐藏下载 / 灰度链接 | Developer ID 签名，尽量公证 | 正常打开概率高 | 若未公证仍可能提示无法验证 |
| 正式官网发布 | 官网下载 DMG / PKG | Developer ID Application + Notarization；安装器用 Developer ID Installer | 标准第三方 macOS App 安装体验 | 附 SHA256；发布页列版本、架构、校验值 |
| Mac App Store | 首版不做 | 需沙盒与审核 | 体验取决于审核 | 除非商业上必须，后续再评估 |

`app.asar` 修补可能影响 Codex.app 原签名。工具必须谨慎说明、保留备份、提供一键恢复，并在版本不兼容时跳过修补。

### 12.2 Windows 跟进（第三版）
- 深链：注册表 `HKEY_CURRENT_USER\Software\Classes\zhumeng-agent`（已具备骨架）。
- 路径：状态文件 `%LocalAppData%\ZhumengAgent\state.json`；目标应用 `%LocalAppData%\Programs\codex` 或安装目录。
- 安装器：MSI/MSIX 任选，建议 MSIX（Win10+，权限友好）；提供独立 EXE 备选。
- 托盘：使用系统通知区图标，注意"溢出区"问题。
- 权限：默认非管理员安装；写注册表用 HKCU。
- 杀软误报：申请代码签名证书；建议加入 Microsoft SmartScreen 信誉库；首次发布版本提交主流杀软白名单。
- 防火墙：仅 `127.0.0.1` 监听，避免弹窗；如必须监听公网（不应该），需要明确说明。

### 12.3 平台通用
- 深链协议名固定 `zhumeng-agent`，子动作通过 `host` + query 区分：`zhumeng-agent://setup?...`、`zhumeng-agent://reauth?...`、`zhumeng-agent://open?app=codex`。
- 多实例策略：同一时间仅允许一个工具实例；新实例若检测到旧实例运行，转交深链参数给旧实例后退出。
- **深链安全**：
  - `code` 在 `setup` 中是一次性、短时效；工具收到后立即 `POST` 兑换并失效。
  - `server` 必须在内置可信域名白名单内（含私有部署允许列表，由 §17 待确认）。陌生 origin 默认弹"未知服务器，是否信任？"对话框，并把 origin 完整展示。
  - 深链不携带任何 token / device_token；仅传 `code` + `server` + `client` + 可选 `nonce`。
  - `nonce` 用于将网页端会话与本机工具会话绑定，防止已有 code 被另一个浏览器窗口冒领。

---

## 13. 技术选型

### 13.1 备选方案对比

| 维度 | Tauri 2 | Electron | 原生 (SwiftUI + WinUI) |
|------|---------|----------|------------------------|
| 体积 | 6~15 MB | 80~150 MB | 5~30 MB |
| 内存 | 低 | 高 | 低 |
| 跨平台开发成本 | 低（Web UI 一套） | 低 | 高（两套代码） |
| 与 Python 后端集成 | 好（sidecar 子进程） | 好（spawn） | 一般（IPC） |
| 系统能力（深链、托盘、Keychain、注册表） | 充足，且通过 plugin 扩展 | 充足，社区生态丰富 | 最强 |
| 动效与设计自由度 | 高（Web） | 高（Web） | 中（按平台） |
| 代码签名 / 公证流程 | 成熟 | 成熟 | 原生最顺 |
| 长期维护 | 活跃，Rust 生态稳健 | 活跃但庞大 | 投入双倍 |

### 13.2 推荐：**Tauri 2 + 现有 Python CLI**

- 前端：Tauri + SvelteKit / Vue 3 / React 任选；本设计推荐 **React + Vite + Tailwind**（团队上手成本最低，组件生态丰富，能快速实现"系统设置面板"风格）。
- 后端：保留 `tools/zhumeng-agent` Python 包，通过 sidecar/子进程方式让 Tauri 调起；Tauri 侧仅做 IPC 桥接、状态管理、托盘、深链。
- IPC：Tauri Command（Rust）+ JSON 协议；Python 子进程暴露 stdio JSON-RPC 或本地 socket（已有 proxy 经验，复用）。
- 状态：Tauri Store + 内存订阅；前端使用 Zustand / Jotai 之类轻量状态库。
- 测试：Tauri E2E（WebDriver） + Python pytest 现有体系；UI 单元测试用 Vitest + React Testing Library。

**为什么不是 Electron**：体积、内存、首启时间不符合"系统工具"调性；签名流程没明显优势。

**为什么不是双原生**：维护成本翻倍，第一阶段没有平台独有功能足以抵消代价。

**澄清**：推荐路线不是苹果专用 SwiftUI，也不是 Windows 专用 WinUI。SwiftUI + WinUI 仅作为对比方案；首版推荐明确为 **Tauri 2 + React/Vite/Tailwind + 现有 Python CLI sidecar**。

### 13.3 内部架构（高层）

```
+--------------------------------------------------+
|                   Tauri Shell                    |
|  - 托盘 / 主窗口 / 深链路由 / 自启 / 通知        |
|  - Keychain / 注册表 / 文件系统权限边界          |
+----------------------+---------------------------+
                       | Tauri IPC (JSON)
+----------------------v---------------------------+
|              Front-end UI (React)                |
|  - 视图层 / 状态机 / i18n / 主题                 |
+----------------------+---------------------------+
                       | Local IPC (stdio/socket)
+----------------------v---------------------------+
|         Injection Engine (Python sidecar)        |
|  - AdapterRegistry / Orchestrator / Health       |
|  - Proxy / Doctor / Logout / Repair              |
+--------+-------------+--------------+------------+
         |             |              |
+--------v---+ +-------v------+ +-----v---------+
| CodexAdapt | | ClaudeAdapt  | | CustomAdapter |
+------------+ +--------------+ +---------------+
                       |
                  Backend API
              (http://...18081 / prod)
```

### 13.4 关键技术约束
- 不在前端持有完整 token；所有敏感数据由 Rust 层经 Keychain/Credential Manager 中转。
- 所有"会修改用户磁盘"的动作必须经过 Adapter 抽象，不允许前端直接写文件。
- Python sidecar 为"可换的"：本设计允许未来用 Rust 重写部分性能/稳定性敏感路径而不动 UI。

---

## 14. 第一版范围

### 14.1 必须做
1. macOS DMG 可安装版本（签名 + 公证）。
2. 注册并响应 `zhumeng-agent://setup`、`zhumeng-agent://reauth`、`zhumeng-agent://open?app=codex`。
3. 内置 `CodexAdapter`：完成授权、写配置、启动代理、健康检查、修复、退出接入闭环。
4. Codex 增强项主流程：`model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace` 检测 / 修补 / 备份 / 恢复，并在成功页展示插件市场状态。
5. 主窗口：概览、已接入应用、应用目录（仅 Codex 可点亮，其余占位）、诊断与日志、设置、关于。
6. 模型目录：固定高度滚动、表头固定、搜索筛选、价格悬浮、数据库单价来源说明。
7. 托盘：连接状态、打开主窗口、打开 Codex、重启代理、修复全部、退出。
8. 错误翻译：覆盖第 10.2 表格列出的全部场景。
9. 一键脱敏诊断报告（本地复制 / 另存为）。
10. 退出接入并恢复原配置与 `app.asar` 增强项备份（含可选撤销后端设备）。
11. 浅色 / 深色双主题；跟随系统 + 手动切换。
12. 多语言基础架构（首发简体中文，预留 en、zh-Hant）。

### 14.2 不做
- 自动更新通道。
- Windows 完整版（深链与状态机要在架构上预留，但第一版不发包）。
- `Claude Desktop` Adapter。
- 自定义 Adapter UI。
- 云端日志聚合 / 多租户企业管理。
- 逐梦自建复杂插件市场。Codex App 自带插件市场的可用化属于第一版必做。
- 模型管理 / Prompt 管理 / 计费看板（继续由 Web 后台承担）。
- 自定义供应商完整接入（第一版只预留入口和数据结构）。

### 14.3 第一版验收清单
连通性
- 从 `/codex` 创建凭证 → 点击"打开本机客户端" → Codex App 在 30 秒内可用。
- 杀掉本机代理后，托盘 5 秒内变黄，10 秒内变红；点击"一键修复"恢复。
- 后端断网，工具不崩溃，状态进入 `error_backend_unreachable` 并给出操作。

授权
- token 主动过期后，Codex 报 401，工具状态变红并提供"重新授权"。
- 后台撤销设备后，工具进入 `error_device_revoked`，引导重新接入。

多供应商门禁
- 后端返回 `capabilities` 不满足 Codex 必须能力的任意示例模型，默认**不**出现在 Codex 主模型列表。
- 设置开启"宽松模式"后，该类模型在主列表出现并带 `聊天可用，工具能力有限` 角标，且首次选择有一次确认。
- 模型目录价格 tooltip 展示输入、输出、缓存命中、缓存写入、单位、来源、更新时间；缺失价格显示 `未配置`。

Codex 插件市场
- 接入后重启 Codex App，插件市场按钮不再灰色。
- 能打开本地插件市场页面。
- 内置插件目录可见。
- `plugin-auth-gate` 与 `plugin-mention-marketplace` 状态健康检查通过。
- 退出接入后 Codex 插件修补可恢复；恢复失败进入 `error_restore_failed`，不显示退出成功。

恢复与隔离
- 退出接入后，`~/.codex/config.toml`、`auth.json`、`zhumeng-codex-models.json` 均可恢复至接入前 hash 一致。
- 同一时间仅一个工具实例；第二次启动只把深链参数转交给第一个实例。

隐私
- 诊断报告中不含完整 token、邮箱、机器名。
- 进程日志中不出现完整 token；CI 校验通过。
- 本机代理仅监听 `127.0.0.1`。

---

## 15. 后续路线

### 15.1 第二阶段（macOS 稳定 + 体验补齐）
- LaunchAgent 守护代理；崩溃自恢复。
- 开机自启选项默认开启（可关闭）。
- 授权过期前 24 小时主动通知。
- 完整事件流持久化与历史回看。
- 自动更新通道（differential update）。
- i18n 真正落地：英文、繁中。

### 15.2 第三阶段（Windows 全量）
- MSIX 安装器、注册表深链、托盘、防火墙白名单脚本。
- 适配 Windows 路径与权限差异。
- 杀软白名单与签名信誉积累。
- Windows 与 macOS 双向特性对齐。

### 15.3 第四阶段（多目标全面铺开）
- `ClaudeDesktopAdapter`（含 deeplink、配置写入、模型映射）。
- `ContinueAdapter`（VS Code 扩展配置注入，作为示范桌面外目标）。
- `CustomAppAdapter` UI：用户在工具内自定义注入项。
- Adapter 远程清单（签名）+ 内置市场视图。

### 15.4 第五阶段（企业能力）
- 设备级策略（强制脱敏强度、强制 origin 锁定、禁用裸捕获）。
- 与企业版 Web 后台同步审计事件。
- MDM / Intune 推送支持。

---

## 16. 视觉方向（执行级建议）

> 目标：让用户**第一眼就觉得"这是系统级工具"**，而不是"这是营销网页"。

- 主色：使用逐梦既有品牌色（具体色值由品牌组确认）作为**强调色**，不作为大面积底色。
- 中性色：以中性灰阶为主，营造"控制台 / 系统设置"质感。
- 状态色：绿/黄/红，仅用于状态点、徽章、按钮主操作；不要用于装饰性 hero。
- 排版：标题层级最多 3 级（页面标题、分区标题、行内标题）；正文 14px，辅助文本 12px。
- 图标：Lucide 风格线性图标，统一 16/20/24 三档；状态图标允许填充。
- 卡片：低饱和、弱阴影、1px 边框；hover 仅"轻微抬起 + 边框加深"。
- 动效：仅在状态变化、流程进度、托盘异常脉冲三处使用，时长 ≤ 240ms，缓动 `ease-out`。
- 插画 / 营销图：禁用。例外：空状态可使用极简单色线性插画，不超过 1 张。
- 字号 / 行高：14 / 22；标题 18 / 28；徽章 12 / 16。
- 信息密度：正常密度；用户可在设置里切"紧凑模式"（行距 -2）。

---

## 17. 待确认问题

1. **品牌主色**：是否已有品牌色规范？需要色值与可用范围。
2. **应用名 / 安装路径**：`Zhumeng Agent` 还是 `逐梦注入工具`？Mac 显示名、Dock 名、二进制名需要统一。
3. **Mac 签名身份**：是否已有 `Developer ID` 团队？Apple ID + AppleTeamID 是否可用？
4. **Windows 签名**：是否已采购代码签名证书？EV 还是 OV？
5. **后端 origin 策略**：是否允许多 origin（私有部署 / 公有云）？工具是否需要内置可信 origin 白名单？
6. **设备指纹算法**：是否沿用现有后端实现？工具侧只需展示脱敏 ID 即可。
7. **token 续期协议**：是否采用刷新 token + 自动续期？工具是否需要承担续期触发？
8. **模型目录刷新策略**：是工具拉取还是后端推送？是否每次启动都拉取一次？
9. **撤销设备的副作用**：调用 `logout --revoke-device` 是否会立即影响其它设备登录？需要确认。
10. **裸协议捕获**：是否在 GA 版本默认隐藏入口？或仅"内部模式"显示？
11. **遥测合规**：是否需要在中国大陆地区显式适配《个人信息保护法》同意流程？
12. **多目标 Adapter 注册清单**：是否会做远程更新？如果是，谁签名、谁负责审核？
13. **后端 capabilities 字段**：`/codex/v1/models` 何时返回每条模型的 `capabilities`？v1 主模型门禁依赖此字段，缺失需要降级策略（如默认按 zhumeng provider 能力填）。
14. **`custom_local` 能力分类（v4）**：本机 LLM 默认走"严格隐藏"还是新增 `ChatOnlyAdapter` 兜底；本设计当前选 A（严格隐藏 + 目录可见），需确认。
15. **多目标应用同时接入的 UX**：第二阶段允许同时有 Codex + Claude，"打开最近接入的应用"的"最近"如何定义（按上次活跃 / 上次接入 / 用户置顶）。
16. **配置写入失败的提权策略**：macOS 不应需要 sudo；若用户 `~/.codex` 路径有特殊 ACL，是否允许工具尝试 `chmod` 而不提权？还是直接放弃。
17. **目标应用进程检测**：仅检查 macOS `Codex.app` 主进程，还是包含 helper / Renderer？检测错误会误报 `app_running_blocking_change`。
18. **多 Adapter 共用本机代理还是各起各的**：当前设计共用单一代理（按 `model` 路由）。是否需要按 Adapter 隔离监听端口（更安全但更重）？

---

## 附录 A：核心信息流（拓扑）

```
[ 浏览器 /codex ]
        |
   生成 code
        |
   zhumeng-agent://setup?code=...&server=...&client=codex
        |
        v
[ 桌面工具 ] -- 拉起接入向导
        |
        | 1. code -> token (HTTPS to backend)
        | 2. detect target app (Adapter)
        | 3. backup + write config
        | 4. start local proxy 127.0.0.1:<dyn>
        | 5. bind proxy URL into target app config
        | 6. health check (loopback + /v1/models)
        v
[ 目标应用，例如 Codex App ]
        |
        v
http://127.0.0.1:<dyn>/v1
        |
        v
[ 本机代理 (proxy-serve) ]
        |
        v
http://127.0.0.1:18081/codex/v1   或   线上服务
        |
        v
[ 逐梦后端 ]
```

## 附录 B：状态色与文案建议（节选）

| 场景 | 颜色 | 文案 |
|------|------|------|
| 接入成功 | 绿 | "已连接 — Codex App" |
| 健康检查抖动 | 黄 | "检查中…刚刚一次失败" |
| 端口失联 | 红 | "本机代理已离线" |
| 授权过期 | 红 | "授权已过期，请重新授权" |
| 设备被撤销 | 红 | "该设备已在后台被撤销" |
| 后端不可达 | 红 | "无法连接到逐梦后端" |
| 目标应用需重启 | 黄 | "Codex 需要重启才能生效" |
| 退出接入完成 | 灰 | "已退出接入并恢复原配置" |

## 附录 C：与现有代码模块的映射

| UI / 行为 | 现有模块 |
|-----------|----------|
| 深链解析 | `zhumeng_agent.deeplink` |
| Adapter 抽象 | `zhumeng_agent.adapters.base` + `adapters/codex/*` |
| 注入与配置 | `adapters/codex/config_manager.py`、`adapters/codex/injection.py` |
| 启动目标应用 | `adapters/codex/launcher.py` |
| 模型选择修补 | `adapters/codex/model_picker.py` |
| 插件鉴权修补 | `adapters/codex/plugins.py` |
| 插件提及 / 市场入口修补 | 已由 `cli.py` 暴露 `codex plugin-mention-marketplace status/patch` 子命令，底层实现位于 `adapters/codex/model_picker.py`；后续可视情况拆到独立模块 |
| 协议捕获 | `adapters/codex/capture_*.py` |
| 本机代理 | `proxy/server.py`、`proxy/upstream.py` |
| 凭据 | `credentials.py`、`security.py` |
| 状态文件 | `state.py`、`platform_paths.py` |
| 诊断 | `doctor.py` |
| HTTP 客户端 | `http_client.py` |
| macOS Bundle | `macos_bundle.py` + `packaging/macos/Info.plist` |
| Windows 注册 | `packaging/windows/zhumeng-agent-protocol.reg` |

> 桌面壳层（Tauri）应该把上述模块作为一个 Python sidecar 调用，不要让 UI 直接读写这些文件。

---

## 附录 D：自检与质量复核（v1 草案）

本节记录设计稿自我复核时识别到的潜在缺陷与决议，避免后续实施踩坑。

1. **接入步骤次序**：原稿中"先写配置占位 → 再启动代理 → 回写端口"是两阶段写盘，存在中间崩溃留下错配置的风险。已改为"先启动代理拿到端口 → 一次性写入正确配置"，详见 §4.1。
2. **状态合成语义**：原稿写 `min(...)` 在严重度方向上语义不一致。已统一改为"取最高严重度"，并明确全局会话状态独立参与合成，详见 §9.3。
3. **状态机错误转移**：原稿未定义红色态之间的关系，存在"红 → 红"反复闪烁可能。已增加"必须先回到 degraded/running 才能进入新错误态"，详见 §9.4。
4. **`injected_proxy_down` 与 `error_proxy_port_lost`**：前者是冷启动尚未起代理（黄），后者是运行中代理意外退出（红）。两者需要明确区分以匹配真实事故场景，详见 §9.1。
5. **修复策略幂等性与频率**：增加 60 秒去抖与"诊断后再处理"两条约束，避免连点放大问题，详见 §10.3。
6. **配置写入提权**：去掉"以管理员身份重试"的引导，避免误导用户给 `~/.codex` 提权（不需要也不应该），详见 §4.2。
7. **目标应用强杀边界**：明确"任何场景都不未授权杀目标应用进程"，详见 §4.2 与 §10.3。
8. **深链安全**：补充 `code` 一次性 + `nonce` + `server` 白名单，避免恶意网页伪造深链拉起注入流程，详见 §12.3。
9. **多供应商主列表门禁的 v1 依赖**：明确指出 v1 主列表门禁依赖后端 `capabilities` 字段；若后端未提供，需要降级策略（默认按 zhumeng provider 能力），详见 §17.13。
10. **本机代理网络面**：固定监听 `127.0.0.1`，把"任何 0.0.0.0 监听是 bug"写成显式规则，详见 §11.1。
11. **凭据落盘形式**：自定义供应商凭据只能以 `keychain://` 引用形式存放，禁止明文，详见 §11.1。
12. **`auto_repair` 与"修复"的边界**：自动行为只做"轻动作"（重启代理、回写端口、刷新心跳）；"重新授权 / 重启目标应用 / 撤销设备"等"重动作"必须用户显式同意，详见 §10.3。
13. **同名状态歧义**：`restoring` 在全局与 Adapter 层都存在，UI 上明确区分（全局态显示在顶部条；Adapter 态显示在卡片）。本节记录是为避免实施侧把它们建模为同一字段。
14. **托盘"打开最近接入的应用"语义**：当存在多个 Adapter（v4+）时，"最近"按"上次成功调用 launch 的 Adapter"为准；v1 仅 Codex，无歧义，详见 §17.15。
15. **目标应用进程探测误报**：`Codex.app` 检测应基于主 bundle 进程，不应把 Renderer/Helper 计入"运行中"，否则会持续触发 `app_running_blocking_change`，详见 §17.17。
16. **`responses` 协议变形点**：协议变形（Responses ↔ ChatCompletions ↔ Messages）只发生在本机代理；UI 进程内存与日志永不持有用户 prompt 原文，详见 §6.5.7 与 §11。
17. **Codex 插件市场可用化不是"逐梦插件市场"**：已把"不做复杂插件市场"限定为不做逐梦自建市场；Codex App 自带插件市场可用化进入首版主流程，避免非目标与首版范围矛盾。
18. **模型价格来源**：价格字段与 tooltip 明确来自后端数据库模型单价 `database_model_pricing`，草图数字为展示字段形态的示例，实施时禁止前端硬编码。
19. **模型兼容性来源**：不按 DeepSeek / Claude / GPT 品牌硬编码兼容性；主列表门禁只读后端 `capabilities` 与 Adapter requirements。
20. **macOS 分发路线**：已明确首版官网 DMG/PKG 直发，不上架 Mac App Store；正式发布必须 Developer ID 签名 + Notarization + SHA256。
