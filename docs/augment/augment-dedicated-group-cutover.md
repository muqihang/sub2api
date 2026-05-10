# Augment Dedicated Group Cutover

This document defines the production release sequence for the Augment dedicated-group isolation rollout introduced by the approved plan on May 9, 2026.

## Scope

Checkpoint 1 introduces only the persisted state and tolerant serialization required for later runtime enforcement:

- `groups.augment_gateway_entitled BOOLEAN NOT NULL DEFAULT FALSE`
- `api_keys.restricted_client_product TEXT NULL`

This checkpoint does not enable runtime rejection of generic keys on Augment routes.

## Ownership

- Gate owner: production backend release owner
- Verification owner: production smoke-test operator

## Tolerant Read/Write Contract

Before the rejection gate is enabled, all production code must remain tolerant of mixed old/new state:

- old groups without explicit Augment entitlement semantics are treated as `augment_gateway_entitled = FALSE`
- old API keys without explicit client restriction are treated as unrestricted (`restricted_client_product IS NULL`)
- reads must accept both unrestricted keys and Augment-restricted keys
- writes may persist the new fields before any runtime policy depends on them
- cached auth snapshots must carry both fields once the new version is deployed

## Ordered Rollout Sequence

This sequence must be executed exactly in order:

1. Apply the schema migration.
2. Deploy tolerant reads/writes for old and new group/API-key state.
3. Create and configure Augment entitlement groups in production.
4. Populate explicit pricing for all Augment-visible models.
5. Expose Augment-only key issuance UX and verify replacement-key creation.
6. Load or refresh official session pool inventory.
7. Enable the runtime rejection gate for generic keys on Augment routes.
8. Run post-cutover verification.
9. If verification fails, disable the rejection gate before schema rollback is considered.

## Post-Cutover Behavior

After step 7 is enabled:

- generic keys must no longer work on Augment runtime routes
- short-lived Quick Login grants need no dedicated invalidation
- existing extension installations may keep official session state, but runtime must fail until the user enters an Augment-only key

## Verification Checklist

The verification owner should confirm all of the following after the gate is enabled:

1. An Augment-entitled group exists and remains marked with `augment_gateway_entitled = TRUE`.
2. An Augment-only API key can be issued with `restricted_client_product = zhumeng_augment`.
3. The Augment-only key is accepted on expected Augment routes.
4. A generic key is rejected on Augment routes by the runtime gate.
5. First-batch Augment-visible priced models are available and resolve without fallback pricing.
6. Official session pool inventory is loaded and usable by the Augment flows being released.

## Rollback Trigger

Rollback must start if any post-cutover verification detects one of the following:

- Augment-only key issuance fails
- Augment route admission fails for a valid Augment-only key
- first-batch priced model availability fails

## Rollback Order

1. Disable the runtime rejection gate.
2. Re-run the smoke checks for generic and Augment-only key behavior.
3. Investigate or repair application logic, cache propagation, pricing, or inventory state.
4. Consider schema rollback only after the gate is disabled and only if a later corrective deployment cannot safely recover the release.

Because the new columns are additive and have backward-compatible defaults, schema rollback is the last option rather than the first response.
