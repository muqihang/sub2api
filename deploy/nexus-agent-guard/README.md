# Nexus Agent Guard

Standalone least-privilege bridge between the NexusVault agent and Sub2API. It does not import Sub2API packages and does not change the Sub2API application or database.

## Security boundary

The Nexus agent receives only `GUARD_API_KEY`, which has no meaning outside this private bridge. The real Sub2API admin key is available only to the Guard container.

Allowed operations:

- `GET /api/v1/admin/accounts`
- `GET /api/v1/admin/accounts/data`
- `POST /api/v1/admin/accounts/data`
- `POST /api/v1/admin/accounts/batch`

All other paths and methods are rejected. Import bodies must match the Sub2API data or batch import envelope, are size limited, and cannot carry extra top-level operations. JSON responses are recursively stripped of token, password, cookie, authorization, API key, session key, and secret fields before they reach the agent.

Neither container publishes a host port. Both run with a read-only filesystem, no Linux capabilities, `no-new-privileges`, resource limits, and non-root users. The third-party agent image is pinned by digest.

## Operations

1. Copy `runtime.env.example` to `runtime.env` outside version control.
2. Set `SUB2API_ADMIN_KEY`, a random `GUARD_API_KEY`, and the Nexus connection token.
3. Set `UPSTREAM_URL` to the active Sub2API container on `deploy_sub2api-network`.
4. Run `make test`.
5. Validate with `docker compose --env-file runtime.env config`.
6. Start the Guard first, verify `/healthz` from the Docker network, then start the agent.

Keep automatic purchasing disabled until connection, status, and a controlled single-account import have all been verified. When a Sub2API hot deployment changes the active application container name, update `UPSTREAM_URL` and recreate only the Guard container.
