# Transactional Hot Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a reusable, rollback-capable Caddy hot-deployment command and make it the documented repository deployment path.

**Architecture:** A Bash orchestrator sources a focused deployment library. The library treats Caddy's active JSON as runtime truth, clones the active application container into an unexposed candidate, validates every direct and public probe, and restores the previous Caddy JSON on any post-cutover failure. A command-injection test harness verifies the state machine without touching Docker or production.

**Tech Stack:** Bash 3.2-compatible shell, Docker CLI, Caddy CLI/Admin API, curl, Python 3 standard library.

## Global Constraints

- Keep Caddy; do not introduce Nginx.
- Work on `main` and create the final commit only after independent review.
- Never modify PostgreSQL or Redis containers, volumes, or data.
- Never stop or remove the old application container automatically.
- Never reload Caddy from the container-mounted `/etc/caddy/Caddyfile`.
- Never print or persist `SMOKE_API_KEY` or inherited application secrets.
- All production API smoke gates are mandatory unless explicitly skipped.

---

### Task 1: Test Harness and Configuration Contract

**Files:**
- Create: `deploy/tests/test-hot-deploy.sh`
- Create: `deploy/tests/fixtures/Caddyfile`
- Create: `deploy/lib/hot-deploy-common.sh`
- Create: `deploy/hot-deploy.sh`

**Interfaces:**
- Consumes: Docker/Caddy names and paths from environment variables and CLI flags.
- Produces: `load_config`, `parse_args`, `require_commands`, `acquire_lock`, `log`, `die`, and a deterministic command exit status.

- [ ] **Step 1: Write failing tests for required image, required smoke key, defaults, explicit smoke skip, dry-run, and lock rejection.**

The harness creates fake `docker` and `curl` executables in a temporary PATH,
runs `deploy/hot-deploy.sh`, and asserts exit status plus redacted output.

- [ ] **Step 2: Run the focused test and verify RED.**

Run: `bash deploy/tests/test-hot-deploy.sh config`

Expected: FAIL because the deployment entry point and library do not exist.

- [ ] **Step 3: Implement strict parsing and configuration validation.**

Support `--image`, `--candidate`, `--active-container`, `--config`,
`--skip-api-smoke`, `--dry-run`, and `--help`. Reject unknown arguments and
invalid numeric timeouts. Default values must match the approved design.

- [ ] **Step 4: Run the focused test and verify GREEN.**

Run: `bash deploy/tests/test-hot-deploy.sh config`

Expected: PASS with no API key in output or state artifacts.

### Task 2: Active Caddy Discovery and Safe Reload

**Files:**
- Modify: `deploy/tests/test-hot-deploy.sh`
- Modify: `deploy/lib/hot-deploy-common.sh`

**Interfaces:**
- Consumes: `CADDY_CONTAINER`, `CADDY_ADMIN_URL`, `CADDYFILE`, and candidate upstream.
- Produces: `snapshot_caddy`, `extract_active_upstream`, `render_candidate_caddyfile`, `validate_caddyfile`, `reload_caddyfile`, `restore_caddy_json`, and `assert_active_upstream`.

- [ ] **Step 1: Write failing tests for Admin API discovery, multiple-upstream rejection, stdin-only validate/reload, active assertion, and JSON rollback.**

The fake Docker command must fail the test if an invocation contains
`--config /etc/caddy/Caddyfile`.

- [ ] **Step 2: Run the Caddy tests and verify RED.**

Run: `bash deploy/tests/test-hot-deploy.sh caddy`

Expected: FAIL because Caddy transaction functions are absent.

- [ ] **Step 3: Implement recursive JSON upstream extraction with Python and stdin-only Caddy operations.**

Require exactly one unique `dial` value matching `APP_PORT`. Save native Caddy
JSON before cutover, render by replacing only the discovered upstream, validate
and adapt the candidate, reload it, and require active JSON to match the adapted
canonical JSON before continuing.

- [ ] **Step 4: Run Caddy tests and verify GREEN.**

Run: `bash deploy/tests/test-hot-deploy.sh caddy`

Expected: PASS and command log contains only stdin-based Caddy operations.

### Task 3: Candidate Container and Probe Gates

**Files:**
- Modify: `deploy/tests/test-hot-deploy.sh`
- Modify: `deploy/lib/hot-deploy-common.sh`
- Modify: `deploy/hot-deploy.sh`

**Interfaces:**
- Consumes: active container inspection, new image, network, probe configuration, and `SMOKE_API_KEY`.
- Produces: `discover_active_container`, `create_candidate`, `wait_candidate`, `probe_health`, `probe_responses`, `probe_compact`, and `run_soak`.

- [ ] **Step 1: Write failing tests for runtime cloning, no published ports, unhealthy candidates, Responses failure, Compact failure, and timeout.**

Assert that environment values are passed to Docker but never emitted to logs
or written under `STATE_DIR`.

- [ ] **Step 2: Run candidate tests and verify RED.**

Run: `bash deploy/tests/test-hot-deploy.sh candidate`

Expected: FAIL because candidate lifecycle functions are absent.

- [ ] **Step 3: Implement candidate creation and direct/public probes.**

Clone environment, volumes, network, and ulimits from the active container
without host port publication. Start with `restart=no`, retain the target
restart policy for final commit, and stop failed candidates without removing
them. Resolve the candidate bridge IP for direct probes. Generate Responses and
configurable-size Compact JSON with Python, and send it through curl without
logging authorization headers.

- [ ] **Step 4: Run candidate tests and verify GREEN.**

Run: `bash deploy/tests/test-hot-deploy.sh candidate`

Expected: PASS; compact failure prevents Caddy cutover.

### Task 4: Transaction State Machine and Rollback

**Files:**
- Modify: `deploy/tests/test-hot-deploy.sh`
- Modify: `deploy/hot-deploy.sh`
- Modify: `deploy/lib/hot-deploy-common.sh`

**Interfaces:**
- Consumes: all Task 1-3 functions.
- Produces: one ordered deployment transaction with an armed rollback trap and auditable stage log.

- [ ] **Step 1: Write failing end-to-end tests for success, public failure rollback, soak failure rollback, rollback verification failure, signal handling, and old-container retention.**

- [ ] **Step 2: Run transaction tests and verify RED.**

Run: `bash deploy/tests/test-hot-deploy.sh transaction`

Expected: FAIL because the orchestrated state machine is incomplete.

- [ ] **Step 3: Implement the ordered transaction and rollback trap.**

Arm rollback immediately before reload, persist the host Caddyfile in place
after active assertion, run public probes and soak, and disarm only at final
success. Compare canonical full Caddy JSON at pre-cutover, rollback ownership,
and final commit gates. Return a distinct status if rollback verification fails.

- [ ] **Step 4: Run transaction tests and verify GREEN.**

Run: `bash deploy/tests/test-hot-deploy.sh transaction`

Expected: PASS and the fake old container receives no stop/remove command.

### Task 5: Repository Policy and Operator Documentation

**Files:**
- Create: `AGENTS.md`
- Create: `deploy/HOT_DEPLOY.md`
- Create: `deploy/hot-deploy.env.example`
- Modify: `deploy/Makefile`
- Modify: `deploy/tests/test-hot-deploy.sh`

**Interfaces:**
- Consumes: the final CLI contract.
- Produces: repository-level deployment policy, parameter reference, production examples, and `make test-hot-deploy`.

- [ ] **Step 1: Write a failing static policy test.**

Assert that `AGENTS.md` identifies `deploy/hot-deploy.sh` as the only supported
production hot-deploy path and prohibits container-mounted Caddy reloads.

- [ ] **Step 2: Run the policy test and verify RED.**

Run: `bash deploy/tests/test-hot-deploy.sh policy`

Expected: FAIL because the repository policy and runbook do not exist.

- [ ] **Step 3: Add concise policy, runbook, example configuration, and Make target.**

Document preflight, invocation, success evidence, rollback behavior, state
artifacts, secret handling, and recovery from a stale Caddy bind mount.

- [ ] **Step 4: Run all deployment checks.**

Run: `make -C deploy test-hot-deploy`

Expected: all test groups PASS; `bash -n` passes; shellcheck runs when installed.

### Task 6: Independent Review and Final Commit

**Files:**
- Review all changed files.

**Interfaces:**
- Consumes: completed implementation and passing tests.
- Produces: reviewed patch and one final commit on `main`.

- [ ] **Step 1: Run the full deployment test suite and inspect the complete diff.**

Run: `make -C deploy test-hot-deploy && git diff --check && git status --short`

Expected: tests PASS, no whitespace errors, only scoped files changed.

- [ ] **Step 2: Dispatch an independent review agent.**

Ask it to prioritize rollback gaps, destructive operations, secret exposure,
Caddy active-state mismatches, Bash portability, and missing failure tests.

- [ ] **Step 3: Address every confirmed finding with a new RED/GREEN cycle and rerun all checks.**

- [ ] **Step 4: Commit after review is clean.**

Run: `git add AGENTS.md deploy docs/superpowers/specs/2026-07-15-transactional-hot-deploy.md docs/superpowers/plans/2026-07-15-transactional-hot-deploy.md && git commit -m "feat(deploy): add transactional caddy hot deploy"`

Expected: one new commit on `main` containing the reviewed deployment workflow.
