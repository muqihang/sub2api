# Augment Dedicated Group Cutover

This document defines the production cutover and steady-state expectations for the post-CP4 Augment dedicated-group isolation rollout.

## Current Scope

The rollout now includes production behavior, not just persisted state:

- dedicated Augment entitlement groups for user access;
- dedicated provider execution groups for Augment routing;
- Augment-only API keys enforced on Augment paths;
- explicit-pricing fail-closed behavior for Augment-visible models;
- shared-wallet settlement with Augment attribution on group and client product;
- Official Quick Login as session custody only, not as a replacement for the Augment-only key;
- automatic Quick Login summary gateway-key resolution only when exactly one active Augment-only key exists for the user.

## Ownership

- Gate owner: production backend release owner
- Verification owner: production smoke-test operator

## Steady-State Contract

Production should satisfy all of the following:

1. Users enter Augment through an Augment-enabled entitlement group.
2. Users send Augment traffic with Augment-only API keys.
3. Official Quick Login binds the official session but does not satisfy gateway admission on its own.
4. Augment models are visible or routable only when:
   - the model is enabled;
   - smoke is passed;
   - provider routing is healthy;
   - explicit pricing exists in the canonical pricing catalog.
5. Shared-wallet billing fields remain visible on usage surfaces.
6. Augment attribution remains tied to Augment group/client_product.
7. Automatic gateway-key resolution requires exactly one active Augment-only key per user.

## Cutover Sequence

Execute in order:

1. Confirm the dedicated Augment entitlement groups exist and are correctly marked.
2. Confirm provider routing groups are configured and healthy for the providers being exposed.
3. Confirm first-batch models have complete explicit pricing in the canonical catalog.
4. Confirm first-batch models have passed smoke and are enabled only where intended.
5. Issue or rotate Augment-only API keys for pilot users.
6. For each pilot user, ensure exactly one active Augment-only key exists if automatic Quick Login summary resolution is expected.
7. Load or refresh official session inventory.
8. Run Quick Login and billing smoke checks.
9. Expand to wider traffic only after verification passes.

## Verification Checklist

The verification owner should confirm all of the following:

1. An Augment-entitled group exists and remains marked with `augment_gateway_entitled = TRUE`.
2. An Augment-only API key can be issued with `restricted_client_product = zhumeng_augment`.
3. The Augment-only key is accepted on expected Augment routes.
4. A generic key is rejected on Augment routes by the runtime gate.
5. First-batch Augment-visible models are backed by complete explicit pricing and do not rely on fallback pricing for visibility or routing.
6. Official session pool inventory is loaded and usable by the Augment flows being released.
7. Quick Login copy and Billing copy both make clear that Quick Login does not replace the Augment-only key requirement.
8. For users expecting automatic gateway-key resolution, exactly one active Augment-only key exists.
9. User billing rows expose Augment attribution fields such as group, session, route policy version, and scope metadata.

## Multi-Key Ambiguity Resolution

Multiple Augment-only keys are not globally forbidden. They are operationally ambiguous for automatic gateway-key resolution.

When ambiguity appears:

1. List the user’s active Augment-only keys.
2. Choose the single key that should remain active for automatic resolution.
3. Disable or revoke the extra active Augment-only keys.
4. Re-run Quick Login summary or the user billing page.
5. Confirm the gateway key now resolves automatically.

## Rollback Trigger

Rollback or partial disablement must start if verification detects one of the following:

- Augment-only key issuance fails;
- Augment route admission fails for a valid Augment-only key;
- first-batch model visibility or routing depends on missing or partial explicit pricing;
- multi-key ambiguity cannot be resolved operationally for the affected user cohort.

## Rollback Order

1. Disable the affected Augment models or routing groups if the issue is scoped.
2. Re-run the smoke checks for generic and Augment-only key behavior.
3. Investigate or repair application logic, key ambiguity, pricing completeness, cache propagation, or inventory state.
4. Disable the broader rejection gate only if scoped mitigation is insufficient.
5. Consider schema rollback only after application-level rollback paths are exhausted.

Because the schema changes are additive, schema rollback remains the last response rather than the first.
