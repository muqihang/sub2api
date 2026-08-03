# Augment Dedicated Group Operations

## Purpose

This runbook covers production operation of the Augment dedicated-group rollout:

- a dedicated Augment entitlement group for users;
- dedicated provider execution groups for Augment routing;
- dedicated Augment-only API keys on the user side;
- Official Quick Login for official cloud session custody;
- shared-wallet settlement with Augment-only attribution.

## Operational Model

1. User access is controlled by an Augment-enabled entitlement group.
2. Request execution is controlled by provider routing groups bound in Augment Gateway admin settings.
3. Users must use dedicated Augment-only API keys for Augment Gateway traffic.
4. Official Quick Login provides the official cloud session only. It does not replace the Augment-only API key requirement.
5. Billing still settles against the shared wallet.
6. Usage attribution must stay on the Augment group and `zhumeng_augment` client product.
7. Automatic gateway-key resolution for Quick Login summary and related billing UX requires exactly one active Augment-only key per user.

## Prerequisites

Before enabling user traffic, confirm all of the following:

1. The user-facing entitlement group has `augment_gateway_entitled=true`.
2. The provider routing groups contain active accounts for each provider you plan to expose.
3. The first-batch models have explicit pricing entries in `backend/resources/model-pricing/model_prices_and_context_window.json`.
4. Smoke status is `passed` for each model you want visible.
5. Users have created Augment-only API keys bound to the Augment-enabled group.
6. Operators understand that Quick Login is additive to the key requirement, not a replacement.
7. For users who rely on automatic Quick Login summary resolution, exactly one active Augment-only key exists.

## Model Enablement Guardrails

Augment models must fail closed unless explicit pricing exists in the canonical pricing catalog.

Current first-batch models:

- `gpt-5.4`
- `gpt-5.5`
- `gpt-5.4-mini`
- `deepseek-v4-pro`
- `deepseek-v4-flash`

Operational implications:

- missing explicit pricing keeps the model effectively disabled;
- the model is not visible on `/get-models`;
- direct routing refuses the model even if a stale config tried to enable it.

## User Setup Flow

1. Put the user in the dedicated Augment entitlement group.
2. Have the user create an Augment-only API key bound to that group.
3. Direct the user to Augment Quick Login to bind the official session.
4. Confirm the user understands:
   - the key is Augment-only;
   - Quick Login does not replace the key;
   - charges still land in the shared wallet.
5. If the user has multiple Augment-only keys, choose one surviving active key for automatic gateway-key resolution and disable or revoke the extras.

## Admin Console Checks

Use `/admin/augment-gateway` to verify:

1. Entitlement groups show the expected Augment-enabled user groups.
2. Provider routing groups point to the intended execution pools.
3. Models show:
   - smoke passed;
   - explicit pricing ready;
   - visible only when provider health is good.
4. Usage summary still shows shared-wallet free quota and paid balance fields.
5. Usage rows keep Augment routing attribution fields such as group ID, session ID, and route policy version.

## Quick Login Operations

Normal production path:

1. User opens `/plugin/augment/quick-login`.
2. User completes Official Quick Login.
3. User continues using the dedicated Augment-only API key for gateway requests.
4. If the Quick Login summary cannot auto-resolve a gateway key, inspect the user’s active Augment-only keys and reduce them to exactly one active key.

Admin notes:

- pool-session capture is an operator tool, not a user substitute for key setup;
- official session diagnostics must remain redacted;
- prefer disable or require relogin before revoke when investigating pool health.

## Incident Response

### Model missing from `/get-models`

Check, in order:

1. explicit pricing exists in the canonical catalog;
2. smoke status is `passed`;
3. provider routing group is configured;
4. provider routing group has active accounts;
5. gateway config still includes the model.

### Direct routing refuses a model

Likely causes:

1. explicit pricing is missing;
2. model is not enabled;
3. provider group is unset;
4. provider group is unhealthy.

### User completed Quick Login but requests still fail

Check:

1. the user is using an Augment-only API key;
2. the key is bound to an Augment-enabled group;
3. the official session exists and is active;
4. the requested model is explicitly priced and enabled.

### Quick Login summary cannot resolve the gateway key

Check:

1. how many active Augment-only keys the user currently has;
2. whether those keys are all bound to Augment-enabled groups;
3. which key should remain active for the user.

Operator resolution steps:

1. list the user’s active Augment-only keys;
2. pick the surviving key that should remain active for automatic resolution;
3. disable or revoke the extra active Augment-only keys;
4. confirm exactly one active Augment-only key remains;
5. ask the user to retry Quick Login summary or reload the billing page.

## Change Management

When adding a new Augment model:

1. add an explicit pricing entry to the canonical pricing catalog first;
2. verify pricing coverage with tests;
3. pass smoke;
4. enable the model in admin;
5. verify visibility and direct routing;
6. verify shared-wallet billing and Augment attribution fields remain intact;
7. verify auto-resolution still works with exactly one active Augment-only key.

## Rollback

If a rollout must be partially reversed:

1. disable the affected model in Augment Gateway admin;
2. if provider capacity is the issue, move or disable the provider routing group;
3. keep the entitlement group intact unless user access itself must be cut;
4. do not remove shared-wallet billing fields or Augment attribution fields from usage surfaces.
