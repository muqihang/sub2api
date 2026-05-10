# OpenAI Gateway Preflight

部署 OpenAI Gateway 前，先跑一轮最小化预检，确认：

- 应用基础健康状态正常
- `OpenAI Gateway Core` 已启用
- 新灰度门禁按计划开启；`gateway.openai_core.tls_binding.enabled`、`entity_orchestration.enabled`、`entity_profile_override.enabled` 默认均为 `false`
- `/_health` 与 `/_verify` 可访问
- 至少一个 OpenAI 账号能正常走 `/v1/responses`
- 生产环境没有意外走直连或未绑定出口桶
- 若启用 TLS binding，生产环境没有隐式明文 TLS 回退，且所有引用的 TLS profile 均存在
- 预检输出不暴露明文 upstream token 或 proxy URL
- 一旦发现不安全 JSON 输出，脚本会直接失败并退出

## 用法

```bash
chmod +x deploy/openai-gateway-preflight.sh

BASE_URL="https://api.example.com" \
ACCOUNT_ID="53" \
API_KEY="sk-xxx" \
GATEWAY_TOKEN="gw-xxx" \
MODEL="gpt-5.4" \
deploy/openai-gateway-preflight.sh
```

## 参数

- `BASE_URL`
  OpenAI Gateway 对外入口，例如 `https://api.example.com`
- `ACCOUNT_ID`
  用于 `/_verify` 的 OpenAI OAuth 账号 ID；不填则跳过 verify
- `API_KEY`
  用于 `/v1/responses` smoke test 的 Sub2API API Key；不填则跳过 smoke
- `GATEWAY_TOKEN`
  如果配置了 `gateway.openai_core.client_tokens`，这里填对应 token
- `MODEL`
  可选，默认 `gpt-5.4`
- `TIMEOUT_SECONDS`
  可选，默认 `20`

## 预期检查项

### 1. `/health`

应用基础健康检查，应返回 `200`。

### 2. `/openai/_health`

应能看到：

- `gateway_status`
- `oauth_status`
- `openai_oauth_accounts_total`
- `egress_buckets`
- `warning_codes`（出现告警时返回，例如 `bucket_concentration_high`、`direct_egress_in_production`、`direct_fallback_disabled`、`credential_storage_not_production_safe`）
- `degraded_reason`（若存在）

### 3. `/openai/_verify`

应能看到：

- `profile.profile_id`
- `egress_bucket`
- `proxy_selected`
- `proxy_label`、`proxy_hash` 或其他非明文代理标识
- 启用 `tls_binding.enabled=true` 后，应能看到 `tls.enabled`、`tls.profile_id`、`tls.profile_name`、`tls.profile_hash`、`tls.source`、`tls.cache_identity`、`tls.http_applicable`、`tls.ws_applicable`
- `requested_user_agent`

默认输出不得包含明文 proxy URL；只有显式 operator/debug 模式才允许展示明文连接信息。
TLS 输出只允许包含 profile ID、名称、hash、来源与 cache identity 等非敏感字段，不应暴露 TLS profile 的完整握手参数或代理明文。

### 4. `/v1/responses`

最小 smoke test，确认：

- API Key 可正常鉴权
- OpenAI 号池可正常调度
- 请求能打到上游并返回 JSON

### 5. 安全边界

上线前还应确认：

- `gateway.openai_core.tls_binding.enabled` 默认为关闭；回滚时关闭该门禁应恢复旧 HTTP/WS 行为，不应触发 stale hard-fail
- 启用 TLS binding 前，每个启用的 egress bucket 若 `tls.enabled=true`，必须配置 `profile_id`、`allow_default_fallback=true` 或 `allow_plain_fallback=true` 之一
- 生产模式启用 TLS binding 时，启用的 egress bucket 必须有显式 TLS policy，且不得设置 `tls.allow_plain_fallback=true`
- `tls.profile_id` 的静态形状由配置校验负责；profile 是否存在由服务/预检通过 `TLSFingerprintProfileService` 校验，不应把 DB 访问耦合进 config validation
- 账号级 OpenAI TLS override 只在 bucket 设置 `tls.allow_account_override=true` 时生效；不要复用 Anthropic 的 `enable_tls_fingerprint` / `tls_fingerprint_profile_id` 语义
- OpenAI OAuth / API-key 上游凭证已使用生产级 secret storage 或明确限制为内部测试
- OAuth callback/session 状态支持当前部署拓扑，或已强制 sticky 单实例 callback
- `egress_fail_closed`、`allow_account_proxy_fallback`、`allow_direct_fallback` 符合生产策略
- canonical route 与兼容 alias 的鉴权矩阵均通过
- WS 与 HTTP 使用同一账号 runtime、egress 与 token secrecy 规则

## 输出安全检查

`deploy/openai-gateway-preflight.sh` 会把每个接口的 JSON 响应单独捕获后再检查，而不是扫描整段终端输出。默认会拦截以下情况并直接失败；即使目标机没有 `jq`，也会走文本级的 fail-closed 检查：

- JSON 中出现 `access_token`、`refresh_token`、`id_token`、`api_key` 的非空明文值
- 响应里出现 `sk-...` 形式的 OpenAI key
- 响应里出现 `scheme://user:pass@host` 形式的代理凭证
- 响应里出现明文 `Bearer ...` token

字段名本身的文档说明不会触发失败；只有具体敏感值暴露才会失败。

## 上线前建议

正式切流前，建议至少做这 7 件事：

1. 选 3-5 个 OpenAI OAuth 账号做 canary
2. 至少验证 1 个 HTTP 请求和 1 个 WS / Codex 请求
3. 用 `/_verify` 检查这些账号的 `egress_bucket` 是否符合预期
4. 若启用 TLS binding，用 `/_verify` 检查 `tls.source`、`tls.profile_hash`、`tls.cache_identity` 是否符合 bucket/proxy/profile 预期
5. 确认没有走到直连回退或隐式明文 TLS 回退
6. 确认 `/_verify` 默认输出没有暴露明文 proxy URL
7. 确认每个 canary 账号的 bucket 绑定比例符合预期
8. 确认日志、usage 记录、admin snapshot 均未出现 upstream secret

## 典型告警码

- `bucket_concentration_high`
- `direct_egress_in_production`
- `direct_fallback_disabled`
- `account_proxy_fallback_disabled`
- `missing_egress_bucket`
- `disabled_egress_bucket`
- `credential_storage_not_production_safe`
- `oauth_session_topology_not_production_safe`
- `tls_policy_missing`
- `tls_policy_no_effective_profile`
- `tls_profile_not_found`
- `tls_profile_service_not_configured`
