#!/usr/bin/env python3
"""Generate a safe report from a Claude Code local capture run."""
from __future__ import annotations

import argparse
import json
import re
import time
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

SENSITIVE_PATTERNS = {
    "authorization_value": re.compile(r"(?i)(authorization|x-api-key|cookie|proxy-authorization)\s*[:=]\s*(bearer|basic|sk-|sk-ant|session)[^\s\"']+"),
    "anthropic_session_key": re.compile(r"sk-ant-[A-Za-z0-9_-]{16,}"),
    "email": re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"),
    "plain_uuid_key": re.compile(r"(?i)\b(account|organization|org|user)[_-]?uuid\b\s*[:=]"),
    "raw_body_marker": re.compile(r"(?i)\b(raw_body|raw_prompt|raw_telemetry|raw_cch)\b"),
}


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rows: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            rows.append({"event": "json_parse_error"})
            continue
        if isinstance(value, dict):
            rows.append(value)
    return rows


def sensitive_scan(run_dir: Path) -> dict[str, Any]:
    findings: list[dict[str, str]] = []
    scanned = 0
    for path in sorted(run_dir.rglob("*")):
        if not path.is_file() or path.suffix not in {".json", ".jsonl", ".md", ".txt"}:
            continue
        scanned += 1
        text = path.read_text(encoding="utf-8", errors="replace")
        for name, pattern in SENSITIVE_PATTERNS.items():
            if pattern.search(text):
                findings.append({"file": str(path.relative_to(run_dir)), "kind": name})
    return {"status": "PASS" if not findings else "FAIL", "findings": findings, "files_scanned": scanned}


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    events = Counter(str(row.get("event", "unknown")) for row in rows)
    decisions = Counter(str(row.get("decision", "none")) for row in rows)
    reasons = Counter(str(row.get("reason", "none")) for row in rows)
    paths = Counter(str(row.get("path_template") or row.get("path") or "none") for row in rows)
    classifications = Counter(str(row.get("classification", "none")) for row in rows)

    message_requests = [row for row in rows if row.get("event") == "request" and row.get("decision") == "forward_messages"]
    message_responses = [row for row in rows if row.get("event") == "messages_upstream_response"]
    control_plane = [row for row in rows if row.get("event") in {"request", "https_control_plane"} and row.get("decision") != "forward_messages"]
    connects = [row for row in rows if str(row.get("event", "")).startswith("connect_")]

    models = Counter()
    max_values = defaultdict(lambda: None)
    body_keys = Counter()
    auth_shapes = Counter()
    selected_headers = Counter()
    for row in message_requests:
        if row.get("model") is not None:
            models[str(row.get("model"))] += 1
        for key in ("body_size", "tools_count", "messages_count", "max_tokens"):
            value = row.get(key)
            if isinstance(value, int):
                current = max_values[key]
                max_values[key] = value if current is None else max(current, value)
        for key in row.get("body_keys", []) if isinstance(row.get("body_keys"), list) else []:
            body_keys[str(key)] += 1
        auth_shape = row.get("auth_shape")
        if isinstance(auth_shape, dict):
            for name, shape in auth_shape.items():
                auth_shapes[f"messages:{name}:{shape}"] += 1
        selected = row.get("selected")
        if isinstance(selected, dict):
            for name in selected.keys():
                selected_headers[f"messages:{name}"] += 1

    cp_auth_shapes = Counter()
    cp_declared_lengths = []
    cp_body_buckets = Counter()
    local_raw_refs = 0
    for row in control_plane:
        if isinstance(row.get("local_raw_ref"), dict):
            local_raw_refs += 1
        transport = row.get("transport_summary")
        if isinstance(transport, dict):
            auth_shape = transport.get("auth_shape")
            if isinstance(auth_shape, dict):
                for name, shape in auth_shape.items():
                    cp_auth_shapes[f"control_plane:{name}:{shape}"] += 1
            selected = transport.get("selected")
            if isinstance(selected, dict):
                for name in selected.keys():
                    selected_headers[f"control_plane:{name}"] += 1
        if isinstance(row.get("declared_content_length"), int):
            cp_declared_lengths.append(row["declared_content_length"])
        if row.get("body_length_bucket"):
            cp_body_buckets[str(row["body_length_bucket"])] += 1

    statuses = Counter(str(row.get("status", "none")) for row in message_responses)
    return {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "row_count": len(rows),
        "events": dict(events),
        "decisions": dict(decisions),
        "reasons_top": dict(reasons.most_common(30)),
        "paths_top": dict(paths.most_common(50)),
        "classifications": dict(classifications),
        "messages": {
            "request_count": len(message_requests),
            "response_count": len(message_responses),
            "response_statuses": dict(statuses),
            "models": dict(models),
            "max_observed": dict(max_values),
            "body_keys": sorted(body_keys.keys()),
            "auth_shapes": dict(auth_shapes),
            "local_raw_redacted_artifacts": sum(1 for row in message_requests + message_responses if isinstance(row.get("local_raw_ref"), dict)),
        },
        "control_plane": {
            "request_count": len(control_plane),
            "connect_count": len(connects),
            "auth_shapes": dict(cp_auth_shapes),
            "body_length_buckets": dict(cp_body_buckets),
            "max_declared_content_length": max(cp_declared_lengths) if cp_declared_lengths else None,
            "local_raw_redacted_artifacts": local_raw_refs,
        },
        "selected_header_presence": dict(selected_headers),
    }


def write_markdown(path: Path, summary: dict[str, Any], scan: dict[str, Any]) -> None:
    msg = summary["messages"]
    cp = summary["control_plane"]
    lines = [
        "# Claude Code 本机隔离捕获报告",
        "",
        f"- 生成时间：{summary['generated_at']}",
        f"- JSONL 行数：{summary['row_count']}",
        f"- 主消息请求数：{msg['request_count']}",
        f"- 主消息响应数：{msg['response_count']}",
        f"- 控制面请求数：{cp['request_count']}",
        f"- CONNECT 事件数：{cp['connect_count']}",
        f"- 敏感扫描：{scan['status']}",
        "",
        "## 主消息摘要",
        "",
        f"- 状态码分布：`{json.dumps(msg['response_statuses'], ensure_ascii=False, sort_keys=True)}`",
        f"- 模型分布：`{json.dumps(msg['models'], ensure_ascii=False, sort_keys=True)}`",
        f"- 最大观测值：`{json.dumps(msg['max_observed'], ensure_ascii=False, sort_keys=True)}`",
        f"- body keys：`{json.dumps(msg['body_keys'], ensure_ascii=False)}`",
        f"- 本机认证形态：`{json.dumps(msg['auth_shapes'], ensure_ascii=False, sort_keys=True)}`",
        f"- 本机打码细节包数量：`{msg['local_raw_redacted_artifacts']}`",
        "",
        "## 控制面摘要",
        "",
        f"- 分类分布：`{json.dumps(summary['classifications'], ensure_ascii=False, sort_keys=True)}`",
        f"- 决策分布：`{json.dumps(summary['decisions'], ensure_ascii=False, sort_keys=True)}`",
        f"- 路径 Top：`{json.dumps(summary['paths_top'], ensure_ascii=False, sort_keys=True)}`",
        f"- 控制面认证形态：`{json.dumps(cp['auth_shapes'], ensure_ascii=False, sort_keys=True)}`",
        f"- 控制面 body 桶：`{json.dumps(cp['body_length_buckets'], ensure_ascii=False, sort_keys=True)}`",
        f"- 最大声明内容长度：`{cp['max_declared_content_length']}`",
        f"- 本机打码细节包数量：`{cp['local_raw_redacted_artifacts']}`",
        "",
        "## 安全说明",
        "",
        "- 报告不包含 raw body、raw prompt、raw token、raw telemetry、raw CCH。",
        "- Authorization / x-api-key / Cookie 只保留存在形态，不保留值。",
        "- 动态路径标识使用模板或引用，不输出原始账号、组织、用户 UUID。",
    ]
    if scan.get("findings"):
        lines += ["", "## 敏感扫描发现", "", f"`{json.dumps(scan['findings'], ensure_ascii=False)}`"]
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Summarize a Zhumeng Claude Code local capture run")
    parser.add_argument("--run-dir", type=Path, required=True)
    args = parser.parse_args(argv)
    run_dir = args.run_dir.expanduser()
    rows = load_jsonl(run_dir / "guard-summary.jsonl")
    summary = summarize(rows)
    scan = sensitive_scan(run_dir)
    (run_dir / "report.json").write_text(json.dumps({"summary": summary, "sensitive_scan": scan}, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    write_markdown(run_dir / "report.md", summary, scan)
    print(json.dumps({"run_dir": str(run_dir), "report": str(run_dir / 'report.md'), "sensitive_scan": scan["status"]}, ensure_ascii=False))
    return 0 if scan["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
