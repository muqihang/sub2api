#!/usr/bin/env python3
"""Local-only readiness gate for A/B diff message-shape summaries."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from tools.cli_control_plane_ab_diff import evaluate_ab_diff_gate, readiness_payload


BLOCKED_EXIT_CODE = 2


def load_shape_summary(path: Path) -> dict[str, Any]:
    with path.open('r', encoding='utf-8') as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        raise ValueError(f'shape summary must be a JSON object: {path}')
    return payload


def render_markdown(payload: dict[str, Any]) -> str:
    lines = [
        '# Claude CLI control-plane readiness',
        '',
        f"- readiness_status: {payload['readiness_status']}",
        f"- allows_real_canary: {str(payload['allows_real_canary']).lower()}",
        f"- status: {payload['status']}",
    ]
    if payload['readiness_block_reason']:
        lines.append(f"- readiness_block_reason: {payload['readiness_block_reason']}")
    if payload['missing_fields']:
        lines.append(f"- missing_fields: {', '.join(payload['missing_fields'])}")
    if payload['differences']:
        lines.append('- differences:')
        lines.extend(f'  - {item}' for item in payload['differences'])
    return '\n'.join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='Evaluate localhost-only A/B diff readiness from safe shape summaries')
    parser.add_argument('--block-summary', type=Path, required=True)
    parser.add_argument('--stub-summary', type=Path, required=True)
    parser.add_argument('--format', choices=('json', 'markdown'), default='json')
    args = parser.parse_args(argv)

    try:
        block_summary = load_shape_summary(args.block_summary)
        stub_summary = load_shape_summary(args.stub_summary)
        result = evaluate_ab_diff_gate(block_summary, stub_summary)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(json.dumps({'readiness_status': 'BLOCKED', 'allows_real_canary': False, 'error': type(exc).__name__}), flush=True)
        return BLOCKED_EXIT_CODE

    payload = readiness_payload(result)
    if args.format == 'markdown':
        print(render_markdown(payload), flush=True)
    else:
        print(json.dumps(payload, sort_keys=True), flush=True)
    return 0 if result.allows_real_canary else BLOCKED_EXIT_CODE


if __name__ == '__main__':
    raise SystemExit(main())
