import json
import tempfile
import unittest
from pathlib import Path

from tools.claude_code_lab_report import load_jsonl, sensitive_scan, summarize, write_markdown


class ClaudeCodeLabReportTests(unittest.TestCase):
    def test_report_summarizes_without_raw_values(self):
        with tempfile.TemporaryDirectory() as td:
            run_dir = Path(td)
            summary_path = run_dir / "guard-summary.jsonl"
            rows = [
                {
                    "event": "request",
                    "decision": "forward_messages",
                    "path": "/v1/messages?beta=true",
                    "model": "claude-opus-4-7",
                    "body_size": 1234,
                    "tools_count": 3,
                    "messages_count": 2,
                    "max_tokens": 64000,
                    "body_keys": ["model", "tools", "max_tokens"],
                    "auth_shape": {"authorization": "Bearer"},
                },
                {
                    "event": "messages_upstream_response",
                    "decision": "forward_messages",
                    "path": "/v1/messages?beta=true",
                    "status": 200,
                    "response_body_size": 88,
                },
                {
                    "event": "https_control_plane",
                    "decision": "suppress_204",
                    "classification": "telemetry_or_eval_suppressed",
                    "path_template": "/api/event_logging/v2/batch",
                    "body_length_bucket": "16384_plus_bytes",
                    "transport_summary": {"auth_shape": {"authorization": "Bearer"}},
                },
            ]
            summary_path.write_text("\n".join(json.dumps(row) for row in rows), encoding="utf-8")
            loaded = load_jsonl(summary_path)
            report = summarize(loaded)
            scan = sensitive_scan(run_dir)
            write_markdown(run_dir / "report.md", report, scan)

            text = (run_dir / "report.md").read_text(encoding="utf-8")
            self.assertIn("claude-opus-4-7", text)
            self.assertIn("telemetry_or_eval_suppressed", text)
            self.assertIn("PASS", text)
            self.assertNotIn("raw_body", text)


if __name__ == "__main__":
    unittest.main()
