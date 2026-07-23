# Transactional Hot Deploy

Production application updates must use `hot-deploy.sh`. The command starts an
unexposed candidate container, probes it directly, switches Caddy through
stdin, verifies Caddy's active Admin API configuration, probes the public
endpoint, and rolls back automatically if a later gate fails.

## Before Deployment

1. Build or load the target image on the production host.
2. Run the deployment regression suite:

   ```bash
   make -C deploy test-hot-deploy
   ```

3. Provide a production API key through `SMOKE_API_KEY`. Do not put the key in
   the repository, command line, or `hot-deploy.env`.
4. Confirm the current application container and Caddy container are healthy.

The script does not build images, pull Git branches, migrate data, or clean old
containers. Those operations remain separate so a deployment cannot quietly
change PostgreSQL, Redis, or production files.

## Current Production Invocation

From the repository root on the target host:

```bash
SMOKE_API_KEY='<production-canary-key>' \
  deploy/hot-deploy.sh \
  --image 'sub2api-zhumeng:<git-sha>'
```

The current production defaults are already configured for:

- Caddy container: `sub2api-caddy`
- Caddyfile: `/opt/sub2api/deploy/Caddyfile.zhumeng`
- Public endpoint: `https://api.5566676.xyz`
- Application port: `8080`
- Smoke model: `gpt-5.6-sol`
- Compact request size: `1048576` bytes

Use a configuration file for another host:

```bash
SMOKE_API_KEY='<production-canary-key>' \
  deploy/hot-deploy.sh \
  --config /etc/sub2api/hot-deploy.env \
  --image 'registry.example/sub2api:<git-sha>'
```

`--active-container` is an assertion, not a way to override reality. If it
disagrees with the upstream returned by Caddy's Admin API, deployment stops.

## Validation Sequence

1. Snapshot active Caddy JSON and the persistent host Caddyfile.
2. Discover the active application container from Caddy's active JSON.
3. Clone its environment, volumes, network, restart policy, and ulimits into a
   candidate that has no published host ports. The candidate starts with
   `restart=no`; its target restart policy is applied only after final commit.
4. Wait for Docker health and probe the candidate bridge IP directly.
5. Run normal Responses, streaming Compact, and both native Search path probes
   directly against the candidate.
6. Validate the candidate Caddyfile through stdin.
7. Re-read Caddy and the host Caddyfile, aborting if the full canonical active
   JSON or host file changed since the initial snapshot; then reload through
   stdin.
8. Adapt the candidate Caddyfile to expected native JSON before reload. After
   reload, require the full active JSON to match that expected JSON before the
   transaction claims ownership.
9. Run public health, Responses, streaming Compact, `/alpha/search`, and
   `/v1/alpha/search` probes.
10. Monitor public health for the configured soak window, then require the full
    active Caddy JSON and persistent Caddyfile to still match the transaction's
    candidate state before declaring success.

The Compact canary reproduces the Codex remote compact v2 request shape. It
sends streaming `POST /responses` input containing `type=compaction_trigger`,
then requires both a `type=compaction` output item and a
`response.completed` SSE event. A plain HTTP 200 is not enough.
Both API probes send paired official-client identity headers (`User-Agent` and
`originator`) so accounts with `codex_cli_only` exercise the real Codex path.
The normal Responses probe also requires `status=completed` and an output text
equal to `OK`; an empty HTTP 200 JSON object cannot pass.

Each native Search probe requires HTTP 200, JSON Content-Type, a top-level JSON
object, a non-empty string `output`, and absent/null/string `encrypted_output`.
Search response bytes flow directly into a validator and are never written to
the deployment state directory or logs. Only a sanitized status/media-type
verdict exists transiently; Search request JSON, `output`, and
`encrypted_output` are not retained.

Only this final line is a successful deployment:

```text
[hot-deploy] DEPLOYMENT SUCCEEDED: old-container:8080 -> new-container:8080
```

## Rollback

Rollback is armed immediately before Caddy cutover. Any reload error, active
upstream mismatch, public request error, malformed Compact stream, soak health
failure, `INT`, or `TERM` restores the saved native Caddy JSON and the previous
host Caddyfile. The command then queries Caddy again.

Successful rollback contains:

```text
[hot-deploy] ROLLBACK VERIFIED: active upstream is old-container:8080
```

Exit status `86` and `CRITICAL: rollback verification failed` mean automatic
rollback could not be verified. Stop deployment activity and inspect the state
directory plus Caddy Admin API before doing anything else.
If another operator or process changes Caddy after this transaction cuts over,
the script reports `rollback ownership lost`, leaves that external change in
place, and exits 86 instead of overwriting it with an old snapshot.
Ownership includes both canonical active Caddy JSON and the persistent host
Caddyfile. Rollback restores the host file only while it still equals either
the transaction's baseline or candidate bytes.

Exit status `87` and `CRITICAL: failed candidate could not be stopped` mean
traffic was not committed to that candidate, but Docker could not quarantine
it. Stop that candidate explicitly after verifying the active Caddy upstream.

The old application container is never stopped or removed automatically. A
failed candidate is stopped to release CPU and database/Redis connections, but
the stopped container is retained for logs and inspection and is never removed.

## State and Secrets

Each run creates a mode-0700 directory below `STATE_DIR`. It contains Caddy
snapshots, smoke request/response artifacts, and `deploy.log`. It never stores
`SMOKE_API_KEY` or a copied application environment file. Inherited values are
exported only inside the short-lived `docker create` subprocess while Docker
arguments contain variable names, not `KEY=secret` values.

Responses and Compact keep their existing diagnostic artifacts. Native Search
is the exception: neither Search requests nor raw Search responses are written
to `STATE_DIR`, including failed probes.

The current Caddy uses a read-only single-file bind mount. If its mounted file
is stale, the command prints a warning. This does not change reload behavior:
validation, cutover, and rollback always use stdin, while the host Caddyfile is
updated without changing its inode. Do not work around the warning with a
manual container-mounted reload.

If the inverse drift occurs, where the Caddy process and its mounted file still
match but the host path was overwritten with older content, normal deployment
stops before cutover. Recover that host baseline only through the guarded mode:

```bash
SMOKE_API_KEY='<production-canary-key>' \
  deploy/hot-deploy.sh \
  --image 'sub2api-zhumeng:<git-sha>' \
  --recover-stale-host-caddyfile
```

This mode validates and adapts the mounted Caddyfile through stdin, requires its
full canonical JSON and active upstream to match a fresh Caddy Admin API
snapshot, confirms neither the active JSON nor host file changed during the
check, then restores the host path with an in-place inode-preserving write. The
overwritten bytes are retained in the deployment state directory as
`Caddyfile.stale-before-recovery`. Any mismatch refuses recovery and leaves the
host path unchanged. The recovery-only comparison normalizes Caddy's equivalent
auto-injected `file_server.hide` source paths (`./-` for stdin and
`/etc/caddy/Caddyfile` for file startup); every other JSON field remains strict.

## Explicit Exceptions

`--skip-api-smoke` is available for a non-production environment without an API
key. It prints `API smoke: SKIPPED BY OPERATOR` and must not be used for normal
production deployment.

`--dry-run` validates arguments and prints topology without creating a
container or reloading Caddy:

```bash
deploy/hot-deploy.sh --image example/sub2api:test --skip-api-smoke --dry-run
```
