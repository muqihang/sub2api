import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools.cli_control_plane_ab_diff import compare_ab_message_shapes, evaluate_ab_diff_gate, message_shape


class CliControlPlaneABDiffTest(unittest.TestCase):
    def _run_readiness(self, block_shape, stub_shape):
        with tempfile.TemporaryDirectory() as td:
            block_path = Path(td) / 'block.json'
            stub_path = Path(td) / 'stub.json'
            block_path.write_text(json.dumps(block_shape), encoding='utf-8')
            stub_path.write_text(json.dumps(stub_shape), encoding='utf-8')
            return subprocess.run(
                [
                    sys.executable,
                    '-m',
                    'tools.cli_control_plane_readiness',
                    '--block-summary',
                    str(block_path),
                    '--stub-summary',
                    str(stub_path),
                    '--format',
                    'json',
                ],
                cwd='/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation',
                capture_output=True,
                text=True,
                timeout=10,
            )

    def test_pass_when_block_and_stub_shapes_match(self):
        block = message_shape()
        stub = message_shape()

        result = compare_ab_message_shapes(block, stub)

        self.assertEqual(result.status, 'PASS')
        self.assertTrue(result.allows_real_canary)
        self.assertEqual(result.differences, [])
        self.assertEqual(result.missing_fields, [])

    def test_unknown_when_body_keys_or_tools_change(self):
        block = message_shape(body_keys=['max_tokens', 'messages', 'model'], tools_count=1)
        stub = message_shape(body_keys=['max_tokens', 'messages', 'model', 'tools'], tools_count=2)

        result = compare_ab_message_shapes(block, stub)

        self.assertEqual(result.status, 'UNKNOWN')
        self.assertFalse(result.allows_real_canary)
        self.assertTrue(any('body_keys' in item for item in result.differences))
        self.assertTrue(any('tools_count' in item for item in result.differences))

    def test_missing_required_field_blocks_readiness(self):
        block = message_shape()
        stub = message_shape()
        del stub['session_uuid_like']

        result = compare_ab_message_shapes(block, stub)

        self.assertEqual(result.status, 'UNKNOWN')
        self.assertFalse(result.allows_real_canary)
        self.assertIn('session_uuid_like', result.missing_fields)

    def test_retry_error_and_extra_message_count_mismatch_is_p1(self):
        block = message_shape(retry_count=0, error_count=0, extra_message_count=0)
        stub = message_shape(retry_count=1, error_count=0, extra_message_count=2)

        result = compare_ab_message_shapes(block, stub)

        self.assertEqual(result.status, 'P1')
        self.assertFalse(result.allows_real_canary)
        self.assertTrue(any('retry_count' in item for item in result.differences))
        self.assertTrue(any('extra_message_count' in item for item in result.differences))

    def test_ab_diff_gate_blocks_readiness_on_unknown_or_p1(self):
        passed = evaluate_ab_diff_gate(message_shape(), message_shape())
        self.assertTrue(passed.allows_real_canary)
        self.assertEqual(passed.readiness_status, 'READY')

        unknown = evaluate_ab_diff_gate(
            message_shape(body_keys=['max_tokens', 'messages', 'model']),
            message_shape(body_keys=['max_tokens', 'messages', 'model', 'tools']),
        )
        self.assertFalse(unknown.allows_real_canary)
        self.assertEqual(unknown.readiness_status, 'BLOCKED')
        self.assertIn('ab_diff_status_UNKNOWN', unknown.readiness_block_reason)

        p1 = evaluate_ab_diff_gate(
            message_shape(retry_count=0),
            message_shape(retry_count=1),
        )
        self.assertFalse(p1.allows_real_canary)
        self.assertEqual(p1.readiness_status, 'BLOCKED')
        self.assertIn('ab_diff_status_P1', p1.readiness_block_reason)

    def test_ab_diff_reports_body_size_bucket_and_delta(self):
        block = message_shape(body_size=260)
        stub = message_shape(body_size=300)

        result = compare_ab_message_shapes(block, stub)

        self.assertEqual(result.status, 'UNKNOWN')
        self.assertFalse(result.allows_real_canary)
        self.assertTrue(any('body_size' in item and 'delta=40' in item and 'same_bucket' in item for item in result.differences))

        different_bucket = compare_ab_message_shapes(
            message_shape(body_size=240),
            message_shape(body_size=2048),
        )
        self.assertTrue(any('body_size' in item and 'delta=1808' in item and 'different_bucket' in item for item in different_bucket.differences))

    def test_readiness_runner_ready_exit_zero_with_matching_shapes(self):
        proc = self._run_readiness(message_shape(), message_shape())
        self.assertEqual(proc.returncode, 0)
        payload = json.loads(proc.stdout)
        self.assertEqual(payload['readiness_status'], 'READY')
        self.assertTrue(payload['allows_real_canary'])

    def test_readiness_runner_blocked_nonzero_on_tools_or_body_key_change(self):
        proc = self._run_readiness(
            message_shape(body_keys=['max_tokens', 'messages', 'model'], tools_count=0),
            message_shape(body_keys=['max_tokens', 'messages', 'model', 'tools'], tools_count=1),
        )
        self.assertNotEqual(proc.returncode, 0)
        payload = json.loads(proc.stdout)
        self.assertEqual(payload['readiness_status'], 'BLOCKED')
        self.assertFalse(payload['allows_real_canary'])
        self.assertEqual(payload['status'], 'UNKNOWN')

    def test_readiness_runner_does_not_echo_unexpected_raw_values(self):
        proc = self._run_readiness(
            {
                **message_shape(),
                'raw_prompt': 'raw-prompt-marker',
            },
            message_shape(),
        )
        self.assertNotIn('raw-prompt-marker', proc.stdout)
        self.assertNotIn('raw-prompt-marker', proc.stderr)


if __name__ == '__main__':
    unittest.main()
