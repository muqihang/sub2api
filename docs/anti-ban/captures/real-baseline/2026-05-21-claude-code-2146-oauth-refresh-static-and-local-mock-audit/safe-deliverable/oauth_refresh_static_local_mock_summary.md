# Claude Code 2.1.146 OAuth refresh static + local mock summary

- Static source: `/Users/muqihang/chelingxi_workspace/cc-gateway/src/oauth.ts`
- Static CLI strings: `/v1/oauth/token, https://platform.claude.com/v1/oauth/token, https://platform.claude.com/oauth/authorize, https://platform.claude.com/oauth/code/callback`
- Local mock request count: `2`
- First request method/url: `POST /success`
- First request body keys: `client_id, grant_type, refresh_token, scope`
- First request content-type: `application/json`
- Success access token present: `true`
- Failure contains 401: `true`

## Observations
- CC Gateway source defines TOKEN_URL as https://platform.claude.com/v1/oauth/token and uses POST application/json.
- Refresh request body keys are grant_type, refresh_token, client_id, scope.
- Success path maps access_token/refresh_token/expires_in to accessToken/refreshToken/expiresAt.
- Failure path throws on non-200 responses; inline refreshOAuthToken has no retry loop. scheduleRefresh handles delayed retry separately in source.
- Local mock executed an extracted-equivalent refresh function against localhost HTTPS only; no real platform.claude.com call was made.

## Safety
- No raw access token or raw refresh token is written to the safe deliverable.
- NODE_TLS_REJECT_UNAUTHORIZED=0 was scoped to the local mock process only.

> Safe deliverable contains only hashes, field names, and booleans. No raw refresh token or raw access token is included.