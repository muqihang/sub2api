# Gemini Preflight

部署 Gemini v0.3 硬化前，先跑一轮最小预检，确认：

- `gemini.production_mode`、`gemini.token_cache_mode`、sticky/session 相关门禁符合预期
- `/api/v1/admin/gemini/health` 与 `/api/v1/admin/gemini/verify` 可访问
- 账号 runtime contract、`project_id` 状态、tier 状态、token-cache 状态都能被运维看见
- 预检输出不暴露 `access_token`、`refresh_token`、`api_key`、`service_account_json`
- 若提供用户 API Key，可额外确认 `/v1beta/models` 至少可访问

## 用法

```bash
chmod +x deploy/gemini-preflight.sh

BASE_URL="https://api.example.com" \
ADMIN_HEADER_NAME="Authorization" \
ADMIN_HEADER_VALUE="Bearer admin-token" \
ACCOUNT_ID="7" \
deploy/gemini-preflight.sh
```

## 参数

- `BASE_URL`
  对外入口，例如 `https://api.example.com`
- `ADMIN_HEADER_NAME`
  访问 admin 路由使用的 header 名，默认 `Authorization`
- `ADMIN_HEADER_VALUE`
  访问 admin 路由使用的 header 值，必填
- `ACCOUNT_ID`
  用于 `/api/v1/admin/gemini/verify` 的 Gemini 账号 ID；必填
- `USER_API_KEY`
  可选。若提供，则额外对 `/v1beta/models` 做一轮 smoke test
- `TIMEOUT_SECONDS`
  可选，默认 `20`

## 预期检查项

### 1. `/api/v1/admin/gemini/health`

应返回：

- `gateway_status`
- `oauth_status`
- `gemini_accounts_total`
- `accounts_by_family`
- `policy.production_mode`
- `policy.token_cache_mode`
- `policy.session_store`
- `warning_codes`

若 `warning_codes` 非空，应逐条确认是否符合预期，例如：

- `memory_session_store`
- `token_cache_plaintext_detected`
- `google_one_default_tier_fallback`
- `missing_required_project_id`

### 2. `/api/v1/admin/gemini/verify`

应返回：

- `runtime_contract`
- `project_id_status`
- `project_id_reason`（若 `project_id_status=unreadable`，必须采集）
- `tier_status`
- `token_cache_state` / `token_cache_reason`
- `oauth_state` / `oauth_reason`
- `session_store`
- `sticky_session_safety_required`

默认输出不应包含：

- `access_token`
- `refresh_token`
- `api_key`
- `service_account_json`
- `private_key`

### 3. `/v1beta/models` 可选 smoke

若提供 `USER_API_KEY`，脚本会请求 `/v1beta/models`。这一步只确认：

- 用户 API Key 能通过 Google/Gemini 路由鉴权
- 基础 Gemini surface 可访问

## Stop Conditions

发现以下任一情况，应停止切流：

- `gateway_status=degraded` 且原因不在预期内
- `invalid_runtime_contract`
- `project_id_status=required_missing`
- `token_cache_state=degraded`
- `oauth_state=degraded` 且原因未知
- 输出中出现明文 token / key / service-account JSON

## 说明

- 当前 preflight 以 admin surface 为主，不要求公开 `/gemini/_health` 或 `/gemini/_verify`
- `gemini.token_cache_mode=encrypted` 在本阶段代表“禁用 Gemini access-token 明文缓存”，不是已实现加密缓存
