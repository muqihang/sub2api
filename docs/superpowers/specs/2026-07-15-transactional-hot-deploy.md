# Transactional Hot Deploy Design

## Goal

Keep Caddy and replace ad-hoc production hot fixes with one parameterized,
transactional deployment command. A deployment is successful only after the
candidate container, the active Caddy configuration, and public Responses and
Compact probes all pass.

## Context

The production application currently runs as manually-created sidecar
containers such as `sub2api-next-v5`. Caddy is a separate container and its
configuration is a read-only single-file bind mount. Replacing the host file
changes its inode, so a later `caddy reload` that reads
`/etc/caddy/Caddyfile` can silently load the old mounted inode. This previously
left public traffic on an older application container even though the host
file named the new container.

## Design

`deploy/hot-deploy.sh` is the only supported production deployment entry
point. It accepts an image and parameterized environment values, discovers the
actual active upstream from Caddy's Admin API, clones the active application's
runtime contract, starts an isolated candidate, probes it directly, switches
Caddy through stdin, probes the public endpoint, and observes a configurable
soak period.

The deployment is a state machine with an ERR/TERM/INT rollback trap. Before
cutover it records both Caddy's active JSON and the host Caddyfile under a
mode-0700 state directory. After cutover, any failure restores the saved JSON
through stdin and restores the host Caddyfile in place. The script then checks
that Caddy again reports the original upstream. It never deletes or stops the
old container automatically.

The script never reloads `/etc/caddy/Caddyfile` inside the Caddy container.
Candidate Caddyfiles are validated and loaded over stdin. The persistent host
Caddyfile is updated in place only after active cutover verification, avoiding
another bind-mount inode replacement. A preflight compares the host and
container-mounted Caddyfile; a mismatch is reported prominently and all reload
operations still use the validated host candidate over stdin.

## Runtime Contract

Required command input:

- `--image IMAGE`
- `SMOKE_API_KEY`, supplied through the environment and never printed

Current production defaults:

- `CADDY_CONTAINER=sub2api-caddy`
- `CADDYFILE=/opt/sub2api/deploy/Caddyfile.zhumeng`
- `PUBLIC_BASE_URL=https://api.5566676.xyz`
- `APP_PORT=8080`
- `CONTAINER_PREFIX=sub2api-next`
- `SMOKE_MODEL=gpt-5.6-sol`
- `RESPONSES_PATH=/responses`
- `COMPACT_PATH=/responses`

Reusable overrides include the active container, candidate name, Docker
network, health path, timeouts, soak duration, Caddy Admin URL, state
directory, smoke model, compact payload size, and response/compact paths.

The candidate inherits environment variables and volumes from the active
container, joins the same Docker network, and uses the new image's entrypoint,
command, and healthcheck. Environment values are exported only in the
short-lived Docker subprocess; Docker arguments contain environment names and
no secret values are written into deployment artifacts.
The candidate starts with `restart=no`. The active container's restart policy
is applied only after the final Caddy and public checks pass. Failed candidates
are stopped but not removed.

## Validation Gates

1. Exclusive deployment lock acquired.
2. Docker, curl, and python3 available.
3. Caddy container running and Admin API readable.
4. Exactly one active application upstream identified from active Caddy JSON.
5. Host Caddyfile contains the active upstream.
6. Candidate name is unused and image exists.
7. Candidate reaches running and healthy state.
8. Direct candidate health probe succeeds.
9. Direct candidate Responses and streaming remote Compact v2 probes succeed.
10. Candidate Caddyfile validates through stdin.
11. Canonical full Caddy JSON and the host Caddyfile still match the initial snapshot.
12. Caddy adapt produces the expected candidate native JSON through stdin.
13. Caddy reload succeeds through stdin and active JSON exactly matches the adapted candidate.
14. Public health, Responses, and streaming remote Compact v2 probes succeed.
15. Health remains good throughout the soak window; full Caddy JSON and the host Caddyfile still match the candidate transaction state.

Compact smoke is mandatory unless the operator explicitly supplies
`--skip-api-smoke`. The skip is visible in the state log and is intended only
for environments without an API key, not normal production deployment.
The compact payload uses `POST /responses`, `stream=true`, and an input
`compaction_trigger`; success requires a compaction output item and a terminal
`response.completed` SSE event.
Normal Responses success requires `status=completed` and a nonempty output text
equal to the `OK` canary.

## Rollback

Rollback is armed immediately before Caddy cutover and disarmed only after all
public checks and soak checks pass. It restores active Caddy JSON first, then
the persistent Caddyfile, verifies the original upstream, and leaves both old
and candidate application containers running for inspection. Failure to
verify rollback is emitted as a critical error and returns a distinct nonzero
status.
Before restoring, rollback requires the active upstream to be either the
transaction's candidate or its original upstream. Any third-party upstream is
left untouched and produces critical exit status 86.
The actual rule is stricter than the upstream label: canonical active JSON and
host Caddyfile bytes must both be owned by the transaction. Candidate stop
failure produces critical exit status 87.

## Safety

- PostgreSQL and Redis containers, volumes, and data are never changed.
- The old application container is never stopped or removed automatically.
- Candidate containers never receive host-published ports.
- API keys and inherited environment values are never written to logs.
- State artifacts contain Caddy configuration and command results only.
- Concurrent deployments are rejected.
- A dry-run mode prints the intended topology without creating or reloading.

## Testing

A Bash integration harness injects fake `docker` and `curl` commands through
`PATH`. Tests exercise successful cutover, configuration rejection, candidate
health failure, direct Compact failure, active-upstream mismatch, public probe
failure, soak failure, rollback success, rollback verification failure,
secret-redaction guarantees, locking, and dry-run behavior. Syntax checks run
with `bash -n`; shellcheck is used when installed.
