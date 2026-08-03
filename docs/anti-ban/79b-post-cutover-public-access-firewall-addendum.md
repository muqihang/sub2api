# Plan79B Post-Cutover Public Access Firewall Addendum

**Date:** 2026-07-03 UTC  
**Production server:** `198.12.67.185`  
**Final decision:** `PASS_PUBLIC_18080_ACCESS_RESTORED`

## Summary

After Plan79B cutover, the latest Sub2API and CC Gateway release pair was confirmed running on the production server, but external access to `http://198.12.67.185:18080/` initially timed out from outside the server.

Root cause: after cutover, `18080` is served by a host process rather than the old Docker proxy path. UFW had default `INPUT DROP` and did not have an allow rule for `18080/tcp`, so public ingress was blocked. The old Docker proxy path had previously made the port reachable through Docker firewall rules.

Fix applied: added a minimal UFW allow rule for `18080/tcp` only.

## Deployment confirmation

| Item | Result |
|---|---:|
| Plan79B Sub2API process | alive, command path bucket matches `/opt/plan79/releases/.../sub2api/backend/sub2api-server` |
| Plan79B CC Gateway process | alive, command path bucket matches `/opt/plan79/releases/.../cc-gateway/dist/index.js` |
| Plan79B egress proxy process | alive |
| Sub2API binary sha256 | `1c6b2772d0932b5f294c8e3033b1c04a1f112bc1efaba8b1a91e5f57c80492be` |
| CC Gateway `dist/index.js` sha256 | `2badf2adeff8d7bda44e547afdda2e68ee0146cbab8cdc2ca7ad5922e7c02a34` |
| CC Gateway `dist/proxy.js` sha256 | `9892c702b77bb06c5bc12a0d8d9cf6c21daa3402b50c83c8944f0f7660d8400e` |
| old production `chelingxi-sub2api` | stopped/exited |
| old production `chelingxi-cc-gateway` | stopped/exited |

## Firewall change

| Item | Result |
|---|---:|
| Before | no UFW allow rule for `18080/tcp` |
| Command | `ufw allow 18080/tcp comment "Plan79B Sub2API production 18080"` |
| After | `18080/tcp ALLOW IN Anywhere`; `18080/tcp (v6) ALLOW IN Anywhere (v6)` |
| `18081` | not changed |
| `3012/3017` | not changed |

## Public access verification

| URL | External result |
|---|---:|
| `http://198.12.67.185:18080/` | HTTP `404`, body bucket `404 page not found`; this means the port is reachable and root route is not defined |
| `http://198.12.67.185:18080/health` | HTTP `200`, body bucket `{"status":"ok"}` |

## Safety confirmations

- No file/directory deletion.
- No DB/Redis destructive operation.
- No secret, DB URL, Redis URL, token, cookie, or raw request body written into this report.
- `18081` was not stopped/restarted/rebound.
- `3012/3017` remained untouched/free.
- Old production `18080` Docker pair is already stopped, not deleted; rollback remains possible.
