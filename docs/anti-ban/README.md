# 反代抗风险 / 降低封号概率 文档索引

> 目录状态：**已切换到“总执行手册”模式**
>
> 从 2026-05-17 起，`docs/anti-ban/09-execution-handbook.md` 是本目录的**唯一执行主文档**。
> 其余文档默认视为背景评审、证据索引、抓包 SOP 或从属路线图。

---

## 先看哪里（给后续代理）

### 必读顺序

1. **[09-execution-handbook.md](./09-execution-handbook.md)** —— **执行主文档 / authoritative handbook**
2. **[10-claude-pre-launch-audit.md](./10-claude-pre-launch-audit.md)** —— **Claude 上线前对抗性审查 (2026-05-19)**
3. **[06-implementation-inventory.md](./06-implementation-inventory.md)** —— 代码索引，找文件和函数入口
4. **[08-traffic-capture-sop.md](./08-traffic-capture-sop.md)** —— 抓包操作说明
5. **[07-pre-launch-checklist.md](./07-pre-launch-checklist.md)** —— 灰度 / 上线前核对清单

### 其余文档怎么用

| 文档 | 角色 | 使用方式 |
|------|------|----------|
| [00-reality-check.md](./00-reality-check.md) | 历史差距分析 | 作背景参考，不直接当执行事实 |
| [01-claude-anthropic.md](./01-claude-anthropic.md) | Claude/Anthropic 背景分析 | 理解现有实现 |
| [02-openai.md](./02-openai.md) | OpenAI/Codex 背景分析 | 理解现有实现 |
| [03-gemini.md](./03-gemini.md) | Gemini/Antigravity 背景分析 | 理解现有实现 |
| [04-cross-cutting.md](./04-cross-cutting.md) | 跨平台对比 | 识别共性能力与不对称性 |
| [05-improvement-roadmap.md](./05-improvement-roadmap.md) | 从属路线图 | 任务拆分参考，服从 09 |
| [10-claude-pre-launch-audit.md](./10-claude-pre-launch-audit.md) | Claude 上线前审查 | 上线 Claude 订阅前必读 |
| [13-claude-oauth-onboarding-sop.md](./13-claude-oauth-onboarding-sop.md) | Claude OAuth 添加账号 SOP | sub2api 添加 Claude OAuth 账号 / 首次 canary 前必读 |
---

## 当前修正后的核心判断（简版）

### Claude / Anthropic
- 现有 mimicking 成熟度最高
- 仍需按真实抓包定期刷新版本与 beta 组合
- 不能把仓库常量直接当成“当前官方真实值”

### OpenAI / Codex
- 当前代码**已经存在** canonical profile / artifact 注入链路
- 后续执行重点是：**核对现有实现与真实官方客户端的一致性、覆盖范围、route 差异、profile mode 风险**
- 不应再以“OpenAI 主路径完全没有 header 注入”为前提展开实施

### Gemini
- 仍然是风险最高、最优先需要重新抓 baseline 的平台
- 重点在于：真实 baseline、Google 特征头、入站客户端限制、账号/IP 隔离

### Antigravity
- 现有代码并不空白，且具备较强热更新灵活性
- 但公开 baseline 最弱，因此抓包优先级高

---

## 重要边界

本目录所有文档都必须服从以下原则：

- **目标是持续降低风险，不是承诺 0 风险**
- **先抓官方 baseline，再改 sub2api**
- **先验证现有实现，再决定是否重写**
- **任何“最新版本”类结论都必须注明来源与日期**

详见：

- [09-execution-handbook.md](./09-execution-handbook.md)

---

## 推荐使用方式

### 如果你要开始执行抓包 / 对齐 / 升级任务
直接按以下路径走：

1. 读 [09-execution-handbook.md](./09-execution-handbook.md)
2. 用 [06-implementation-inventory.md](./06-implementation-inventory.md) 找代码入口
3. 用 [08-traffic-capture-sop.md](./08-traffic-capture-sop.md) 抓官方流量
4. 形成 baseline → diff → 最小修复 → 灰度 → 回滚记录

### 如果你只是想快速了解现状
读：

1. 本 README
2. [09-execution-handbook.md](./09-execution-handbook.md)
3. 视需要再看 01-04 / 06

---

## 文档维护说明

如果未来抓包证据、代码实现、上游行为与本目录已有判断发生冲突：

1. 以**最新 Traffic Verified 证据**为准
2. 先更新 **09-execution-handbook.md**
3. 再按需更新对应背景文档与路线图

不要只在聊天记录里说明“这个结论过时了”，而不把文档修正回来。


---

## 逆向工程产物（2026-05-19 ~ 2026-05-20）

所有逆向产物在 `captures/` 目录下：

| 文件 | 内容 |
|------|------|
| `captures/2026-05-20-reverse-engineering-summary.md` | **逆向总结 + 代码修改清单（必读）** |
| `captures/2026-05-19-claude-code-reverse-engineering.md` | 主逆向报告 |
| `captures/2026-05-19-event-logging-live-behavior.md` | Event Logging 行为 |
| `captures/2026-05-19-transport-fingerprint.md` | TLS/JA3/Transport 指纹 |
| `captures/2026-05-20-cch-final-attempt.md` | CCH 算法攻克尝试 |
| `captures/2026-05-20-header-wire-order.md` | Header Wire Order |
| `captures/2026-05-19-extracted-fragments/` | 119 个原始证据文件 |
