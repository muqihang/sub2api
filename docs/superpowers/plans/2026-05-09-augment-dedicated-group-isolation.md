# Augment Dedicated Group Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Zhumeng Augment into the core sub2api platform so users consume one shared wallet balance, but must join a dedicated Augment group and use an Augment-only API key for Quick Login, model access, usage accounting, and billing.

**Architecture:** Introduce a strict separation between the **user entitlement group** for Augment and the existing **provider routing groups** used by Augment Gateway internals. Users join a dedicated Augment group, create an API key bound to that group, and use that key in the Zhumeng panel or extension. Augment Quick Login, gateway runtime, usage logs, billing, and admin reporting all remain inside the existing sub2api systems, but every Augment request must carry a deterministic Augment group/billing identity. Admins continue to manage official sessions, model visibility, and upstream provider bindings through Augment Gateway, while account/URL inventory stays inside the standard account and group admin surfaces.

**Tech Stack:** Go backend, Gin handlers, existing group/API key/subscription system, Augment Gateway runtime, Vue 3 frontend, TypeScript, Vue I18n, existing usage log + billing pipeline, Augment admin console.

---

## Scope and Product Decisions

- Strong isolation is mandatory.
- Users must join an **Augment-dedicated entitlement group** before using Zhumeng Augment.
- Users must generate an **Augment-only API key** from that entitlement group.
- The Augment-only API key is entered directly into the Zhumeng panel; the panel URL is fixed to the production sub2api host at deployment time.
- Wallet balance remains unified across the site.
- Billing attribution, usage logs, and consumption reports for Augment must still record:
  - `group_id = augment entitlement group`
  - `client_product = zhumeng_augment`
  - the normal sub2api cost fields (`estimated_cost`, `settled_cost`, token counts, etc.)
- Non-Augment programs such as Codex continue to use ordinary groups and ordinary API keys.
- Augment-only API keys should be rejected when used on non-Augment routes where that would blur billing or routing isolation.
- Admin routing for upstream accounts/URLs does **not** create a separate account system. It extends current admin flows with clearer Augment-specific bindings and affordances.

## Critical Terminology

This project already uses “group” in more than one sense. The implementation must keep them distinct.

### 1. Entitlement Group

User-facing group for subscriptions, API keys, rate multipliers, and billing attribution.

Examples after this change:
- `Augment Starter`
- `Augment Pro`

This is the group a user joins and the group their API key is bound to.

### 2. Provider Routing Group

Backend routing group used by Augment Gateway provider selection, account pools, and upstream execution.

Examples already present in Augment Gateway admin:
- OpenAI provider group
- DeepSeek provider group
- Anthropic provider group
- Gemini provider group

These are admin-owned routing primitives. Users should not need to understand them.

### 3. Official Session Pool

Platform-managed official tenant/access-token inventory used for official cloud capability takeover (Quick Login, CE, Prompt Enhancer, adjacent official capability).

This plan does not replace the official session pool design. It integrates the pool into the user-entitlement and billing model cleanly.

## Required End State

After implementation:

1. A user who does **not** belong to an Augment entitlement group cannot use Zhumeng Augment.
2. A user who belongs to an Augment entitlement group can generate an Augment-only API key.
3. That API key can be pasted into the Zhumeng panel and load models successfully.
4. Augment Quick Login can launch VS Code / Cursor / other supported IDEs, but only for eligible Augment users.
5. All Augment requests consume the user’s normal balance, but usage is attributed to the Augment entitlement group.
6. Admin usage and billing pages can filter Augment traffic by:
   - `client_product = zhumeng_augment`
   - `group_id`
   - `model`
   - `endpoint`
7. Admins can still manage upstream accounts/URLs, provider group bindings, model visibility, and official session pool from the standard sub2api admin system.
8. First-batch production models no longer rely on fallback pricing.

## Resolved Contract Decisions

These are fixed implementation contracts for this work item.

### A. Authoritative Augment Entitlement Field

Use an explicit durable group field as the source of truth for Augment entitlement.

Canonical field:
- `groups.augment_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE`

Rules:
- `TRUE` means the group is a user-facing Augment entitlement group.
- provider routing groups are not identified by this field.
- `supported_model_scopes` may still carry Augment-related hints for UI filtering, but it is not the authority for entitlement.

### B. Authoritative Augment-Only Key Restriction Field

Use an explicit durable API key field as the source of truth for Augment-only restriction.

Canonical field:
- `api_keys.restricted_client_product TEXT NULL`

Allowed value for this feature:
- `zhumeng_augment`

Rules:
- an Augment-only key must be bound to a group where `augment_gateway_entitled = TRUE`
- a key without `restricted_client_product = zhumeng_augment` is never treated as an Augment-only key
- this field is authoritative for route allow/deny behavior

### C. Billing Identity Model

Quick Login is not the billing identity. Runtime API traffic is.

Rules:
- billing attribution for Augment traffic is derived exclusively from the presented Augment-only API key
- that API key maps to exactly one entitlement `group_id`
- if a user has multiple Augment entitlement groups, they may create multiple Augment-only keys; each key deterministically binds one billing group
- website-authenticated Quick Login grant/exchange only checks user eligibility, not billing-group selection
- the Zhumeng panel/extension must never silently substitute a generic site key in place of an Augment-only key

### D. Non-Augment Route Rejection Policy

Augment-only keys are allowed only on an explicit Augment route allowlist.

Allowed examples:
- `/chat-stream`
- `/prompt-enhancer`
- `/agents/*`
- `/get-models`
- `/usage/api/balance`
- `/usage/api/get-models`
- Augment-specific event/metrics endpoints required by the panel/plugin

Denied examples:
- generic OpenAI-compatible programmatic routes
- generic Responses API routes for non-Augment programs
- non-Augment gateway routes used by Codex or other clients

Required failure mode:
- deterministic scope-mismatch error such as `AUGMENT_ONLY_KEY_SCOPE_MISMATCH`

Canonical v1 allowlist source:
- use one dedicated backend policy file for Augment-only key scope decisions
- do not leave the allowlist implicit inside scattered handlers
- tests must enumerate the v1 allowlist so route drift is visible

### E. Entitlement Assignment Product Flow

This work item supports two ways to place a user into an Augment entitlement group:

1. existing subscription/purchase flow
2. admin assignment

No third separate membership system is introduced.

Concrete implementation anchors:
- purchase/subscription fulfillment path currently flows through `backend/internal/service/payment_fulfillment.go` and `backend/internal/service/subscription_service.go`
- admin assignment path currently flows through `backend/internal/handler/admin/user_handler.go`, `backend/internal/service/admin_service.go`, and `backend/internal/repository/user_repo.go`

### F. Release Cutover Rule

After rollout:
- generic keys must no longer work on Augment runtime routes
- existing Quick Login grants need no dedicated invalidation because grant TTL is already short-lived
- existing extension installations may keep official session state, but runtime must fail until the user enters an Augment-only key
- Augment enablement stays behind an operational release gate until migration, route rejection, and explicit pricing are all live

Ordered rollout sequence:
1. apply schema migration
2. deploy tolerant reads/writes for old and new group/API-key state
3. create/configure Augment entitlement groups in production
4. populate explicit pricing for all Augment-visible models
5. expose Augment-only key issuance UX and verify replacement-key creation
6. load or refresh official session pool inventory
7. enable runtime rejection gate for generic keys on Augment routes
8. run post-cutover verification
9. if verification fails, disable the rejection gate before schema rollback is considered

## Planned File Structure

### Backend

- Modify: `backend/internal/handler/admin/group_handler.go`
  - Allow and validate an Augment-specific group platform or capability marker.
- Modify or create: backend group persistence/repository/migration files
  - Persist the explicit Augment entitlement field.
- Modify: `backend/internal/service/group`-related service files as needed
  - Persist and expose Augment entitlement metadata cleanly.
- Modify: `backend/internal/handler/admin/apikey_handler.go`
  - Enforce Augment-only key creation/update rules.
- Modify or create: backend API key persistence/repository/migration files
  - Persist Augment-only key restriction state durably.
- Create: `backend/internal/service/augment_key_scope_policy.go`
  - Canonical Augment-only route allowlist and scope-mismatch policy.
- Create: `backend/internal/service/augment_key_scope_policy_test.go`
  - Guard the allowlist and rejection contract.
- Modify: `backend/internal/service/augment_plugin_service.go`
  - Surface only Augment-eligible API keys/group metadata to Augment plugin/account flows.
- Modify: `backend/internal/handler/auth_augment_plugin.go`
  - Require Augment entitlement before Quick Login grant.
- Modify: `backend/internal/handler/auth_augment_runtime.go`
  - Reject Augment runtime requests that do not authenticate as Augment-entitled API key traffic.
- Modify: `backend/internal/service/augment_gateway_usage_service.go`
  - Ensure Augment usage reporting exposes group-attributed records consistently.
- Modify: `backend/internal/service/billing_service.go`
  - Replace fallback pricing for first-batch Augment models.
- Modify: Augment model-visibility update path and any supporting service files
  - Refuse to enable Augment-visible models without explicit pricing.
- Modify: `backend/internal/handler/admin/augment_gateway_handler.go`
  - Add dedicated-group-aware admin summary fields and helpful entry points.
- Modify: `backend/internal/server/routes/*` tests and handler tests covering the above.

### Frontend

- Modify: `frontend/src/views/user/KeysView.vue`
  - Make Augment group selection and Augment-only key semantics explicit.
- Modify: `frontend/src/api/keys.ts`
  - Support any new request flags for Augment-only key creation.
- Modify: `frontend/src/views/plugin/augment/QuickLoginView.vue`
  - Explain eligibility and key expectations in user-facing terms.
- Modify: `frontend/src/components/layout/AppSidebar.vue`
  - Keep Augment Quick Login discoverable from the standard nav.
- Modify: `frontend/src/components/user/dashboard/UserDashboardQuickActions.vue`
  - Keep the dashboard shortcut.
- Modify: `frontend/src/views/admin/AugmentGatewayView.vue`
  - Add dedicated-group summary, direct links, and clearer upstream management guidance.
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
  - Add/adjust group, key, billing, and Augment eligibility copy.

### Documentation

- Create: `docs/augment/augment-dedicated-group-operations.md`
  - Runbook for setting up entitlement groups, provider groups, official session pool inventory, API keys, and pricing.

## Execution Strategy

Implement in six checkpoints:

1. Domain model and contract for Augment entitlement groups
2. Persistence, migration, and release cutover
3. Augment-only API key lifecycle and eligibility enforcement
4. Augment Quick Login and runtime authorization gates
5. Admin integration for upstream/provider/session management
6. Billing, pricing, UX, and runbook completion

Each checkpoint should land with targeted tests and a thematic commit.

---

### Task 1: Define the Augment Entitlement Group Contract

**Files:**
- Modify: `backend/internal/handler/admin/group_handler.go`
- Modify: backend group service files that validate/create/update groups
- Modify: `backend/internal/service/payment_fulfillment.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/handler/admin/user_handler.go`
- Modify: `backend/internal/service/admin_service.go`
- Modify: `backend/internal/repository/user_repo.go`
- Modify: `frontend/src/types/index.ts`
- Test: backend group handler/service tests

- [ ] **Step 1: Write failing tests for Augment entitlement group creation**

Cover:
- creating a group flagged for Augment entitlement succeeds
- invalid platform/capability combinations fail
- non-Augment groups are unchanged
- update flows preserve existing groups
- subscription fulfillment can assign an Augment-entitled `group_id`
- admin assignment can assign an Augment-entitled `group_id`
- repeat assignment is idempotent and auditable

- [ ] **Step 2: Run the targeted backend tests to confirm failure**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin ./internal/service -run 'Group.*Augment|Augment.*Group')
```

Expected:
- fail because Augment group semantics are not encoded yet

- [ ] **Step 3: Implement the Augment entitlement marker**

Required rules:
- add one explicit Augment entitlement discriminator
- keep existing group behavior backward compatible
- expose enough metadata for frontend filtering and admin reporting
- do not overload provider routing groups with user entitlement semantics
- keep ordinary group `platform` semantics unchanged unless a separate display hint is strictly needed

- [ ] **Step 4: Implement entitlement-assignment plumbing**

Concrete codepaths to wire:
- purchase/subscription fulfillment -> `payment_fulfillment.go` -> `AssignOrExtendSubscription(...)`
- subscription validation/state -> `subscription_service.go`
- admin reassignment/manual join -> `user_handler.go` -> `admin_service.go` -> `user_repo.go`

Required behavior:
- subscription-based Augment products assign the correct Augment-entitled `group_id`
- admin assignment path can move a user into an Augment entitlement group intentionally
- repeated assignment is idempotent
- assignment leaves an auditable trail of the target `group_id`

- [ ] **Step 5: Re-run targeted backend tests**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin ./internal/service -run 'Group.*Augment|Augment.*Group')
```

Expected:
- PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handler/admin/group_handler.go backend/internal/service/payment_fulfillment.go backend/internal/service/subscription_service.go backend/internal/handler/admin/user_handler.go backend/internal/service/admin_service.go backend/internal/repository/user_repo.go backend/internal/service frontend/src/types/index.ts
git commit -m "feat: add augment entitlement group contract"
```

---

### Task 2: Add Persistence, Migration, and Release Cutover

**Files:**
- Create/modify: backend migration files for groups and API keys
- Modify: backend repositories that read/write groups and API keys
- Modify: any DTO/cache layers that serialize group or API key capability state
- Modify: release-gate configuration and any admin setting surface used to control rollout
- Create: `docs/augment/augment-dedicated-group-cutover.md`
- Test: repository and migration tests

- [ ] **Step 1: Write failing tests for persisted entitlement and key restriction state**

Cover:
- group entitlement flag round-trips through repository
- API key restriction field round-trips through repository
- old rows without new fields retain backward-compatible defaults

- [ ] **Step 2: Run the targeted tests to confirm failure**

Run:
```bash
(cd backend && go test -count=1 ./internal/repository ./internal/service -run 'Augment.*Migration|Augment.*Repository|APIKey.*Restriction')
```

- [ ] **Step 3: Implement schema, repository, and DTO changes**

Required changes:
- add durable group entitlement field
- add durable API-key restriction field
- update select/insert/update scan logic
- update DTOs/admin responses that need the new state

- [ ] **Step 4: Define release cutover behavior**

Document and implement:
- generic keys stop working on Augment runtime routes after gate enablement
- short-lived Quick Login grants need no special invalidation
- existing installs may keep official session state but must re-enter an Augment-only key if currently using a generic key
- rollback story for schema and release gate
- ordered rollout procedure must be captured exactly as the sequence defined in `Resolved Contract Decisions`
- gate owner: production backend release owner
- verification owner: production smoke-test operator
- rollback trigger: any post-cutover failure in Augment-only key issuance, Augment route admission, or first-batch priced model availability

- [ ] **Step 5: Re-run repository/migration tests**

Run:
```bash
(cd backend && go test -count=1 ./internal/repository ./internal/service -run 'Augment.*Migration|Augment.*Repository|APIKey.*Restriction')
```

- [ ] **Step 6: Commit**

```bash
git add backend/migrations backend/internal/repository backend/internal/service docs/augment/augment-dedicated-group-cutover.md
git commit -m "feat: persist augment entitlement and key restriction"
```

---

### Task 3: Make Augment API Keys Group-Bound and Augment-Only

**Files:**
- Modify: `backend/internal/handler/admin/apikey_handler.go`
- Modify: API key service files that validate key create/update behavior
- Modify: `frontend/src/api/keys.ts`
- Modify: `frontend/src/views/user/KeysView.vue`
- Test: API key handler/service tests, relevant frontend tests if present

- [ ] **Step 1: Write failing tests for Augment-only API key behavior**

Cover:
- user can create API key bound to Augment entitlement group
- Augment-only API key must reference an Augment entitlement group
- non-Augment key cannot masquerade as Augment key
- Augment-only key use on non-Augment routes is rejected cleanly
- user with multiple Augment entitlement groups must explicitly choose which group the key is bound to

- [ ] **Step 2: Run targeted tests to verify failure**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin ./internal/service -run 'APIKey.*Augment|Augment.*APIKey')
```

- [ ] **Step 3: Implement Augment-only key semantics**

Required behavior:
- API key creation/update path can identify Augment entitlement groups
- Augment-only key state is stored in a durable, queryable way
- key validation can cheaply determine whether the key is eligible for Augment
- ordinary keys remain valid for ordinary routes
- no generic primary key is silently promoted into an Augment-only key

- [ ] **Step 4: Update user key management UX**

Required frontend behavior:
- user can see which groups are Augment groups
- creating an Augment key is explicit, not accidental
- form copy explains that this key is for Zhumeng Augment panel/extension

- [ ] **Step 5: Re-run backend and frontend tests**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin ./internal/service -run 'APIKey.*Augment|Augment.*APIKey')
(cd frontend && npm run typecheck)
```

Expected:
- PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handler/admin/apikey_handler.go backend/internal/service frontend/src/api/keys.ts frontend/src/views/user/KeysView.vue
git commit -m "feat: enforce augment-only api keys"
```

---

### Task 4: Gate Quick Login and Augment Runtime by Entitlement Group

**Files:**
- Modify: `backend/internal/service/augment_plugin_service.go`
- Modify: `backend/internal/handler/auth_augment_plugin.go`
- Modify: `backend/internal/handler/auth_augment_runtime.go`
- Modify or create: `backend/internal/service/augment_key_scope_policy.go`
- Modify: `frontend/src/views/plugin/augment/QuickLoginView.vue`
- Test: `backend/internal/handler/auth_augment_plugin_test.go`
- Test: `backend/internal/handler/auth_augment_runtime_test.go`
- Test: `backend/internal/service/augment_key_scope_policy_test.go`

- [ ] **Step 1: Write failing tests for Augment eligibility enforcement**

Cover:
- Quick Login grant denied for user with no Augment entitlement
- Quick Login grant allowed for Augment-entitled user
- Augment runtime routes reject non-Augment keys
- Augment runtime routes accept Augment-only keys
- user with multiple Augment groups and multiple key types is handled deterministically

- [ ] **Step 2: Run targeted tests to verify failure**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler -run 'Augment.*QuickLogin|Augment.*Runtime')
```

- [ ] **Step 3: Implement grant/runtime guards**

Required behavior:
- Quick Login must only proceed when the acting user has Augment eligibility
- the plugin summary/account path must stop advertising a generic key as if it were an Augment key
- runtime requests must carry a validated Augment-only API key identity
- error responses must tell the user they need an Augment group/key, not emit ambiguous auth failures
- Quick Login grant/exchange does not itself choose a billing group
- runtime billing identity comes exclusively from the presented Augment-only API key
- route allow/deny follows the explicit Augment allowlist and deterministic scope-mismatch failure for everything else
- the canonical v1 allowlist must live in one policy file with dedicated tests

- [ ] **Step 4: Update Quick Login UX copy**

Required frontend behavior:
- page explains this flow is for Augment-entitled users
- if the user lacks entitlement, show actionable guidance instead of generic failure

- [ ] **Step 5: Re-run handler tests and frontend typecheck**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler -run 'Augment.*QuickLogin|Augment.*Runtime')
(cd frontend && npm run typecheck)
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/augment_plugin_service.go backend/internal/handler/auth_augment_plugin.go backend/internal/handler/auth_augment_runtime.go frontend/src/views/plugin/augment/QuickLoginView.vue
git commit -m "feat: gate augment quick login by entitlement"
```

---

### Task 5: Fully Integrate Admin Augment Gateway with Existing Upstream Management

**Files:**
- Modify: `frontend/src/views/admin/AugmentGatewayView.vue`
- Modify: `backend/internal/handler/admin/augment_gateway_handler.go`
- Modify: any admin service files that shape Augment Gateway summary/provider group data
- Test: `frontend/src/views/admin/__tests__/AugmentGatewayView.spec.ts`
- Test: backend Augment Gateway admin handler tests

- [ ] **Step 1: Write failing tests for dedicated-group-aware admin summary**

Cover:
- Augment Gateway summary shows entitlement-group state
- admin can see which provider routing groups back Augment
- admin can still manage official pool sessions and model visibility
- diagnostics never leak secrets

- [ ] **Step 2: Run targeted tests to verify failure**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin -run 'AugmentGateway')
(cd frontend && npm run test:run -- src/views/admin/__tests__/AugmentGatewayView.spec.ts)
```

- [ ] **Step 3: Add dedicated-group-aware admin affordances**

Required behavior:
- Augment Gateway page must clearly distinguish:
  - Augment entitlement groups
  - provider routing groups
  - official session pool
- page should include direct operational guidance for:
  - where to bind provider groups
  - where to manage upstream accounts/URLs
  - where to manage official session pool
- do not build a second account management system; extend visibility and navigation only as needed

- [ ] **Step 4: Re-run targeted admin tests**

Run:
```bash
(cd backend && go test -count=1 ./internal/handler/admin -run 'AugmentGateway')
(cd frontend && npm run test:run -- src/views/admin/__tests__/AugmentGatewayView.spec.ts)
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handler/admin/augment_gateway_handler.go frontend/src/views/admin/AugmentGatewayView.vue
git commit -m "feat: integrate augment gateway admin controls"
```

---

### Task 6: Align Billing, Usage Logs, Pricing, UX, and Runbook

**Files:**
- Modify: `backend/internal/service/billing_service.go`
- Modify: pricing/model registry files used by Augment Gateway billing
- Modify: `backend/internal/service/augment_gateway_usage_service.go`
- Modify: Augment model visibility update path and related admin APIs
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/components/user/dashboard/UserDashboardQuickActions.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Create: `docs/augment/augment-dedicated-group-operations.md`
- Modify: any tests around usage logs, pricing, billing, and frontend discoverability

- [ ] **Step 1: Write failing tests for first-batch Augment model pricing and route admission**

Cover:
- `gpt-5.4`
- `gpt-5.5`
- `gpt-5.4-mini`
- `deepseek-v4-pro`
- `deepseek-v4-flash`

Expected behavior:
- none of these models use fallback pricing in Augment flows
- usage rows carry group attribution + `client_product = zhumeng_augment`
- no Augment-visible model can be enabled without explicit pricing

- [ ] **Step 2: Run the targeted pricing tests**

Run:
```bash
(cd backend && go test -count=1 ./internal/service ./internal/repository -run 'Augment.*Pricing|Pricing.*Augment|Usage.*Augment')
```

- [ ] **Step 3: Implement explicit Augment model pricing coverage**

Required behavior:
- first-batch model IDs resolve to explicit prices
- no fallback-pricing warnings for first-batch Augment models
- usage log rows preserve normal sub2api cost fields and settlement behavior
- group attribution follows the Augment entitlement group, not provider routing groups
- model visibility toggles fail closed if explicit pricing is missing

- [ ] **Step 4: Add end-to-end and failure-path billing tests**

Cover:
- a user with both ordinary and Augment groups, plus both key types
- Augment traffic logs `client_product = zhumeng_augment`
- Augment traffic uses the dedicated Augment `group_id`
- unified wallet decrements exactly once
- Codex/non-Augment traffic still uses ordinary groups/keys
- retries/upstream errors/partial Quick Login sessions do not create wrong-group or double-settlement rows

- [ ] **Step 5: Implement final UX polish and write operations runbook**

Required UX/runbook:
- sidebar/dashboard entry points stay visible
- wording consistently says `Official Quick Login`
- wording explains `shared wallet, dedicated Augment key/group`
- runbook covers entitlement assignment through subscription flow and admin assignment
- runbook covers token inventory refill and release cutover

- [ ] **Step 6: Re-run targeted tests and inspect logs**

Run:
```bash
(cd backend && go test -count=1 ./internal/service ./internal/repository -run 'Augment.*Pricing|Pricing.*Augment|Usage.*Augment')
(cd backend && go test -count=1 ./internal/handler -run 'Augment.*Billing|Billing.*Augment')
(cd frontend && npm run typecheck)
```

Expected:
- PASS
- no fallback-pricing warnings for first-batch models during Augment smoke tests

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/billing_service.go backend/internal/service/augment_gateway_usage_service.go backend/internal/repository frontend/src/components/layout/AppSidebar.vue frontend/src/components/user/dashboard/UserDashboardQuickActions.vue frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts docs/augment/augment-dedicated-group-operations.md
git commit -m "docs: add augment dedicated group runbook"
```

---

## Final Verification Checklist

- [ ] User with no Augment entitlement cannot use Augment Quick Login
- [ ] User with Augment entitlement can create Augment-only API key
- [ ] Zhumeng panel can connect with Augment-only API key against fixed production URL
- [ ] Quick Login launches supported IDEs successfully
- [ ] Official CE / Prompt Enhancer still route through official cloud takeover
- [ ] Usage rows show `client_product = zhumeng_augment`
- [ ] Usage rows are attributed to Augment entitlement group
- [ ] Shared balance decreases correctly
- [ ] First-batch models no longer emit fallback pricing warnings
- [ ] Codex/non-Augment groups remain unaffected

## Open Questions to Resolve During Implementation

1. Should admin be able to bulk-migrate users from old groups into Augment groups?
   - If needed, do it as a follow-up; keep this work item focused on first-class support.

## Suggested Execution Order

1. Task 1
2. Task 2
3. Task 3
4. Task 4
5. Task 5
6. Task 6

Reasoning:
- group contract, persistence, and key semantics are the hard foundation
- runtime gating depends on those
- admin integration depends on stable entitlement/routing vocabulary
- pricing and UX must be finalized together before release readiness
