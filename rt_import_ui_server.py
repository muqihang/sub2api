#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
import webbrowser
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 18600
DEFAULT_SUB2API_URL = "http://127.0.0.1:18081"
SCRIPT_NAME = "tools/import_openai_rt_to_sub2api.py"
HTML_NAME = "rt_import_ui.html"


def normalize_ui_tokens(raw_text: str) -> list[str]:
    seen: set[str] = set()
    tokens: list[str] = []
    for line in raw_text.splitlines():
        token = line.strip()
        if not token or token in seen:
            continue
        seen.add(token)
        tokens.append(token)
    return tokens


def build_import_command(
    *,
    repo_root: Path,
    report_path: Path,
    validate_only: bool,
    mode: str = "rt",
    sub2api_url: str = DEFAULT_SUB2API_URL,
) -> list[str]:
    command = [
        sys.executable,
        str((repo_root / SCRIPT_NAME).resolve()),
        "--stdin",
        "--mode",
        mode,
        "--sub2api-url",
        sub2api_url,
    ]
    if validate_only:
        command.append("--validate-only")
    command.extend(["--report-file", str(report_path)])
    return command


class RTImportUIServer(ThreadingHTTPServer):
    def __init__(self, server_address: tuple[str, int], repo_root: Path, sub2api_url: str) -> None:
        self.repo_root = repo_root
        self.sub2api_url = sub2api_url
        super().__init__(server_address, RTImportUIHandler)


class RTImportUIHandler(BaseHTTPRequestHandler):
    server: RTImportUIServer

    def do_GET(self) -> None:  # noqa: N802
        if self.path in ("/", "/index.html"):
            html_path = self.server.repo_root / HTML_NAME
            body = html_path.read_bytes()
            self._send_bytes(HTTPStatus.OK, body, "text/html; charset=utf-8")
            return
        if self.path == "/health":
            self._send_json(HTTPStatus.OK, {"ok": True, "sub2api_url": self.server.sub2api_url})
            return
        self._send_json(HTTPStatus.NOT_FOUND, {"ok": False, "error": "Not found"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/api/import":
            self._send_json(HTTPStatus.NOT_FOUND, {"ok": False, "error": "Not found"})
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length).decode("utf-8"))
        except (ValueError, json.JSONDecodeError):
            self._send_json(HTTPStatus.BAD_REQUEST, {"ok": False, "error": "Invalid JSON payload"})
            return

        raw_tokens = str(payload.get("tokens", ""))
        validate_only = bool(payload.get("validate_only", False))
        mode = str(payload.get("mode", "rt")).strip().lower()
        if mode not in {"rt", "at"}:
            self._send_json(HTTPStatus.BAD_REQUEST, {"ok": False, "error": "Unsupported mode"})
            return
        tokens = normalize_ui_tokens(raw_tokens)
        if not tokens:
            self._send_json(HTTPStatus.BAD_REQUEST, {"ok": False, "error": "No token lines provided"})
            return

        with tempfile.NamedTemporaryFile(prefix="rt-import-", suffix=".json", delete=False) as temp_file:
            report_path = Path(temp_file.name)

        command = build_import_command(
            repo_root=self.server.repo_root,
            report_path=report_path,
            validate_only=validate_only,
            mode=mode,
            sub2api_url=self.server.sub2api_url,
        )

        completed = subprocess.run(
            command,
            input="\n".join(tokens) + "\n",
            text=True,
            capture_output=True,
            cwd=self.server.repo_root,
            timeout=600,
            check=False,
        )

        report_data: dict[str, Any] | None = None
        if report_path.exists():
            try:
                report_data = json.loads(report_path.read_text(encoding="utf-8"))
            finally:
                report_path.unlink(missing_ok=True)

        response = {
            "ok": completed.returncode in (0, 1) and report_data is not None,
            "returncode": completed.returncode,
            "stdout": completed.stdout,
            "stderr": completed.stderr,
            "tokens": tokens,
            "mode": mode,
            "validate_only": validate_only,
            "report": report_data,
        }
        status = HTTPStatus.OK if response["ok"] else HTTPStatus.INTERNAL_SERVER_ERROR
        self._send_json(status, response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        return

    def _send_json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        self._send_bytes(status, json.dumps(payload, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8")

    def _send_bytes(self, status: HTTPStatus, body: bytes, content_type: str) -> None:
        self.send_response(status.value)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Serve a local RT import web UI that targets sub2api.")
    parser.add_argument("--host", default=DEFAULT_HOST)
    parser.add_argument("--port", type=int, default=DEFAULT_PORT)
    parser.add_argument("--sub2api-url", default=DEFAULT_SUB2API_URL)
    parser.add_argument("--open", action="store_true", help="Open the UI in the default browser.")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    repo_root = Path(__file__).resolve().parent
    server = RTImportUIServer((args.host, args.port), repo_root=repo_root, sub2api_url=args.sub2api_url)
    url = f"http://{args.host}:{args.port}"
    print(f"RT import UI listening on {url}")
    print(f"Target sub2api: {args.sub2api_url}")
    if args.open:
        webbrowser.open(url)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
