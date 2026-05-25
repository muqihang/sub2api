#!/usr/bin/env python3
"""Scan safe deliverables/capture summaries for forbidden sensitive artifacts.

The scanner intentionally reports only file, line, and rule names. It never
prints matched values, so a failed scan is still a safe deliverable.
"""

from __future__ import annotations

import argparse
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


TEXT_SUFFIXES = {
    ".go",
    ".js",
    ".json",
    ".jsonl",
    ".log",
    ".md",
    ".mjs",
    ".py",
    ".toml",
    ".ts",
    ".txt",
    ".yaml",
    ".yml",
}


@dataclass(frozen=True)
class Finding:
    path: Path
    line: int
    rule: str


SENSITIVE_TERMS = (
    "body",
    "query",
    "session",
    "account",
    "metadata_user_id",
    "user_id",
    "user",
    "org",
    "organization",
    "project",
    "target",
    "prompt",
    "text",
    "email",
    "proxy",
    "token",
    "authorization",
    "cookie",
    "cch",
    "telemetry",
    "final_body",
    "request_body",
    "safe_ref",
    "thread",
    "device",
    "uuid",
)

SENSITIVE_TERM_RE = "|".join(re.escape(term) for term in SENSITIVE_TERMS)

FIELD_DIGEST_BEFORE = re.compile(
    rf"(?i)[\"']?[^\"'\n:=,{{}}]*(?:{SENSITIVE_TERM_RE})[^\"'\n:=,{{}}]*"
    r"(?:hash|digest|sha256|md5)[^\"'\n:=,{}]*[\"']?\s*[:=]"
)
FIELD_DIGEST_AFTER = re.compile(
    rf"(?i)[\"']?[^\"'\n:=,{{}}]*(?:hash|digest|sha256|md5)[^\"'\n:=,{{}}]*"
    rf"(?:{SENSITIVE_TERM_RE})[^\"'\n:=,{{}}]*[\"']?\s*[:=]"
)
GENERIC_SHA_KEY = re.compile(r"(?i)[\"'](?:sha256|md5|body_sha256|query_sha256|decoded_sha256)[\"']\s*:")
PLAIN_PREFIXED_DIGEST = re.compile(r"(?i)(?<!hmac-)\b(?:sha256|md5):[0-9a-f]{16,}\b")
BARE_HEX_64 = re.compile(r"(?i)(?<![a-f0-9])[a-f0-9]{64}(?![a-f0-9])")
UUID_LIKE = re.compile(
    r"(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b"
)
EMAIL = re.compile(r"(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b")
PROXY_CREDENTIAL = re.compile(r"(?i)\bhttps?://[^\s/@:]+:[^\s/@]+@")
RAW_AUTH_OR_COOKIE = re.compile(
    r"(?i)\b(authorization\s*[:=]\s*(bearer|basic)\s+|x-api-key\s*[:=]\s*[^\s,}]+|cookie\s*[:=]\s*[^\n]+)"
)
CCH_MARKER = re.compile(r"(?i)cch=")
RAW_FIELD = re.compile(r"(?i)[\"']?(raw_prompt|raw_body|raw_telemetry|raw_cch)[\"']?\s*[:=]")


def default_scan_roots(repo_root: Path) -> list[Path]:
    roots = [repo_root / "docs/anti-ban/captures"]
    for rel in (
        "docs/anti-ban/runtime-productization/2026-05-24-cli-through",
        "docs/anti-ban/staging",
        "backend/.tmp-harness",
        "backend/internal/service/testdata",
    ):
        path = repo_root / rel
        if path.exists():
            roots.append(path)
    return roots


def iter_files(repo_root: Path, roots: Iterable[Path]) -> Iterable[Path]:
    for root in roots:
        if not root.exists():
            continue
        if root.is_file():
            yield root
            continue
        for path in root.rglob("*"):
            if not path.is_file():
                continue
            rel_parts = path.relative_to(repo_root).parts if path.is_relative_to(repo_root) else path.parts
            in_capture_deliverable = "docs" in rel_parts and "anti-ban" in rel_parts and "captures" in rel_parts
            if in_capture_deliverable and "safe-deliverable" not in rel_parts:
                # Current control-plane audit dirs are intentionally scanned; old raw captures are not.
                rel = path.as_posix()
                if not (
                    "cli-through-highmax-localhost-preflight-2026-05-24" in rel
                    or "real-cli-through-capability-field-audit-2026-05-24" in rel
                ):
                    continue
            if path.suffix.lower() in TEXT_SUFFIXES or path.name.endswith(".redacted"):
                yield path


def scan_file(path: Path) -> Iterable[Finding]:
    text = path.read_text(errors="ignore")
    for line_no, line in enumerate(text.splitlines(), 1):
        if FIELD_DIGEST_BEFORE.search(line) or FIELD_DIGEST_AFTER.search(line):
            yield Finding(path, line_no, "sensitive_digest_field")
            continue
        if GENERIC_SHA_KEY.search(line):
            yield Finding(path, line_no, "generic_sha_key")
            continue
        if PLAIN_PREFIXED_DIGEST.search(line):
            yield Finding(path, line_no, "plain_prefixed_digest")
            continue
        if "hmac-sha256" not in line.lower() and BARE_HEX_64.search(line) and re.search(
            r"(?i)(hash|digest|sha|body|query|session|account|user|org|project|proxy|safe_ref)", line
        ):
            yield Finding(path, line_no, "bare_hex_digest_context")
            continue
        if UUID_LIKE.search(line):
            yield Finding(path, line_no, "uuid_like_id")
            continue
        if EMAIL.search(line):
            yield Finding(path, line_no, "email")
            continue
        if PROXY_CREDENTIAL.search(line):
            yield Finding(path, line_no, "proxy_credential")
            continue
        if RAW_AUTH_OR_COOKIE.search(line):
            yield Finding(path, line_no, "raw_auth_or_cookie")
            continue
        if CCH_MARKER.search(line):
            yield Finding(path, line_no, "cch_marker")
            continue
        if RAW_FIELD.search(line):
            yield Finding(path, line_no, "raw_field")
            continue


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo-root", type=Path, default=Path.cwd())
    parser.add_argument("--root", action="append", type=Path, default=[])
    parser.add_argument("--max-findings", type=int, default=200)
    args = parser.parse_args()

    repo_root = args.repo_root.resolve()
    roots = [p.resolve() if p.is_absolute() else (repo_root / p).resolve() for p in args.root]
    if not roots:
        roots = default_scan_roots(repo_root)

    files = sorted(set(iter_files(repo_root, roots)))
    findings = [finding for path in files for finding in scan_file(path)]

    print(f"files_scanned={len(files)}")
    print(f"findings={len(findings)}")
    for rule, count in Counter(f.rule for f in findings).most_common():
        print(f"rule.{rule}={count}")
    for finding in findings[: args.max_findings]:
        rel = finding.path.relative_to(repo_root) if finding.path.is_relative_to(repo_root) else finding.path
        print(f"{rel}:{finding.line}:{finding.rule}")
    return 1 if findings else 0


if __name__ == "__main__":
    raise SystemExit(main())
