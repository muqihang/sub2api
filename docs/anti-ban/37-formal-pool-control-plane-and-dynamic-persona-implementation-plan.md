# 正式号池控制面上传与动态 Persona 实施计划

日期：2026-05-24  
状态：B2 Checkpoint 1-7 已全部 APPROVED；允许作为后续 Phase 0/Session Budget 对齐基线
Source of truth：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation`

## 0. 执行原则

本计划不是重新验证已成功的 `/v1/messages` 主链路。已验证的能力包括：

- Claude Code CLI-through 主链路；
- `claude-sonnet-4-6` / Opus 4.x Claude Code models；
- 1m context；
- tools；
- thinking；
- context_management；
- stream；
- `max_tokens=32000`；
- CC Gateway sign-primary / verifier / no fallback。

后续 localhost-only 和 canary 只用于**新增控制面上传路径**或**新增动态 persona 决策**，不是反复打同一种 messages canary。原则：

```text
已验证主链路：不重复 canary。
新增控制面 path：先 localhost replay，再单路径小 canary。
新增 persona/version family：先 resolver localhost，再单账号灰度。
生产放量：按 path/account/profile kill switch 分批打开。
```

## 0.1 控制面与 CCH 的硬边界

这是实现硬性不变量：

- CCH 仅用于 `/v1/messages` final-output 签名、billing block 和 verifier；
- 控制面 path 不生成、不复用、不校验 messages CCH；
- control-plane adapter 只能规范 UA、beta、session、persona headers 和安全审计字段；
- control-plane adapter 不能调用 messages CCH 生成链路；
- 控制面误带 CCH/billing marker 时必须 strip 或 reject，并记录 safe audit；外部输入默认 reject，内部兼容/迁移路径才允许 strip + audit；
- 控制面失败不得 fallback 到 messages/direct/legacy/sign-to-strip；
- `/v1/messages` 已验证的 CCH verifier 仍保持原行为，不因控制面 adapter 改动而降低安全性。


## 0.2 P0 追加硬约束：safe intent 不允许 plain hash 变成长期关联标识

这是 Checkpoint 1 前必须先落地的隐私边界。历史 B1 工具里曾出现 `query_hash: sha256:...`、`body_hash: sha256:...` 或 fallback intent 里直接对 query/body 做 SHA256 的设计；B2 实施时必须移除或替换。

正式规则：

- 禁止 plain SHA/MD5/长期 deterministic digest 用作 body/query/session/account 的正式审计或缓存关联字段；
- telemetry / eval / high-risk POST 不计算 raw body digest，不保存 raw body，不保存 raw prompt，不保存 raw telemetry；
- high-risk POST 只记录 `body_length_bucket`、`schema_summary`、`digest_omitted_reason`、`policy decision`、`path_template`、`classification`；
- GET query 只允许 path-specific allowlist 字段进入 `normalized_query`，例如经逐 path 批准的 `entrypoint`、`limit`、`version`、模型名等；
- account/org/user/session/project/email/token/dynamic id 等不得原样保存；
- 如果确实需要关联，只能使用 server-scoped opaque id，或 scoped keyed HMAC；HMAC 必须包含 `key_id`、`scope`、`version`，并支持轮换；
- fallback intent 不能比主路径更宽松。validation 失败时不能退回到带 plain sha256 的 fallback；无法安全摘要时必须 quarantine / fail closed；
- safe deliverable 不输出 raw token、Authorization、x-api-key、cookie、raw body、raw prompt、raw telemetry、raw CCH、account UUID、email、proxy credential。

`body_omitted_reason` 与 `digest_omitted_reason` 的命名规则：high-risk POST 使用 `body_omitted_reason` 表示没有保存/摘要 raw body；如果某个低风险字段级 digest 被批准，才使用 `digest_omitted_reason` 描述为什么没有计算该 digest。B2-P0 默认不引入 raw body digest。

推荐字段替换：

| 禁止字段 | 替代字段 | 说明 |
| --- | --- | --- |
| `query_hash` | `query_ref` 或 `query_omitted_reason` | `query_ref` 只能是 scoped keyed HMAC/opaque id |
| `body_hash` | `body_omitted_reason` + `body_length_bucket` + `schema_summary` | telemetry/eval 默认 omission，不做 digest |
| raw session id | `session_ref` + server-issued UUID-like session | production 默认不透传本机 raw session |
| fallback sha256 | quarantine reason | fallback 不能降级隐私策略 |

## 0.3 P0 追加硬约束：Opus / future Sonnet-Opus 不得机械阻断

已确认产品化示例和 canary cost envelope 曾有 only `claude-sonnet-4-6` 的风险。当前已知 Claude Code 1m-capable model family 必须包含：

- `claude-sonnet-4-6`；
- `claude-opus-4-7`；
- `claude-opus-4-7-thinking`；
- `claude-opus-4-6`；
- `claude-opus-4-6-thinking`。

动态 model/persona resolver 必须满足：

- 不因安全策略削弱 1m context、tools、thinking、context_management、stream、`max_tokens=32000`；
- 未来 trusted CLI-through 观测到 `claude-sonnet-4-8`、`claude-opus-4-8` 或后续 Sonnet/Opus 新版本时，不得 mechanical broad block；
- 新模型进入 `candidate_model_allowlist` / gray decision / per-model kill switch / audit / budget / rollout；
- untrusted spoof fail closed；trusted candidate 只做灰度；限制只能是流量、并发、账号预算、灰度比例、审计增强，不能降级模型能力；
- `config.example.yaml`、runtime productization manifest、tests 不得误导运维把 canary envelope 当成生产能力上限。

## 1. 为什么还需要 localhost-only 和单路径 canary

### 1.1 localhost-only 的作用

localhost-only 不访问 Anthropic/Claude 真实上游。它只验证：

- 请求是否被正确分类；
- raw token/body/prompt/telemetry 是否被阻断；
- safe intent schema 是否符合预期；
- cache key 是否隔离；
- dynamic persona 是否生成正确 final headers；
- spoof/unknown path 是否 fail closed；
- 1m/tools/thinking 是否没有被配置削掉。

这相当于上线前的单元/集成验收，不是消耗真实账号资源。

### 1.2 单路径真实 canary 的作用

控制面真实上传不同于 messages。每个 path 的语义、响应字段、缓存风险都不同。单路径 canary 只在以下情况需要：

- 第一次开放某个控制面 GET path 真实上游；
- 第一次开放某个新的 Claude Code version family；
- 第一次启用某个 unknown beta candidate；
- 第一次启用 synthetic telemetry。

不需要对已经通过的 `/v1/messages` 主链路反复 canary。单路径 canary 的目标是确认：

- 上游接受该控制面路径；
- 返回 schema 与 allowlist 一致；
- 不含 private/raw/token/account 泄露字段；
- 账号/路径/profile 熔断可用。

## 2. 对审查 P1/P2 的处理

审查代理给出的 P1/P2 是**实现计划必须纳入的约束**，不是阻断文档进入计划的 P0。

### 2.1 P1-1：raw session id 隐私边界

实现默认：

- 本机 raw `X-Claude-Code-Session-Id` 不直接作为正式号池上游 session；
- local guard / Sub2API 生成 server-issued UUID-like session id；
- 维护本机 session -> server session 的本地/服务端安全映射；
- safe audit 只记录 session scoped opaque id / scoped keyed HMAC；
- metadata.user_id.session_id 与 final header 一致。

例外：只有单独批准的真实 CLI-through 调试模式可保留 raw session 形态，但不得作为 production-session 默认。

### 2.2 P1-2：GET query allowlist

实现默认：

- 仅允许低风险 query 原样通过：`entrypoint`、`model`、`limit`、`version` 等经 path 逐项批准的字段；
- account/org/user/session/project/path/env 等动态值不得原样上传；
- 需要账号上下文的 query 由 selected pool account metadata 重建；
- 未知 query 进入 quarantine。

### 2.3 P1-3：dynamic persona 轻度限制不削能力

`observed_minor_drift` 的限制只能是：

- 并发；
- 流量百分比；
- session/account budget；
- 灰度账号池；
- 审计增强。

不得降低：

- 1m context；
- tools；
- thinking；
- context_management；
- stream；
- `max_tokens=32000`。

### 2.4 P1-4：guard attestation 实现规范

实现必须包含：

- key id；
- key rotation；
- HMAC/JWS 或 mTLS；
- timestamp；
- nonce；
- nonce 去重存储；
- clock skew 容忍窗口；
- method/path_template/body omission decision/session scoped id/policy_version；
- 外部同名 header 剥离测试；
- attestation 失败审计。

### 2.5 P1-5：account settings 更严格

`/api/oauth/account/settings` 不作为普通首批 GET 自动开放。它进入 B2-P1-sensitive：

- 默认 user-session 或 account+user partition；
- 不得 stale fallback；
- response 字段逐项 allowlist；
- private-field scan 必须 PASS；
- 单路径 canary 后才能灰度。

### 2.6 P2

- telemetry synthetic 默认关闭；启用必须单独批准，且不得含 raw telemetry/body/prompt；
- MCP registry 必须 TTL + schema/version 绑定；
- unknown beta candidate allowlist 作为运维对象维护。

## 3. 目标架构

```text
Claude Code CLI
  -> local guard / redacting forwarder
  -> messages: Sub2API messages route -> CC Gateway messages sign-primary -> Anthropic
  -> control-plane: safe intent -> Sub2API control-plane router
       -> policy / cache / quarantine
       -> optional CC Gateway control-plane adapter (headers/persona only; no messages CCH)
       -> selected pool account upstream, public cache, synthetic, suppress, or quarantine
```

## 4. Checkpoint 1：B2-P0 Safe Intent 中心化

目标：所有控制面请求都能以 safe intent 进入 Sub2API 中心策略；不真实上游。

### 4.1 文件范围

Worktree：

- `tools/cli_control_plane_intent.py`
- `tools/cli_control_plane_policy.py`
- `tools/cli_control_plane_guard.py`
- `tools/tests/test_cli_control_plane_intent.py`
- `tools/tests/test_cli_control_plane_guard.py`
- `backend/internal/handler/gateway_handler.go`
- `backend/internal/service/gateway_service.go`
- 新增：`backend/internal/service/control_plane_intent.go`
- 新增：`backend/internal/service/control_plane_intent_test.go`

### 4.2 任务

1. 定义 strict safe intent schema，字段必须为 allowlist，禁止 unknown fields。
2. 删除/拒绝 raw Authorization、cookie、x-api-key、raw body、raw prompt、raw telemetry、raw CCH。
3. 移除旧 `query_hash` / `body_hash` plain SHA 字段；使用 `query_ref`（scoped keyed HMAC/opaque only）或 `query_omitted_reason`。
4. high-risk POST 不计算 raw body digest；只记录 `body_omitted_reason`、`body_length_bucket`、`schema_summary`。
5. 所有可关联摘要必须是 server-scoped opaque id 或 scoped keyed HMAC，且带 `key_id` / `scope` / `version`；禁止 plain SHA/MD5/长期 deterministic hash。
6. fallback intent 必须 obey same schema/privacy rules；validation 失败不能退回到更宽松 schema，无法安全摘要时 quarantine / fail closed。
7. local guard 把控制面转成 safe intent；本地摘要和传给 Sub2API 的 intent 都不得包含敏感值。
8. Sub2API 新增 control-plane intent endpoint/router，并剥离外部伪造 intent/attestation headers。
9. B2-P0 中 router 只做 policy/cache/quarantine dry-run，不真实上游。
10. unknown path quarantine。
11. safe audit 只输出分类、path_template、长度 bucket、schema summary、omission reason、policy decision。

### 4.3 测试

- raw token/header/body 字段出现即拒绝；
- `query_hash`、`body_hash`、plain SHA/MD5/长期 deterministic digest 出现即失败；
- telemetry/eval body digest omission，且只保留 length bucket / schema summary / omission reason；
- fallback intent 不得包含 plain hash，不得比主路径更宽松；
- unknown route quarantine；
- safe intent 不进入 `/v1/messages` route；
- 控制面误带 CCH/billing marker 时 strip/reject，并不调用 messages CCH；
- safe audit sensitive scan PASS；
- external client 不能直接伪造 intent。

## 5. Checkpoint 2：Guard Attestation 与入口信任

目标：只有可信 local guard 产生的 intent/persona evidence 能触发动态 persona 或控制面中心决策。

### 5.1 文件范围

- `tools/cli_control_plane_guard.py`
- 新增：`tools/cli_guard_attestation.py`
- 新增：`tools/tests/test_cli_guard_attestation.py`
- `backend/internal/handler/gateway_handler.go`
- 新增：`backend/internal/service/control_plane_attestation.go`
- 新增：`backend/internal/service/control_plane_attestation_test.go`

### 5.2 任务

1. 定义 attestation payload。
2. 支持 key id / rotation。
3. 支持 timestamp / nonce / replay window。
4. nonce 去重存储。
5. 对 telemetry/eval/raw prompt/raw body 使用 digest_omitted_reason；不得为 raw body 生成 plain hash。
6. Sub2API 验证 HMAC/JWS 或等价内部机制。
7. 入口层剥离外部同名 attestation headers。
8. 失败审计。

### 5.3 测试

- valid attestation accepted；
- missing attestation rejected for dynamic persona/control-plane center decision；
- wrong key rejected；
- expired timestamp rejected；
- clock skew outside configured window rejected；
- replay nonce rejected；
- nonce TTL enforced；
- key rotation overlap window accepted only for configured key ids；
- external spoof header stripped/rejected；
- telemetry raw body digest forbidden；plain SHA/MD5 body/query digest forbidden。

## 6. Checkpoint 3：Dynamic Persona Registry / Resolver

目标：不再长期硬编码 2.1.150；可信 CLI-through 小版本漂移可兼容，伪造 fail closed。

### 6.1 文件范围

CC Gateway：

- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/config.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts`
- 新增：`/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-registry.ts`
- 新增：`/Users/muqihang/chelingxi_workspace/cc-gateway/src/persona-resolver.ts`
- 新增 tests：`/Users/muqihang/chelingxi_workspace/cc-gateway/tests/persona-registry.test.ts`
- 新增 tests：`/Users/muqihang/chelingxi_workspace/cc-gateway/tests/persona-resolver.test.ts`

Worktree：

- `backend/internal/service/cc_gateway_adapter.go`
- `backend/internal/service/cc_gateway_adapter_test.go`
- `tools/cli_runtime_productization.py`
- `tools/tests/test_cli_runtime_productization.py`
- `docs/anti-ban/runtime-productization/2026-05-24-cli-through/manifest.json`
- `docs/anti-ban/runtime-productization/2026-05-24-cli-through/cc-gateway.config.yaml`

CC Gateway docs/config：

- `/Users/muqihang/chelingxi_workspace/cc-gateway/config.example.yaml`

### 6.2 任务

1. 建立 profile registry。
2. 写入 `claude_code_2_1_150_subscription_1m`。
3. exact known profile 正常通过。
4. same minor drift 进入 `observed_minor_drift`。
5. minor drift 只能限制流量/并发/budget，不能削能力。
6. unknown major quarantine。
7. spoof/untrusted header fail closed。
8. unknown beta 必须 candidate allowlist + replay/fixture + kill switch，否则 quarantine / registry profile。
9. 新 Sonnet/Opus Claude Code model 必须进入 model resolver：trusted CLI-through 可进入 candidate_model_allowlist 灰度；限制只能是流量/并发/budget/灰度比例，不能削 1m/tools/thinking/context_management/stream/max_tokens；untrusted client 不得靠未知 model 绕过。
10. `config.example.yaml` 与 productized runtime manifest/config 必须列出已知 Opus 4.7/4.6 family，并明确 canary envelope 是本地 canary 护栏，不是生产能力上限。
11. `X-Stainless-*` 字段名和值 schema 双 allowlist。
12. `/v1/messages` verifier 检查 final UA/beta/session/CCH version 与 resolver decision 一致；control-plane persona mismatch fail closed，但不涉及 messages CCH。

### 6.3 测试

- 2.1.150 exact；
- 已知 Opus 4.7/4.6 model family 正常放行，且断言完整能力集不变：1m context、tools、thinking、context_management、stream、`max_tokens=32000`；
- 2.1.151 minor drift，且断言完整能力集不变：1m context、tools、thinking、context_management、stream、`max_tokens=32000`；
- unknown beta not allowlisted -> quarantine；
- unknown beta 即使格式合法，如果缺少 candidate allowlist、replay/fixture proof、kill switch 任一项 -> quarantine；
- unknown beta allowlisted + replay/fixture proof + kill switch + audit budget -> pass with audit；
- future trusted CLI-through `claude-sonnet-4-8` / `claude-opus-4-8` candidate model -> gray decision without capability downgrade，且断言完整能力集不变；
- untrusted unknown model -> reject or server policy, no spoof inheritance；
- untrusted spoof ignored/rejected；
- `/v1/messages` persona/CCH version mismatch fail closed；control-plane persona mismatch fail closed，但不涉及 messages CCH；control-plane path 经过 adapter 时不会生成 messages CCH；控制面误带 CCH/billing marker 时 strip/reject；`/v1/messages` CCH verifier 回归正常。

## 7. Checkpoint 4：Session Mapping 与 Session Budget 正式化

目标：正式号池默认不透传本机 raw session id，而是映射为 server-issued UUID-like session。

### 7.1 文件范围

- `tools/cli_session_budget.py`
- `tools/tests/test_cli_session_budget.py`
- 新增：`backend/internal/service/claude_code_session_mapper.go`
- 新增：`backend/internal/service/claude_code_session_mapper_test.go`
- `backend/internal/service/cc_gateway_adapter.go`

### 7.2 任务

1. raw local session -> server-issued UUID-like session。
2. mapping 使用 scoped opaque id/HMAC，不记录 raw session。
3. metadata.user_id.session_id 与 final header 一致。
4. session budget 以 server session 为单位。
5. 不同用户/session 隔离。

### 7.3 测试

- UUID-like 输出；
- raw session 不落 audit；
- metadata/header 一致；
- user A/B 隔离；
- budget 不因 session mapping 绕过。

## 8. Checkpoint 5：Control-plane Cache / Quarantine / Kill Switch

目标：为后续真实 GET 上传准备生产控制面，不直接放开上游。

### 8.1 文件范围

- 新增：`backend/internal/service/control_plane_policy.go`
- 新增：`backend/internal/service/control_plane_cache.go`
- 新增：`backend/internal/service/control_plane_quarantine.go`
- 新增：`backend/internal/service/control_plane_policy_test.go`
- 新增：`backend/internal/service/control_plane_cache_test.go`
- 新增：`backend/internal/service/control_plane_quarantine_test.go`

### 8.2 任务

1. path policy matrix。
2. query allowlist。
3. query canonicalization：重复参数、数组、大小写、percent-encoding、空值、排序、未知嵌套字段必须有 deterministic 规则；未知或非规范 query quarantine。
4. cache key scoped HMAC，组成至少包含 path_template、normalized_query、account_scope、user/session partition、persona/profile、model/version、schema_version。
5. user_partition 强制规则。
6. stale-safe 规则。
7. path/account/profile kill switch。
8. response schema allowlist 框架。
9. private-field scan 框架。

### 8.3 测试

- account A cache 不给 account B；
- user A cache 不给 user B；
- account settings 不 stale fallback；
- 401/403/429/risk/schema drift 熔断；
- query unknown quarantine；
- query canonicalization 对重复参数、percent-encoding、空值、排序行为稳定；
- cache key 包含 path_template、normalized_query、account_scope、user/session partition、persona/profile、model/version、schema_version；
- kill switch 生效。

## 9. Checkpoint 6：B2-P1 首批 GET 路径灰度准备

目标：准备逐 path 开放，但不批量真实上游。

### 9.1 首批候选

低风险优先：

1. `GET /api/claude_cli/bootstrap?...`
2. `GET /api/claude_code_*` 候选族（不得作为生产通配 allowlist；上线前必须枚举具体 `path_template`）
3. `GET /mcp-registry/*` 候选族（不得作为生产通配 allowlist；上线前必须枚举具体 `path_template` 或严格 prefix policy）

敏感路径单独队列：

4. `GET /api/oauth/account/settings`
5. `GET /v1/mcp_servers`

### 9.2 每个 path 的上线门槛

- localhost replay；
- A/B diff；
- schema allowlist；
- query allowlist；
- cache scope；
- TTL；
- stale-safe 或 no-stale；
- kill switch；
- private-field scan；
- 单账号单路径真实 canary，需用户单独批准。

## 10. Checkpoint 7：验证与安全扫描

必须运行：

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation
PYTHONPATH=. python3 -m unittest discover -s tools/tests -v

cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/backend
go test ./internal/pkg/oauth ./internal/repository -run 'BuildAuthorizationURL|OAuth|Proxy|Refresh' -count=1 -timeout=120s
go test ./internal/service ./internal/handler ./internal/server/routes -run 'InferenceScope|CCGateway|ControlPlane|EventLogging|LocalCapture|OAuth|GatewayForward|ExplicitCanary|Adapter|AnthropicAPIKey|StrictPassthrough|JointLocalCaptureAcceptanceArtifact|ControlPlane|Persona|Session' -count=1 -timeout=180s

cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
```

还必须执行 safe deliverable sensitive scan，确认无：

- raw token；
- raw Authorization；
- x-api-key；
- cookie；
- raw prompt；
- raw body；
- raw telemetry；
- raw CCH；
- account UUID；
- email；
- raw account/org/user/session/project id；
- proxy credential；
- plain deterministic body/query/session/account hash；
- fallback intent 中的 plain SHA/MD5。

扫描范围必须覆盖 safe deliverable、audit logs、fixtures、test snapshots、cache entries；不能只扫最终报告。B2-P0 测试必须注入 forbidden-network transport 或 fake upstream，任何真实 Anthropic/Claude 网络调用即失败。

## 11. 实施顺序建议

推荐主控拆成 4 个实施批次：

### 批次 A：B2-P0 skeleton

- safe intent schema；
- Sub2API control-plane router；
- quarantine；
- no real upstream。

### 批次 B：信任与 persona

- guard attestation；
- dynamic persona registry/resolver；
- spoof fail closed。

### 批次 C：正式号池隔离能力

- session mapping；
- session budget；
- cache/quarantine/kill switch。

### 批次 D：首批 GET path 准备

- bootstrap / feature flags / registry；
- account settings 与 MCP servers 只做更严格准备，不直接开放。

每个批次完成后派审查代理复审，发现 P0/P1 必须修完再进入下一批。审查必须额外检查：

1. 是否还有 plain SHA/MD5 body/query digest；
2. telemetry/eval 是否仍可能上传 raw body；
3. fallback intent 是否比主路径更宽松；
4. 控制面是否误用了 messages CCH；
5. Opus 4.x / future Sonnet-Opus 是否被机械阻断；
6. 1m/tools/thinking/context_management/stream/max_tokens 是否被削弱；
7. safe deliverable 是否无敏感信息。

## 12. 非目标

本计划不直接执行真实请求。  
本计划不批量开放控制面真实上游。  
本计划不改变已成功的 messages 主链路能力。  
本计划不 git add / commit，除非用户单独要求。

## 13. 完成定义

实现计划完成后应达到：

- 所有控制面都有 safe intent 中心化入口；
- unknown 控制面 quarantine；
- dynamic persona 不硬编码 2.1.150；
- 可信小版本漂移兼容，不削能力；
- 伪造 header fail closed；
- raw token/body/prompt/telemetry 不上传、不落盘；
- session/account/user/cache 隔离；
- 可按 path/account/profile/model 熔断；
- 已知 Opus 4.x 不被 canary/productized runtime 机械阻断；
- future trusted Sonnet/Opus 进入 candidate/gray 流程而非 broad block；
- 具备逐 path 开放真实 GET 的条件，但不自动放开。
