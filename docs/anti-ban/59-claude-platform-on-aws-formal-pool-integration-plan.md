# 59 - Claude Platform on AWS Formal-Pool Integration Plan

> **For agentic workers:** This is a design and execution plan for adding **Claude Platform on AWS** as a separate Anthropic-compatible upstream in Sub2API and CC Gateway. Implement task-by-task with TDD and review gates. Do not implement this by modifying the existing Bedrock card/path in-place.

**Goal:** Add a new **Claude Platform on AWS** account/card that can import multiple `anthropic_workspace_id` workspaces, bind each workspace to its own egress proxy, and route native Claude formal-pool traffic safely through Sub2API -> CC Gateway -> Claude Platform on AWS.

**Recommended baseline:** API-key authentication first, with `x-api-key` as the Phase 1 upstream auth header because the current Anthropic Claude Platform on AWS documentation says `apiKey` / `ANTHROPIC_AWS_API_KEY` resolves to `x-api-key`. SigV4 is a later gated phase because final SigV4 signing must happen after CC Gateway body/header rewrite and final verifier.

**Tech stack / working roots:**

- Sub2API only under `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`.
- CC Gateway only under `/Users/muqihang/chelingxi_workspace/cc-gateway` main/c37a234+; do not use `cc-gateway/.worktrees/claude-code-2173-main`.
- Docs 56 and 58 remain mandatory safety boundaries for formal-pool native Claude traffic.
- Doc 59 implementation MUST land after, or explicitly rebase on, the latest passing doc 58 safety substrate. In practice, do not start production-path Checkpoint 4+ until doc 58 trusted context/profile fields, session ledger, final verifier, `strip_attribution` default, and signed-CCH fail-closed gates are stable and targeted tests are green. Additive account/UI work may be prepared earlier only if feature-flagged and not enabled for formal-pool production.

## 1. Confirmed external facts

Official AWS Claude Platform documentation establishes these facts:

- Claude Platform on AWS uses the Anthropic Messages API (`/v1/messages`) and differs from first-party Claude mainly by base URL, authentication method, and the required `anthropic-workspace-id` header. See [AWS Making requests](https://docs.aws.amazon.com/claude-platform/latest/userguide/making-requests.html).
- User-provided target endpoint for this rollout is `https://aws-external-anthropic.us-east-1.api.aws`; therefore the initial configured `aws_region` must be `us-east-1`, and the workspace must be a workspace bound to `us-east-1`. The raw workspace ID is not recorded in this plan; only the `wrkspc_...` format and a safe workspace ref may appear in evidence.
- The regional endpoint shape is `https://aws-external-anthropic.<region>.api.aws`. See [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html).
- Each data-plane request must include `anthropic-workspace-id`; SDKs can read it from `ANTHROPIC_AWS_WORKSPACE_ID`, and base Anthropic clients must pass the header explicitly. See [AWS Workspaces](https://docs.aws.amazon.com/claude-platform/latest/userguide/workspaces.html).
- Workspaces are region-scoped. The workspace ID belongs to the same ARN resource namespace used by IAM: `arn:aws:aws-external-anthropic:{region}:{account-id}:workspace/{workspace-id}`. See [AWS Workspaces](https://docs.aws.amazon.com/claude-platform/latest/userguide/workspaces.html).
- Claude Platform on AWS supports IAM SigV4 and API-key authentication. For SigV4, the service name is `aws-external-anthropic`, and the SigV4 region must match the endpoint region. See [AWS Making requests](https://docs.aws.amazon.com/claude-platform/latest/userguide/making-requests.html) and [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html).
- Current Anthropic Claude Platform on AWS documentation states the platform-specific SDK credential precedence as `apiKey` constructor argument -> `x-api-key` header and `ANTHROPIC_AWS_API_KEY` -> `x-api-key` header. The AWS User Guide also describes API-key authentication in terms of bearer-token authorization. To avoid an unsafe/wrong hard-code, Phase 1 MUST use `x-api-key` as the default Anthropic-SDK-compatible mode and keep `Authorization: Bearer` as an explicit compatibility profile only after mock/oracle evidence confirms it for the target account. See [Anthropic Claude Platform on AWS](https://platform.claude.com/docs/en/build-with-claude/claude-platform-on-aws).
- API keys for this service are not standard Claude Console keys and not Bedrock API keys. AWS docs state Claude Platform on AWS API keys are generated under AWS Console -> Claude Platform on AWS -> API keys, and Bedrock API keys do not work for this endpoint. See [AWS Authentication](https://docs.aws.amazon.com/claude-platform/latest/userguide/authentication.html).

## 2. Current local code observations

CodeGraph/source inspection confirms the screenshot is the existing **AWS Bedrock** path, not Claude Platform on AWS:

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/domain/constants.go` defines `AccountTypeBedrock = "bedrock"` and `DefaultBedrockModelMapping`, which maps Anthropic model names to Bedrock model IDs.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/bedrock_signer.go` signs requests with SigV4 service name `bedrock`, not `aws-external-anthropic`.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend/internal/service/bedrock_request.go` prepares Bedrock-specific request bodies, including Bedrock `anthropic_version` conversion and unsupported-field stripping.
- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/frontend/src/components/account/CreateAccountModal.vue` currently has an Anthropic account card named `AWS Bedrock` with `SigV4 / API Key` modes. This card should remain Bedrock-only.
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/proxy.ts` currently models formal-pool account context with provider/account/token/credential/egress/policy/session/profile fields but no `workspace_ref`, AWS endpoint kind, or auth scheme.
- `/Users/muqihang/chelingxi_workspace/cc-gateway/src/rewriter.ts` currently preserves `authorization` for OAuth or `x-api-key` for API-key upstreams; it does not yet know provider-specific Claude Platform on AWS workspace injection or how to ensure the upstream API-key header is selected only from verified server-owned credentials.
- Current Sub2API account type validation and frontend typings are not generic enough for a new Anthropic type without explicit changes: `backend/internal/domain/constants.go`, `backend/internal/handler/admin/account_handler.go`, and `frontend/src/types/index.ts` need first-class `claude-platform-aws` support.
- Current Sub2API formal-pool predicates primarily target OAuth/setup-token accounts. `IsFormalPoolAccount`, `serviceFormalPoolAccount`, and `formalPoolRuntimeReplayEligible` must be extended via a dedicated formal-pool eligibility predicate rather than by treating the new type as ordinary `apikey` or Bedrock.
- Current Sub2API -> CC Gateway attestation serialization must be verified for canonical parity with CC Gateway's sorted JSON verifier before any new fields are added; otherwise the new context can be rejected even when semantically correct.
- Current CC Gateway runtime registration/config/persistence schema has no provider/workspace/upstream endpoint fields, and current upstream-safety allowlisting does not allow `aws-external-anthropic.<region>.api.aws`; both must be extended explicitly.

Conclusion: the current Bedrock card/path is not the same service and must not be overloaded.

## 3. User requirements restated

1. Add a new UI card: **Claude Platform on AWS**.
2. Do not break existing Anthropic OAuth, Setup Token, Claude Console API-key, or AWS Bedrock login/add-account flows.
3. Support multiple `anthropic_workspace_id` values.
4. Each workspace entry must have a selected egress proxy.
5. In formal-pool production, authority must remain server-owned per doc 56:
   - Sub2API strips/rejects user-forged authority headers.
   - Sub2API generates trusted context from server scheduler state only.
   - CC Gateway independently verifies account, credential, egress, persona/profile, session, metadata identity, billing/CCH, and final output.
6. Incorporate doc 58:
   - `observed_client_profile` is audit-only.
   - `trusted_egress_profile_ref` and profile fields are authoritative.
   - Default shared formal-pool remains `strip_attribution`.
   - `signed_cch` / sign-primary remain fail-closed unless oracle/profile gates explicitly pass.
7. Do not log or persist evidence containing API keys, tokens, cookies, Authorization, raw prompt/body/response/telemetry/CCH, raw account identity, proxy credentials, raw workspace IDs in evidence, or raw HMAC inputs.

## 4. Core design decision

### 4.1 Create a separate account type

Add a new Anthropic-platform account type:

```text
platform = "anthropic"
type     = "claude-platform-aws"
```

User-facing label:

```text
Claude Platform on AWS
```

This type is distinct from:

- `oauth` / `setup-token`: first-party Claude Code OAuth-like accounts.
- `apikey`: first-party Claude Console API keys.
- `bedrock`: Amazon Bedrock Runtime.
- `service_account`: Vertex Anthropic.
- `upstream`: generic base-url passthrough.

### 4.2 API-key MVP, SigV4 gated phase

Phase 1 implements Claude Platform on AWS API-key mode only:

- It is compatible with the current Anthropic SDK documented Claude Platform on AWS path, where API-key auth resolves to `x-api-key`.
- It avoids adding a final SigV4 signer to CC Gateway before the formal-pool rewrite/final-verifier path is fully covered.
- It is enough to validate the new account model, workspace header injection, proxy assignment, and formal-pool safety boundary.

Phase 2 adds SigV4 only after targeted tests prove signing happens after CC Gateway final body/header rewrite and uses service `aws-external-anthropic` with endpoint-region equality.

### 4.3 One workspace is one schedulable account identity

For correctness and sticky scheduling, treat each `anthropic_workspace_id` as a distinct Sub2API account row and a distinct CC Gateway account identity.

Multiple workspaces can be added in one UI operation, but the backend should create one account row per workspace:

```text
Claude Platform on AWS import batch
  row 1: workspace A + region + credential + proxy X -> account A
  row 2: workspace B + region + credential + proxy Y -> account B
  row 3: workspace C + region + credential + proxy Z -> account C
```

This keeps quota, concurrency, scheduler state, sticky session binding, egress proxy, healthcheck status, and evidence simple and auditable.

Implementation MUST introduce a formal-pool eligibility helper, for example:

```go
func IsClaudePlatformAWSFormalPoolAccount(account *Account) bool
func IsFormalPoolEligibleAccount(account *Account) bool
```

`IsFormalPoolEligibleAccount` should include existing OAuth/setup-token formal-pool accounts and `claude-platform-aws` accounts only when required safe refs, workspace ref, credential binding, proxy/egress binding, and profile fields are present. This avoids accidentally making every Anthropic API-key account formal-pool eligible.

## 5. Data model

### 5.1 Credentials stored by Sub2API

For `type = "claude-platform-aws"`, store credentials with this shape:

```json
{
  "auth_mode": "apikey",
  "api_key": "<secret; never log>",
  "aws_region": "us-west-2",
  "anthropic_workspace_id": "<workspace id; redact in evidence>",
  "base_url": "https://aws-external-anthropic.us-west-2.api.aws"
}
```

Validation rules:

- `auth_mode` must be `apikey` in Phase 1.
- `api_key` must be non-empty and must never be returned in ordinary API responses except explicit admin backup/export paths that already expose credentials by design.
- `aws_region` must be a valid AWS region string and must equal the region embedded in `base_url`.
- `base_url` must be derived from `aws_region` in production. Do not allow arbitrary hosts in formal-pool production.
- `anthropic_workspace_id` must match the AWS documented tagged workspace form, e.g. `wrkspc_` followed by alphanumeric characters. Do not write real workspace IDs into logs or evidence.
- `proxy_id` is required for this account type.

Redaction/storage rules:

- Add `anthropic_workspace_id` to backend and DTO credential redaction helpers, or store the raw workspace ID only in the same sensitive credential map as `api_key` while exposing only `anthropic_aws_workspace_ref` in ordinary DTOs.
- Admin backup/export paths that intentionally export raw credentials must be documented as sensitive exports and must not be used as evidence artifacts.
- Logs, ops errors, healthcheck results, and formal-pool evidence must show only `workspace_ref`, region, endpoint ref, booleans, and status codes.

### 5.2 Safe refs stored in account extra

Generate safe refs from server-owned secrets, never from client-supplied headers:

```json
{
  "cc_gateway_account_ref": "account:<safe-ref>",
  "cc_gateway_credential_ref": "credential:<safe-ref>",
  "cc_gateway_credential_binding_hmac": "hmac-sha256:<safe-ref>",
  "cc_gateway_egress_bucket": "egress:<safe-ref>",
  "anthropic_aws_workspace_ref": "workspace:<safe-ref>",
  "anthropic_aws_endpoint_ref": "endpoint:<safe-ref>",
  "anthropic_aws_region": "us-west-2",
  "anthropic_aws_auth_scheme": "x_api_key"
}
```

Raw `anthropic_workspace_id`, raw API key, and raw HMAC inputs must remain outside logs/evidence. Safe refs are acceptable in evidence.

### 5.3 CC Gateway runtime/config identity

Extend CC Gateway account identity records with provider/upstream metadata:

```ts
type AccountIdentityConfig = {
  // existing fields
  device_id: string
  account_ref?: string
  credential_ref?: string
  credential_binding_hmac?: string
  token_type?: 'oauth' | 'apikey'
  persona_variant: string
  session_policy: 'preserve_downstream_session_id'
  policy_version: string

  // new fields for Claude Platform on AWS
  provider_kind?: 'anthropic_first_party' | 'claude_platform_aws'
  upstream_auth_scheme?: 'x_api_key' | 'bearer_api_key' | 'sigv4'
  aws_region?: string
  upstream_base_url?: string
  workspace_ref?: string
  workspace_binding_hmac?: string
  // raw workspace id may exist only in sensitive runtime/config storage,
  // not in evidence/log output.
  anthropic_workspace_id?: string
}
```

For Phase 1, `upstream_auth_scheme = "x_api_key"` for Claude Platform on AWS. CC Gateway receives the selected credential through the protected internal API-key path, verifies the credential binding, then emits `x-api-key: <selected credential>` to AWS. It must not forward client-supplied `Authorization` or `x-api-key`. A `bearer_api_key` mode may be added only as a separate compatibility profile after mock/oracle evidence proves it is accepted for the target account.

## 6. Request flow

### 6.1 Admin import flow

```text
Admin UI
  -> selects "Claude Platform on AWS"
  -> enters one or more workspace rows
       workspace_id + region + api_key + proxy_id + optional name/concurrency/group
  -> Sub2API validates all rows
  -> Sub2API creates one account per row
  -> Sub2API generates safe refs and credential/workspace bindings
  -> Sub2API registers/updates CC Gateway runtime identity for each account
```

Batch creation should be all-or-nothing if practical. If the existing repository layer makes full transactionality too invasive, the UI/API must return a per-row result and mark partial success explicitly; it must not silently drop failed workspace rows.

Concrete API choice for implementation planning:

- Preferred: add a dedicated admin batch route/service, for example `POST /admin/accounts/claude-platform-aws/batch`, that validates every row first, then creates accounts. If the repository layer can provide a transaction, use all-or-nothing semantics.
- Acceptable fallback: the frontend may call a single-row create API per workspace only if the response is surfaced as explicit per-row success/failure and no row is silently ignored. This fallback is not enough for formal-pool production automation unless retry/idempotency and safe cleanup semantics are documented.
- Do not overload the existing `POST /admin/accounts` single-row API with an ambiguous multi-row payload.

### 6.2 Formal-pool request flow

```text
User / unmodified Claude Code CLI
  -> Sub2API / Server API
     -> authenticate end user
     -> strip/reject user-forged authority headers
     -> scheduler selects one claude-platform-aws account row
     -> sticky session binds account + credential + workspace + proxy + profile tuple
     -> trusted context is signed from server state
  -> CC Gateway
     -> verifies attested context
     -> resolves account identity and workspace_ref
     -> verifies credential/account/workspace binding
     -> verifies egress allowlist and proxy_identity_ref
     -> applies doc 58 persona/profile/billing/CCH policy
     -> rewrites metadata.user_id/session/header/body
     -> injects exactly one server-owned anthropic-workspace-id header
     -> emits the selected API key using the verified AWS Claude Platform auth scheme
     -> final verifier before upstream
  -> https://aws-external-anthropic.<region>.api.aws/v1/messages
     through the selected proxy
```

### 6.3 Direct non-formal-pool path

If a non-formal-pool admin test route or account test route sends directly from Sub2API to AWS, it may inject `anthropic-workspace-id` directly from the selected server-owned account. That path is not a substitute for production formal-pool traffic. Production native CLI formal-pool traffic must go through CC Gateway when doc 56/58 safety is required.

The direct path still needs a dedicated dispatcher/builder for `type = "claude-platform-aws"`. It must not reuse the existing first-party Anthropic `apikey` passthrough blindly because that path does not validate workspace/region/base URL and may reject unknown account types in token selection. The builder must:

- derive `https://aws-external-anthropic.<region>.api.aws/v1/messages`;
- inject server-owned `anthropic-workspace-id`;
- use `x-api-key` for Phase 1;
- keep native Claude model IDs unchanged;
- avoid Bedrock model mapping/body conversion.

## 7. Header and authority rules

### 7.1 Inbound from end users

Sub2API must strip or reject these end-user-supplied headers before any trusted context is built:

- `x-cc-*`
- `x-sub2api-*`
- `authorization`
- `x-api-key`
- `anthropic-workspace-id`
- `x-amz-*`
- persona/context/runtime/account/credential/egress/policy/session headers
- any future header that can influence account, credential, workspace, proxy, persona, policy, billing/CCH, or session identity

For native Claude Code users, this does not break the client. The user client is not the authority for the AWS workspace. Sub2API/CC Gateway re-inject the correct workspace header after scheduler selection.

### 7.2 Sub2API -> CC Gateway internal headers

Sub2API may send internal selected credential material only after scheduler selection and only through the existing protected CC Gateway path. CC Gateway must:

- treat raw credential headers as sensitive;
- verify them against the account's `credential_binding_hmac`;
- never log them;
- rewrite them into the correct upstream auth shape;
- strip all CC/Sub2API internal headers before AWS egress.

### 7.3 CC Gateway -> AWS upstream headers

For Phase 1 API-key mode, final upstream headers must include:

```text
x-api-key: <selected AWS Claude Platform API key>
anthropic-workspace-id: <server-owned workspace id>
anthropic-version: 2023-06-01
content-type: application/json
```

`Authorization: Bearer <selected credential>` is not the Phase 1 default. It may be enabled only as a separately named compatibility profile after targeted evidence confirms it for Claude Platform on AWS in our request path.

The final request must not include:

- `x-cc-*`
- `x-sub2api-*`
- user-supplied `anthropic-workspace-id`
- user-supplied `authorization` or `x-api-key`
- raw CCH/billing material when `strip_attribution` is selected
- proxy credentials in headers or evidence

## 8. Formal-pool attestation extensions

Extend the doc 56/58 attested context with these authority fields:

```json
{
  "provider_kind": "claude_platform_aws",
  "upstream_auth_scheme": "x_api_key",
  "aws_region": "us-west-2",
  "upstream_endpoint_ref": "endpoint:<safe-ref>",
  "workspace_ref": "workspace:<safe-ref>",
  "workspace_binding_ref": "workspace-binding:<safe-ref>",

  "account_id": "account:<safe-ref>",
  "credential_ref": "credential:<safe-ref>",
  "egress_bucket": "egress:<safe-ref>",
  "proxy_identity_ref": "proxy:<safe-ref>",
  "policy_version": "...",
  "persona_profile": "...",
  "trusted_egress_profile_ref": "strip_attribution",
  "profile_policy_version": "...",
  "billing_shape_policy": "strip",
  "request_shape_profile_ref": "...",
  "cache_parity_profile_ref": "...",
  "observed_client_profile": { "safe_summary_only": true },
  "session_id": "<canonical session ref>",
  "timestamp_ms": 0,
  "nonce": "<safe nonce>"
}
```

CC Gateway must bind `provider_kind`, `upstream_auth_scheme`, `aws_region`, `workspace_ref`, and `upstream_endpoint_ref` into its session authority ledger. A same formal-pool session must fail closed if any of these change, just like account, credential, egress, persona, policy, device, or billing profile changes.

Canonical serialization requirement:

- Before adding these fields, Sub2API and CC Gateway must agree on the exact canonical JSON used for HMAC/signature verification.
- Add shared fixture vectors for old and new contexts.
- Sub2API tests must prove the byte string signed for the context matches CC Gateway's sorted/canonical verifier, not merely Go's default `json.Marshal` map order.

## 9. CC Gateway requirements

For Claude Platform on AWS, CC Gateway must independently enforce:

1. `mode: sub2api` for shared formal-pool production.
2. Account identity exists for the selected `account_id`.
3. `provider_kind = claude_platform_aws` matches the selected account identity.
4. `credential_ref` and selected credential binding match the account identity.
5. `workspace_ref` and workspace binding match the account identity.
6. `aws_region` matches the configured endpoint host.
7. `upstream_base_url` is allowlisted as `https://aws-external-anthropic.<region>.api.aws`.
8. `egress_bucket` allows the account and has a proxy identity; no proxy means fail closed for this type.
9. Persona/profile/billing fields from doc 58 match the attested context.
10. `metadata.user_id` and `X-Claude-Code-Session-Id` are rewritten/verified from server-owned session/account/device refs.
11. `anthropic-workspace-id` is injected from CC Gateway trusted account identity or sensitive runtime mapping, not from user input.
12. Final verifier confirms no internal control headers or forbidden billing/CCH material remain.
13. Evidence and raw capture files use only safe summaries: header names, booleans, lengths, schema summaries, safe refs.
14. Runtime registration and runtime mapping persistence include provider/workspace/upstream fields in `RuntimeRegisterRequest`, `RuntimeMappingRecord`, conflict comparison, replay, and redacted diagnostics. Unknown extra provider/workspace fields must not be silently ignored.
15. Upstream safety allows `aws-external-anthropic.<region>.api.aws` only when `provider_kind = claude_platform_aws`, `aws_region` matches the endpoint region, and the formal-pool account identity matches the workspace/endpoint refs.
16. Final verifier explicitly checks `anthropic-workspace-id` presence/source, upstream endpoint host/region, selected auth scheme, absence of internal control headers, and absence of user-supplied auth/workspace headers after rewrite.

## 10. Sub2API requirements

Sub2API must implement:

1. New account type constant and validators for `claude-platform-aws`.
2. UI card and import form that does not alter existing OAuth/Setup Token/API-key/Bedrock branches.
3. Multiple workspace rows, each with required proxy assignment.
4. Server-side workspace/account/credential safe refs and HMAC bindings.
5. Scheduler selection that treats each workspace account as an independent formal-pool candidate.
6. Sticky session tuple extended with `workspace_ref`, `provider_kind`, `upstream_auth_scheme`, `aws_region`, and `endpoint_ref`.
7. CC Gateway runtime registration update for each account.
8. Account test path that uses safe mock or controlled upstream and never stores raw request/response bodies or secret headers.
9. Redaction for raw `anthropic_workspace_id` in logs, admin diagnostic responses, test artifacts, and evidence files.
10. A batch create/import API or explicit per-row result flow for multiple workspaces; implementation must choose one before coding.
11. Canonical HMAC context serialization parity with CC Gateway, covered by shared fixtures.
12. A dedicated direct request builder/dispatcher for `claude-platform-aws` account tests and non-formal-pool diagnostics.

## 11. UI design

Add a new Anthropic account card next to the existing Anthropic cards:

```text
Claude Platform on AWS
API Key / SigV4 (SigV4 gated)
```

Phase 1 UI behavior:

- Shows API-key mode as enabled.
- Shows SigV4 as disabled or marked "later / gated" unless Phase 2 is implemented.
- Allows adding multiple workspace rows:
  - `Workspace ID`
  - `AWS Region`
  - `API Key` or shared API-key selector depending on final UX
  - `Proxy`
  - optional account name suffix / group / concurrency
- Requires proxy selection on every row.
- Derives and displays the base URL from region, e.g. `https://aws-external-anthropic.<region>.api.aws`.
- Does not expose a free-form production base URL.
- Does not change existing Claude Code OAuth/Setup Token controls or Bedrock region/auth controls.

## 12. Why this is not the current AWS Bedrock card

| Aspect | Existing AWS Bedrock | New Claude Platform on AWS |
|---|---|---|
| Endpoint | Bedrock Runtime endpoint | `aws-external-anthropic.<region>.api.aws` |
| API shape | Bedrock InvokeModel style and Bedrock body conversion | Anthropic-native `/v1/messages` |
| Model IDs | Bedrock model IDs / region prefixes | Native Claude model names |
| Auth | Bedrock SigV4 service `bedrock` or Bedrock API key | AWS Claude Platform API key (`x-api-key` Phase 1; Bearer only if separately proven) or SigV4 service `aws-external-anthropic` |
| Required workspace header | No | Yes, `anthropic-workspace-id` |
| Formal-pool identity | Account/proxy only today | Account + credential + workspace + endpoint + proxy |

Therefore, extending the Bedrock card would be misleading and risky.

## 13. Checkpoint implementation plan

### Checkpoint 0 - Document approval and branch hygiene

- Confirm this plan with the user before code changes.
- Verify work roots:
  - Sub2API: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime`
  - CC Gateway: `/Users/muqihang/chelingxi_workspace/cc-gateway`
- Do not touch main checkout, 3012, or stale 2173 worktree.

Exit gate: user approves scope and API-key-first recommendation.

### Checkpoint 1 - Sub2API account model and validation

TDD first:

- Add backend tests proving:
  - `claude-platform-aws` account type accepts valid region/workspace/API-key/proxy.
  - Missing proxy fails.
  - Invalid workspace ID fails.
  - Base URL/region mismatch fails.
  - Multiple different workspace IDs create separate account identities.
  - Existing OAuth/Setup Token/API-key/Bedrock creation still passes.
  - `claude-platform-aws` is formal-pool eligible only through the new eligibility predicate and only when safe refs/proxy/workspace binding exist.
  - Ordinary first-party Anthropic `apikey` accounts do not become formal-pool eligible by accident.
  - Logs/errors redact API key and raw workspace ID.

Implementation:

- Add `AccountTypeClaudePlatformAWS = "claude-platform-aws"`.
- Add credential validator and safe-ref builder.
- Add redaction helpers for raw workspace ID.
- Add or extend admin account creation API for batch workspace rows.

Exit gate: targeted Go tests pass and CodeGraph incrementally indexed if code changed.

### Checkpoint 2 - UI card and multi-workspace import UX

TDD first:

- Add frontend tests proving:
  - The new card renders independently.
  - Bedrock card remains unchanged.
  - OAuth/Setup Token card remains unchanged.
  - Each workspace row requires proxy.
  - Payload never places raw workspace ID in `extra` safe-ref fields.

Implementation:

- Update `CreateAccountModal.vue` or split a focused `ClaudePlatformAWSFields.vue` component if the modal becomes too large.
- Add i18n strings in `frontend/src/i18n/locales/en.ts` and `frontend/src/i18n/locales/zh.ts`.
- Prefer a dedicated component if it prevents further growth of the already large modal.

Exit gate: targeted frontend tests pass and existing account modal tests do not regress.

### Checkpoint 3 - API-key upstream path and mock AWS tests

TDD first:

- Add mock upstream tests proving:
  - Request URL is `/v1/messages` on the derived AWS endpoint.
  - Model names remain Anthropic-native and are not Bedrock-mapped.
  - Server-owned `anthropic-workspace-id` is injected.
  - User-forged `anthropic-workspace-id` is stripped/rejected.
  - Auth is `x-api-key: <selected credential>` for Phase 1 AWS API-key mode.
  - No raw API key, workspace ID, Authorization, prompt/body/response, or CCH appears in evidence.

Implementation:

- Add a Claude Platform on AWS request builder separate from Bedrock request builder.
- Add dispatch so `type = "claude-platform-aws"` reaches that builder and does not enter existing first-party Anthropic `apikey` passthrough or Bedrock paths.
- Keep Bedrock `ResolveBedrockModelID`, Bedrock signer, and Bedrock body conversion untouched.
- Ensure direct account test routes use safe captures only.

Exit gate: mock AWS targeted tests pass.

### Checkpoint 4 - Formal-pool Sub2API -> CC Gateway contract

TDD first:

- Add Sub2API tests proving trusted context includes safe AWS provider/workspace/endpoint fields from scheduler state only.
- Add shared canonical JSON/HMAC fixture tests proving Sub2API serialized bytes match CC Gateway verifier bytes.
- Add forged-header tests for `anthropic-workspace-id`, `authorization`, `x-api-key`, `x-amz-*`, `x-cc-*`, and `x-sub2api-*`.
- Add sticky session mismatch tests for workspace/account/credential/egress/policy/persona/device/profile changes.

Implementation:

- Extend `cc_gateway_adapter.go` formal-pool attestation fields.
- Extend scheduler/session mapper sticky tuple with provider/workspace/endpoint fields.
- Extend CC Gateway runtime registration payload with safe refs and sensitive workspace storage.

Exit gate: Sub2API targeted tests pass and no evidence leakage.

### Checkpoint 5 - CC Gateway verifier, ledger, and final upstream injection

TDD first:

- Add CC Gateway tests proving:
  - Missing workspace identity fails closed.
  - Workspace ref mismatch fails closed.
  - Region/base URL mismatch fails closed.
  - `aws-external-anthropic.<region>.api.aws` is allowed only for `provider_kind=claude_platform_aws`; other providers cannot use it.
  - Runtime registration persists provider/workspace/upstream fields and rejects conflicting replay.
  - Same session cannot swap workspace.
  - Egress allowlist must include the account.
  - Auth scheme `x_api_key` emits the internal selected credential as `x-api-key` only after binding verification.
  - Final upstream request has exactly one `anthropic-workspace-id`, exactly one verified API-key auth header, no internal headers, and safe evidence only.
  - Existing first-party Anthropic OAuth/API-key standalone and formal-pool tests still pass.

Implementation:

- Extend `AccountContext`, `AttestedFormalPoolContext`, `AccountIdentityConfig`, and `FormalPoolSessionAuthorityBinding`.
- Add provider-aware upstream URL resolution.
- Extend runtime registration/request/config schemas and redacted persistence for provider/workspace/upstream fields.
- Extend upstream-safety allowlist with region-bound AWS Claude Platform endpoints.
- Add provider-aware header rewrite for Claude Platform on AWS.
- Add final verifier checks for workspace/endpoint/auth scheme.
- Keep personal standalone behavior intact unless formal-pool/shared-account config is present.

Exit gate: CC Gateway targeted tests pass on `/Users/muqihang/chelingxi_workspace/cc-gateway` main-based checkout.

### Checkpoint 6 - Local full-chain E2E with safe mock upstream

TDD/E2E first:

- Run local full chain:

```text
native Claude-compatible request
  -> Sub2API local
  -> CC Gateway local
  -> mock aws-external-anthropic upstream
```

Assertions:

- Mock upstream receives `/v1/messages`.
- Mock upstream receives server-owned workspace header; reports only `workspace_header_present: true` and safe `workspace_ref`, not the raw value.
- Mock upstream receives server-owned API-key auth; reports only `x_api_key_present: true` by default. If a later `bearer_api_key` compatibility profile is enabled, that separate test reports only `authorization_present: true`.
- Proxy selection is reflected by safe proxy identity ref.
- No internal headers reach upstream.
- Evidence contains only safe refs, booleans, lengths, status codes, and schema summaries.

Exit gate: localhost full-chain green before any 3017 or deployed consideration.

### Checkpoint 7 - Optional SigV4 phase

Do not start until API-key mode is green.

TDD first:

- Add SigV4 tests proving:
  - Service name is `aws-external-anthropic`.
  - Signing region equals endpoint region.
  - Signing happens after final body/header rewrite.
  - `anthropic-workspace-id` is included exactly as required by the canonical request.
  - Temporary session token handling is correct.
  - No signing canonical request, secret key, token, raw body, or raw workspace ID is logged.

Implementation:

- Add a CC Gateway SigV4 signer for final outbound AWS requests.
- Do not reuse the Bedrock signer, because it signs service `bedrock` and belongs to a different request path.

Exit gate: SigV4 mock/canonical tests pass; live canary still requires explicit user approval.

## 14. Production gates

Production enablement requires all of the following:

- Doc 58 safety substrate merged/rebased into the working branch, with targeted tests green for trusted profile fields, session ledger, final verifier, `strip_attribution`, and signed-CCH fail-closed behavior.
- Sub2API targeted tests green.
- CC Gateway targeted tests green.
- Local full-chain safe mock E2E green.
- CodeGraph incrementally indexed after code changes in each repo.
- 55 evidence report and final evidence map updated with safe evidence only.
- No live formal-pool traffic until localhost E2E and deployed equivalence pass.
- No 3017 rebuild/smoke until targeted tests and local full-chain E2E are green.
- No 3012 changes.
- No standalone formal-pool production. Personal standalone remains allowed.
- `signed_cch` / sign-primary remains fail-closed unless doc 58 oracle/profile gates pass.

## 15. Evidence status for this plan

| Item | Status | Evidence |
|---|---|---|
| Official endpoint/header semantics | PASS | AWS docs linked in section 1 |
| Current screenshot equals Bedrock path | PASS | `CreateAccountModal.vue` Bedrock card and Bedrock backend signer/request code |
| New card/type needed | PASS | Bedrock endpoint/auth/model/body semantics differ from Claude Platform on AWS |
| API-key MVP feasibility | PASS | AWS supports API-key auth against same regional endpoint |
| Multiple workspace support | PASS as design | One workspace per account row, optional batch import |
| Per-workspace proxy | PASS as design | `proxy_id` required per created account row |
| Formal-pool safety integration | PASS as design | Extends doc 56/58 context, verifier, and ledger fields |
| SigV4 implementation | BLOCKED_EXTERNAL_EVIDENCE until tests | Official docs confirm service/region; local final-signer implementation and tests not yet done |
| Current code feasibility review | PASS_WITH_REQUIRED_EDITS | Raman explorer found required edits around account predicates, batch API, redaction, direct builder, canonical attestation parity, runtime schema, upstream safety, final verifier, and doc 58 sequencing |
| Production readiness | BLOCKED_EXTERNAL_EVIDENCE | Requires code implementation, targeted tests, local E2E, deployed equivalence, and evidence updates |

## 16. Non-goals

- Do not merge Claude Platform on AWS into the Bedrock card.
- Do not permit arbitrary custom base URLs for formal-pool production.
- Do not use user-supplied `anthropic-workspace-id` as authority.
- Do not change existing OAuth/Setup Token login behavior.
- Do not enable `signed_cch` or sign-primary as part of this AWS upstream work.
- Do not claim live production readiness from this document alone.
- Do not implement production formal-pool routing before rebasing on the stable doc 58 safety substrate.
