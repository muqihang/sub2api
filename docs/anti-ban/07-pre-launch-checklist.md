# 上号池前必读清单

> 这是从「评估报告」到「实际可用」之间的桥梁。
> 上号池前请逐项核对，每项都给出**判定标准** + **不达标时的兜底方案**。
>
> 适用对象：Anthropic / OpenAI / Gemini / Antigravity OAuth 账号池
> 不适用：纯 API key 账号（风险特征不同）

---

## 0. 总原则

| 原则 | 说明 |
|------|------|
| 先抓包，再上号 | 任何反代方案的有效性都取决于"上游真实接受什么"，不是"代码声称什么" |
| 1:1 代理 | OAuth 个人账号原则上一个号一个独立代理（特别是 Gemini） |
| 渐进扩量 | 1 个测试号 → 5 个 → 20 个 → 100+，每阶段观察 7 天 |
| 监控先行 | health_score / 401/403/429 比例 / 代理集中度 三大监控就绪后再上号 |
| 紧急回滚 | 任何常量变更前 `git tag`；封号率上升立刻 revert |

---

## 1. 平台无关的通用检查

### 1.1 基础设施

- [ ] **数据库**：PostgreSQL 主从可用，监控告警就绪
- [ ] **Redis**：单节点或集群可用；OAuth callback 多实例必须用 Redis 模式
- [ ] **代理池**：每平台至少 N 个代理（N = 计划上号数 × 1.5，预留扩量空间）
- [ ] **代理类型**：OAuth 账号优先住宅 IP，避免数据中心 IP（数据中心 IP 在 Google/Anthropic 风控里几乎=黑名单）
- [ ] **Channel Monitor 模板**：每平台至少配置一个 challenge 模板（见 `backend/internal/service/channel_monitor_template_service.go`）

### 1.2 凭证保护

- [ ] **OpenAI**：`gateway.openai_core.credential_encryption_key` 已配置（32-byte hex）
- [ ] **OpenAI**：`production_mode = true` + `require_encrypted_credentials = true`（生产）
- [ ] **Gemini**：`GeminiSecretProtector` 加密 key 已配置
- [ ] **Anthropic**：⚠️ 当前没有专属 protector，凭证以明文存储（参见 [04-cross-cutting.md](./04-cross-cutting.md) 2.7）

**不达标兜底**：测试环境可以裸跑；生产建议至少 OpenAI/Gemini 启用，Anthropic 把 DB 访问权限收紧。

### 1.3 监控就绪

- [ ] `ops_health_score.go` 输出可视化（< 70 触发告警）
- [ ] `channel_monitor_runner.go` 已启动并跑通至少 24 小时
- [ ] 错误日志聚合（按 platform / status_code / proxy_hash 维度）
- [ ] 代理集中度看板（max accounts per proxy_hash 报表）

---

## 2. Anthropic（Claude）专项

### 2.1 客户端常量验证

按 [08-traffic-capture-sop.md](./08-traffic-capture-sop.md) 抓真实 `claude-cli` 流量，对比仓库：

- [ ] **CLI 版本**：`backend/internal/pkg/claude/constants.go: CLICurrentVersion`
  - 仓库当前：`2.1.92`
  - 上游真实：≥2.1.118（参考 [Anthropic 文档示例](https://docs.anthropic.com/en/docs/claude-code/cli-usage)）
  - 判定：差距 ≤ 10 个版本算 OK；> 20 个版本必须升级

- [ ] **DefaultHeaders 全套**：与抓包结果 diff
  - 重点：`User-Agent`、`X-Stainless-*` 全套、`X-App`、`Anthropic-Dangerous-Direct-Browser-Access`
  - 判定：每一项必须与抓包完全一致（包括大小写）

- [ ] **anthropic-beta 组合**：
  - `BetaContext1M` (`context-1m-2025-08-07`) — **建议从 sonnet-4 / sonnet-4-5 路径移除**（已变 no-op）
  - `BetaFastMode` (`fast-mode-2026-02-01`) — 仓库已临时移除，确认不要重新加入
  - 新版 CLI 可能新增的 beta token —— 抓包 diff 看是否有新增

- [ ] **`FullClaudeCodeMimicryBetas`**：
  - 仓库当前 8 项
  - 抓包对比官方 OAuth + 非 Haiku 的 beta header

### 2.2 入站校验

- [ ] **`claude_code_validator.go` 阈值**：`systemPromptThreshold = 0.5`
  - 判定：上号前用真实 Claude Code 跑一次完整对话，确认通过校验

### 2.3 路径配置

- [ ] **OAuth 账号路径**：`/v1/messages` 走 OAuth + Claude Code 伪装
- [ ] **API Key 账号路径**：使用 `APIKeyBetaHeader`（不带 oauth-* / claude-code-*）
- [ ] **Bedrock 路径**：`PrepareBedrockRequestBody` 已启用（迁移 anthropic-beta → body）

### 2.4 出口隔离

- [ ] **代理类型**：必须住宅 IP（机房 IP 在 Anthropic 风控里很容易识别）
- [ ] **代理:账号比**：建议 1:1 至 1:3（即一个代理最多 3 个账号）
- [ ] **同代理账号类型**：不要混 OAuth 和 API key

### 2.5 上号顺序

```
Day 1: 1 个测试 OAuth 账号 + 1 个测试 API key 账号，跑 24 小时
Day 2-3: 观察 401/429 比例 + health_score
Day 4: 扩到 5 个 OAuth + 3 个 API key
Day 5-7: 观察
Week 2: 扩到 20 个
Week 3-4: 扩到目标规模
```

### 2.6 Claude OAuth 添加账号 / 首次 Canary

> 详细 SOP 见 [13-claude-oauth-onboarding-sop.md](./13-claude-oauth-onboarding-sop.md)。
> 这一节是进入真实 sub2api OAuth 添加账号前的强制核对项。

- [ ] **只用手动 OAuth flow**：禁止 cookie/sessionKey 自动授权，禁止导入 refresh token。
- [ ] **授权出口一致**：
  - 本机第一轮：浏览器授权与 sub2api 后端 token exchange 使用同一出口；默认不选账号代理。
  - 服务器正式：生成 auth URL 前已绑定稳定代理/出口；浏览器授权也走同一出口。
- [ ] **干净浏览器 profile / 隐身窗口**：避免旧 cookie 自动选错账号/组织。
- [ ] **高级选项第一轮全关**：
  - TLS 指纹模拟：关
  - 会话 ID 伪装：关
  - 缓存 TTL 强制替换：关
  - 自定义转发地址：关
- [ ] **全局风险开关**：
  - `enable_anthropic_cache_ttl_1h_injection=false`
  - `enable_cch_signing=false`，除非已有 CCH 签名验证闭环
  - `enable_metadata_passthrough=false`
  - `enable_fingerprint_unification=true`
- [ ] **部署形态**：OAuth session 当前为内存存储；多实例必须 sticky，第一轮建议单实例。
- [ ] **日志安全**：不要开启 raw body / raw OAuth debug；OAuth 错误体必须脱敏。
- [ ] **首次 canary 限制**：添加成功后只发 1 个短 `hello` 请求，不并发、不连续重试。
- [ ] **停止条件**：出现 `invalid_grant`、401、403、KYC、unusual activity、third-party warning、token refresh 失败，立即停止。

---

## 3. OpenAI（含 Codex / Realtime / WS）专项

### 3.1 客户端常量验证

⚠️ **关键缺口**：仓库主代码层 X-Stainless-* 没有主动注入。

- [ ] **决策**：选择以下之一
  - **方案 A（推荐）**：先实现 X-Stainless 注入（参见 [05-improvement-roadmap.md](./05-improvement-roadmap.md) P0-1），再上号
  - **方案 B**：仅允许真实 Codex CLI（`codex_cli_rs`）调用，Cursor / Cline 等第三方走纯 API key 渠道（不混 OAuth 账号）
  - **方案 C**（不推荐）：透传客户端 X-Stainless，赌客户端发的是真实 SDK 头

- [ ] **如选 A**：抓真实 `codex_cli_rs` 流量获取最新 X-Stainless 全套
- [ ] **如选 B**：在 sub2api 入站层加白名单（`force_codex_cli`），仅允许 `codex_*` UA

### 3.2 OAuth Lifecycle

- [ ] **`tryRecoverOpenAIAuth401`** 在生产已启用
- [ ] **`OpenAIAuthStateTerminal`** 监控告警就绪（账号永久失效需要人工介入）
- [ ] **OAuth 401 cooldown**：`OAuth401CooldownMinutes` 默认 10 分钟，按需调整

### 3.3 Egress / TLS

- [ ] **Egress Bucket**：至少配置 1 个生产 bucket
- [ ] **`production_mode + egress_fail_closed`**：建议生产开启
- [ ] **TLS Profile**：至少 1 个 bucket 启用 TLS profile
- [ ] **Canary 探针**：`RunOpenAITLSCanaryProbe` 跑过且通过

### 3.4 客户端识别

- [ ] **`codex_cli_only` 账号开关**：根据账号性质设置
  - Codex Plus / Pro 订阅 OAuth：建议开启 `codex_cli_only` + 仅放真实 Codex 客户端
  - 一般 ChatGPT Plus OAuth：可关闭，但要监控 X-Stainless 异常
- [ ] **`Gateway.ForceCodexCLI`**：灰度阶段可临时开启全局放行

### 3.5 WS / Realtime

- [ ] **`openai_ws_pool` MaxConnsPerAccount**：建议 4-8
- [ ] **`applyOpenAIWSRetryPayloadStrategy`**：默认 `trim_optional_fields`，确认未关闭
- [ ] **OAuth callback sticky**：多实例部署必须用 Redis（参见 [05-improvement-roadmap.md](./05-improvement-roadmap.md) P1-3）

### 3.6 上号顺序

```
Day 1: 1 个 Codex Plus OAuth 账号 + 1 个 ChatGPT Plus OAuth 账号
Day 2-3: 重点观察 401/403 比例（403 是封号前兆）
Day 4: 如 health_score > 80 且 403 < 1%，扩到 5 个
Day 5-7: 观察 WS / Realtime 错误率
Week 2: 扩到 20 个
Week 3-4: 扩到目标规模
```

---

## 4. Gemini 专项（最高风险）

⚠️ **极重要警告**：仓库 `GeminiCLIUserAgent = "GeminiCLI/0.1.5"` 落后真实客户端 39 个版本。
**直接上号 = 高封号率**。

### 4.1 客户端常量验证（必须做）

- [ ] **抓真实 `gemini-cli` 最新版本流量**（v0.40.0+）
- [ ] **修改仓库常量**：
  - `backend/internal/pkg/geminicli/constants.go: GeminiCLIUserAgent`
  - 至少改成 `GeminiCLI/0.40.0 (操作系统; 架构)`
  - 或者通过 env 临时覆盖（如有）

- [ ] **手动添加缺失头**（在 `gemini_oauth_service.go` 或新建 helper）：
  ```go
  // 抓包应该看到类似的真实头
  req.Header.Set("x-goog-api-client", "gl-node/24.X.X gccl/0.40.X")
  req.Header.Set("x-goog-user-project", projectID)  // OAuth 拿到的 project_id
  ```

### 4.2 账号类型选择

- [ ] **优先级排序**：
  1. **Vertex Service Account**：风险最低（GCP IAM，正式企业流）
  2. **Gemini Code Assist Standard / Enterprise**：中等
  3. **Gemini for Google Workspace**：中等
  4. **Gemini Personal (gemini.google.com)**：风险最高 ⚠️

- [ ] **Personal 账号**：如果必须上，每号配独立住宅 IP，单代理 1 账号

### 4.3 thoughtSignature 安全

- [ ] **`gemini_sticky_session.go`** 安全策略已启用
- [ ] **`gemini_native_signature_cleaner.go`** 跨账号清理已启用
- [ ] **响应头观察**：`X-Sub2API-Gemini-Degraded` 触发频率应低于 5%

### 4.4 配额预检

- [ ] **`PreCheckUsage`** 已启用（`geminiPrecheckCacheTTL = 1min`）
- [ ] **每日配额监控**：避免账号打满后才发现没额度

### 4.5 入站客户端校验

⚠️ **当前缺失**：Gemini 路径没有等价于 `claude_code_validator` 的入站校验。

- [ ] **决策**：
  - **方案 A**：实现 `gemini_client_restriction_detector.go`（参见 [05-improvement-roadmap.md](./05-improvement-roadmap.md) P0-3）
  - **方案 B**：仅放真实 `gemini-cli` 流量上 Gemini Personal OAuth；其他客户端走 Vertex
  - **方案 C**（不推荐）：放任所有 UA

### 4.6 上号顺序

```
Day 1-3: 0 个 Personal OAuth，仅上 Vertex Service Account 测试
Day 4-7: 1 个 Gemini Code Assist 账号试水
Week 2: 如 health_score > 80，可考虑加 1-2 个 Personal OAuth（独立住宅 IP）
Week 3-4: 缓慢扩量，每加 5 个号观察 3-5 天
```

> **特别强调**：Gemini Personal OAuth 不要追求规模，质量第一。
> 上 100 个 Personal OAuth 不如上 10 个 Vertex Service Account。

---

## 5. Antigravity 专项

### 5.1 客户端常量

- [ ] **当前默认**：`antigravity/1.21.9 windows/amd64`（最近 2026-05-09 对齐）
- [ ] **如需更新**：通过环境变量临时覆盖
  ```bash
  export ANTIGRAVITY_VERSION=1.22.x
  # 或
  export ANTIGRAVITY_USER_AGENT='antigravity/1.22.x windows/amd64'
  export ANTIGRAVITY_V1INTERNAL_USER_AGENT='antigravity/1.22.x'
  ```
- [ ] **`X-Goog-Api-Client`**：当前硬编码 `gl-node/22.21.1`，如官方升级 Node 版本需要在源码里改

### 5.2 路径验证

- [ ] **`daily-cloudcode-pa.googleapis.com`** 路由可达（通过代理也能命中）
- [ ] **OAuth scopes** 完整（参见 `backend/internal/pkg/antigravity/oauth.go` 的 scopes 列表）

### 5.3 隐私设置

- [ ] **`setAntigravityPrivacy`** 在 OAuth 绑定流程内已调用
- [ ] **二次校验** `IsPrivate` 通过

### 5.4 限流处理

- [ ] **Internal 500 计数**：`Internal500CounterCache` 已启用，阈值合理
- [ ] **Smart Retry**：`handleSmartRetry` 已启用

### 5.5 上号顺序

```
Day 1: 1 个测试账号
Day 2-7: 重点观察 INTERNAL 500 计数 + smart retry 是否正常
Week 2: 扩到 5 个
Week 3-4: 缓慢扩量
```

---

## 6. 上号后的持续观察

### 6.1 第一周（关键观察期）

| 指标 | 阈值 | 触发动作 |
|------|------|---------|
| 任意账号 401 比例 > 5% | 立即介入 | 检查 OAuth lifecycle |
| 任意账号 403 比例 > 1% | 立即介入 | 检查 X-Stainless / 客户端伪装 |
| health_score < 70 | 24h 内介入 | 综合诊断 |
| 代理集中度（max per proxy_hash）> 5 | 立即介入 | 拆分代理 |
| TLS canary 失败 | 立即介入 | 暂停该 bucket 上号 |
| 单账号每日配额触顶 | 24h 内调整 | 调整 group RPM |

### 6.2 第二周-第四周

- 每周一次 channel_monitor 全量 challenge
- 每周一次 TLS canary 复测
- 每两周一次抓包对齐复核（至少 Gemini）
- 每月一次 Anthropic / Codex / Antigravity 抓包对齐

### 6.3 紧急响应

封号事件按 [08-traffic-capture-sop.md](./08-traffic-capture-sop.md) 第 6 节处理：

1. 立即停止该账号调度（`SetTempUnschedulable`）
2. 检查同代理其他账号状态
3. 抓最近一次成功响应 vs 失败响应做 diff
4. 评估是否需要批量切换代理 / 客户端常量
5. 灰度回滚或前进

---

## 7. 决策树：是否可以上号？

```
1. 是否抓过真实客户端流量并对齐了仓库常量？
   └─ 否 → ⛔ 不要上号，先做抓包对齐
   └─ 是 → 继续

2. 是否有住宅 IP 代理池？
   └─ 否 → ⛔ Gemini Personal / Anthropic OAuth 不要上
   └─ 是 → 继续

3. 是否配置了 channel_monitor + health_score 监控？
   └─ 否 → ⛔ 不要上号，否则封号了都不知道
   └─ 是 → 继续

4. OpenAI 路径是否处理了 X-Stainless 缺口？
   └─ 否 → ⚠️ 仅上真实 Codex CLI 流量，第三方客户端走 API key
   └─ 是 → 可以上 OAuth + 第三方

5. 是否有紧急回滚预案（git tag + 灰度配置）？
   └─ 否 → ⛔ 不要上号，封号风险无法收敛
   └─ 是 → 可以上

6. 是否做过 7 天小规模灰度测试？
   └─ 否 → ⛔ 不要上规模号，先 1-5 个号测试
   └─ 是 → 可以扩量
```

---

## 8. 上号检查表（一页式打印版）

### 通用
- [ ] 数据库 / Redis / 代理池就绪
- [ ] channel_monitor 运行 ≥24 小时
- [ ] health_score / 401/403/429 监控就绪
- [ ] 紧急回滚预案就绪（git tag + 灰度开关）

### Anthropic
- [ ] CLI 版本对齐（≥2.1.118）
- [ ] DefaultHeaders 抓包验证通过
- [ ] anthropic-beta 列表已清理（移除 no-op token）
- [ ] 住宅 IP 代理 1:1 至 1:3
- [ ] 1 个测试号跑 24h 通过

### OpenAI
- [ ] 决策方案（A/B/C）已选定
- [ ] X-Stainless 注入已实现 OR 仅放真实 Codex 流量
- [ ] OAuth callback sticky（多实例 + Redis）
- [ ] Egress Bucket + TLS production_mode 已配置
- [ ] Canary 探针通过
- [ ] 1 个测试号跑 24h 通过

### Gemini
- [ ] **GeminiCLI 版本已升到 0.40+**
- [ ] **x-goog-api-client / x-goog-user-project 已注入**
- [ ] Vertex Service Account 优先于 Personal OAuth
- [ ] 入站校验方案已选定
- [ ] thoughtSignature 安全策略生效
- [ ] 1 个测试号跑 72h 通过（Gemini 比其他平台多观察 48h）

### Antigravity
- [ ] User-Agent 版本号合理（默认 1.21.9 或 env 覆盖）
- [ ] OAuth scopes 完整
- [ ] 隐私设置 + 二次验证通过
- [ ] 1 个测试号跑 24h 通过

---

## 9. 常见误区

| 误区 | 真相 |
|------|------|
| "用 utls 就不会封" | TLS 指纹只是封号信号之一，header / 行为 / IP 同样重要 |
| "代理多了就安全" | 代理质量 > 代理数量；机房 IP 即使再多也容易封 |
| "仓库代码已经做完了" | 仓库常量需要持续维护，不是一次性的 |
| "几个号封了再说" | 风控有滚动窗口，等封了再处理已经晚了 |
| "OAuth 比 API key 安全" | 错。OAuth 账号有更多识别特征，错一处就封 |
| "我抓过包了所以没问题" | 抓包是起点不是终点；上游每周都在变 |

---

## 10. 联系与升级

如需要更深度的协助：

1. **抓包对齐**：参见 [08-traffic-capture-sop.md](./08-traffic-capture-sop.md)
2. **改进路线图**：参见 [05-improvement-roadmap.md](./05-improvement-roadmap.md)
3. **代码 Inventory**：参见 [06-implementation-inventory.md](./06-implementation-inventory.md)
4. **Reality Check**：参见 [00-reality-check.md](./00-reality-check.md)

> 上号池前请把本清单逐项打勾。
> 任何"留白"项都是潜在的封号源。
