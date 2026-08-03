# 逐梦注入工具桌面版 Mac MVP 验收记录

## 基本信息

- 当前日期：2026-05-22
- 记录时间：2026-05-22 06:51:18 PDT / 2026-05-22T13:51:18Z
- 工作分支：`feature/zhumeng-agent-desktop-release-preflight`
- 记录时提交：`035d3278fa6bbeb63cee0d5fd5798d729e0b2979`
- 文档提交后最终验证提交：`dea6c911d30b99d3ed6f3f5d930d3f1e00e681b6`
- Worktree：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight`

## 系统环境

- macOS：14.5 (Build 23F79)
- 架构：arm64
- Darwin：23.5.0
- Node.js：v24.7.0
- npm：11.5.1
- Rust：rustc 1.92.0 (ded5c06cf 2025-12-08)
- Cargo：cargo 1.92.0 (344c4567c 2025-10-21)
- Tauri CLI：tauri-cli 2.11.2
- Python：main venv `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/tools/zhumeng-agent/.venv/bin/python`，版本 Python 3.14.3

## 前端验证结果

执行目录：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop`

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `npm ci` | 通过 | 安装 lockfile 依赖到 worktree 本地 `node_modules/`。 |
| `npm run lint` | 通过 | `tsc` 类型检查通过。 |
| `npm test` | 通过 | Vitest 5 个文件、13 个测试通过。 |
| `npm run build` | 通过 | Vite 生产构建生成 `dist/`。 |

补充检查：

- Tauri 前端 sidecar client 只通过 `run_sidecar` 调用 `zhumeng-agent desktop ... --json` 命令族。
- 已补充测试覆盖 `status`、`models status`、`open --app codex`、`setup`、`reauth`、`codex-enhancements patch` 的 desktop JSON 调用契约。

## Rust/Tauri 验证结果

执行目录：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop/src-tauri`

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `cargo test` | 通过 | 4 个 Rust 单元测试通过。 |

补充检查：

- Tauri command `run_sidecar` 增加了最小契约校验：只允许 `desktop` 子命令且必须包含 `--json`。
- sidecar stderr 错误路径仍保持 token/secret/authorization 关键词脱敏。

## Python sidecar 验证结果

Python 命令使用 main venv，并显式设置 worktree 源码路径：

```bash
PYTHONPATH="/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/src" \
  "/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/tools/zhumeng-agent/.venv/bin/python" -m zhumeng_agent desktop status --json
```

| 验证 | 结果 | 说明 |
| --- | --- | --- |
| 导入路径校验 | 通过 | `zhumeng_agent.__file__` 指向 release-preflight worktree 源码。 |
| `desktop status --json` | 通过 | 使用隔离 HOME 返回合法 JSON envelope，状态为 `not_configured`。 |
| `desktop diagnose --redacted --json` | 通过 | 使用隔离 HOME 返回合法 JSON envelope，并包含 `<redacted_home>`。 |

## Deep link 验证结果

- Tauri 配置：`src-tauri/tauri.conf.json` 已配置 scheme `zhumeng-agent`。
- 前端解析测试覆盖：
  - `zhumeng-agent://setup?client=codex&code=...&server=...`
  - `zhumeng-agent://reauth?client=codex&code=...&server=...`
  - `zhumeng-agent://open?app=codex`
  - 非法 scheme 与缺失参数拒绝路径
- Tauri runtime 集成：已启用 `tauri-plugin-deep-link` 和 `tauri-plugin-single-instance`，二次打开参数中的 `zhumeng-agent://` URL 会转发到前端 `deep-link` 事件。

未进行真实系统层面的 URL 点击/浏览器跳转验证；该项需要安装构建产物后在目标机器上手动验证。

## 打包验证结果

执行目录：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop`

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `npm run tauri build` | 通过 | 已新增 `tauri` npm script 并生成 macOS `.app` 与 `.dmg`；最终验证时重新构建并刷新 SHA256。 |

产物：

- `.app`：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop/src-tauri/target/release/bundle/macos/逐梦注入工具.app`
- `.dmg`：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop/src-tauri/target/release/bundle/dmg/逐梦注入工具_0.1.0_aarch64.dmg`

SHA256：

```text
74262c0a6ef82be8ed716bf9d134a82e790f7d12b75b48a9ea7f44d62475889c  /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/zhumeng-agent-desktop-release-preflight/tools/zhumeng-agent/desktop/src-tauri/target/release/bundle/dmg/逐梦注入工具_0.1.0_aarch64.dmg
```

签名/门禁检查：

- `codesign -dv` 显示当前 `.app` 为 ad-hoc 签名，`TeamIdentifier=not set`。
- `spctl --assess --type execute --verbose=4` 未通过，输出为 `code has no resources but signature indicates they must be present`。
- 未执行正式 Developer ID 签名或公证。

## 未验证项

- Developer ID 正式签名。
- Apple notarization 公证。
- 安装后的 macOS Launch Services deep link 真实唤起。
- 真实授权 code 的 `desktop setup` / `desktop reauth` 端到端接入。
- 真实 Codex App 的打开、修复、增强项 patch/restore 端到端验证。
- Intel x86_64 Mac 打包与运行验证。

## 阻塞项

- 对外分发阻塞：缺少 Developer ID 签名和 notarization，且本地 `spctl` 评估未通过。
- 内测本地构建：未发现阻塞；可使用生成的 unsigned/ad-hoc `.app` / `.dmg` 做受控开发机验证。

## 下一步建议

1. 在目标内测 Mac 上安装 `.dmg`，验证 `zhumeng-agent://setup`、`zhumeng-agent://reauth`、`zhumeng-agent://open?app=codex` 真实唤起。
2. 准备 Developer ID Application 证书、notarytool 凭据与 CI secret 后，再做签名、公证和 Gatekeeper 验证。
3. 用真实授权 code 跑一遍 setup/reauth/logout 恢复链路，并记录脱敏诊断报告。
4. 在真实 Codex App 上验证增强项 status/patch/restore 和 app.asar 恢复失败路径。
