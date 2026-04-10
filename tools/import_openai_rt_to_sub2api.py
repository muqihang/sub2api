#!/usr/bin/env python3

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from urllib import error, parse, request


DEFAULT_CLIENT_ID = "app_LlGpXReQgckcGGUo2JrYvtJK"
DEFAULT_REDIRECT_URI = "com.openai.chat://auth0.openai.com/ios/com.openai.chat/callback"
DEFAULT_OPENAI_TOKEN_URL = "https://auth.openai.com/oauth/token"
DEFAULT_SUB2API_URL = "http://127.0.0.1:18081"
DEFAULT_FALLBACK_PREFIX = "openai-oauth"
DEFAULT_REPORT_NAME = "import_openai_token_report.json"


class ScriptError(RuntimeError):
    pass


@dataclass
class ImportResult:
    refresh_token: str
    success: bool
    action: str
    account_id: int | None = None
    account_name: str | None = None
    message: str | None = None
    error: str | None = None

    def as_dict(self) -> dict[str, Any]:
        return {
            "refresh_token": self.refresh_token,
            "success": self.success,
            "action": self.action,
            "account_id": self.account_id,
            "account_name": self.account_name,
            "message": self.message,
            "error": self.error,
        }


def normalize_refresh_tokens(raw_text: str) -> list[str]:
    seen: set[str] = set()
    tokens: list[str] = []
    for line in raw_text.splitlines():
        token = line.strip()
        if not token or token in seen:
            continue
        seen.add(token)
        tokens.append(token)
    return tokens


def parse_access_token_entries(raw_text: str) -> list[dict[str, str]]:
    seen: set[tuple[str, str]] = set()
    entries: list[dict[str, str]] = []
    for line in raw_text.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        if "----" in stripped:
            access_token, refresh_token = stripped.split("----", 1)
        else:
            access_token, refresh_token = stripped, ""
        access_token = access_token.strip()
        refresh_token = refresh_token.strip()
        if not access_token:
            continue
        key = (access_token, refresh_token)
        if key in seen:
            continue
        seen.add(key)
        entries.append({"access_token": access_token, "refresh_token": refresh_token})
    return entries


def decode_jwt_payload_unverified(token: str) -> dict[str, Any]:
    parts = token.split(".")
    if len(parts) != 3 or not parts[1]:
        return {}
    payload = parts[1]
    padding = "=" * (-len(payload) % 4)
    try:
        decoded = base64.urlsafe_b64decode(payload + padding)
        return json.loads(decoded.decode("utf-8"))
    except (ValueError, json.JSONDecodeError, UnicodeDecodeError):
        return {}


def _apply_openai_claims(bundle: dict[str, Any], claims: dict[str, Any]) -> None:
    for source_key, bundle_key in (
        ("email", "email"),
        ("chatgpt_account_id", "chatgpt_account_id"),
        ("chatgpt_user_id", "chatgpt_user_id"),
        ("organization_id", "organization_id"),
    ):
        value = claims.get(source_key)
        if value:
            bundle[bundle_key] = value

    auth_section = claims.get("https://api.openai.com/auth", {})
    if isinstance(auth_section, dict):
        for source_key, bundle_key in (
            ("chatgpt_account_id", "chatgpt_account_id"),
            ("chatgpt_user_id", "chatgpt_user_id"),
            ("organization_id", "organization_id"),
            ("chatgpt_plan_type", "plan_type"),
        ):
            value = auth_section.get(source_key)
            if value:
                bundle[bundle_key] = value

    profile_section = claims.get("https://api.openai.com/profile", {})
    if isinstance(profile_section, dict):
        email = profile_section.get("email")
        if email:
            bundle["email"] = email


def choose_account_name(
    token_info: dict[str, Any],
    fallback_prefix: str = DEFAULT_FALLBACK_PREFIX,
    index: int = 1,
    now: datetime | None = None,
) -> str:
    for key in ("email", "chatgpt_account_id", "chatgpt_user_id", "organization_id"):
        value = str(token_info.get(key, "")).strip()
        if value:
            return value
    current_time = now or datetime.now(timezone.utc)
    return f"{fallback_prefix}-{current_time.strftime('%Y%m%d-%H%M%S')}-{index:02d}"


def find_existing_account(accounts: list[dict[str, Any]], refresh_token: str) -> dict[str, Any] | None:
    target = refresh_token.strip()
    for account in accounts:
        credentials = account.get("credentials") or {}
        existing_refresh_token = str(credentials.get("refresh_token", "")).strip()
        if existing_refresh_token == target:
            return account
    return None


def find_existing_account_by_access_token(accounts: list[dict[str, Any]], access_token: str) -> dict[str, Any] | None:
    target = access_token.strip()
    for account in accounts:
        credentials = account.get("credentials") or {}
        existing_access_token = str(credentials.get("access_token", "")).strip()
        if existing_access_token == target:
            return account
    return None


def token_response_to_bundle(response_data: dict[str, Any], now: datetime | None = None) -> dict[str, Any]:
    current_time = now or datetime.now(timezone.utc)
    expires_in = int(response_data.get("expires_in", 0) or 0)
    expires_at = current_time + timedelta(seconds=max(expires_in, 0))

    bundle: dict[str, Any] = {
        "access_token": response_data["access_token"],
        "refresh_token": response_data.get("refresh_token", ""),
        "expires_in": expires_in,
        "expires_at": expires_at.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }
    if response_data.get("id_token"):
        bundle["id_token"] = response_data["id_token"]
        claims = decode_jwt_payload_unverified(str(response_data["id_token"]))
        _apply_openai_claims(bundle, claims)

    if response_data.get("email"):
        bundle["email"] = response_data["email"]
    return bundle


def build_token_bundle_from_access_token(
    access_token: str,
    refresh_token: str = "",
    now: datetime | None = None,
) -> dict[str, Any]:
    current_time = now or datetime.now(timezone.utc)
    claims = decode_jwt_payload_unverified(access_token)
    exp = claims.get("exp")
    expires_at_dt: datetime
    if isinstance(exp, (int, float)) and exp > 0:
        expires_at_dt = datetime.fromtimestamp(exp, tz=timezone.utc)
    else:
        expires_at_dt = current_time + timedelta(minutes=55)

    bundle: dict[str, Any] = {
        "access_token": access_token,
        "refresh_token": refresh_token,
        "expires_at": expires_at_dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "expires_in": max(int((expires_at_dt - current_time).total_seconds()), 0),
    }
    if claims:
        _apply_openai_claims(bundle, claims)
    return bundle


def build_account_payload(name: str, token_bundle: dict[str, Any], client_id: str = DEFAULT_CLIENT_ID) -> dict[str, Any]:
    credentials = {
        "access_token": token_bundle["access_token"],
        "expires_at": token_bundle["expires_at"],
    }
    refresh_token = str(token_bundle.get("refresh_token", "")).strip()
    if refresh_token:
        credentials["refresh_token"] = refresh_token
        credentials["client_id"] = client_id
    for key in ("id_token", "email", "chatgpt_account_id", "chatgpt_user_id", "organization_id", "plan_type"):
        value = token_bundle.get(key)
        if value:
            credentials[key] = value

    return {
        "name": name,
        "platform": "openai",
        "type": "oauth",
        "credentials": credentials,
        "concurrency": 1,
        "priority": 0,
        "confirm_mixed_channel_risk": True,
    }


def build_update_payload(
    existing_account: dict[str, Any],
    name: str,
    token_bundle: dict[str, Any],
    client_id: str = DEFAULT_CLIENT_ID,
) -> dict[str, Any]:
    existing_credentials = dict(existing_account.get("credentials") or {})
    update_payload = build_account_payload(name=name, token_bundle=token_bundle, client_id=client_id)
    merged_credentials = dict(existing_credentials)
    merged_credentials.update(update_payload["credentials"])
    new_refresh_token = str(token_bundle.get("refresh_token", "")).strip()
    if not new_refresh_token and "refresh_token" in existing_credentials:
        merged_credentials["refresh_token"] = existing_credentials["refresh_token"]
        if "client_id" in existing_credentials:
            merged_credentials["client_id"] = existing_credentials["client_id"]
    return {
        "name": name,
        "credentials": merged_credentials,
        "confirm_mixed_channel_risk": True,
    }


def read_env_file_defaults(repo_root: Path) -> dict[str, str]:
    env_path = repo_root / "deploy" / ".env"
    values: dict[str, str] = {}
    if not env_path.exists():
        return values
    for line in env_path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        key, value = stripped.split("=", 1)
        values[key] = value
    return values


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Import OpenAI RT or AT tokens into sub2api with create/update behavior."
    )
    parser.add_argument("--file", help="Text file containing token lines.")
    parser.add_argument("--sub2api-url", default=DEFAULT_SUB2API_URL, help="sub2api base URL.")
    parser.add_argument("--admin-email", help="sub2api admin email.")
    parser.add_argument("--admin-password", help="sub2api admin password.")
    parser.add_argument("--client-id", default=DEFAULT_CLIENT_ID, help="OAuth client_id used for refresh.")
    parser.add_argument("--redirect-uri", default=DEFAULT_REDIRECT_URI, help="OAuth redirect_uri for refresh request.")
    parser.add_argument("--openai-token-url", default=DEFAULT_OPENAI_TOKEN_URL, help="OpenAI token endpoint.")
    parser.add_argument("--mode", choices=("rt", "at"), default="rt", help="Import mode: refresh-token or access-token.")
    parser.add_argument("--report-file", help="Optional JSON report output path.")
    parser.add_argument("--fallback-prefix", default=DEFAULT_FALLBACK_PREFIX, help="Fallback account name prefix.")
    parser.add_argument("--timeout", type=int, default=30, help="HTTP timeout in seconds.")
    parser.add_argument("--retries", type=int, default=3, help="Per-token retry attempts before skipping.")
    parser.add_argument("--validate-only", action="store_true", help="Validate/parse only; do not import into sub2api.")
    parser.add_argument("--stdin", action="store_true", help="Read token lines from stdin.")
    return parser


def gather_input_text(args: argparse.Namespace) -> str:
    if args.file:
        return Path(args.file).read_text(encoding="utf-8")
    if args.stdin or not sys.stdin.isatty():
        return sys.stdin.read()

    prompt = "Paste refresh_token lines, then press Enter on an empty line to finish:"
    if args.mode == "at":
        prompt = "Paste access_token lines (or AT----RT pairs), then press Enter on an empty line to finish:"
    print(prompt, file=sys.stderr)
    lines: list[str] = []
    while True:
        try:
            line = input()
        except EOFError:
            break
        if not line.strip():
            break
        lines.append(line)
    return "\n".join(lines)


def json_request(
    url: str,
    *,
    method: str = "GET",
    headers: dict[str, str] | None = None,
    payload: dict[str, Any] | None = None,
    timeout: int = 30,
) -> dict[str, Any]:
    body = None
    request_headers = dict(headers or {})
    if payload is not None:
        body = json.dumps(payload).encode("utf-8")
        request_headers.setdefault("Content-Type", "application/json")
    req = request.Request(url, data=body, headers=request_headers, method=method)
    try:
        with request.urlopen(req, timeout=timeout) as resp:
            response_body = resp.read().decode("utf-8")
    except error.HTTPError as exc:
        response_body = exc.read().decode("utf-8", errors="replace")
        raise ScriptError(f"HTTP {exc.code} {url}: {response_body}") from exc
    except error.URLError as exc:
        raise ScriptError(f"Request failed for {url}: {exc}") from exc
    try:
        return json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Non-JSON response from {url}: {response_body}") from exc


def unwrap_data(payload: dict[str, Any]) -> Any:
    if "data" in payload:
        return payload["data"]
    return payload


def login_sub2api(base_url: str, email: str, password: str, timeout: int) -> str:
    payload = json_request(
        f"{base_url.rstrip('/')}/api/v1/auth/login",
        method="POST",
        payload={"email": email, "password": password},
        timeout=timeout,
    )
    data = unwrap_data(payload)
    token = str((data or {}).get("access_token", "")).strip()
    if not token:
        raise ScriptError("sub2api login succeeded but no access_token was returned")
    return token


def list_openai_oauth_accounts(base_url: str, bearer_token: str, timeout: int) -> list[dict[str, Any]]:
    page = 1
    items: list[dict[str, Any]] = []
    while True:
        query = parse.urlencode(
            {"page": page, "page_size": 200, "platform": "openai", "type": "oauth", "lite": "false"}
        )
        payload = json_request(
            f"{base_url.rstrip('/')}/api/v1/admin/accounts?{query}",
            headers={"Authorization": f"Bearer {bearer_token}"},
            timeout=timeout,
        )
        data = unwrap_data(payload) or {}
        page_items = list(data.get("items") or [])
        items.extend(page_items)
        pages = int(data.get("pages") or 1)
        if page >= pages:
            break
        page += 1
    return items


def create_account(base_url: str, bearer_token: str, payload: dict[str, Any], timeout: int) -> dict[str, Any]:
    response_payload = json_request(
        f"{base_url.rstrip('/')}/api/v1/admin/accounts",
        method="POST",
        headers={"Authorization": f"Bearer {bearer_token}"},
        payload=payload,
        timeout=timeout,
    )
    return unwrap_data(response_payload) or {}


def update_account(
    base_url: str, bearer_token: str, account_id: int, payload: dict[str, Any], timeout: int
) -> dict[str, Any]:
    response_payload = json_request(
        f"{base_url.rstrip('/')}/api/v1/admin/accounts/{account_id}",
        method="PUT",
        headers={"Authorization": f"Bearer {bearer_token}"},
        payload=payload,
        timeout=timeout,
    )
    return unwrap_data(response_payload) or {}


def refresh_with_openai(
    refresh_token: str,
    *,
    client_id: str,
    redirect_uri: str,
    token_url: str,
    timeout: int,
) -> dict[str, Any]:
    payload = json_request(
        token_url,
        method="POST",
        payload={
            "client_id": client_id,
            "grant_type": "refresh_token",
            "redirect_uri": redirect_uri,
            "refresh_token": refresh_token,
        },
        timeout=timeout,
    )
    if not payload.get("access_token"):
        raise ScriptError(f"OpenAI response missing access_token: {json.dumps(payload, ensure_ascii=False)}")
    return payload


def import_tokens(args: argparse.Namespace) -> list[ImportResult]:
    repo_root = Path(__file__).resolve().parents[1]
    env_defaults = read_env_file_defaults(repo_root)
    admin_email = args.admin_email or os.getenv("SUB2API_ADMIN_EMAIL") or env_defaults.get("ADMIN_EMAIL", "")
    admin_password = (
        args.admin_password or os.getenv("SUB2API_ADMIN_PASSWORD") or env_defaults.get("ADMIN_PASSWORD", "")
    )

    input_text = gather_input_text(args)
    tokens = normalize_refresh_tokens(input_text) if args.mode == "rt" else parse_access_token_entries(input_text)
    if not tokens:
        raise ScriptError("No token input provided")

    bearer_token = ""
    accounts: list[dict[str, Any]] = []
    if not args.validate_only:
        if not admin_email or not admin_password:
            raise ScriptError("sub2api admin credentials are required unless --validate-only is used")
        bearer_token = login_sub2api(args.sub2api_url, admin_email, admin_password, args.timeout)
        accounts = list_openai_oauth_accounts(args.sub2api_url, bearer_token, args.timeout)

    results: list[ImportResult] = []
    timestamp_now = datetime.now(timezone.utc)

    if args.mode == "rt":
        return _import_refresh_tokens(args, accounts, bearer_token, tokens, timestamp_now)
    return _import_access_tokens(args, accounts, bearer_token, tokens, timestamp_now)


def _import_refresh_tokens(
    args: argparse.Namespace,
    accounts: list[dict[str, Any]],
    bearer_token: str,
    tokens: list[str],
    timestamp_now: datetime,
) -> list[ImportResult]:
    results: list[ImportResult] = []
    for index, refresh_token in enumerate(tokens, 1):
        last_error = ""
        for attempt in range(1, args.retries + 1):
            try:
                token_response = refresh_with_openai(
                    refresh_token,
                    client_id=args.client_id,
                    redirect_uri=args.redirect_uri,
                    token_url=args.openai_token_url,
                    timeout=args.timeout,
                )
                bundle = token_response_to_bundle(token_response)
                if not bundle.get("refresh_token"):
                    bundle["refresh_token"] = refresh_token
                name = choose_account_name(bundle, fallback_prefix=args.fallback_prefix, index=index, now=timestamp_now)

                if args.validate_only:
                    results.append(
                        ImportResult(
                            refresh_token=refresh_token,
                            success=True,
                            action="validated",
                            account_name=name,
                            message="Validated against OpenAI token endpoint",
                        )
                    )
                    break

                existing = find_existing_account(accounts, refresh_token)
                if existing:
                    payload = build_update_payload(existing, name, bundle, args.client_id)
                    updated = update_account(args.sub2api_url, bearer_token, int(existing["id"]), payload, args.timeout)
                    updated_account = updated or existing
                    results.append(
                        ImportResult(
                            refresh_token=refresh_token,
                            success=True,
                            action="updated",
                            account_id=int(updated_account.get("id", existing["id"])),
                            account_name=str(updated_account.get("name", name)),
                            message="Updated existing account with matching refresh_token",
                        )
                    )
                    for pos, account in enumerate(accounts):
                        if int(account.get("id", 0) or 0) == int(updated_account.get("id", existing["id"])):
                            accounts[pos] = updated_account
                            break
                else:
                    payload = build_account_payload(name, bundle, args.client_id)
                    created = create_account(args.sub2api_url, bearer_token, payload, args.timeout)
                    created_id = created.get("id")
                    results.append(
                        ImportResult(
                            refresh_token=refresh_token,
                            success=True,
                            action="created",
                            account_id=int(created_id) if created_id is not None else None,
                            account_name=str(created.get("name", name)),
                            message="Created new OpenAI OAuth account",
                        )
                    )
                    if created:
                        accounts.append(created)
                break
            except ScriptError as exc:
                last_error = str(exc)
                if attempt < args.retries:
                    time.sleep(min(attempt, 2))
                    continue
                results.append(
                    ImportResult(
                        refresh_token=refresh_token,
                        success=False,
                        action="skipped",
                        error=last_error,
                    )
                )
        else:
            results.append(
                ImportResult(
                    refresh_token=refresh_token,
                    success=False,
                    action="skipped",
                    error=last_error or "unknown error",
                )
            )

    return results


def _import_access_tokens(
    args: argparse.Namespace,
    accounts: list[dict[str, Any]],
    bearer_token: str,
    entries: list[dict[str, str]],
    timestamp_now: datetime,
) -> list[ImportResult]:
    results: list[ImportResult] = []
    for index, entry in enumerate(entries, 1):
        access_token = entry["access_token"]
        refresh_token = entry["refresh_token"]
        bundle = build_token_bundle_from_access_token(access_token, refresh_token, now=timestamp_now)
        name = choose_account_name(bundle, fallback_prefix=args.fallback_prefix, index=index, now=timestamp_now)

        if args.validate_only:
            message = "Parsed access token only"
            if refresh_token:
                message = "Parsed access token with linked refresh token"
            results.append(
                ImportResult(
                    refresh_token=access_token,
                    success=True,
                    action="validated",
                    account_name=name,
                    message=message,
                )
            )
            continue

        existing = find_existing_account(accounts, refresh_token) if refresh_token else None
        if existing is None:
            existing = find_existing_account_by_access_token(accounts, access_token)

        if existing:
            payload = build_update_payload(existing, name, bundle, args.client_id)
            updated = update_account(args.sub2api_url, bearer_token, int(existing["id"]), payload, args.timeout)
            updated_account = updated or existing
            message = "Updated existing account from access token"
            if not refresh_token:
                message = "Updated existing account from access token (no refresh_token)"
            results.append(
                ImportResult(
                    refresh_token=access_token,
                    success=True,
                    action="updated",
                    account_id=int(updated_account.get("id", existing["id"])),
                    account_name=str(updated_account.get("name", name)),
                    message=message,
                )
            )
            for pos, account in enumerate(accounts):
                if int(account.get("id", 0) or 0) == int(updated_account.get("id", existing["id"])):
                    accounts[pos] = updated_account
                    break
        else:
            payload = build_account_payload(name, bundle, args.client_id)
            created = create_account(args.sub2api_url, bearer_token, payload, args.timeout)
            created_id = created.get("id")
            message = "Created new account from access token"
            if not refresh_token:
                message = "Created new account from access token (no refresh_token)"
            results.append(
                ImportResult(
                    refresh_token=access_token,
                    success=True,
                    action="created",
                    account_id=int(created_id) if created_id is not None else None,
                    account_name=str(created.get("name", name)),
                    message=message,
                )
            )
            if created:
                accounts.append(created)
    return results


def write_report(results: list[ImportResult], report_file: str | None) -> Path:
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


def print_summary(results: list[ImportResult], report_path: Path) -> None:
    success_count = sum(1 for item in results if item.success)
    failed_count = len(results) - success_count
    print(f"Processed {len(results)} token(s): {success_count} success, {failed_count} failed")
    for item in results:
        token_preview = f"{item.refresh_token[:8]}...{item.refresh_token[-6:]}" if len(item.refresh_token) > 18 else item.refresh_token
        if item.success:
            print(
                f"[OK] {token_preview} -> {item.action}"
                f" account_id={item.account_id or '-'} name={item.account_name or '-'}"
            )
        else:
            print(f"[FAIL] {token_preview} -> {item.error}")
    print(f"Report written to {report_path}")


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        results = import_tokens(args)
        report_path = write_report(results, args.report_file)
        print_summary(results, report_path)
        return 0 if all(item.success for item in results) else 1
    except ScriptError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
