#!/usr/bin/env python3
"""Batch import Anthropic setup-token accounts through Formal Pool onboarding.

The script intentionally never prints setup tokens/JWTs. It drives the existing
admin API flow:
  create session -> test proxy -> optional admin attestation -> setup-token create

Typical safe plan preview:
  python tools/formal_pool_setup_token_import.py \
    --source-dir '/path/to/[max] [email]' \
    --proxy-ids 6,8,12,13 \
    --group-id 1 \
    --target-count 4

Production execution requires --execute and an admin JWT. If you intentionally
bypass the browser egress check for a trusted setup-token batch, also pass the
explicit admin-bypass flag and a reason; this makes the risky path auditable.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib import error, request

SETUP_TOKEN_RE = re.compile(r"sk-ant-sid[^\s\"'<>]+")
TRAILING_TOKEN_PUNCTUATION = ",;.)]}"
EMAIL_IN_BRACKETS_RE = re.compile(r"\[([^\[\]]+@[^\[\]]+)\]")
BEARER_RE = re.compile(r"(?i)\bBearer\s+[A-Za-z0-9._-]+")
AUTHORIZATION_HEADER_RE = re.compile(r"(?i)\bAuthorization\s*:\s*(?:Bearer\s+)?[A-Za-z0-9._-]+")
SENSITIVE_ASSIGNMENT_RE = re.compile(
    r"(?i)\b(access_token|refresh_token|session_key|authorization|auth_token|jwt|token|cookie|password|client_secret)\s*=\s*([^\s,;}]+)"
)
SENSITIVE_JSON_FIELD_RE = re.compile(
    r'(?i)(["\'](?:access_token|refresh_token|session_key|authorization|auth_token|jwt|token|cookie|password|client_secret)["\']\s*:\s*["\'])(.*?)(["\'])'
)


@dataclass(frozen=True)
class SetupTokenEntry:
    email: str
    session_key: str
    source_file: str = ""


@dataclass(frozen=True)
class ImportAttempt:
    email: str
    session_key: str
    proxy_id: int
    account_name: str


@dataclass(frozen=True)
class ImportPlan:
    attempts: list[ImportAttempt]
    skipped_emails: list[str]


class FormalPoolImportError(RuntimeError):
    pass


def sanitize_for_log(value: Any) -> str:
    text = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False, default=str)
    text = SETUP_TOKEN_RE.sub("sk-ant-sid[redacted]", text)
    text = AUTHORIZATION_HEADER_RE.sub("Authorization: [redacted]", text)
    text = BEARER_RE.sub("Bearer [redacted]", text)
    text = SENSITIVE_ASSIGNMENT_RE.sub(lambda m: f"{m.group(1)}=[redacted]", text)
    text = SENSITIVE_JSON_FIELD_RE.sub(lambda m: f"{m.group(1)}[redacted]{m.group(3)}", text)
    return text[:2000]


def _normalize_setup_token(raw: str) -> str:
    return raw.strip().rstrip(TRAILING_TOKEN_PUNCTUATION)


def _email_from_filename(path: Path) -> str:
    matches = EMAIL_IN_BRACKETS_RE.findall(path.name)
    return matches[-1].strip() if matches else ""


def parse_setup_token_entries_from_directory(source_dir: Path) -> list[SetupTokenEntry]:
    source_dir = Path(source_dir)
    if not source_dir.exists() or not source_dir.is_dir():
        raise FormalPoolImportError(f"source directory not found: {source_dir}")
    seen_tokens: set[str] = set()
    entries: list[SetupTokenEntry] = []
    for path in sorted(source_dir.glob("*.txt")):
        raw = path.read_text(encoding="utf-8", errors="replace")
        tokens = SETUP_TOKEN_RE.findall(raw)
        if not tokens:
            continue
        email = _email_from_filename(path)
        for token in tokens:
            token = _normalize_setup_token(token)
            if not token or token in seen_tokens:
                continue
            seen_tokens.add(token)
            entries.append(SetupTokenEntry(email=email, session_key=token, source_file=str(path)))
            break
    return entries


def parse_proxy_ids(raw: str) -> list[int]:
    out: list[int] = []
    for part in re.split(r"[,\s]+", raw.strip()):
        if not part:
            continue
        try:
            value = int(part)
        except ValueError as exc:
            raise FormalPoolImportError(f"invalid proxy id: {part}") from exc
        if value <= 0:
            raise FormalPoolImportError("proxy ids must be positive")
        out.append(value)
    if not out:
        raise FormalPoolImportError("at least one proxy id is required")
    return out


def build_import_plan(
    entries: list[SetupTokenEntry],
    proxy_ids: list[int],
    date_prefix: str,
    target_count: int,
) -> ImportPlan:
    if target_count <= 0:
        raise FormalPoolImportError("target count must be positive")
    attempts: list[ImportAttempt] = []
    skipped: list[str] = []
    limit = min(target_count, len(proxy_ids), len(entries))
    for index, entry in enumerate(entries):
        email = entry.email or f"unknown-{index + 1}"
        if len(attempts) < limit:
            attempts.append(
                ImportAttempt(
                    email=email,
                    session_key=entry.session_key,
                    proxy_id=proxy_ids[len(attempts)],
                    account_name=f"{date_prefix}-{email}",
                )
            )
        else:
            skipped.append(email)
    return ImportPlan(attempts=attempts, skipped_emails=skipped)


class AdminAPIClient:
    def __init__(self, api_base: str, auth_token: str, timeout: int = 180) -> None:
        self.api_base = api_base.rstrip("/")
        self.auth_token = auth_token.strip()
        self.timeout = timeout
        if not self.auth_token:
            raise FormalPoolImportError("admin auth token is required")

    def post(self, path: str, payload: dict[str, Any] | None = None, timeout: int | None = None) -> dict[str, Any]:
        data = json.dumps(payload or {}).encode("utf-8")
        req = request.Request(
            self.api_base + path,
            data=data,
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + self.auth_token},
            method="POST",
        )
        try:
            with request.urlopen(req, timeout=timeout or self.timeout) as resp:
                raw = resp.read().decode("utf-8")
        except error.HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            raise FormalPoolImportError(f"POST {path} HTTP {exc.code}: {sanitize_for_log(body)}") from exc
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise FormalPoolImportError(f"POST {path} returned non-json: {sanitize_for_log(raw)}") from exc
        if isinstance(parsed, dict) and "code" in parsed:
            if parsed.get("code") != 0:
                raise FormalPoolImportError(f"POST {path} API error: {sanitize_for_log(parsed)}")
            data_obj = parsed.get("data")
            return data_obj if isinstance(data_obj, dict) else {}
        return parsed if isinstance(parsed, dict) else {}


def run_attempt(
    client: AdminAPIClient,
    attempt: ImportAttempt,
    group_id: int,
    pool_profile: str,
    concurrency: int,
    allow_admin_browser_egress_attestation_bypass: bool,
    attestation_bypass_reason: str,
    note: str,
) -> dict[str, Any]:
    result: dict[str, Any] = {
        "email": attempt.email,
        "account_name": attempt.account_name,
        "proxy_id": attempt.proxy_id,
        "status": "started",
    }
    session = client.post(
        "/admin/claude-onboarding/sessions",
        {
            "proxy_mode": "existing",
            "proxy_id": attempt.proxy_id,
            "pool_profile": pool_profile,
            "group_id": group_id,
            "account_name": attempt.account_name,
            "notes": note,
            "concurrency": concurrency,
        },
    )
    session_id = str(session.get("id") or "")
    if not session_id:
        raise FormalPoolImportError("create session did not return id")
    result["session_id"] = session_id

    client.post(f"/admin/claude-onboarding/sessions/{session_id}/test-proxy", {}, timeout=120)
    result["proxy_test"] = "passed"

    if allow_admin_browser_egress_attestation_bypass:
        client.post(
            f"/admin/claude-onboarding/sessions/{session_id}/browser-egress-attestation",
            {
                "confirmed": True,
                "verification_code": f"batch-st-import-{int(time.time())}",
                "reason": attestation_bypass_reason,
            },
            timeout=60,
        )
        result["browser_attestation"] = "admin_bypass_confirmed_without_browser_check"

    created = client.post(
        f"/admin/claude-onboarding/sessions/{session_id}/setup-token-cookie-auth-and-create",
        {"session_key": attempt.session_key},
        timeout=240,
    )
    result.update(
        {
            "status": "created",
            "account_id": created.get("account_id"),
            "session_status": created.get("status"),
            "runtime_registered": created.get("cc_gateway_runtime_registered"),
        }
    )
    return result


def _validate_positive(name: str, value: int) -> None:
    if value <= 0:
        raise FormalPoolImportError(f"{name} must be positive")


def _dry_run_result(plan: ImportPlan) -> dict[str, Any]:
    return {
        "status": "dry_run",
        "created_count": 0,
        "planned_count": len(plan.attempts),
        "results": [
            {
                "email": attempt.email,
                "account_name": attempt.account_name,
                "proxy_id": attempt.proxy_id,
                "status": "planned",
            }
            for attempt in plan.attempts
        ],
        "skipped_emails": plan.skipped_emails,
    }


def run_import(args: argparse.Namespace) -> dict[str, Any]:
    _validate_positive("group_id", args.group_id)
    _validate_positive("concurrency", args.concurrency)
    _validate_positive("timeout", args.timeout)
    _validate_positive("target_count", args.target_count)
    entries = parse_setup_token_entries_from_directory(Path(args.source_dir))
    proxy_ids = parse_proxy_ids(args.proxy_ids)
    date_prefix = args.date_prefix or datetime.now(timezone.utc).strftime("%Y%m%d")
    plan = build_import_plan(entries, proxy_ids, date_prefix, args.target_count)
    if not args.execute:
        return _dry_run_result(plan)
    if args.allow_admin_browser_egress_attestation_bypass and not args.attestation_bypass_reason.strip():
        raise FormalPoolImportError("attestation bypass requires --attestation-bypass-reason")
    client = AdminAPIClient(args.api_base, args.auth_token, timeout=args.timeout)
    results: list[dict[str, Any]] = []
    for attempt in plan.attempts:
        try:
            results.append(
                run_attempt(
                    client,
                    attempt,
                    group_id=args.group_id,
                    pool_profile=args.pool_profile,
                    concurrency=args.concurrency,
                    allow_admin_browser_egress_attestation_bypass=args.allow_admin_browser_egress_attestation_bypass,
                    attestation_bypass_reason=args.attestation_bypass_reason,
                    note=args.note,
                )
            )
        except Exception as exc:  # keep batch resilient; sanitized below
            results.append(
                {
                    "email": attempt.email,
                    "account_name": attempt.account_name,
                    "proxy_id": attempt.proxy_id,
                    "status": "failed",
                    "error": sanitize_for_log(str(exc)),
                }
            )
            if args.stop_on_failure:
                break
    return {"created_count": sum(1 for r in results if r.get("status") == "created"), "results": results, "skipped_emails": plan.skipped_emails}


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Import Formal Pool setup-token accounts through the admin onboarding API.")
    parser.add_argument("--source-dir", required=True, help="Directory containing .txt setup-token materials.")
    parser.add_argument("--api-base", default="http://127.0.0.1:18080/api/v1")
    parser.add_argument("--auth-token", default="", help="Admin JWT. Prefer env SUB2API_ADMIN_JWT instead of CLI.")
    parser.add_argument("--proxy-ids", required=True, help="Comma/space separated existing proxy IDs.")
    parser.add_argument("--group-id", type=int, required=True)
    parser.add_argument("--target-count", type=int, default=4)
    parser.add_argument("--date-prefix", default="")
    parser.add_argument("--pool-profile", default="normal", choices=("normal", "aggressive"))
    parser.add_argument("--concurrency", type=int, default=5)
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--execute", action="store_true", help="Execute API writes. Omit for dry-run plan output only.")
    parser.add_argument(
        "--allow-admin-browser-egress-attestation-bypass",
        action="store_true",
        help="Explicitly bypass browser egress verification after proxy health passes. Use only for trusted ST batches.",
    )
    parser.add_argument("--attestation-bypass-reason", default="")
    parser.add_argument("--stop-on-failure", action="store_true")
    parser.add_argument("--note", default="formal-pool setup-token batch import")
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_arg_parser()
    args = parser.parse_args(argv)
    if not args.auth_token:
        import os

        args.auth_token = os.environ.get("SUB2API_ADMIN_JWT", "")
    try:
        result = run_import(args)
    except Exception as exc:
        print(json.dumps({"error": sanitize_for_log(str(exc))}, ensure_ascii=False), file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
