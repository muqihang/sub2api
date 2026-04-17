# OpenAI Gateway Preflight

部署 OpenAI Gateway 前，先跑一轮最小化预检，确认：

- 应用基础健康状态正常
- `OpenAI Gateway Core` 已启用
- `/_health` 与 `/_verify` 可访问
- 至少一个 OpenAI 账号能正常走 `/v1/responses`

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
- `degraded_reason`（若存在）

### 3. `/openai/_verify`

应能看到：

- `profile.profile_id`
- `egress_bucket`
- `egress_proxy_url`
- `requested_user_agent`

### 4. `/v1/responses`

最小 smoke test，确认：

- API Key 可正常鉴权
- OpenAI 号池可正常调度
- 请求能打到上游并返回 JSON

## 上线前建议

正式切流前，建议至少做这 4 件事：

1. 选 3-5 个 OpenAI OAuth 账号做 canary
2. 至少验证 1 个 HTTP 请求和 1 个 WS / Codex 请求
3. 用 `/_verify` 检查这些账号的 `egress_bucket` 是否符合预期
4. 确认没有走到直连回退
