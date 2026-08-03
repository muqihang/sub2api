"""Safe helpers for Plan 71 Claude Code local-env attribution evidence.

This module classifies raw in-memory observations into buckets only. Callers
must not persist request bodies, prompts, decoded domain lists, or secrets.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any, Iterable, Mapping
from urllib.parse import parse_qsl, urlsplit
import json
import re


TIMEZONE_BUCKETS = {
    "America/Los_Angeles": "us_pacific",
    "America/New_York": "us_eastern",
    "UTC": "utc",
    "Asia/Taipei": "taipei",
    "Asia/Tokyo": "tokyo",
    "Asia/Seoul": "seoul",
    "Asia/Shanghai": "shanghai",
    "Asia/Urumqi": "urumqi",
}

TIMEZONE_VALUES = tuple(TIMEZONE_BUCKETS)
BASE_URL_BUCKETS = (
    "official_anthropic",
    "neutral_loopback_gateway",
    "neutral_non_china_gateway",
    "china_tld_gateway",
    "china_org_domain_gateway",
    "china_cloud_domain_gateway",
    "china_ai_keyword_gateway",
    "claude_proxy_resale_like_gateway",
)
PROXY_BUCKETS = (
    "no_proxy_env",
    "loopback_proxy_only",
    "non_loopback_proxy_rejected",
    "no_proxy_bypass_guarded",
)
REQUIRED_COMBINATION_ROWS = (
    ("us_pacific", "neutral_gateway"),
    ("us_pacific", "ai_lab_keyword"),
    ("taipei", "neutral_gateway"),
    ("shanghai", "neutral_gateway"),
    ("shanghai", "ai_lab_keyword"),
)

NETWORK_ENV_DENYLIST = {
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
    "npm_config_proxy",
    "npm_config_http_proxy",
    "npm_config_https_proxy",
    "NPM_CONFIG_PROXY",
    "NPM_CONFIG_HTTP_PROXY",
    "NPM_CONFIG_HTTPS_PROXY",
    "NODE_EXTRA_CA_CERTS",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "CURL_CA_BUNDLE",
    "REQUESTS_CA_BUNDLE",
}

CHINA_ORG_KEYWORDS = ("baidu", "alibaba", "tencent", "bytedance", "zhipu", "moonshot", "minimax")
CHINA_CLOUD_KEYWORDS = ("aliyun", "tencentcloud", "volcengine", "huaweicloud", "baidubce")
AI_KEYWORDS = ("qwen", "kimi", "deepseek", "zhipu", "moonshot", "minimax", "doubao", "openai", "gemini")
PROXY_RESALE_KEYWORDS = ("claude-proxy", "resale", "shared", "pool", "relay")
NEUTRAL_GATEWAY_KEYWORDS = ("gateway",)


def timezone_bucket(value: str | None) -> str:
    return TIMEZONE_BUCKETS.get(value or "", "other")


def date_format_bucket(text: str) -> str:
    if not _has_today_or_date_marker(text):
        return "not_observed"
    if re.search(r"\b\d{4}-\d{2}-\d{2}\b", text):
        return "hyphen"
    if re.search(r"\b\d{1,2}/\d{1,2}/\d{2,4}\b", text):
        return "slash"
    return "other"


def apostrophe_bucket(text: str) -> str:
    if "Today's" in text:
        return "ascii"
    if "Today\u2019s" in text:
        return "unicode_variant_1"
    if "Today\u02bcs" in text:
        return "unicode_variant_2"
    if "Today\uff07s" in text:
        return "unicode_variant_3"
    return "not_observed"


def classify_base_url(raw_url: str | None) -> dict[str, str]:
    parsed = urlsplit(raw_url or "")
    host = (parsed.hostname or "").lower().rstrip(".")
    if host in {"api.anthropic.com", "anthropic.com"}:
        return _base_result("official_anthropic", "exact_domain")
    if host in {"127.0.0.1", "::1", "localhost"}:
        return _base_result("neutral_gateway", "exact_domain")
    if host.endswith(".cn") or host == "cn":
        return _base_result("china_tld", "subdomain_suffix")
    if any(keyword in host for keyword in CHINA_ORG_KEYWORDS):
        return _base_result("china_org_domain", "substring_keyword")
    if any(keyword in host for keyword in CHINA_CLOUD_KEYWORDS):
        return _base_result("china_cloud_domain", "substring_keyword")
    if any(keyword in host for keyword in AI_KEYWORDS):
        return _base_result("ai_lab_keyword", "substring_keyword")
    if any(keyword in host for keyword in PROXY_RESALE_KEYWORDS):
        return _base_result("claude_proxy_resale_like", "substring_keyword")
    if any(keyword in host for keyword in NEUTRAL_GATEWAY_KEYWORDS):
        return _base_result("neutral_gateway", "substring_keyword")
    return _base_result("neutral_gateway", "no_match")


def classify_proxy_env(env: Mapping[str, str]) -> str:
    proxy_values = [
        env.get(key)
        for key in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy")
        if env.get(key)
    ]
    no_proxy = env.get("NO_PROXY") or env.get("no_proxy")
    if no_proxy and ("*" in no_proxy or "0.0.0.0/0" in no_proxy):
        return "no_proxy_bypass_guarded"
    if not proxy_values:
        return "no_proxy_env"
    return "loopback_proxy_only" if all(_is_loopback_proxy(value) for value in proxy_values) else "non_loopback_proxy_rejected"


def validate_child_env_allowlist(env: Mapping[str, str]) -> None:
    present = sorted(key for key in env if key in NETWORK_ENV_DENYLIST)
    if present:
        raise ValueError(f"child env contains network/base-url/trust influencers: {present}")


def classify_request_summary(
    *,
    version: str,
    timezone: str,
    base_url: str,
    proxy_env: Mapping[str, str],
    method: str,
    path: str,
    headers: Mapping[str, str],
    body: bytes,
) -> dict[str, Any]:
    text = body.decode("utf-8", "ignore")
    parsed_path = urlsplit(path)
    base = classify_base_url(base_url)
    proxy_bucket = classify_proxy_env(proxy_env)
    try:
        decoded = json.loads(text) if text else {}
    except json.JSONDecodeError:
        decoded = {}
    residue_location = _residue_location_bucket(decoded, headers)
    billing_present = "billing" in text.lower()
    cch_present = "cch" in text.lower()
    return {
        "schema": "claude_code_local_env_attribution_oracle.v1",
        "version": version,
        "client_family": "cli",
        "timezone_bucket": timezone_bucket(timezone),
        "base_url_bucket": _safe_base_url_bucket_from_category(base["known_domain_category_bucket"]),
        "proxy_bucket": proxy_bucket,
        "method": method.upper(),
        "path": parsed_path.path or "/",
        "query_keys": sorted({key for key, _value in parse_qsl(parsed_path.query, keep_blank_values=True)}),
        "system_prompt_present": _system_present(decoded),
        "today_date_marker_present": _has_today_or_date_marker(text),
        "residue_location_bucket": residue_location,
        "date_format_bucket": date_format_bucket(text),
        "apostrophe_bucket": apostrophe_bucket(text),
        "timezone_signal_residue_present": False,
        "base_url_signal_residue_present": False,
        "known_domain_match_bucket": base["known_domain_match_bucket"],
        "known_domain_category_bucket": base["known_domain_category_bucket"],
        "current_env_only_evidence": "ambiguous",
        "proxy_signal_residue_present": proxy_bucket != "no_proxy_env",
        "billing_marker_present": billing_present,
        "cch_marker_present": cch_present,
        "billing_cch_location_bucket": residue_location if billing_present or cch_present else "not_observed",
        "billing_cch_covaries_with_env_residue": "unknown",
        "raw_body_omitted_reason": "raw_body_forbidden",
    }


def build_expected_matrix_manifest(versions: Iterable[str]) -> dict[str, Any]:
    rows: list[dict[str, Any]] = []
    for version in versions:
        for tz_name, tz_bucket in TIMEZONE_BUCKETS.items():
            rows.append(
                {
                    "row_id": f"{version}:timezone:{tz_bucket}",
                    "version": version,
                    "dimension": "timezone",
                    "timezone": tz_name,
                    "timezone_bucket": tz_bucket,
                    "base_url_bucket": "neutral_gateway",
                    "proxy_bucket": "no_proxy_env",
                    "requirement": "required",
                }
            )
        for base_bucket in BASE_URL_BUCKETS:
            rows.append(
                {
                    "row_id": f"{version}:base_url:{base_bucket}",
                    "version": version,
                    "dimension": "base_url",
                    "timezone_bucket": "us_pacific",
                    "base_url_bucket": base_bucket,
                    "proxy_bucket": "no_proxy_env",
                    "requirement": "required",
                }
            )
        for proxy_bucket in PROXY_BUCKETS:
            rows.append(
                {
                    "row_id": f"{version}:proxy:{proxy_bucket}",
                    "version": version,
                    "dimension": "proxy",
                    "timezone_bucket": "us_pacific",
                    "base_url_bucket": "neutral_gateway",
                    "proxy_bucket": proxy_bucket,
                    "requirement": "required",
                }
            )
        for tz_bucket, base_bucket in REQUIRED_COMBINATION_ROWS:
            rows.append(
                {
                    "row_id": f"{version}:combo:{tz_bucket}:{base_bucket}",
                    "version": version,
                    "dimension": "timezone_base_url_combo",
                    "timezone_bucket": tz_bucket,
                    "base_url_bucket": base_bucket,
                    "proxy_bucket": "no_proxy_env",
                    "requirement": "required",
                    "risk_signal_interpretation": "client_side_attribution_residue_not_full_risk_model",
                }
            )
    return {
        "schema": "claude_code_local_env_attribution_oracle.expected_matrix.v1",
        "rows": rows,
        "matrix_goal": "test timezone/base-url/proxy independent and combination client-side residue signals",
    }


def compare_actual_vs_expected(manifest: Mapping[str, Any], actual_rows: Iterable[Mapping[str, Any]]) -> dict[str, Any]:
    actual_keys = {
        _coverage_key(row)
        for row in actual_rows
        if row.get("version") and row.get("dimension") and row.get("timezone_bucket") and row.get("base_url_bucket") and row.get("proxy_bucket")
    }
    missing = []
    total_required = 0
    for row in manifest.get("rows", []):
        if row.get("requirement") != "required":
            continue
        total_required += 1
        if _coverage_key(row) not in actual_keys:
            missing.append(row.get("row_id", "unknown"))
    return {
        "schema": "claude_code_local_env_attribution_oracle.coverage.v1",
        "required_row_count": total_required,
        "actual_row_count": len(actual_keys),
        "missing_required_row_count": len(missing),
        "missing_required_row_id_prefixes": missing[:50],
        "coverage_decision": "matrix_complete" if not missing else "partial_with_blockers",
    }


def scan_for_raw_leaks(paths: Iterable[Path]) -> dict[str, Any]:
    rules = {
        "secret_or_token_pattern": re.compile(r"(Authorization:\s*Bearer\s+|sk-[A-Za-z0-9]|x-api-key|cookie[:=])", re.I),
        "raw_request_body_marker": re.compile(r"raw request body|raw_body\\s*[:=]\\s*[{\\[]", re.I),
        "tls_material_marker": re.compile(r"BEGIN (?:RSA |EC |OPENSSH |)?PRIVATE KEY|BEGIN CERTIFICATE|pcap", re.I),
    }
    hits: list[dict[str, Any]] = []
    for root in paths:
        root = Path(root)
        files = [root] if root.is_file() else [p for p in root.rglob("*") if p.is_file()]
        for path in files:
            try:
                text = path.read_text("utf-8", "ignore")
            except OSError:
                continue
            for rule, pattern in rules.items():
                count = len(pattern.findall(text))
                if count:
                    hits.append({"path": str(path), "rule": rule, "count": count})
    return {
        "schema": "claude_code_local_env_attribution_oracle.leak_scan.v1",
        "blocking_hit_count": len(hits),
        "hits": hits,
        "scanner_output_contract": "path_rule_count_only_no_matched_content",
    }


def _base_result(category: str, match: str) -> dict[str, str]:
    return {"known_domain_category_bucket": category, "known_domain_match_bucket": match}


def _is_loopback_proxy(value: str) -> bool:
    parsed = urlsplit(value)
    return parsed.scheme in {"http", "https"} and (parsed.hostname or "").lower() in {"127.0.0.1", "::1", "localhost"}


def _has_today_or_date_marker(text: str) -> bool:
    return "today" in text.lower()


def _system_present(decoded: Any) -> bool:
    return isinstance(decoded, dict) and bool(decoded.get("system"))


def _residue_location_bucket(decoded: Any, headers: Mapping[str, str]) -> str:
    if _system_present(decoded):
        return "system"
    if isinstance(decoded, dict) and decoded:
        return "message_body"
    if headers:
        return "header"
    return "not_observed"


def _safe_base_url_bucket_from_category(category: str) -> str:
    mapping = {
        "official_anthropic": "official_anthropic",
        "neutral_gateway": "neutral_non_china_gateway",
        "china_tld": "china_tld_gateway",
        "china_org_domain": "china_org_domain_gateway",
        "china_cloud_domain": "china_cloud_domain_gateway",
        "ai_lab_keyword": "china_ai_keyword_gateway",
        "claude_proxy_resale_like": "claude_proxy_resale_like_gateway",
    }
    return mapping.get(category, "neutral_non_china_gateway")


def _coverage_key(row: Mapping[str, Any]) -> tuple[Any, Any, Any, Any, Any]:
    return (
        row.get("version"),
        row.get("dimension"),
        row.get("timezone_bucket"),
        row.get("base_url_bucket"),
        row.get("proxy_bucket"),
    )
