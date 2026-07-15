# Project Agent Instructions

## Production Hot Deploy

- `deploy/hot-deploy.sh` is the only supported production hot-deploy entry point. Run `make -C deploy test-hot-deploy` before using it.
- Never hand-edit an upstream and never run `caddy reload --config /etc/caddy/Caddyfile` inside the Caddy container. The single-file bind mount may reference an old inode. The deployment tool validates and reloads Caddy through stdin and verifies the active Admin API configuration.
- A deployment is complete only when the command prints `DEPLOYMENT SUCCEEDED`. Container health or a successful Caddy reload alone is insufficient.
- Normal Responses and streaming remote Compact probes are mandatory in production. Compact must exercise `POST /responses` with a `compaction_trigger`, not only `/responses/compact`.
- Never modify, restart, replace, or clean PostgreSQL or Redis containers, volumes, and production data as part of an application hot deploy.
- Do not stop or remove the prior application container during the deployment. It is the immediate rollback target; retire it only in a separate, explicitly approved operation.
- Candidate containers start with `restart=no`. A failed candidate is stopped but retained for evidence; only a fully committed candidate receives the prior restart policy.
- Follow `deploy/HOT_DEPLOY.md` for parameters, evidence, rollback behavior, and recovery.
