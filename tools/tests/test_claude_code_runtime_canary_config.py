import contextlib
import io
import json
import unittest

from tools.claude_code_runtime_canary_config import (
    NATIVE_GROUP_ID,
    build_bridge_placeholder_plan,
    main,
)


class ClaudeCodeRuntimeCanaryConfigTests(unittest.TestCase):
    def test_dry_run_plan_contains_only_disabled_bridge_placeholders(self):
        plan = build_bridge_placeholder_plan("http://127.0.0.1:3017")

        self.assertEqual("dry-run", plan["mode"])
        self.assertFalse(plan["writes_enabled"])
        self.assertEqual("http://127.0.0.1:3017", plan["target"])
        expected_names = {
            "zhumeng-claude-code-bridge-openai",
            "zhumeng-claude-code-bridge-deepseek",
            "zhumeng-claude-code-bridge-agnes",
            "zhumeng-claude-code-bridge-anthropic-compat",
            "zhumeng-claude-code-bridge-glm",
            "zhumeng-claude-code-bridge-kimi",
        }
        actions = plan["actions"]
        self.assertEqual(expected_names, {action["group"]["name"] for action in actions})

        expected_routes = {
            "zhumeng-claude-code-bridge-openai": ("openai", "openai_bridge", "claude_code_bridge_openai"),
            "zhumeng-claude-code-bridge-deepseek": ("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
            "zhumeng-claude-code-bridge-agnes": ("agnes", "agnes_bridge", "claude_code_bridge_agnes"),
            "zhumeng-claude-code-bridge-anthropic-compat": (
                "anthropic_compat",
                "anthropic_compat_bridge",
                "claude_code_bridge_anthropic_compat",
            ),
            "zhumeng-claude-code-bridge-glm": ("zai_glm", "zai_glm_bridge", "claude_code_bridge_zai_glm"),
            "zhumeng-claude-code-bridge-kimi": ("kimi", "kimi_bridge", "claude_code_bridge_kimi"),
        }
        self.assertTrue(all(plan["safety_invariants"].values()))

        for action in actions:
            self.assertEqual("ensure_disabled_placeholder_group", action["action"])
            self.assertEqual("plan_only", action["write_mode"])
            group = action["group"]
            provider, route, client_type = expected_routes[group["name"]]
            self.assertEqual("claude_code_bridge", group["platform"])
            self.assertEqual(provider, group["provider"])
            self.assertEqual(route, group["route"])
            self.assertEqual(client_type, group["client_type"])
            self.assertEqual("disabled", group["status"])
            self.assertTrue(group["claude_code_only"])
            self.assertFalse(group["codex_gateway_entitled"])
            self.assertFalse(group["augment_gateway_entitled"])
            self.assertFalse(group["formal_pool_allowed"])
            self.assertFalse(group["native_attestation_allowed"])
            self.assertFalse(group["native_group_membership"])
            self.assertIn(NATIVE_GROUP_ID, group["excluded_group_ids"])
            self.assertEqual([], group["upstream_account_bindings"])
            self.assertTrue(group["no_live_upstream_account_binding"])
            self.assertFalse(group["models_list_config"]["enabled"])
            self.assertFalse(group["models_list_config"]["live_bridge_enabled"])
            self.assertEqual([], group["models_list_config"]["models"])

    def test_dry_run_output_redacts_target_credentials_and_never_prints_secrets(self):
        stdout = io.StringIO()
        secret_target = "http://admin:sk-secret-token@127.0.0.1:3017/admin/sk-path-secret?api_key=sk-query"

        with contextlib.redirect_stdout(stdout):
            rc = main(["--dry-run", "--target", secret_target, "--format", "json"])

        self.assertEqual(0, rc)
        dumped = stdout.getvalue()
        self.assertNotIn("sk-secret-token", dumped)
        self.assertNotIn("sk-query", dumped)
        self.assertNotIn("sk-path-secret", dumped)
        payload = json.loads(dumped)
        self.assertFalse(payload["writes_enabled"])
        self.assertEqual("http://***:***@127.0.0.1:3017", payload["target"])

    def test_target_redaction_fails_closed_for_invalid_port_without_traceback(self):
        stdout = io.StringIO()
        stderr = io.StringIO()

        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = main(["--dry-run", "--target", "http://127.0.0.1:999999/sk-secret", "--format", "json"])

        self.assertEqual(0, rc)
        self.assertEqual("", stderr.getvalue())
        dumped = stdout.getvalue()
        self.assertNotIn("999999", dumped)
        self.assertNotIn("sk-secret", dumped)
        payload = json.loads(dumped)
        self.assertEqual("<redacted_target>", payload["target"])

    def test_apply_is_fail_closed_without_explicit_user_approved_db_write(self):
        stdout = io.StringIO()
        stderr = io.StringIO()

        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = main(["--apply", "--target", "http://127.0.0.1:3017"])

        self.assertEqual(2, rc)
        self.assertEqual("", stdout.getvalue())
        self.assertIn("dry-run only", stderr.getvalue())
        self.assertIn("user-approved", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
