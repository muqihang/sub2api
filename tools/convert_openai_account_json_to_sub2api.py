#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import os
from datetime import datetime, timezone
from pathlib import Path
from dataclasses import dataclass
from typing import Any

try:
    from .import_openai_rt_to_sub2api import (
        DEFAULT_CLIENT_ID,
        DEFAULT_OPENAI_TOKEN_URL,
        DEFAULT_REDIRECT_URI,
        DEFAULT_SUB2API_URL,
        ScriptError,
        build_token_bundle_from_access_token,
        choose_account_name,
        json_request,
        list_openai_oauth_accounts,
        login_sub2api,
        read_env_file_defaults,
        refresh_with_openai,
        token_response_to_bundle,
        unwrap_data,
    )
except ImportError:
    from import_openai_rt_to_sub2api import (
        DEFAULT_CLIENT_ID,
        DEFAULT_OPENAI_TOKEN_URL,
        DEFAULT_REDIRECT_URI,
        DEFAULT_SUB2API_URL,
        ScriptError,
        build_token_bundle_from_access_token,
        choose_account_name,
        json_request,
        list_openai_oauth_accounts,
        login_sub2api,
        read_env_file_defaults,
        refresh_with_openai,
        token_response_to_bundle,
        unwrap_data,
    )


DEFAULT_CONCURRENCY = 10
DEFAULT_PRIORITY = 1
DEFAULT_RATE_MULTIPLIER = 1.0
DEFAULT_TIMEOUT = 30
DEFAULT_FALLBACK_PREFIX = "openai-oauth"
DEFAULT_REPORT_NAME = "convert_openai_account_report.json"


@dataclass
class ConversionResult:
    preview: str
    success: bool
    action: str
    account_name: str | None = None
    account_id: int | None = None
    message: str | None = None
    error: str | None = None

    def as_dict(self) -> dict[str, Any]:
        return {
            "preview": self.preview,
            "success": self.success,
            "action": self.action,
            "account_name": self.account_name,
            "account_id": self.account_id,
            "message": self.message,
            "error": self.error,
        }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Convert flat OpenAI account JSON files into sub2api import/export JSON."
    )
    parser.add_argument("inputs", nargs="+", help="Path(s) to source JSON files.")
    parser.add_argument("--output", help="Output JSON path. Defaults to a timestamped file in the current directory.")
    parser.add_argument(
        "--import-to-sub2api",
        action="store_true",
        help="Import the converted payload into sub2api after conversion.",
    )
    parser.add_argument(
        "--validate-only",
        action="store_true",
        help="Validate and convert only; do not import into sub2api.",
    )
    parser.add_argument("--report-file", help="Optional JSON report output path.")
    parser.add_argument("--sub2api-url", default=DEFAULT_SUB2API_URL, help="sub2api base URL.")
    parser.add_argument("--admin-email", help="sub2api admin email.")
    parser.add_argument("--admin-password", help="sub2api admin password.")
    parser.add_argument(
        "--refresh-with-rt",
        action="store_true",
        help="Refresh access_token/refresh_token through the official OpenAI refresh endpoint before exporting.",
    )
    parser.add_argument("--client-id", default=DEFAULT_CLIENT_ID, help="OpenAI OAuth client_id used for RT refresh.")
    parser.add_argument(
        "--redirect-uri",
        default=DEFAULT_REDIRECT_URI,
        help="OpenAI OAuth redirect URI used for RT refresh.",
    )
    parser.add_argument(
        "--token-url",
        default=DEFAULT_OPENAI_TOKEN_URL,
        help="OpenAI OAuth token endpoint used for RT refresh.",
    )
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT, help="HTTP timeout in seconds for RT refresh.")
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY, help="sub2api account concurrency.")
    parser.add_argument("--priority", type=int, default=DEFAULT_PRIORITY, help="sub2api account priority.")
    parser.add_argument(
        "--rate-multiplier",
        type=float,
        default=DEFAULT_RATE_MULTIPLIER,
        help="sub2api account rate multiplier.",
    )
    parser.add_argument(
        "--fallback-prefix",
        default=DEFAULT_FALLBACK_PREFIX,
        help="Fallback account name prefix when email/account id are missing.",
    )
    return parser.parse_args()


def load_json_file(path: Path) -> dict[str, Any]:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ScriptError(f"Input file not found: {path}") from exc
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Invalid JSON in {path}: {exc}") from exc


def is_sub2api_export_payload(source: Any) -> bool:
    return isinstance(source, dict) and isinstance(source.get("accounts"), list) and isinstance(source.get("proxies"), list)


def parse_iso_datetime(raw: str) -> datetime | None:
    text = str(raw or "").strip()
    if not text:
        return None
    normalized = text.replace("Z", "+00:00")
    try:
        dt = datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if dt.tzinfo is None:
        return dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def normalize_expiry_to_unix(
    bundle: dict[str, Any],
    *,
    source_expired: str = "",
    prefer_source_expired: bool = False,
) -> int:
    if prefer_source_expired:
        dt = parse_iso_datetime(source_expired)
        if dt is not None:
            return int(dt.timestamp())

    bundle_expiry = bundle.get("expires_at")
    if isinstance(bundle_expiry, (int, float)):
        return int(bundle_expiry)
    if isinstance(bundle_expiry, str):
        dt = parse_iso_datetime(bundle_expiry)
        if dt is not None:
            return int(dt.timestamp())

    dt = parse_iso_datetime(source_expired)
    if dt is not None:
        return int(dt.timestamp())

    raise ScriptError("Unable to determine expires_at from source JSON or token payload")


def build_token_bundle_from_source(
    source: dict[str, Any],
    *,
    refresh_with_rt_enabled: bool,
    client_id: str,
    redirect_uri: str,
    token_url: str,
    timeout: int,
    now: datetime | None = None,
) -> dict[str, Any]:
    current_time = now or datetime.now(timezone.utc)
    refresh_token_value = str(source.get("refresh_token", "")).strip()

    if refresh_with_rt_enabled:
        if not refresh_token_value:
            raise ScriptError("refresh_token is required when --refresh-with-rt is used")
        refreshed = refresh_with_openai(
            refresh_token_value,
            client_id=client_id,
            redirect_uri=redirect_uri,
            token_url=token_url,
            timeout=timeout,
        )
        bundle = token_response_to_bundle(refreshed, now=current_time)
        refreshed_access_token = str(refreshed.get("access_token", "")).strip()
        if refreshed_access_token:
            access_claim_bundle = build_token_bundle_from_access_token(
                refreshed_access_token,
                refresh_token=str(bundle.get("refresh_token", "")).strip(),
                now=current_time,
            )
            for key, value in access_claim_bundle.items():
                if value not in (None, "") and not str(bundle.get(key, "")).strip():
                    bundle[key] = value
    else:
        access_token_value = str(source.get("access_token", "")).strip()
        if not access_token_value:
            raise ScriptError("access_token is required for pure conversion")
        bundle = build_token_bundle_from_access_token(
            access_token_value,
            refresh_token=refresh_token_value,
            now=current_time,
        )

    for source_key, bundle_key in (
        ("email", "email"),
        ("account_id", "chatgpt_account_id"),
        ("chatgpt_account_id", "chatgpt_account_id"),
        ("chatgpt_user_id", "chatgpt_user_id"),
        ("organization_id", "organization_id"),
        ("plan_type", "plan_type"),
    ):
        value = str(source.get(source_key, "")).strip()
        if value and not str(bundle.get(bundle_key, "")).strip():
            bundle[bundle_key] = value

    id_token_value = str(source.get("id_token", "")).strip()
    if id_token_value and not str(bundle.get("id_token", "")).strip():
        bundle["id_token"] = id_token_value

    return bundle


def convert_source_account(
    source: dict[str, Any],
    *,
    refresh_with_rt: bool = False,
    client_id: str = DEFAULT_CLIENT_ID,
    redirect_uri: str = DEFAULT_REDIRECT_URI,
    token_url: str = DEFAULT_OPENAI_TOKEN_URL,
    timeout: int = DEFAULT_TIMEOUT,
    concurrency: int = DEFAULT_CONCURRENCY,
    priority: int = DEFAULT_PRIORITY,
    rate_multiplier: float = DEFAULT_RATE_MULTIPLIER,
    fallback_prefix: str = DEFAULT_FALLBACK_PREFIX,
    now: datetime | None = None,
) -> dict[str, Any]:
    current_time = now or datetime.now(timezone.utc)
    bundle = build_token_bundle_from_source(
        source,
        refresh_with_rt_enabled=refresh_with_rt,
        client_id=client_id,
        redirect_uri=redirect_uri,
        token_url=token_url,
        timeout=timeout,
        now=current_time,
    )

    account_name = str(source.get("name", "")).strip() or choose_account_name(
        bundle,
        fallback_prefix=fallback_prefix,
        index=1,
        now=current_time,
    )
    effective_client_id = str(source.get("client_id", "")).strip() or client_id
    expires_at_unix = normalize_expiry_to_unix(
        bundle,
        source_expired=str(source.get("expired", "")).strip(),
        prefer_source_expired=not refresh_with_rt,
    )

    credentials: dict[str, Any] = {
        "access_token": bundle["access_token"],
        "expires_at": expires_at_unix,
    }
    refresh_token_value = str(bundle.get("refresh_token", "")).strip()
    if refresh_token_value:
        credentials["refresh_token"] = refresh_token_value
        credentials["client_id"] = effective_client_id

    expires_in_value = bundle.get("expires_in")
    if isinstance(expires_in_value, (int, float)) and int(expires_in_value) >= 0:
        credentials["expires_in"] = int(expires_in_value)

    for key in ("id_token", "email", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "plan_type"):
        value = bundle.get(key)
        if value not in (None, ""):
            credentials[key] = value

    account: dict[str, Any] = {
        "name": account_name,
        "platform": "openai",
        "type": "oauth",
        "credentials": credentials,
        "extra": {
            "email": str(bundle.get("email", "")).strip(),
            "openai_oauth_responses_websockets_v2_enabled": False,
            "openai_oauth_responses_websockets_v2_mode": "off",
        },
        "concurrency": concurrency,
        "priority": priority,
        "rate_multiplier": rate_multiplier,
        "auto_pause_on_expired": True,
    }

    notes_value = str(source.get("notes", "")).strip()
    if notes_value:
        account["notes"] = notes_value

    return account


def build_export_payload(accounts: list[dict[str, Any]], *, now: datetime | None = None) -> dict[str, Any]:
    current_time = now or datetime.now(timezone.utc)
    return {
        "type": "sub2api-data",
        "version": 1,
        "exported_at": current_time.astimezone(timezone.utc).isoformat().replace("+00:00", "Z"),
        "proxies": [],
        "accounts": accounts,
    }


def default_output_path(cwd: Path, *, now: datetime | None = None) -> Path:
    current_time = now or datetime.now(timezone.utc)
    timestamp = current_time.strftime("%Y%m%d%H%M%S")
    return cwd / f"sub2api-account-{timestamp}.json"


def collect_accounts_from_source(
    source: Any,
    *,
    refresh_with_rt: bool,
    client_id: str,
    redirect_uri: str,
    token_url: str,
    timeout: int,
    concurrency: int,
    priority: int,
    rate_multiplier: float,
    fallback_prefix: str,
    now: datetime | None = None,
) -> list[dict[str, Any]]:
    if is_sub2api_export_payload(source):
        return list(source.get("accounts") or [])
    if isinstance(source, list):
        return [
            convert_source_account(
                item,
                refresh_with_rt=refresh_with_rt,
                client_id=client_id,
                redirect_uri=redirect_uri,
                token_url=token_url,
                timeout=timeout,
                concurrency=concurrency,
                priority=priority,
                rate_multiplier=rate_multiplier,
                fallback_prefix=fallback_prefix,
                now=now,
            )
            for item in source
        ]
    if isinstance(source, dict):
        return [
            convert_source_account(
                source,
                refresh_with_rt=refresh_with_rt,
                client_id=client_id,
                redirect_uri=redirect_uri,
                token_url=token_url,
                timeout=timeout,
                concurrency=concurrency,
                priority=priority,
                rate_multiplier=rate_multiplier,
                fallback_prefix=fallback_prefix,
                now=now,
            )
        ]
    raise ScriptError("Source JSON must be an object, array, or sub2api export payload")


def import_payload_to_sub2api(
    payload: dict[str, Any],
    *,
    repo_root: Path,
    sub2api_url: str,
    admin_email: str = "",
    admin_password: str = "",
    timeout: int,
) -> dict[str, Any]:
    env_defaults = read_env_file_defaults(repo_root)
    email = admin_email or os.getenv("SUB2API_ADMIN_EMAIL") or env_defaults.get("ADMIN_EMAIL", "")
    password = admin_password or os.getenv("SUB2API_ADMIN_PASSWORD") or env_defaults.get("ADMIN_PASSWORD", "")
    if not email or not password:
        raise ScriptError("sub2api admin credentials are required for import")
    bearer_token = login_sub2api(sub2api_url, email, password, timeout)
    response_payload = json_request(
        f"{sub2api_url.rstrip('/')}/api/v1/admin/accounts/data",
        method="POST",
        headers={"Authorization": f"Bearer {bearer_token}"},
        payload={"data": payload, "skip_default_group_bind": True},
        timeout=timeout,
    )
    return unwrap_data(response_payload) or {}


def build_results_for_validate(payload: dict[str, Any]) -> list[ConversionResult]:
    results: list[ConversionResult] = []
    for account in payload.get("accounts") or []:
        preview = str(account.get("name", "")).strip() or "converted-account"
        results.append(
            ConversionResult(
                preview=preview,
                success=True,
                action="validated",
                account_name=preview,
                message="Converted to sub2api import format",
            )
        )
    return results


def _account_refresh_token(account: dict[str, Any]) -> str:
    credentials = account.get("credentials") or {}
    return str(credentials.get("refresh_token", "")).strip()


def _account_email(account: dict[str, Any]) -> str:
    credentials = account.get("credentials") or {}
    extra = account.get("extra") or {}
    return str(credentials.get("email") or extra.get("email") or "").strip().lower()


def partition_accounts_for_import(
    payload_accounts: list[dict[str, Any]],
    existing_accounts: list[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[ConversionResult]]:
    existing_by_refresh = {
        _account_refresh_token(account): account
        for account in existing_accounts
        if _account_refresh_token(account)
    }
    existing_by_email = {
        _account_email(account): account
        for account in existing_accounts
        if _account_email(account)
    }

    filtered: list[dict[str, Any]] = []
    results: list[ConversionResult] = []
    seen_refresh: dict[str, str] = {}
    seen_email: dict[str, str] = {}

    for account in payload_accounts:
        name = str(account.get("name", "")).strip() or "converted-account"
        refresh_token_value = _account_refresh_token(account)
        email_value = _account_email(account)

        if refresh_token_value and refresh_token_value in seen_refresh:
            results.append(
                ConversionResult(
                    preview=name,
                    success=False,
                    action="skipped_duplicate",
                    account_name=name,
                    error=f"Duplicate refresh_token in this batch (already seen in {seen_refresh[refresh_token_value]})",
                )
            )
            continue
        if email_value and email_value in seen_email:
            results.append(
                ConversionResult(
                    preview=name,
                    success=False,
                    action="skipped_duplicate",
                    account_name=name,
                    error=f"Duplicate email in this batch (already seen in {seen_email[email_value]})",
                )
            )
            continue
        if refresh_token_value and refresh_token_value in existing_by_refresh:
            existing = existing_by_refresh[refresh_token_value]
            results.append(
                ConversionResult(
                    preview=name,
                    success=False,
                    action="skipped_duplicate",
                    account_name=name,
                    error=f"Duplicate refresh_token matches existing account #{existing.get('id')} {existing.get('name', '')}".strip(),
                )
            )
            continue
        if email_value and email_value in existing_by_email:
            existing = existing_by_email[email_value]
            results.append(
                ConversionResult(
                    preview=name,
                    success=False,
                    action="skipped_duplicate",
                    account_name=name,
                    error=f"Duplicate email matches existing account #{existing.get('id')} {existing.get('name', '')}".strip(),
                )
            )
            continue

        if refresh_token_value:
            seen_refresh[refresh_token_value] = name
        if email_value:
            seen_email[email_value] = name
        filtered.append(account)

    return filtered, results


def build_results_for_import(payload: dict[str, Any], import_result: dict[str, Any]) -> list[ConversionResult]:
    errors = import_result.get("errors") or []
    errors_by_name: dict[str, list[str]] = {}
    for item in errors:
        name = str(item.get("name", "")).strip()
        message = str(item.get("message", "")).strip() or "Import failed"
        errors_by_name.setdefault(name, []).append(message)

    created = int(import_result.get("account_created") or 0)
    failed = int(import_result.get("account_failed") or 0)
    summary_message = f"Imported via /admin/accounts/data (created={created}, failed={failed})"

    results: list[ConversionResult] = []
    for account in payload.get("accounts") or []:
        preview = str(account.get("name", "")).strip() or "converted-account"
        messages = errors_by_name.get(preview)
        if messages:
            results.append(
                ConversionResult(
                    preview=preview,
                    success=False,
                    action="import_failed",
                    account_name=preview,
                    error="; ".join(messages),
                )
            )
            continue
        results.append(
            ConversionResult(
                preview=preview,
                success=True,
                action="imported",
                account_name=preview,
                message=summary_message,
            )
        )
    return results


def write_report(results: list[ConversionResult], report_file: str | None) -> Path:
    target = Path(report_file or DEFAULT_REPORT_NAME).expanduser().resolve()
    report = {
        "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "summary": {
            "total": len(results),
            "success": sum(1 for item in results if item.success),
            "failed": sum(1 for item in results if not item.success),
        },
        "results": [item.as_dict() for item in results],
    }
    target.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return target


def print_summary(results: list[ConversionResult], report_path: Path) -> None:
    success_count = sum(1 for item in results if item.success)
    failed_count = len(results) - success_count
    print(f"Processed {len(results)} account file item(s): {success_count} success, {failed_count} failed")
    for item in results:
        if item.success:
            print(f"[OK] {item.preview} -> {item.action} name={item.account_name or '-'}")
        else:
            print(f"[FAIL] {item.preview} -> {item.error}")
    print(f"Report written to {report_path}")


def main() -> int:
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[1]
    accounts: list[dict[str, Any]] = []
    current_time = datetime.now(timezone.utc)

    for raw_path in args.inputs:
        source_path = Path(raw_path).expanduser().resolve()
        source = load_json_file(source_path)
        accounts.extend(
            collect_accounts_from_source(
                source,
                refresh_with_rt=args.refresh_with_rt,
                client_id=args.client_id,
                redirect_uri=args.redirect_uri,
                token_url=args.token_url,
                timeout=args.timeout,
                concurrency=args.concurrency,
                priority=args.priority,
                rate_multiplier=args.rate_multiplier,
                fallback_prefix=args.fallback_prefix,
                now=current_time,
            )
        )

    payload = build_export_payload(accounts, now=current_time)

    should_write_output = bool(args.output) or (not args.import_to_sub2api and not args.validate_only)
    if should_write_output:
        output_path = Path(args.output).expanduser().resolve() if args.output else default_output_path(Path.cwd(), now=current_time)
        output_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(output_path)

    results: list[ConversionResult]
    if args.validate_only:
        results = build_results_for_validate(payload)
        report_path = write_report(results, args.report_file)
        print_summary(results, report_path)
        return 0

    if args.import_to_sub2api:
        env_defaults = read_env_file_defaults(repo_root)
        admin_email = args.admin_email or os.getenv("SUB2API_ADMIN_EMAIL") or env_defaults.get("ADMIN_EMAIL", "")
        admin_password = args.admin_password or os.getenv("SUB2API_ADMIN_PASSWORD") or env_defaults.get("ADMIN_PASSWORD", "")
        if not admin_email or not admin_password:
            raise ScriptError("sub2api admin credentials are required for import")
        bearer_token = login_sub2api(args.sub2api_url, admin_email, admin_password, args.timeout)
        existing_accounts = list_openai_oauth_accounts(args.sub2api_url, bearer_token, args.timeout)
        filtered_accounts, duplicate_results = partition_accounts_for_import(payload.get("accounts") or [], existing_accounts)
        results = list(duplicate_results)
        if filtered_accounts:
            filtered_payload = dict(payload)
            filtered_payload["accounts"] = filtered_accounts
            response_payload = json_request(
                f"{args.sub2api_url.rstrip('/')}/api/v1/admin/accounts/data",
                method="POST",
                headers={"Authorization": f"Bearer {bearer_token}"},
                payload={"data": filtered_payload, "skip_default_group_bind": True},
                timeout=args.timeout,
            )
            import_result = unwrap_data(response_payload) or {}
            results.extend(build_results_for_import(filtered_payload, import_result))
        report_path = write_report(results, args.report_file)
        print_summary(results, report_path)
        return 0 if results and all(item.success for item in results) else (0 if not results else 1)

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ScriptError as exc:
        raise SystemExit(str(exc))
