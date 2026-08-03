import json
import tempfile
import unittest
from pathlib import Path

from tools.claude_code_lab_netwatch import (
    classify_remote,
    parse_lsof_tcp_rows,
    summarize_netwatch_rows,
)


class ClaudeCodeLabNetwatchTests(unittest.TestCase):
    def test_parse_lsof_rows_records_safe_process_network_summary(self):
        output = "\n".join([
            "p123",
            "cnode",
            "n127.0.0.1:60123->127.0.0.1:18080",
            "TST=ESTABLISHED",
            "n192.168.1.9:50100->93.184.216.34:443",
            "TST=SYN_SENT",
            "p456",
            "cGoogle Chrome Helper",
            "n10.0.0.2:50000->api.anthropic.com:443",
            "TST=ESTABLISHED",
        ])
        rows = parse_lsof_tcp_rows(output, watched_pids={123})

        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0]["pid"], 123)
        self.assertEqual(rows[0]["process_name"], "node")
        self.assertEqual(rows[0]["remote_port"], 18080)
        self.assertEqual(rows[0]["remote_host_bucket"], "loopback")
        self.assertFalse(rows[0]["potential_guard_bypass"])
        self.assertEqual(rows[1]["remote_host_bucket"], "public_ip")
        self.assertTrue(rows[1]["potential_guard_bypass"])
        self.assertNotIn("raw_body", json.dumps(rows))

    def test_classify_remote_marks_anthropic_and_private_safely(self):
        self.assertEqual(classify_remote("127.0.0.1"), "loopback")
        self.assertEqual(classify_remote("api.anthropic.com"), "anthropic_or_claude")
        self.assertEqual(classify_remote("platform.claude.com"), "anthropic_or_claude")
        self.assertEqual(classify_remote("10.0.0.1"), "private_ip")
        self.assertEqual(classify_remote("93.184.216.34"), "public_ip")

    def test_summarize_netwatch_rows_counts_bypass_without_raw_payloads(self):
        rows = [
            {"event": "process_net_connection", "remote_host_bucket": "loopback", "potential_guard_bypass": False, "remote_port": 18080, "state": "ESTABLISHED"},
            {"event": "process_net_connection", "remote_host_bucket": "anthropic_or_claude", "potential_guard_bypass": True, "remote_port": 443, "state": "ESTABLISHED"},
        ]
        summary = summarize_netwatch_rows(rows)
        self.assertEqual(summary["connection_count"], 2)
        self.assertEqual(summary["potential_guard_bypass_count"], 1)
        self.assertEqual(summary["remote_host_buckets"], {"loopback": 1, "anthropic_or_claude": 1})
        self.assertNotIn("api.anthropic.com", json.dumps(summary))


if __name__ == "__main__":
    unittest.main()
