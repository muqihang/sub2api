# 逐梦注入工具桌面版 Mac MVP 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 按任务逐项实现。本文是实现计划，不包含实现代码；进入实现阶段前必须先确认当前工作区已有改动归属，禁止回退他人改动。

**目标：** 在 macOS 上交付“逐梦注入工具”桌面版 MVP：首版只支持 Codex App + 逐梦托管模型 + Codex App 自带插件市场可用化，并为后续多目标 Adapter、多供应商模型目录和 Windows 版本预留接口。

**架构：** 以现有 `tools/zhumeng-agent` Python CLI 作为 sidecar 能力核心，新增面向桌面壳的稳定 JSON 接口；桌面壳使用 Tauri 2 调用 sidecar，通过深链接收 `/codex` 授权，完成配置注入、本机代理、Codex 增强项修补、模型目录展示、诊断和退出恢复。Codex 是首个 Adapter，不把产品写死为 Codex 专用工具。

**技术栈：** Python sidecar（现有 `zhumeng_agent`）、Tauri 2、React + Vite + Tailwind、Tauri IPC、macOS deep link `zhumeng-agent://`、macOS 托盘与单实例、官网 DMG/PKG 分发。

---

## 0. 首版范围边界

### 0.1 首版必须做

- macOS 可安装桌面 App。
- 支持 `zhumeng-agent://setup?...` 深链从网页 `/codex` 拉起；同时预留并实现首版必须的 `zhumeng-agent://reauth?...`、`zhumeng-agent://open?app=codex` 路由。
- 支持 Codex App Adapter：授权、写入 `~/.codex/config.toml`、`~/.codex/auth.json`、`~/.codex/zhumeng-codex-models.json`、启动/修复本机代理。
- 支持 Codex 增强项主流程：`model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace` 检测、修补、备份、恢复。
- 支持模型目录：完整列表、固定高度滚动、搜索、筛选、能力标识、价格 tooltip、价格缺失展示“未配置”。
- 支持状态总览、应用详情、接入向导、诊断报告、退出接入并恢复配置与本工具修补过的 `app.asar`。
- macOS 官网下载 / 内测包路线；正式官网发布前 Developer ID 签名 + Apple Notarization。

### 0.2 首版不做

- 不做 Windows 完整版。
- 不做 Claude Desktop 注入。
- 不做用户自定义供应商真正可用，只保留界面入口与数据结构占位。
- 不做自动更新。
- 不做逐梦自建复杂插件市场。
- 不上架 Mac App Store。
- 不做完整命令面板；如设计草图中出现命令面板入口，MVP 仅作为 v2 占位。
- 不做多租户企业管理、云端日志上传、远程诊断收集。

---

## 1. 推荐实现顺序与依赖关系

### 1.1 串行主线

1. **里程碑 0：实现前清点**：冻结现有 CLI/后端/配置写入能力与缺口，避免桌面壳建立在不稳定输出上。
2. **里程碑 1：Python CLI / sidecar 接口稳定化**：先给 Tauri 壳提供稳定 JSON API。
3. **里程碑 2：Codex 增强项主流程**：把插件市场可用化从“高级修复”提升为接入主流程。
4. **里程碑 3：模型目录与价格数据**：补齐 `/codex/v1/models`、本地 catalog 与桌面模型目录展示契约。
5. **里程碑 4：Tauri 2 Mac 桌面壳**：在 sidecar 契约稳定后实现 UI。
6. **里程碑 5：macOS 打包与分发**：内测可先用未签名包，正式发布前补签名、公证和校验。
7. **里程碑 6：验证与验收**：贯穿每个阶段，最后做端到端验收。

### 1.2 可并行工作

- 里程碑 1 的 Python sidecar JSON 契约设计，可以与里程碑 4 的 Tauri UI 组件静态开发并行；但 UI 接入真实 IPC 必须等 sidecar 契约落定。
- 里程碑 2 的 `app.asar` 安全修补测试，可以与里程碑 3 的后端 `capabilities/pricing` 输出并行。
- 里程碑 5 的签名、公证调研可以提前并行；但正式分发产物必须等里程碑 6 验收通过。

### 1.3 不可并行 / 必须串行

- `setup`、`repair`、`logout` 的恢复语义必须先确定，再接桌面 UI 的成功/失败状态。
- `app.asar` 修补与恢复必须先完成命令级测试，再进入 UI 一键修复和退出接入主流程。
- `/codex/v1/models` 的 `capabilities/pricing` 字段必须先稳定，再做 UI 兼容性门禁和价格 tooltip 的真实数据接入。

---

## 里程碑 0：实现前清点

### 目标

确认当前现有 CLI、代理、配置管理、诊断、模型目录和 Codex 增强项修补能力，列出桌面壳所需缺口，并把每个缺口映射到文件、函数和测试点。

### 当前已确认能力

| 能力 | 当前文件 / 函数 | 当前状态 | 说明 |
| --- | --- | --- | --- |
| 深链转 setup | `tools/zhumeng-agent/src/zhumeng_agent/cli.py:main`、`zhumeng_agent.deeplink` | 已有 | 单参数 `zhumeng-agent://setup?...` 会转为 `setup --client codex --code --server`。 |
| 授权交换 | `AgentHTTPClient.exchange_setup_grant` | 已有 | 调后端 `api/v1/codex/setup-grants/exchange`。 |
| Codex 配置写入 | `CodexConfigManager.plan_configure/apply_configure/repair` | 已有 | 写 `config.toml`、`auth.json`、模型 catalog。 |
| 本机代理 | `ensure_proxy_running`、`ManagedProxyServer` | 已有 | 动态端口，健康端点 `__zhumeng/health`，支持 token refresh 和模型目录周期同步。 |
| repair codex | `cli.py:repair` | 部分已有 | 当前会修复配置、启动代理，并调用 `patch_detected_codex_desktop()` 修补三项增强项。 |
| Codex 增强项状态 / 修补 | `adapters/codex/model_picker.py`、`cli.py` handlers | 部分已有 | 已有 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace` status/patch；前两者有 restore，第三者缺 restore 命令。 |
| doctor JSON | `doctor.codex_doctor_report` | 部分已有 | 已含插件、model-picker、plugin-auth-gate、plugin-mention-marketplace、capture；缺桌面主状态、代理、心跳、模型目录和备份摘要。 |
| logout | `cli.py:logout`、`restore_local_managed_config` | 不完整 | 恢复 `config.toml` 和 `auth.json`，但未完整恢复/删除 `zhumeng-codex-models.json`，未恢复本工具修补过的 `app.asar`。 |
| `/codex/v1/models` | `backend/internal/service/codex_gateway_service.go`、`codex_gateway_model_registry.go` | 部分已有 | 已支持 `catalog_format=codex_cli`，但模型结构未包含设计要求的通用 `capabilities` 与 `pricing`。 |
| state.json | `state.JsonStateStore`、`cli.py setup/repair` | 部分已有 | 有设备、token、端口、备份路径等；缺桌面 UI 需要的 Adapter 状态、增强项状态、catalog metadata、心跳和恢复状态。 |

### 缺口清单、涉及文件、函数与测试点

| 缺口 | 需要改 / 新增文件 | 需要新增 / 调整函数 | 测试点 |
| --- | --- | --- | --- |
| `setup` 返回桌面壳完整 JSON 状态不足 | 修改 `tools/zhumeng-agent/src/zhumeng_agent/cli.py`；建议新增 `tools/zhumeng-agent/src/zhumeng_agent/desktop.py` | 新增 `build_desktop_status()`、`desktop setup` 包装器；`setup` 可复用但不要破坏原输出 | `tests/test_cli_desktop.py::test_desktop_setup_returns_full_state_without_tokens` |
| `repair codex` 虽调用三项 patch，但未明确检查 Codex 正在运行时的主流程策略 | 修改 `cli.py`、`model_picker.py` 或新增 `adapters/codex/enhancements.py` | `repair_codex_desktop_enhancements(app_path, allow_patch_when_running=False)`；运行中返回 `restart_or_quit_required`，不静默强杀 | `test_repair_codex_reports_running_app_instead_of_force_kill` |
| `plugin-mention-marketplace` 缺 restore 命令 | 修改 `model_picker.py`、`cli.py` | 新增 `restore_latest_plugin_mention_marketplace_backup()`；`handle_codex_plugin_mention_marketplace` 增加 `restore` | `test_plugin_mention_marketplace_restore_command` |
| 深链范围缺 `reauth/open` | 修改 `deeplink.py`、`cli.py`；新增/修改 `desktop.py`；Tauri 侧 deep link handler | 支持解析 `setup`、`reauth`、`open?app=codex`；`desktop reauth` 进入重新授权流程，`desktop open --app codex` 打开目标应用 | `test_deeplink_reauth_and_open_routes`、桌面单实例 deep link 测试 |
| `doctor --json` 缺代理端口、心跳、模型目录、app.asar 备份状态 | 修改 `doctor.py`；可能新增 `desktop.py` 公共聚合函数 | `codex_desktop_health_report()`、`proxy_health_state()`、`catalog_health_state()`、`app_asar_backup_state()` | `test_doctor_json_includes_proxy_catalog_heartbeat_and_backup_state` |
| logout 未恢复模型 catalog 与 app.asar | 修改 `cli.py`、`config_manager.py`、`model_picker.py` | `restore_local_managed_config()` 增强：恢复/删除 catalog；调用三项增强项 restore；失败返回结构化错误 | `test_logout_restores_catalog_or_deletes_created_catalog`、`test_logout_restores_app_asar_patches` |
| logout 可能覆盖用户接入后的手动修改 | 修改 `config_manager.py`、`state.py`、`desktop.py` | 记录接入前 hash、工具写入后 hash、managed marker；退出前再次备份当前文件；当前 hash 与工具写入后 hash 不一致时返回 `restore_conflict` | `test_logout_detects_config_restore_conflict`、`test_catalog_delete_requires_managed_marker` |
| `/codex/v1/models` 缺 `capabilities/pricing` | 修改后端：`backend/internal/service/codex_gateway_types.go`、`codex_gateway_model_registry.go`、相关 tests | 增加 `CodexGatewayModelCapabilities`、`CodexGatewayModelPricing`；导出普通和 `codex_cli` catalog 均保留字段 | Go 单测：`TestCodexGatewayModelsIncludeCapabilitiesAndPricing` |
| 本地 catalog 结构缺 `origin/provider_id/capabilities/pricing` 透传 | 修改 `config_manager.py` | `_catalog_model()` 保留并规范化这些字段；缺失价格不补假值 | `test_build_model_catalog_preserves_capabilities_and_pricing` |
| 桌面诊断报告缺统一脱敏 | 建议新增 `tools/zhumeng-agent/src/zhumeng_agent/redaction.py` 或复用现有测试里的敏感检测 | `redact_diagnostic_report()`、`desktop diagnose --json` | `test_desktop_diagnose_redacts_token_email_machine_name` |
| 凭据存储暂不符合 Keychain 优先目标 | 修改 `state.py`、新增 `desktop_security.py` 或在 `desktop.py` 聚合安全检查；后续再接 Keychain | MVP 明确为有意识偏离：token 仍由 Python state 管理；补偿 state 权限 0600 检查、日志/诊断 token lint、UI 不展示 token；后续迁移 macOS Keychain | `test_state_file_permissions_are_private`、`test_diagnostics_and_logs_do_not_contain_tokens` |
| 桌面壳读取 state.json 不足 | 修改 `state.py` 或新增 `desktop_state.py` | 版本化 state schema：`schema_version`、`adapters.codex`、`proxy`、`model_catalog`、`enhancements`、`last_seen_at` | `test_desktop_status_handles_old_state_schema` |

### 验收标准

- 形成上述缺口的 issue / task 列表，且每个缺口有明确文件、函数、测试点。
- 不要求此阶段改实现代码；只允许补充计划或任务拆分。
- 确认现有 `plugin-mention-marketplace` 命令已经由 `cli.py` 暴露 status/patch，不重复造同名命令；只补 restore 和桌面聚合接口。

### 风险和回滚策略

- 风险：现有工作区已有大量未提交改动。实现前必须先确认这些改动归属，避免覆盖他人代码。
- 回滚：本阶段不改实现代码；若清点结论错误，只修正文档和任务列表。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent doctor --json
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent status --json
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent codex plugin-mention-marketplace status --app /Applications/Codex.app
```

预期：命令输出 JSON；若本机没有 Codex App，返回 `app_not_found` 或等价结构化状态，而不是 Python traceback。

---

## 里程碑 1：Python CLI / sidecar 接口稳定化

### 目标

给 Tauri 壳提供稳定、版本化、机器可读的 JSON 接口。桌面壳不解析人类文本，不直接读散落的配置文件，也不依赖非稳定字段。

### 推荐方案：新增 `desktop` 子命令，复用现有 CLI 能力

推荐新增命令族：

```bash
zhumeng-agent desktop status --json
zhumeng-agent desktop setup --client codex --code <code> --server <origin>
zhumeng-agent desktop reauth --client codex --code <code> --server <origin>
zhumeng-agent desktop open --app codex
zhumeng-agent desktop repair --client codex
zhumeng-agent desktop codex-enhancements status --app /Applications/Codex.app
zhumeng-agent desktop codex-enhancements patch --app /Applications/Codex.app --item all
zhumeng-agent desktop codex-enhancements restore --app /Applications/Codex.app --item all
zhumeng-agent desktop models sync --client codex
zhumeng-agent desktop logout --local-only
zhumeng-agent desktop logout --revoke-device
zhumeng-agent desktop diagnose --redacted --json
```

理由：

- 不破坏已有 `setup`、`repair codex`、`doctor --json`、`logout` 对脚本用户和测试的兼容性。
- 桌面壳需要更完整的状态聚合，而不是单一命令输出。
- 后续 `ClaudeDesktopAdapter`、`CustomAppAdapter` 可继续挂在 `desktop` 接口下，不让顶层 CLI 过度膨胀。
- `desktop` 子命令可以强制所有输出使用统一 envelope：`schema_version`、`ok`、`status`、`data`、`error`、`warnings`、`redactions`。

### 需要改哪些文件或新增哪些文件

- 修改：`tools/zhumeng-agent/src/zhumeng_agent/cli.py`
  - 注册 `desktop` 子命令并分发。
- 新增：`tools/zhumeng-agent/src/zhumeng_agent/desktop.py`
  - 桌面壳 JSON 聚合、状态机、命令处理。
- 新增：`tools/zhumeng-agent/src/zhumeng_agent/desktop_schema.py`
  - 输出 schema 常量、状态枚举、兼容旧 state 的读写逻辑。
- 可选新增：`tools/zhumeng-agent/src/zhumeng_agent/diagnostics.py`
  - 脱敏诊断报告生成。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/state.py`
  - 保持 `JsonStateStore` 兼容，必要时增加 schema migration helper。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
  - 把 doctor 与 desktop diagnose 共用底层聚合函数。
- 新增测试：`tools/zhumeng-agent/tests/test_desktop_cli.py`
- 新增测试：`tools/zhumeng-agent/tests/test_desktop_diagnostics.py`

### 关键实现步骤

1. 定义统一 JSON envelope：

   ```json
   {
     "schema_version": 1,
     "ok": true,
     "command": "desktop status",
     "status": "running",
     "data": {},
     "warnings": [],
     "error": null
   }
   ```

2. 定义桌面总状态结构：

   - `global_status`：`not_connected | authorizing | configured | running | degraded | error | logged_out`
   - `proxy`：`status`、`host`、`port`、`pid`、`health_url`、`runtime_signature_match`、`last_checked_at`
   - `backend`：`status`、`server_base_url`、`gateway_base_url`、`last_success_at`、`last_error_code`
   - `authorization`：`status`、`device_id`、`managed_session_id_redacted`、`expires_hint`，禁止返回 token 原文
   - `adapters.codex`：安装状态、配置状态、增强项状态、模型目录状态、需要重启状态
   - `model_catalog`：模型数量、主列表数量、受限数量、pricing 缺失数量、`updated_at`

3. `desktop status --json`：

   - 读取 `state.json`，兼容旧字段。
   - 检测代理端口是否监听，调用 `__zhumeng/health`。
   - 检查 Codex App 是否存在。
   - 读取 Codex 增强项 status，但不写盘。
   - 读取模型 catalog 摘要。
   - 返回脱敏结构。

4. `desktop setup`：

   - 复用现有 `setup` 的授权交换、配置写入、代理启动。
   - 不在此命令里静默修补正在运行的 Codex App。
   - 输出完整桌面状态，并把下一步 `codex_enhancements` 的状态放入 `data.adapters.codex.enhancements`。

5. `desktop repair`：

   - 复用 `repair codex` 的配置修复和代理启动。
   - 增强项修补遵守里程碑 2 的安全流程。
   - 若 Codex 正在运行，返回 `requires_quit_codex`，由 UI 提示用户退出后继续。

6. `desktop reauth` 与 `desktop open`：

   - `reauth` 复用 `setup` 的授权交换，但保留现有 Adapter、增强项和代理状态；完成后刷新 token、模型目录和配置。
   - `open --app codex` 只负责打开目标应用；若状态异常，返回 `requires_repair` 或 `not_configured`，不暗中修补。
   - `zhumeng-agent://reauth?...` 和 `zhumeng-agent://open?app=codex` 必须走同一个 Tauri 单实例 deep link 分发器。

7. `desktop diagnose --redacted --json`：

   - 汇总 doctor、state、proxy health、catalog、enhancements。
   - 自动脱敏：access token、refresh token、loopback secret、邮箱、机器名、用户目录、完整 session id。
   - 默认只复制到剪贴板 / 输出到 stdout，不自动上传。

8. 凭据存储的 MVP 偏离与补偿：

   - 设计目标是 macOS Keychain 优先；MVP 为降低 sidecar 改造面，允许 token 暂由 Python `state.json` 管理，这是有意识的阶段性偏离。
   - 补偿措施必须同步实现：`state.json` 权限校验为 0600、诊断/日志 token lint、UI 不展示完整 token、测试覆盖 stdout/stderr 不泄露 token。
   - 后续迁移到 macOS Keychain 时，`desktop_schema.py` 的状态结构保持不变，只替换凭据读取后端。

### 验收标准

- 所有 `desktop ... --json` 命令均输出合法 JSON，失败也输出 JSON，不输出 traceback 给桌面壳。
- JSON 中不包含完整 token、refresh token、loopback secret、邮箱、机器名、完整本机用户目录。
- `desktop reauth` 能完成重新授权并刷新状态；`desktop open --app codex` 能打开 Codex App，未配置时返回结构化错误。
- `state.json` 权限不宽于 0600；诊断报告、命令 stdout/stderr、桌面日志均不包含完整 token。
- 旧的 `zhumeng-agent setup/repair/logout/doctor` 仍可运行，原有测试不因新增 `desktop` 子命令失败。
- Tauri 壳只需要调用 `desktop` 命令族即可完成 MVP 主流程。

### 风险和回滚策略

- 风险：新增 `desktop` 命令导致 `argparse` 解析与现有 `codex` passthrough 冲突。
  - 回滚：保持 `desktop` 为独立顶层子命令，不触碰 `codex` remainder 解析。
- 风险：状态结构过大导致 UI 依赖不稳定。
  - 回滚：schema 加 `schema_version`，新增字段只追加不改名；UI 只依赖 documented fields。
- 风险：诊断脱敏误删关键信息。
  - 回滚：保留 `raw_available=false` 和字段级摘要，不提供原文；必要时用户手动复制特定日志。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests/test_desktop_cli.py -v
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests/test_cli.py -v
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop status --json | python -m json.tool
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop diagnose --redacted --json | python -m json.tool
```

---

## 里程碑 2：Codex 增强项主流程

### 目标

把 `model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace` 纳入首版 Codex 接入主流程，而不是只放在高级排障区。接入成功必须明确展示“插件市场已可用 / 重启后生效 / 修补失败 / 当前版本不支持”。

### 状态机

每个增强项统一输出：

| 状态 | 含义 | UI 操作 |
| --- | --- | --- |
| `not_found` | 未找到 Codex App 或 `app.asar` | 选择应用 / 查看说明 |
| `unsupported` | 当前 Codex 版本没有唯一补丁点或结构不兼容 | 查看诊断 / 等待适配 |
| `clean` | 未修补，但可安全修补 | 修补 |
| `patched` | 已修补且完整性校验通过 | 无需操作 |
| `restart_required` | 已修补，但正在运行的 Codex 需要重启后生效 | 提示退出并重启 Codex |
| `failed` | 检测、修补、签名、校验或写入失败 | 复制诊断 / 重试 / 恢复 |
| `restore_available` | 存在本工具创建的备份，可一键恢复 | 恢复增强项 |

聚合状态：

- `marketplace_available`：三项均 `patched` 且 Codex 已重启后可判定为可用。
- `marketplace_restart_required`：至少一项 `restart_required`。
- `marketplace_unsupported`：任一必需项 `unsupported`，不强行修补。
- `marketplace_failed`：任一项 `failed`。

### 需要改哪些文件或新增哪些文件

- 修改：`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/model_picker.py`
  - 保留现有实现；补齐 `plugin-mention-marketplace restore`。
  - 统一 status 输出字段：`status`、`integrity_ok`、`backup_available`、`restart_required`、`running_app_detected`、`app_asar_sha256`。
- 新增或修改：`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/enhancements.py`
  - 聚合三项增强项的 status/patch/restore 流程。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/cli.py`
  - `codex plugin-mention-marketplace restore`。
  - `desktop codex-enhancements status/patch/restore`。
  - `repair codex` 可继续保留，但内部应复用聚合函数。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/doctor.py`
  - 增强项 health 进入 doctor JSON。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/desktop.py`
  - 接入向导和成功页所需的增强项聚合结果。
- 新增/修改测试：`tools/zhumeng-agent/tests/test_codex_model_picker.py`、`tools/zhumeng-agent/tests/test_cli.py`、`tools/zhumeng-agent/tests/test_desktop_cli.py`

### 关键实现步骤

1. 增加增强项聚合模型：

   - `model_picker`
   - `plugin_auth_gate`
   - `plugin_mention_marketplace`
   - `app_asar`：路径、hash、plist hash 状态、签名状态、备份根目录、备份数量。

2. 修补前安全检查：

   - 检测 Codex App 路径，默认查 `/Applications` 和 `~/Applications`。
   - 检测 Codex 是否运行。主流程默认不静默强杀；如运行中，返回 `requires_quit_codex`，让 UI 显示“请退出 Codex 后继续”。
   - 检测 `Codex.app`、`app.asar`、`Info.plist` 可写性；默认不使用 `sudo`，不执行 `chmod` / `chown` 改权限。
   - 若应用包不可写，返回结构化错误 `app_bundle_not_writable`；UI 只给出安全建议（选择用户可写的 Codex App、手动授权、跳过增强项），不得假装修补成功。
   - 读取 `app.asar` hash 和 `Info.plist` header hash。
   - 检查唯一补丁点：表达式计数必须符合预期；不符合则 `unsupported`，不能写入。
   - 检查已修补状态完整性；若已修补但完整性损坏，进入 `failed` 或 `integrity_repair_available`。

3. 修补写入流程：

   - 创建备份，记录 label：`model-picker`、`plugin-auth-gate`、`plugin-mention-marketplace`。
   - 执行替换。
   - 更新 asar entry integrity。
   - 更新 `Info.plist` asar header hash。
   - 执行签名 / 重签名策略：内测可 ad-hoc `codesign --sign -`，正式包需明确提示这会影响 Codex.app 原签名；失败则回滚。
   - 校验签名。
   - 返回 `restart_required=true` 和 `running_app_detected`。

4. 恢复流程：

   - `restore all` 顺序建议与修补相反：`plugin-mention-marketplace → plugin-auth-gate → model-picker`。
   - 对每项查找本工具创建的最新备份。
   - 恢复后更新完整性和签名。
   - 任一失败时停止并返回 `error_restore_failed`，保留备份路径和恢复建议。

5. 退出接入联动：

   - `desktop logout` 必须先停止/确认代理退出，再恢复 `~/.codex` 文件和增强项备份。
   - 每次配置写入前记录原始 hash、写入后 hash、managed marker 和备份路径；退出前再次备份当前文件。
   - 若当前 `config.toml`、`auth.json` 或 catalog hash 与本工具最近一次写入 hash 不一致，返回 `restore_conflict`，要求用户确认覆盖或查看手动恢复建议。
   - `zhumeng-codex-models.json` 仅在“接入前不存在，且当前文件仍带本工具 managed marker / hash 匹配”时删除；否则进入冲突态。
   - 如果 `app.asar` 恢复失败，不删除 state.json，不显示退出成功；进入可诊断错误态。

### 验收标准

- `desktop codex-enhancements status` 能同时返回三项状态和备份状态。
- `desktop codex-enhancements patch --item all` 在 Codex 正在运行时不强杀，返回可读状态让 UI 提示用户退出。
- `Codex.app` 不可写时返回 `app_bundle_not_writable`，不使用 sudo、不改权限、不破坏应用。
- 修补后重启 Codex App，插件市场按钮不再灰色，并能打开本地插件市场页面。
- `plugin-mention-marketplace` 有 restore 命令，并通过单元测试。
- `desktop logout` 能恢复本工具修补过的三项增强项；失败进入错误态。

### 风险和回滚策略

- 风险：`app.asar` patch point 不唯一，误改 Codex 资源。
  - 回滚：严格唯一补丁点校验；不唯一则 `unsupported`，不写盘。
- 风险：签名失败导致 Codex App 无法打开。
  - 回滚：`write_archive_with_rollback` 保持原子回滚；UI 提供“一键恢复增强项”。
- 风险：不同 Codex 版本结构变化。
  - 回滚：版本不兼容时跳过增强项修补，基础代理和模型注入仍可继续，但成功页标注“当前版本不支持插件市场增强”。
- 风险：`/Applications/Codex.app` 权限不足。
  - 回滚：不提权、不改权限；返回 `app_bundle_not_writable`，让用户选择跳过增强项或手动处理应用位置/权限。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests/test_codex_model_picker.py -v
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests/test_cli.py -k "model_picker or plugin_auth_gate or plugin_mention_marketplace" -v
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop codex-enhancements status --app /Applications/Codex.app | python -m json.tool
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop codex-enhancements patch --app /Applications/Codex.app --item all | python -m json.tool
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop codex-enhancements restore --app /Applications/Codex.app --item all | python -m json.tool
```

---

## 里程碑 3：模型目录与价格数据

### 目标

让后端 `/codex/v1/models`、本地 `~/.codex/zhumeng-codex-models.json` 和桌面 UI 共享同一套模型目录契约：模型能力由后端返回，价格来自后端数据库模型单价，前端不硬编码兼容性或真实价格。

### 需要改哪些文件或新增哪些文件

#### 后端改动

- 修改：`backend/internal/service/codex_gateway_types.go`
  - `CodexGatewayModel` 增加 `Origin`、`ProviderID`、`Capabilities`、`Pricing`。
  - `CodexGatewayCodexCLIModel` 也保留这些字段，供本地 catalog 和桌面 UI 使用。
- 修改：`backend/internal/service/codex_gateway_model_registry.go`
  - `ModelsResponse()` 和 `ExportCodexCLICatalogJSON()` 输出 `capabilities/pricing`。
  - 从数据库定价服务或已有 pricing resolver 读取模型单价；缺失时字段为空或单项为 null，不臆造。
- 修改：`backend/internal/service/codex_gateway_model_registry_test.go`
- 可能修改：`backend/internal/service/codex_gateway_service_test.go`

#### 本机工具改动

- 修改：`tools/zhumeng-agent/src/zhumeng_agent/adapters/codex/config_manager.py`
  - `_catalog_model()` 透传并规范化 `origin`、`provider_id`、`capabilities`、`pricing`。
  - `build_model_catalog()` 不因 Codex CLI 字段转换丢失桌面 UI 字段。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/http_client.py`
  - 如有需要，为桌面模型目录新增 `catalog_format=desktop`；MVP 可先复用 `codex_cli`，但必须保留 `capabilities/pricing`。
- 修改：`tools/zhumeng-agent/src/zhumeng_agent/cli.py` 或 `desktop.py`
  - `desktop models sync/status`。
- 新增/修改测试：`tools/zhumeng-agent/tests/test_codex_config_manager.py`、`tools/zhumeng-agent/tests/test_http_client.py`、`tools/zhumeng-agent/tests/test_desktop_cli.py`

#### 桌面 UI 改动

- 新增：`tools/zhumeng-agent/desktop/src/pages/ModelCatalogPage.tsx` 或等价组件。
- 新增：`tools/zhumeng-agent/desktop/src/components/ModelPriceTooltip.tsx`。
- 新增：`tools/zhumeng-agent/desktop/src/lib/modelCatalog.ts`。

### 数据结构要求

`pricing` 至少预留：

```json
{
  "input_price": "1.25",
  "output_price": "10.00",
  "cached_input_price": "0.125",
  "cache_write_price": "1.25",
  "currency": "USD",
  "unit": "per_1m_tokens",
  "updated_at": "2026-05-21T00:00:00Z",
  "source": "database_model_pricing"
}
```

如后端已有字段名为 `cache_read_price` / `cache_creation_price`，实现时应在后端输出层明确映射到设计字段，或在计划修订时统一命名，避免 UI 同时支持多套字段名。

`capabilities` 至少预留：

```json
{
  "responses": true,
  "streaming": true,
  "tool_calls": true,
  "image_input": false,
  "cache_pricing": true,
  "context_continuation": true
}
```

兼容性门禁：

- Codex App 主模型列表只读后端 `capabilities` 和 Adapter requirements。
- 不按 GPT / DeepSeek / Claude 品牌硬编码兼容性。
- 不满足 Codex Agent 能力的模型不默认进入 Codex App 主模型列表，或标注“聊天可用，工具能力有限”。MVP 推荐严格模式：不进入主列表，只在桌面模型目录展示原因。

### 关键实现步骤

1. 后端先定义 `capabilities/pricing` 输出契约和测试样例。
2. 后端 `/codex/v1/models?catalog_format=codex_cli` 输出保留 Codex CLI 必需字段，同时附加桌面 UI 字段。
3. 本机工具 `fetch_codex_model_catalog()` 同步模型目录时保留新增字段。
4. `~/.codex/zhumeng-codex-models.json` 扩展字段，但不破坏 Codex CLI 对既有字段的读取。
5. `desktop models status` 返回：模型总数、主列表数量、受限数量、不兼容数量、价格缺失数量、最后同步时间、同步来源。
6. 桌面 UI 模型目录：
   - 固定高度滚动容器；表头 sticky。
   - 搜索：模型名。
   - 筛选：供应商、能力、是否进入 Codex 主列表、可用/受限/不兼容、价格是否配置。
   - 分组：逐梦托管、自定义云端占位、自定义本机占位、未来远程签名 Provider 清单占位。
   - 价格列 tooltip 显示输入、输出、缓存命中、缓存写入、单位、来源、更新时间；缺失字段显示“未配置”。

### 验收标准

- `/codex/v1/models` 返回每条模型的 `capabilities`，且字段由后端模型目录/真实能力生成。
- `/codex/v1/models` 返回每条模型的 `pricing` 或明确缺失；价格来源为数据库模型单价，不由前端硬编码。
- 本地 `zhumeng-codex-models.json` 保留 `capabilities/pricing`。
- 桌面模型目录展示 GPT / DeepSeek / Claude 示例来源均为后端数据；UI 不写死 DeepSeek 或 Claude 兼容性结论。
- 价格 tooltip 能展示四类价格；缺失显示“未配置”。

### 风险和回滚策略

- 风险：后端定价字段来源不统一。
  - 回滚：先输出 `pricing: null` 或单项 `null`，UI 显示“未配置”；不得填假价格。
- 风险：Codex CLI 读取 catalog 因新增字段异常。
  - 回滚：新增字段只追加，不替换已有字段；保留现有 Codex CLI 字段。
- 风险：能力字段不准确导致主列表误展示。
  - 回滚：MVP 默认严格模式；不确定能力的模型不进入主列表。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
go test ./backend/internal/service -run 'CodexGateway.*Model|ModelRegistry' -count=1
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests/test_codex_config_manager.py -k "catalog or pricing or capabilities" -v
PYTHONPATH=tools/zhumeng-agent/src python -m zhumeng_agent desktop models sync --client codex | python -m json.tool
python - <<'PY'
import json, pathlib
p = pathlib.Path.home()/'.codex'/'zhumeng-codex-models.json'
if p.exists():
    data=json.loads(p.read_text())
    print(data.get('models', [])[:1])
PY
```

---

## 里程碑 4：Tauri 2 Mac 桌面壳

### 目标

实现 macOS 桌面壳：系统工具风格、浅深色、托盘常驻、单实例、深链拉起、接入向导、Codex App 详情、模型目录、诊断与设置。桌面壳只调用 sidecar 稳定 JSON 接口，不直接实现注入逻辑。

### 建议目录结构

```text
tools/zhumeng-agent/desktop/
  package.json
  vite.config.ts
  tailwind.config.ts
  src/
    App.tsx
    main.tsx
    routes.tsx
    pages/
      OverviewPage.tsx
      ConnectedAppsPage.tsx
      CodexDetailPage.tsx
      SetupWizardPage.tsx
      DiagnosticsPage.tsx
      SettingsPage.tsx
      AboutDistributionPage.tsx
    components/
      GlobalStatusBar.tsx
      ConnectedAppCard.tsx
      HealthCheckList.tsx
      CodexEnhancementsCard.tsx
      ModelCatalogTable.tsx
      ModelPriceTooltip.tsx
      TrayStatusSummary.tsx
    lib/
      sidecar.ts
      desktopStatus.ts
      modelCatalog.ts
      redaction.ts
  src-tauri/
    tauri.conf.json
    Cargo.toml
    src/main.rs
    capabilities/default.json
    icons/
```

### 技术栈

- Tauri 2。
- React + Vite + Tailwind。
- Python CLI sidecar：调用 `zhumeng-agent desktop ... --json`。
- Tauri IPC：Rust command 只负责执行 sidecar、管理窗口/托盘/深链事件。
- macOS Keychain：MVP 可预留接口；首版 token 仍由 Python state 管理是阶段性偏离，必须配套 0600 权限校验、日志/诊断脱敏测试，UI 不展示 token。
- 深链：`zhumeng-agent://setup`。
- 深链范围：首版同时处理 `setup`、`reauth`、`open?app=codex`，并在单实例中转发到已打开窗口。
- 托盘：打开主窗口、当前状态、重启本机代理、修复 Codex、打开 Codex App、退出。
- 单实例：第二次深链打开时聚焦已有窗口并进入向导。
- 主题与语言：MVP 支持浅色/深色跟随系统；中文为默认文案，同时保留 i18n 文件结构，英文可后续补齐。完整命令面板降级为 v2。
- 视觉基准：最终 UI 以 `tools/zhumeng-agent/docs/zhumeng-agent-desktop-mockup.html` 为最低视觉标准；可以优化和超越，但不得降级为粗糙 demo、普通后台管理页或营销页。

### 页面与 sidecar 接口依赖

| 页面 | 关键内容 | 依赖 sidecar 接口 |
| --- | --- | --- |
| 概览 | 全局状态、当前代理端口、Codex 状态、快速操作 | `desktop status --json`、`desktop repair --client codex` |
| 已接入应用 | 已接入 Adapter 列表，MVP 只 Codex，未来多目标 | `desktop status --json` |
| Codex App 详情 | 代理、授权、配置、模型目录摘要、Codex 增强项、健康检查 | `desktop status --json`、`desktop codex-enhancements status`、`desktop models status` |
| 接入向导 | 深链接收、授权/重新授权、检测安装、写配置、启动代理、启用 Codex 增强项、成功页 | `desktop setup`、`desktop reauth`、`desktop codex-enhancements patch`、`desktop status` |
| 诊断与日志 | 脱敏报告、复制、错误解释 | `desktop diagnose --redacted --json` |
| 设置 | 开机启动占位、代理端口策略、严格模型门禁、隐私选项 | MVP 部分只展示不可用占位；状态读取 `desktop status` |
| 关于 / 分发与安全 | 官网下载、非 Mac App Store、签名、公证、SHA256、架构 | 本地构建 metadata；可不依赖 sidecar |

### 关键实现步骤

1. 初始化 Tauri 2 + React + Vite + Tailwind 桌面目录。
2. Rust 侧实现 sidecar runner：
   - 统一超时。
   - 读取 stdout JSON。
   - stderr 仅进入脱敏诊断，不直接展示敏感内容。
   - 非 0 exit code 也解析 JSON error。
3. 注册 deep link：
   - macOS `Info.plist` scheme `zhumeng-agent`。
   - 单实例下把 URL 传给前端 router。
   - 路由表必须覆盖 `setup`、`reauth`、`open?app=codex`：`setup/reauth` 进入接入或重新授权向导，`open` 调用 `desktop open --app codex`。
4. 实现主页面骨架：左侧导航、顶部全局状态、内容区。视觉语言必须对齐静态草图：系统设置面板感、低装饰、清晰层级、克制品牌色、状态一眼可辨。
5. 实现接入向导：
   - 步骤 1：收到网页授权。
   - 步骤 2：检测 Codex App。
   - 步骤 3：完成授权与配置注入。
   - 步骤 4：启动本机代理。
   - 步骤 5：启用 Codex 增强项。
   - 步骤 6：健康检查。
   - 步骤 7：成功页。
6. 实现 Codex 增强项小卡片：Model Picker、Plugin Auth Gate、Plugin Mention Marketplace、备份状态。
7. 实现模型目录表：固定高度、sticky header、搜索、筛选、价格 tooltip。
8. 实现托盘菜单和主窗口显示/隐藏。
9. 实现状态轮询 / 守护：
   - 前台窗口打开时每 3 秒调用 `desktop status --json`。
   - 托盘后台每 5 秒检查代理和后端摘要状态。
   - 代理首次不可达后进入黄色 `degraded`；持续超过 10 秒不可达进入红色 `error_proxy_down`。
   - 状态恢复后回到绿色 `running`，并记录最近一次错误供诊断页显示。
10. 实现关于 / 分发与安全卡片，明确“官网下载安装，不走 Mac App Store”。
11. 做视觉还原检查：
   - 对照 `zhumeng-agent-desktop-mockup.html` 检查布局、间距、圆角、阴影、字体层级、状态色、卡片密度、浅色/深色效果。
   - 保留草图中的关键体验：左侧导航、顶部全局状态、应用卡片、Codex 增强项卡片、健康检查、模型目录滚动表格、价格 tooltip、诊断与分发安全卡片。
   - 若实现因 Tauri/React 组件限制调整样式，必须保证同等或更高视觉完成度。

### 明确不做的内容

- 不在 Tauri 壳里直接编辑 `~/.codex` 或 `app.asar`。
- 不在 UI 里持久化 token。
- 不实现 Claude Desktop 页面真实接入，只可在应用目录里显示“规划中”。
- 不实现自定义供应商真实添加，只显示禁用入口或“第二版支持”。
- 不做自动更新。

### 验收标准

- 双击 App 能打开主窗口。
- 网页 `setup` deep link 能拉起已有实例并进入接入向导；`reauth` 能进入重新授权；`open?app=codex` 能打开 Codex App 或返回结构化错误。
- UI 所有状态来自 sidecar JSON，不解析命令文本。
- 断开代理后，点击“一键修复”能恢复代理并刷新 UI。
- 杀掉代理后托盘 5 秒内进入黄色降级态，持续 10 秒不可达进入红色错误态；恢复后回到运行中。
- Codex 增强项作为向导主步骤出现，不只在高级区。
- 模型目录滚动和 tooltip 可用。
- 最终 UI 视觉质量不得低于 `zhumeng-agent-desktop-mockup.html`：布局层级、信息密度、状态表达、按钮优先级、卡片质感、浅深色观感均需达到草图水平或更高。
- 浅色 / 深色跟随系统；中文文案可用；i18n 文件结构存在但不要求首版完整英文翻译。
- 诊断报告复制内容已脱敏。

### 风险和回滚策略

- 风险：Tauri sidecar 打包 Python 运行环境复杂。
  - 回滚：内测阶段先要求安装 `zhumeng-agent` CLI，Tauri 调用系统 PATH；正式包再内置 sidecar。
- 风险：深链注册与已有 CLI 协议注册冲突。
  - 回滚：保留 CLI 协议处理作为 fallback；桌面 App 注册为主处理器。
- 风险：UI 误导用户以为修补失败等于接入失败。
  - 回滚：状态拆分“基础代理可用”和“插件市场增强状态”，成功页分别展示。
- 风险：实现阶段为了赶进度降低 UI 完成度。
  - 回滚：先保持草图同款布局和组件密度，延后非核心动效；任何样式重构都要以不低于草图为验收门槛。
- 风险：后台轮询过频导致耗电或日志噪音。
  - 回滚：窗口关闭时降频到 5 秒或更低；连续相同状态不重复写日志。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/tools/zhumeng-agent/desktop
npm install
npm run lint
npm run test
npm run tauri dev
open 'zhumeng-agent://setup?client=codex&code=mock-code&server=http%3A%2F%2F127.0.0.1%3A18081'
open 'zhumeng-agent://reauth?client=codex&code=mock-code&server=http%3A%2F%2F127.0.0.1%3A18081'
open 'zhumeng-agent://open?app=codex'
```

---

## 里程碑 5：macOS 打包与分发

### 目标

明确首版 macOS 分发路径：内测可降低门槛但必须提示风险；正式官网发布必须 Developer ID 签名 + Apple Notarization + SHA256。首版不走 Mac App Store。

### 需要改哪些文件或新增哪些文件

- 新增/修改：`tools/zhumeng-agent/desktop/src-tauri/tauri.conf.json`
  - bundle id、应用名、图标、deep link、macOS bundle 配置。
- 新增：`tools/zhumeng-agent/desktop/src-tauri/Entitlements.plist`（如 Tauri/macOS 配置需要）
- 修改：`tools/zhumeng-agent/packaging/macos/Info.plist`
  - 与桌面 App deep link 配置对齐，避免 CLI 骨架和 Tauri bundle 冲突。
- 新增：`tools/zhumeng-agent/packaging/macos/README-release.md`
  - 内测、灰度、正式发布步骤。
- 新增：`tools/zhumeng-agent/packaging/macos/notarize.sh` 或 CI 脚本（实现阶段再决定是否脚本化）。
- 修改/新增测试：`tools/zhumeng-agent/tests/test_packaging_static.py`

### 分发阶段

| 阶段 | 分发方式 | 签名 / 公证 | 说明 |
| --- | --- | --- | --- |
| 内测 | 直接发 `.app` / DMG | 可未签名或临时签名 | 必须提示 Gatekeeper 摩擦：右键打开 / 系统设置允许。 |
| 小范围灰度 | 官网隐藏下载 / 灰度链接 | Developer ID 签名，尽量公证 | 用于验证安装、深链、托盘和 Codex 修补。 |
| 正式官网发布 | 官网 DMG / PKG | Developer ID Application + Apple Notarization；如提供 PKG，Developer ID Installer | 发布页列版本、架构、SHA256。 |
| Mac App Store | 首版不做 | 需沙盒和审核 | 除非商业上必须，后续再评估。 |

### 为什么不走 Mac App Store

- 工具需要写入 `~/.codex/config.toml`、`~/.codex/auth.json`、`~/.codex/zhumeng-codex-models.json`。
- 工具需要注册 `zhumeng-agent://` 深链。
- 工具需要运行本机代理。
- 工具可能修补 `Codex.app/Contents/Resources/app.asar` 以启用模型选择和 Codex App 自带插件市场。
- 上述能力与 Mac App Store 沙盒和审核路径不匹配，首版不应为上架牺牲核心可用性。

### 关键实现步骤

1. 内测包：先完成 Tauri bundle，可未签名，但安装说明必须明确 Gatekeeper 提示。
2. 配置 bundle identifier、图标、deep link scheme。
3. 验证 `.app` 能启动 sidecar、打开托盘、接收 deep link。
4. 正式发布前：
   - 使用 Developer ID Application 签名 `.app`。
   - 如提供 `.pkg`，使用 Developer ID Installer 签名。
   - 提交 Apple Notarization 并 staple。
   - 生成 DMG/PKG SHA256。
   - 发布页展示：版本、架构（Universal / Apple Silicon / Intel）、SHA256、发布日期。
5. 对 `app.asar` 修补的风险做安装页和 App 内说明：可能影响 Codex.app 原签名，工具会备份并可恢复。

### 验收标准

- 内测包在干净 macOS 用户环境能打开，若未签名，安装文档说明与实际 Gatekeeper 行为一致。
- 正式候选包 `codesign --verify --deep --strict` 通过。
- 正式候选包 `spctl --assess --type execute` 通过或有明确公证结果。
- DMG/PKG 有 SHA256，发布页内容与产物一致。
- 文档明确“不走 Mac App Store”。

### 风险和回滚策略

- 风险：未签名包被用户误认为病毒。
  - 回滚：内测限定小范围，说明右键打开和风险；尽快进入签名灰度。
- 风险：修补 Codex.app 后影响 Codex 签名。
  - 回滚：修补前备份；提供一键恢复；版本不兼容时不修补。
- 风险：Universal 包体积或 sidecar 兼容性问题。
  - 回滚：MVP 可先发 Apple Silicon 包；官网明确架构，后续补 Intel/Universal。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/tools/zhumeng-agent/desktop
npm run tauri build
codesign --verify --deep --strict --verbose=2 src-tauri/target/release/bundle/macos/*.app
spctl --assess --type execute --verbose=4 src-tauri/target/release/bundle/macos/*.app
shasum -a 256 path/to/ZhumengAgent.dmg
```

---

## 里程碑 6：验证与验收

### 目标

用单元测试、CLI 集成测试、桌面 UI 测试和人工端到端路径证明 Mac MVP 可用、可诊断、可恢复、不会泄露敏感信息。

### 需要改哪些文件或新增哪些文件

- 新增/修改 Python 测试：
  - `tools/zhumeng-agent/tests/test_desktop_cli.py`
  - `tools/zhumeng-agent/tests/test_desktop_diagnostics.py`
  - `tools/zhumeng-agent/tests/test_codex_model_picker.py`
  - `tools/zhumeng-agent/tests/test_codex_config_manager.py`
  - `tools/zhumeng-agent/tests/test_proxy_server.py`
- 新增后端测试：
  - `backend/internal/service/codex_gateway_model_registry_test.go`
  - `backend/internal/service/codex_gateway_service_test.go`
- 新增桌面测试：
  - `tools/zhumeng-agent/desktop/src/**/*.test.tsx`
  - `tools/zhumeng-agent/desktop/src-tauri` 侧 command 测试（如项目采用 Rust 单测）
- 新增手动验收文档：
  - `tools/zhumeng-agent/docs/zhumeng-agent-desktop-mac-mvp-acceptance.md`

### 单元测试范围

- CLI JSON envelope 成功和失败都可解析。
- setup 不泄露 token，状态字段完整。
- repair 恢复代理端口。
- 三项 Codex 增强项 status/patch/restore。
- logout 恢复 `~/.codex` 配置和本工具修补过的 `app.asar`。
- 模型 catalog 保留 `capabilities/pricing`。
- 诊断报告脱敏。
- 错误翻译矩阵：旧端口无监听、授权过期、设备被撤销、后端不可达、配置文件无法写入、Codex 正在运行需重启、模型目录同步失败、`app_bundle_not_writable`、`restore_conflict`、`app.asar` patch point 不唯一。每项都要有错误 code、用户标题、说明文案、建议动作。

### CLI 集成测试范围

- mock 后端 setup grant。
- mock `/codex/v1/models` 返回 GPT / DeepSeek / Claude 风格模型，但能力由字段指定。
- mock 旧端口无监听，`desktop repair` 重新拉起代理。
- mock token 过期，refresh 后重试模型目录同步。

### 桌面 UI 测试范围

- 接入向导步骤顺序正确，包含“启用 Codex 增强项”。
- 成功页展示代理端口、模型摘要、插件市场状态。
- Codex 增强项卡片展示 Model Picker / Plugin Auth Gate / Plugin Mention Marketplace / 备份。
- 模型目录表固定高度滚动，表头固定，搜索筛选可用，价格 tooltip 展示字段。
- 关于页展示官网分发、不走 Mac App Store、Developer ID、Notarized、SHA256 示例。
- 浅色 / 深色主题可切换或跟随系统；中文文案完整；i18n 结构存在；命令面板入口如出现必须标注“后续版本”或隐藏。
- 视觉回归测试以 `zhumeng-agent-desktop-mockup.html` 为基准：实现页面至少覆盖草图的关键区域与视觉质量，不得出现明显低保真占位、未对齐的卡片层级、状态色混乱或信息密度大幅劣化。

### 手动验收路径

必须覆盖：

1. 从网页 `/codex` 点击“打开本机客户端”。
2. 从网页或工具触发 `reauth` 时进入重新授权流程；触发 `open?app=codex` 时打开 Codex App 或显示结构化错误。
3. 30 秒内 Codex 可用：代理启动、配置写入、Codex App 能访问 `http://127.0.0.1:<port>/v1/models`。
4. Codex 模型目录显示 GPT / DeepSeek / Claude 来源模型；兼容性由后端 `capabilities` 判定。
5. 模型价格 tooltip 展示输入、输出、缓存命中、缓存写入、单位、来源、更新时间；缺失价格显示“未配置”。
6. Codex 插件市场按钮不再灰色。
7. 能打开 Codex App 本地插件市场页面，内置插件目录可见。
8. 手动杀掉本机代理后，托盘 5 秒内黄、10 秒内红；点击一键修复恢复。
9. token 过期后，桌面工具提示重新授权，并可从网页重新走 setup 或 reauth。
10. 退出接入恢复 `~/.codex/config.toml`、`~/.codex/auth.json`、`~/.codex/zhumeng-codex-models.json`；若接入前不存在 catalog，退出后仅在 managed marker/hash 匹配时删除。
11. 退出接入遇到用户手动修改配置时进入 `restore_conflict`，不覆盖用户改动，提供当前文件备份和手动恢复建议。
12. 退出接入恢复本工具修补过的 `app.asar`；恢复失败进入可诊断错误态，不显示退出成功。
13. `Codex.app` 不可写时提示 `app_bundle_not_writable`，不使用 sudo、不 chmod/chown、不破坏应用。
14. 诊断报告不泄露 token、refresh token、loopback secret、邮箱、机器名、完整本机用户名；state 文件权限不宽于 0600。

### 验收标准

- 所有 Python 单元测试通过。
- 相关 Go 单元测试通过。
- 桌面 UI 单元测试 / 组件测试通过。
- Mac 手动验收清单逐项通过并记录版本、系统、Codex App 版本、构建号。
- 若某项因环境缺失无法测试，必须标为“阻塞 / 未验证”，不能标为通过。

### 风险和回滚策略

- 风险：端到端依赖真实 Codex App 和后端，环境不稳定。
  - 回滚：保留 mock 集成测试；真实验收单独记录环境。
- 风险：退出接入恢复失败破坏用户已有配置。
  - 回滚：退出接入前再次创建恢复点；失败保留 state 和备份路径，UI 提供手动恢复指引。
- 风险：诊断报告误泄露。
  - 回滚：默认只输出摘要；敏感字段用 hash 或 `<redacted>`。

### 建议测试命令

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
PYTHONPATH=tools/zhumeng-agent/src pytest tools/zhumeng-agent/tests -v
go test ./backend/internal/service ./backend/internal/handler -count=1
cd tools/zhumeng-agent/desktop && npm run lint && npm run test && npm run tauri build
```

---

## 7. 实现任务拆分建议

### Task A：sidecar 桌面 JSON 契约

**文件：** `cli.py`、新增 `desktop.py`、新增 `desktop_schema.py`、新增 `tests/test_desktop_cli.py`

- [ ] 写 `desktop status --json` 失败测试。
- [ ] 实现最小 envelope。
- [ ] 补 state 兼容读取。
- [ ] 补代理 health 检查。
- [ ] 补 Codex App 检测。
- [ ] 跑 `pytest tools/zhumeng-agent/tests/test_desktop_cli.py -v`。

### Task B：Codex 增强项 restore 与聚合

**文件：** `model_picker.py`、`cli.py`、可新增 `enhancements.py`、测试文件。

- [ ] 给 `plugin-mention-marketplace restore` 写失败测试。
- [ ] 实现 restore。
- [ ] 新增三项聚合 status/patch/restore。
- [ ] repair/logout 复用聚合函数。
- [ ] 跑相关 pytest。

### Task C：logout 完整恢复

**文件：** `cli.py`、`config_manager.py`、`state.py`、测试文件。

- [ ] 测试接入前不存在 catalog 时退出后删除。
- [ ] 测试接入前存在 catalog 时恢复 hash。
- [ ] 测试用户手动修改配置后退出进入 `restore_conflict`。
- [ ] 测试 app.asar restore 失败时不删除 state。
- [ ] 实现结构化错误返回。

### Task D：模型 capabilities/pricing 后端和本地 catalog

**文件：** 后端 service/types/registry 测试、`config_manager.py`、`http_client.py`。

- [ ] Go 测试要求 `/codex/v1/models` 返回 `capabilities/pricing`。
- [ ] Python 测试要求 catalog 透传字段。
- [ ] 实现字段输出和本地保留。

### Task E1：Tauri 初始化 + 可构建空壳

**文件：** `tools/zhumeng-agent/desktop/**`

- [ ] 初始化 Tauri 2 项目。
- [ ] 接入 React + Vite + Tailwind。
- [ ] 实现系统工具风格基础布局与浅色/深色主题。
- [ ] `npm run lint && npm run test && npm run tauri build` 通过。

### Task E2：sidecar IPC + JSON 错误处理

**文件：** `desktop/src/lib/sidecar.ts`、`desktop/src-tauri/src/main.rs`

- [ ] Tauri command 调用 `zhumeng-agent desktop ... --json`。
- [ ] 非 0 exit code 也解析 JSON envelope。
- [ ] stderr 脱敏进入诊断，不直接展示敏感原文。
- [ ] mock sidecar 成功/失败测试通过。

### Task E3：deep link + single instance

**文件：** `desktop/src-tauri/src/main.rs`、`desktop/src/routes.tsx`

- [ ] 注册 `zhumeng-agent://setup`、`reauth`、`open?app=codex`。
- [ ] 第二次打开时聚焦已有窗口并转发 URL。
- [ ] `setup/reauth/open` 三条手动 deep link 验收通过。

### Task E4：主壳、状态轮询与托盘

**文件：** `OverviewPage.tsx`、`GlobalStatusBar.tsx`、`TrayStatusSummary.tsx`

- [ ] 概览页展示全局状态、代理端口、后端状态。
- [ ] 前台 3 秒轮询，托盘后台 5 秒轮询。
- [ ] 代理断开后 5 秒黄、10 秒红，恢复后回绿。
- [ ] 托盘菜单：打开主窗口、重启代理、修复 Codex、打开 Codex、退出。

### Task E5：接入向导

**文件：** `SetupWizardPage.tsx`

- [ ] `setup` 与 `reauth` 共用向导，但文案区分首次接入 / 重新授权。
- [ ] 步骤包含“启用 Codex 增强项”。
- [ ] 成功页展示代理、模型目录摘要、插件市场状态。

### Task E6：Codex 详情与增强项卡片

**文件：** `CodexDetailPage.tsx`、`CodexEnhancementsCard.tsx`、`HealthCheckList.tsx`

- [ ] 展示 Model Picker、Plugin Auth Gate、Plugin Mention Marketplace、备份状态。
- [ ] 支持高级手动修复入口，但主流程仍走向导/一键修复。
- [ ] 处理 `app_bundle_not_writable`、`unsupported`、`restart_required`、`failed`。

### Task E7：模型目录

**文件：** `ModelCatalogTable.tsx`、`ModelPriceTooltip.tsx`、`modelCatalog.ts`

- [ ] 固定高度滚动、sticky 表头。
- [ ] 搜索和筛选：供应商、能力、主列表状态、可用性、价格配置。
- [ ] 价格 tooltip 展示后端 pricing 字段；缺失显示“未配置”。
- [ ] 不按模型品牌硬编码兼容性。

### Task E8：诊断、设置、关于 / 分发与安全

**文件：** `DiagnosticsPage.tsx`、`SettingsPage.tsx`、`AboutDistributionPage.tsx`

- [ ] 诊断报告复制前脱敏。
- [ ] 设置页显示隐私与严格模型门禁；自定义供应商入口标注后续版本。
- [ ] 关于页展示官网下载、不走 Mac App Store、Developer ID、Notarized、SHA256 示例。
- [ ] 命令面板入口如保留，标注 v2；否则隐藏。

### Task F：打包、分发与验收文档

**文件：** `desktop/src-tauri/**`、`packaging/macos/**`、新增验收文档。

- [ ] 内测打包说明。
- [ ] 签名/公证说明。
- [ ] SHA256 发布说明。
- [ ] 手动验收清单。

---

## 8. 实现代理注意事项

- 不要把 Codex App 支持写成唯一目标；命名保持 Adapter 化。
- 不要把自定义供应商做成可用功能；首版只保留占位。
- 不要在 UI 或前端常量里硬编码真实模型价格。
- 不要按 GPT / DeepSeek / Claude 品牌硬编码兼容性。
- 不要在 Codex 正在运行时静默强杀。
- 不要在 `app.asar` patch point 不唯一时强行修补。
- 不要用 sudo、chmod、chown 处理不可写的 Codex App；返回 `app_bundle_not_writable`。
- 不要在退出接入恢复失败时删除 state 或显示成功。
- 不要在 `restore_conflict` 时覆盖用户接入后的手动配置变更。
- 不要把 token 迁移到 UI；MVP 暂用 state.json 时必须校验 0600 权限并覆盖日志/诊断脱敏测试。
- 不要上传本机日志；诊断报告默认本地生成、脱敏复制。
- 不要把桌面 UI 做得低于静态草图水准；实现时可超越草图，但不能牺牲草图已经确定的系统工具感、信息层级、状态表达和卡片质感。
- 不要改动与本计划无关的后端、前端或工具文件；当前仓库已有大量未提交改动，进入实现前必须确认改动归属。
