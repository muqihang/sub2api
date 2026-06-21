import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path

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
            "zhumeng-claude-code-bridge-glm",
            "zhumeng-claude-code-bridge-kimi",
        }
        actions = plan["actions"]
        self.assertEqual(expected_names, {action["group"]["name"] for action in actions})

        expected_routes = {
            "zhumeng-claude-code-bridge-openai": ("openai", "openai_bridge", "claude_code_bridge_openai"),
            "zhumeng-claude-code-bridge-deepseek": ("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
            "zhumeng-claude-code-bridge-agnes": ("agnes", "agnes_bridge", "claude_code_bridge_agnes"),
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
            self.assertNotIn("live_bridge_enabled", group["models_list_config"])
            model_ids = set(group["models_list_config"]["models"])
            expected_models = {
                "zhumeng-claude-code-bridge-openai": {
                    "claude-code-bridge-gpt-5.5",
                    "claude-code-bridge-gpt-5.4",
                    "claude-code-bridge-gpt-5.4-mini",
                },
                "zhumeng-claude-code-bridge-deepseek": {
                    "claude-code-bridge-deepseek-v4-pro",
                    "claude-code-bridge-deepseek-v4-flash",
                },
                "zhumeng-claude-code-bridge-agnes": {"claude-code-bridge-agnes-2.0-flash"},
                "zhumeng-claude-code-bridge-glm": {"claude-code-bridge-glm-5.2-1m"},
                "zhumeng-claude-code-bridge-kimi": {"claude-code-bridge-kimi-k2.7-code"},
            }[group["name"]]
            self.assertEqual(expected_models, model_ids)
            for model_id in group["models_list_config"]["models"]:
                self.assertIsInstance(model_id, str)
                self.assertTrue(model_id.startswith("claude-code-bridge-"))

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

    def test_provider_catalog_env_contains_bridge_display_ids_and_loopback_backends(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            live_bridge_models=("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"),
        )

        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_LIVE_ENABLED"])
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}
        self.assertIn("claude-code-bridge-gpt-5.5", models)
        self.assertIn("claude-code-bridge-deepseek-v4-pro", models)
        self.assertIn("claude-code-bridge-agnes-2.0-flash", models)
        self.assertEqual("gpt-5.5", models["claude-code-bridge-gpt-5.5"]["upstream_model"])
        self.assertEqual("deepseek-v4-pro", models["claude-code-bridge-deepseek-v4-pro"]["upstream_model"])
        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_CHAT_COMPLETIONS_FALLBACK_ENABLED"])
        self.assertEqual("openai_bridge", models["claude-code-bridge-gpt-5.5"]["route"])
        self.assertEqual("deepseek_bridge", models["claude-code-bridge-deepseek-v4-pro"]["route"])
        self.assertEqual("http://127.0.0.1:3017", models["claude-code-bridge-gpt-5.5"]["openai_base_url"])
        self.assertEqual("http://127.0.0.1:3017", models["claude-code-bridge-deepseek-v4-pro"]["anthropic_base_url"])
        self.assertEqual("anthropic_messages", models["claude-code-bridge-deepseek-v4-pro"]["preferred_protocol"])
        self.assertNotIn("openai_base_url", models["claude-code-bridge-deepseek-v4-pro"])
        self.assertNotIn("fallback_protocol", models["claude-code-bridge-deepseek-v4-pro"])
        self.assertNotIn("fallback_reason", models["claude-code-bridge-deepseek-v4-pro"])
        self.assertTrue(models["claude-code-bridge-gpt-5.5"]["supports_cache_audit"])
        self.assertTrue(models["claude-code-bridge-deepseek-v4-pro"]["supports_reasoning_mapping"])
        self.assertEqual(["high", "max"], models["claude-code-bridge-deepseek-v4-pro"]["reasoning_effort_levels"])
        self.assertNotIn("xhigh", models["claude-code-bridge-deepseek-v4-pro"]["reasoning_effort_levels"])
        self.assertEqual(["low", "medium", "high", "xhigh"], models["claude-code-bridge-gpt-5.5"]["reasoning_effort_levels"])
        self.assertEqual("responses_prompt_cache_key_exact_prefix", models["claude-code-bridge-gpt-5.5"]["cache_policy"])
        self.assertFalse(models["claude-code-bridge-agnes-2.0-flash"].get("live_enabled", False))
        self.assertFalse(models["claude-code-bridge-glm-5.2-1m"].get("live_enabled", False))
        self.assertFalse(models["claude-code-bridge-kimi-k2.7-code"].get("live_enabled", False))
        self.assertNotIn("api_key", json.dumps(catalog).lower())



    def test_live_bridge_provider_scope_defaults_to_openai_and_deepseek_only(self):
        from tools.claude_code_runtime_canary_config import (
            build_provider_catalog_env,
            validate_live_bridge_models_supported,
        )

        # L8 default live scope is GPT/OpenAI + DeepSeek. AGNES is conditional
        # on strict-live provider evidence; GLM/Kimi stay catalog-visible but
        # live-disabled unless explicitly expanded later.
        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            live_bridge_models=(
                "claude-code-bridge-gpt-5.5",
                "claude-code-bridge-deepseek-v4-pro",
            ),
        )
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}

        self.assertTrue(models["claude-code-bridge-gpt-5.5"]["live_enabled"])
        self.assertTrue(models["claude-code-bridge-deepseek-v4-pro"]["live_enabled"])
        self.assertFalse(models["claude-code-bridge-agnes-2.0-flash"]["live_enabled"])
        self.assertFalse(models["claude-code-bridge-glm-5.2-1m"]["live_enabled"])
        self.assertFalse(models["claude-code-bridge-kimi-k2.7-code"]["live_enabled"])

        with self.assertRaisesRegex(ValueError, "AGNES.*strict-live"):
            validate_live_bridge_models_supported(("claude-code-bridge-agnes-2.0-flash",))
        with self.assertRaisesRegex(ValueError, "GLM.*expanded live scope"):
            validate_live_bridge_models_supported(("claude-code-bridge-glm-5.2-1m",))
        with self.assertRaisesRegex(ValueError, "Kimi.*expanded live scope"):
            validate_live_bridge_models_supported(("claude-code-bridge-kimi-k2.7-code",))

    def test_conditional_and_expanded_providers_require_strict_live_evidence(self):
        from tools.claude_code_runtime_canary_config import validate_live_bridge_models_supported

        validate_live_bridge_models_supported(
            ("claude-code-bridge-agnes-2.0-flash",),
            provider_release_statuses={"agnes": "strict-live-pass"},
        )
        with self.assertRaisesRegex(ValueError, "AGNES.*strict-live"):
            validate_live_bridge_models_supported(
                ("claude-code-bridge-agnes-2.0-flash",),
                provider_release_statuses={"agnes": "fixture-pass-only"},
            )
        with self.assertRaisesRegex(ValueError, "GLM.*expanded live scope"):
            validate_live_bridge_models_supported(
                ("claude-code-bridge-glm-5.2-1m",),
                provider_release_statuses={"zai_glm": "strict-live-pass"},
            )
        with self.assertRaisesRegex(ValueError, "unsupported live bridge provider without runtime account: zai_glm"):
            validate_live_bridge_models_supported(
                ("claude-code-bridge-glm-5.2-1m",),
                provider_release_statuses={"zai_glm": "strict-live-pass"},
                expanded_live_providers=("zai_glm",),
            )
        with self.assertRaisesRegex(ValueError, "unsupported live bridge provider without runtime account: kimi"):
            validate_live_bridge_models_supported(
                ("claude-code-bridge-kimi-k2.7-code",),
                provider_release_statuses={"kimi": "strict-live-pass"},
                expanded_live_providers=("kimi",),
            )


    def test_provider_catalog_env_can_enable_agnes_only_with_strict_live_evidence(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            live_bridge_models=("claude-code-bridge-agnes-2.0-flash",),
            provider_release_statuses={"agnes": "strict-live-pass"},
        )
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}

        self.assertTrue(models["claude-code-bridge-agnes-2.0-flash"]["live_enabled"])
        self.assertEqual("responses", models["claude-code-bridge-agnes-2.0-flash"]["preferred_protocol"])
        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_AGNES_LIVE_ENABLED"])
        self.assertEqual("false", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED"])


    def test_provider_catalog_env_docker_target_uses_container_reachable_origin(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            live_bridge_models=("claude-code-bridge-gpt-5.4-mini",),
        )
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}

        self.assertEqual("http://127.0.0.1:8080", models["claude-code-bridge-gpt-5.4-mini"]["openai_base_url"])
        self.assertEqual("http://127.0.0.1:8080", models["claude-code-bridge-agnes-2.0-flash"]["openai_base_url"])
        self.assertEqual("http://127.0.0.1:3017", env["SUB2API_CLAUDE_CODE_PUBLIC_TARGET_ORIGIN"])
        self.assertEqual("http://127.0.0.1:8080", env["SUB2API_CLAUDE_CODE_RUNTIME_TARGET_ORIGIN"])

    def test_deepseek_live_catalog_uses_anthropic_messages_by_default(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            live_bridge_models=("claude-code-bridge-deepseek-v4-pro", "claude-code-bridge-deepseek-v4-flash"),
        )
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}

        for model_id in ("claude-code-bridge-deepseek-v4-pro", "claude-code-bridge-deepseek-v4-flash"):
            entry = models[model_id]
            self.assertEqual("anthropic_messages", entry["preferred_protocol"])
            self.assertEqual("http://127.0.0.1:8080", entry["anthropic_base_url"])
            self.assertNotIn("openai_base_url", entry)
            self.assertNotIn("fallback_protocol", entry)
            self.assertNotIn("fallback_reason", entry)
            self.assertTrue(entry["supports_cache_audit"])
            self.assertTrue(entry["supports_reasoning_mapping"])
            self.assertEqual(["high", "max"], entry["reasoning_effort_levels"])
        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED"])
        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED"])
        self.assertEqual("false", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED"])

    def test_apply_sql_creates_dedicated_deepseek_anthropic_runtime_account_from_codex_key(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql()

        self.assertIn("zhumeng-claude-code-bridge-runtime-deepseek", sql)
        self.assertIn("zhumeng-claude-code-bridge-deepseek-anthropic", sql)
        self.assertIn("codex-upstream-deepseek-v4", sql)
        self.assertIn("https://api.deepseek.com/anthropic", sql)
        self.assertIn("'anthropic'", sql)
        self.assertIn("claude_code_bridge_runtime", sql)
        self.assertIn("Do not add native Claude formal-pool accounts to this group", sql)

    def test_deepseek_live_catalog_can_explicitly_fallback_to_chat_when_anthropic_fixture_not_green(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            live_bridge_models=("claude-code-bridge-deepseek-v4-pro",),
            deepseek_anthropic_fixture_green=False,
        )
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}
        entry = models["claude-code-bridge-deepseek-v4-pro"]
        self.assertEqual("openai_chat_completions", entry["preferred_protocol"])
        self.assertEqual("openai_chat_completions", entry["fallback_protocol"])
        self.assertEqual("anthropic_cache_fixture_failed", entry["fallback_reason"])
        self.assertNotIn("anthropic_base_url", entry)
        self.assertEqual("true", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED"])


    def test_deepseek_openai_fallback_gate_stays_closed_without_deepseek_live_model(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            live_bridge_models=("claude-code-bridge-gpt-5.5",),
            deepseek_anthropic_fixture_green=False,
        )

        self.assertEqual("false", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_OPENAI_FALLBACK_ENABLED"])


    def test_glm_and_kimi_catalog_also_prefer_anthropic_messages_without_openai_fallback_by_default(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env("http://127.0.0.1:3017", runtime_target="http://127.0.0.1:8080")
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        models = {entry["model_id"]: entry for entry in catalog["models"]}

        for model_id in ("claude-code-bridge-glm-5.2-1m", "claude-code-bridge-kimi-k2.7-code"):
            entry = models[model_id]
            self.assertEqual("anthropic_messages", entry["preferred_protocol"])
            self.assertEqual("http://127.0.0.1:8080", entry["anthropic_base_url"])
            self.assertNotIn("openai_base_url", entry)
            self.assertNotIn("fallback_protocol", entry)
            self.assertNotIn("fallback_reason", entry)
            self.assertFalse(entry["live_enabled"])
        self.assertEqual(["high", "max"], models["claude-code-bridge-glm-5.2-1m"]["reasoning_effort_levels"])
        self.assertNotIn("low", models["claude-code-bridge-glm-5.2-1m"]["reasoning_effort_levels"])
        self.assertNotIn("xhigh", models["claude-code-bridge-glm-5.2-1m"]["reasoning_effort_levels"])
        self.assertNotIn("reasoning_effort_levels", models["claude-code-bridge-kimi-k2.7-code"], "Kimi docs expose Thinking-on, not a multi-level effort enum")
        self.assertFalse(models["claude-code-bridge-kimi-k2.7-code"].get("supports_reasoning_mapping", False))
        self.assertEqual("false", env["SUB2API_CLAUDE_CODE_BRIDGE_ANTHROPIC_LIVE_ENABLED"])
        self.assertEqual("false", env["SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_LIVE_ENABLED"])

    def test_apply_refuses_live_glm_or_kimi_until_runtime_accounts_exist(self):
        stderr = io.StringIO()
        stdout = io.StringIO()

        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = main([
                "--apply",
                "--user-approved-db-write",
                "--postgres-container",
                "sub2api-codex-gateway-live-postgres",
                "--target",
                "http://127.0.0.1:3017",
                "--live-bridge-models",
                "claude-code-bridge-glm-5.2-1m",
            ])

        self.assertEqual(2, rc)
        self.assertEqual("", stdout.getvalue())
        self.assertIn("GLM live bridge requires explicit expanded live scope", stderr.getvalue())

    def test_provider_catalog_env_native_models_are_curated_not_legacy_or_fictional(self):
        from tools.claude_code_runtime_canary_config import build_provider_catalog_env

        env = build_provider_catalog_env("http://127.0.0.1:3017")
        native_models = set(env["SUB2API_CLAUDE_CODE_NATIVE_FORMAL_POOL_MODELS"].split(","))
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        catalog_native_models = {
            entry["model_id"]
            for entry in catalog["models"]
            if entry["route"] == "claude_code_native"
        }

        self.assertEqual(
            {
                "claude-opus-4-8",
                "claude-sonnet-4-6",
                "claude-haiku-4-5-20251001",
            },
            native_models,
        )
        self.assertEqual(native_models, catalog_native_models)
        self.assertNotIn("claude-opus-4-7", native_models)
        self.assertNotIn("claude-fable-5", native_models)
        self.assertNotIn("claude-opus-4-5-20251101", native_models)
        self.assertNotIn("claude-sonnet-4-5-20250929", native_models)

    def test_apply_cli_passes_conditional_agnes_strict_live_scope_metadata(self):
        import tools.claude_code_runtime_canary_config as config

        captured = {}
        original_apply = config.apply_bridge_groups
        try:
            def fake_apply_bridge_groups(**kwargs):
                captured.update(kwargs)
                return {
                    "mode": "applied",
                    "writes_enabled": True,
                    "target": "http://127.0.0.1:3017",
                    "runtime_target": "http://127.0.0.1:3017",
                    "postgres_container": kwargs["postgres_container"],
                    "groups": [],
                    "runtime_dispatch_groups": [],
                    "runtime_dispatch_account_bindings": {},
                    "env_out": str(kwargs["env_out"]),
                    "env_keys": [],
                    "bridge_api_key_env_names": {},
                    "bridge_api_key_values": {},
                    "live_bridge_models": list(kwargs["live_bridge_models"]),
                }

            config.apply_bridge_groups = fake_apply_bridge_groups
            stdout = io.StringIO()
            stderr = io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                rc = config.main([
                    "--apply",
                    "--user-approved-db-write",
                    "--postgres-container",
                    "pg",
                    "--target",
                    "http://127.0.0.1:3017",
                    "--live-bridge-models",
                    "claude-code-bridge-agnes-2.0-flash",
                    "--bridge-provider-release-statuses-json",
                    '{"agnes":"strict-live-pass"}',
                    "--format",
                    "json",
                ])
        finally:
            config.apply_bridge_groups = original_apply

        self.assertEqual(0, rc)
        self.assertEqual("", stderr.getvalue())
        self.assertEqual(("claude-code-bridge-agnes-2.0-flash",), captured["live_bridge_models"])
        self.assertEqual({"agnes": "strict-live-pass"}, captured["provider_release_statuses"])
        self.assertEqual((), captured["expanded_live_providers"])


    def test_apply_requires_explicit_user_approved_db_write_even_with_container(self):
        stdout = io.StringIO()
        stderr = io.StringIO()

        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = main([
                "--apply",
                "--postgres-container",
                "sub2api-codex-gateway-live-postgres",
                "--target",
                "http://127.0.0.1:3017",
            ])

        self.assertEqual(2, rc)
        self.assertEqual("", stdout.getvalue())
        self.assertIn("user-approved", stderr.getvalue())

    def test_apply_sql_creates_dedicated_runtime_bridge_groups_keys_and_account_bindings(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql()

        self.assertIn("zhumeng-claude-code-bridge-runtime-openai", sql)
        self.assertIn("zhumeng-claude-code-bridge-runtime-deepseek", sql)
        self.assertIn("zhumeng-claude-code-bridge-runtime-agnes", sql)
        self.assertIn("zhumeng-claude-code-bridge-openai-runtime-key", sql)
        self.assertIn("zhumeng-claude-code-bridge-deepseek-runtime-key", sql)
        self.assertIn("zhumeng-claude-code-bridge-agnes-runtime-key", sql)
        self.assertIn("INSERT INTO account_groups", sql)
        self.assertIn("zhumeng-claude-code-bridge-openai-runtime", sql)
        self.assertIn("zhumeng-claude-code-bridge-deepseek-anthropic", sql)
        self.assertIn("zhumeng-claude-code-bridge-agnes-runtime", sql)
        self.assertIn("source_account_name", sql)
        self.assertIn("codex-upstream-openai-compatible", sql)
        self.assertIn("codex-upstream-deepseek-v4", sql)
        self.assertIn("codex-upstream-agnes-apihub", sql)
        self.assertIn("allow_messages_dispatch", sql)
        self.assertIn("true", sql)
        self.assertIn("codex_gateway_entitled = false", sql)
        self.assertIn("augment_gateway_entitled = false", sql)
        self.assertNotIn("('codex-upstream-openai-compatible','zhumeng-claude-code-bridge-runtime-openai'", sql)
        self.assertNotIn("('codex-upstream-agnes-apihub','zhumeng-claude-code-bridge-runtime-agnes'", sql)
        self.assertNotIn("group_id = 8", sql)
        self.assertNotIn("zhumeng-claude-code-native-upstream", sql)
        self.assertNotIn("openai-default", sql)
        self.assertNotIn("agnes-default", sql)

    def test_apply_sql_creates_runtime_accounts_for_every_live_bridge_provider_without_binding_source_rows(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})
        account_section = sql.split("WITH desired_runtime_accounts", 1)[1].split("WITH desired_bindings", 1)[0]
        binding_section = sql.split("WITH desired_bindings", 1)[1].split("WITH desired_keys", 1)[0]

        expected_runtime_accounts = {
            "zhumeng-claude-code-bridge-openai-runtime",
            "zhumeng-claude-code-bridge-deepseek-anthropic",
            "zhumeng-claude-code-bridge-agnes-runtime",
        }
        for name in expected_runtime_accounts:
            self.assertIn(name, account_section)
            self.assertIn(name, binding_section)

        for source_name in {
            "codex-upstream-openai-compatible",
            "codex-upstream-deepseek-v4",
            "codex-upstream-agnes-apihub",
        }:
            self.assertIn(source_name, account_section)
            self.assertNotIn(f"({source_name!r},", binding_section)
            self.assertNotIn(f",{source_name!r},", binding_section)

    def test_apply_sql_runtime_api_keys_are_idempotent_by_name_not_random_key_only(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})

        self.assertIn("existing_runtime_keys", sql)
        self.assertIn("updated_runtime_keys", sql)
        self.assertIn("api_keys.id = existing_runtime_keys.id", sql)
        self.assertNotIn("ON CONFLICT (key) DO UPDATE", sql)



    def test_apply_sql_openai_runtime_account_inherits_codex_upstream_ua_and_stable_model_mapping(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})
        account_section = sql.split("WITH desired_runtime_accounts", 1)[1].split("WITH desired_bindings", 1)[0]

        self.assertIn("zhumeng-claude-code-bridge-openai-runtime", account_section)
        self.assertIn('"gpt-5.4-mini": "gpt-5.4-mini"', account_section)
        self.assertIn('"claude-code-bridge-gpt-5.4-mini": "gpt-5.4-mini"', account_section)
        self.assertNotIn("gpt-5.4-mini-2026-03-17", account_section)
        self.assertIn("__SOURCE_ACCOUNT_USER_AGENT__", account_section)
        self.assertIn("source_runtime_accounts.credentials->>'user_agent'", account_section)
        self.assertIn("safe_source_extra", account_section)
        self.assertIn("jsonb_build_object('codex_gateway_local_test'", account_section)
        self.assertIn("safe_source_extra || desired_runtime_accounts.extra", account_section)
        self.assertNotIn("COALESCE(source_runtime_accounts.extra", account_section)
        self.assertIn("temp_unschedulable_until = NULL", account_section)
        self.assertIn("temp_unschedulable_reason = NULL", account_section)

    def test_apply_sql_fails_closed_when_source_runtime_accounts_are_missing_or_unschedulable(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})
        account_section = sql.split("WITH desired_runtime_accounts", 1)[1].split("WITH desired_bindings", 1)[0]

        self.assertIn("validate_runtime_sources", account_section)
        self.assertIn("missing Claude Code bridge runtime source accounts", account_section)
        self.assertIn("accounts.status = 'active'", account_section)
        self.assertIn("accounts.schedulable = true", account_section)
        self.assertIn("accounts.platform = desired_runtime_accounts.source_platform", account_section)

    def test_apply_sql_creates_deepseek_anthropic_runtime_account_without_reusing_codex_account(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})

        self.assertIn("zhumeng-claude-code-bridge-deepseek-anthropic", sql)
        self.assertIn("https://api.deepseek.com/anthropic", sql)
        self.assertIn("jsonb_set", sql)
        self.assertIn("source_runtime_accounts.credentials->>'api_key'", sql)
        self.assertIn("'anthropic','apikey','active',true", sql)
        self.assertIn("anthropic_passthrough", sql)
        self.assertIn("deepseek-v4-pro", sql)
        self.assertIn("deepseek-v4-flash", sql)
        self.assertNotIn("https://api.deepseek.com/chat/completions", sql)
        self.assertNotIn("https://api.deepseek.com/v1/chat/completions", sql)


    def test_apply_sql_clears_bridge_runtime_proxy_bindings_instead_of_using_canary_host_proxy(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})
        account_section = sql.split("WITH desired_runtime_accounts", 1)[1].split("WITH desired_bindings", 1)[0]

        self.assertNotIn("zhumeng-claude-code-deepseek-host-proxy", sql)
        self.assertNotIn("host.docker.internal", sql)
        self.assertNotIn("WITH desired_runtime_account_proxies", sql)
        self.assertIn("proxy_id = NULL", account_section)
        self.assertIn("proxy_fallback_origin_id = NULL", account_section)
        self.assertIn("zhumeng-claude-code-bridge-deepseek-anthropic", sql)


    def test_apply_sql_upserts_runtime_accounts_without_requiring_account_name_unique_index(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})

        account_section = sql.split("WITH desired_runtime_accounts", 1)[1].split("WITH desired_bindings", 1)[0]
        self.assertIn("existing_runtime_accounts", account_section)
        self.assertIn("updated_runtime_accounts", account_section)
        self.assertIn("WHERE NOT EXISTS", account_section)
        self.assertNotIn("ON CONFLICT (name)", account_section)

    def test_apply_sql_prunes_stale_runtime_group_bindings_before_inserting_desired_bindings(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})

        self.assertIn("pruned_runtime_bindings", sql)
        self.assertIn("DELETE FROM account_groups", sql)
        self.assertIn("desired_bindings", sql)
        self.assertIn("account_groups.group_id = runtime_groups.group_id", sql)
        self.assertIn("account_groups.account_id <> ALL(runtime_groups.desired_account_ids)", sql)

    def test_apply_result_and_env_file_report_only_key_names_not_values(self):
        from tools.claude_code_runtime_canary_config import build_apply_result_metadata, build_provider_catalog_env

        env = build_provider_catalog_env(
            "http://127.0.0.1:3017",
            live_bridge_models=("claude-code-bridge-gpt-5.5", "claude-code-bridge-deepseek-v4-pro"),
            bridge_api_keys={
                "openai": "sk-openai-secret",
                "deepseek": "sk-deepseek-secret",
                "agnes": "sk-agnes-secret",
            },
        )
        result = build_apply_result_metadata(
            postgres_container="pg",
            env_out="/tmp/cc.env",
            target="http://127.0.0.1:3017",
            live_bridge_models=("claude-code-bridge-gpt-5.5",),
            env=env,
        )
        dumped = json.dumps(result, sort_keys=True)

        self.assertIn("SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY", result["env_keys"])
        self.assertIn("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY", result["env_keys"])
        self.assertIn("SUB2API_CLAUDE_CODE_BRIDGE_AGNES_API_KEY", result["env_keys"])
        self.assertNotIn("sk-openai-secret", dumped)
        self.assertNotIn("sk-deepseek-secret", dumped)
        self.assertNotIn("sk-agnes-secret", dumped)
        self.assertEqual("<redacted>", result["bridge_api_key_values"]["openai"])

    def test_runtime_bridge_api_keys_reuse_existing_env_values_to_avoid_db_env_drift(self):
        from tools.claude_code_runtime_canary_config import build_runtime_bridge_api_keys

        keys = build_runtime_bridge_api_keys(existing_env={
            "SUB2API_CLAUDE_CODE_BRIDGE_OPENAI_API_KEY": "sk-existing-openai",
            "SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY": "sk-existing-deepseek",
            "SUB2API_CLAUDE_CODE_BRIDGE_AGNES_API_KEY": "sk-existing-agnes",
        })

        self.assertEqual("sk-existing-openai", keys["openai"])
        self.assertEqual("sk-existing-deepseek", keys["deepseek"])
        self.assertEqual("sk-existing-agnes", keys["agnes"])


    def test_apply_sql_models_list_config_uses_string_arrays_not_objects(self):
        from tools.claude_code_runtime_canary_config import (
            build_apply_sql,
            build_provider_catalog_env,
            runtime_models_list_config_for_test,
            placeholder_models_list_config_for_test,
        )

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})
        self.assertNotIn('"model_id"', sql)
        self.assertNotIn('"upstream_model"', sql)
        self.assertNotIn('"capability_tier"', sql)

        for config in placeholder_models_list_config_for_test() + runtime_models_list_config_for_test():
            self.assertIsInstance(config["models"], list)
            self.assertTrue(config["models"], "each Claude Code bridge group should expose at least one display id")
            for model in config["models"]:
                self.assertIsInstance(model, str)
                self.assertTrue(model.startswith("claude-code-bridge-"))

        env = build_provider_catalog_env("http://127.0.0.1:3017")
        catalog = json.loads(env["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])
        bridge = {entry["model_id"]: entry for entry in catalog["models"] if entry["model_id"].startswith("claude-code-bridge-")}
        self.assertEqual("gpt-5.5", bridge["claude-code-bridge-gpt-5.5"]["upstream_model"])
        self.assertEqual("openai_bridge", bridge["claude-code-bridge-gpt-5.5"]["route"])

    def test_apply_sql_updates_claude_code_native_display_catalog_with_curated_strings(self):
        from tools.claude_code_runtime_canary_config import build_apply_sql

        sql = build_apply_sql(bridge_api_keys={"openai": "sk-openai", "deepseek": "sk-deepseek", "agnes": "sk-agnes"})

        self.assertIn("zhumeng-claude-code-native", sql)
        self.assertIn("WHERE id = 8", sql)
        self.assertIn("claude-opus-4-8", sql)
        self.assertIn("claude-sonnet-4-6", sql)
        self.assertIn("claude-haiku-4-5-20251001", sql)
        self.assertIn("claude-code-bridge-gpt-5.5", sql)
        self.assertIn("claude-code-bridge-deepseek-v4-pro", sql)
        self.assertIn("claude-code-bridge-agnes-2.0-flash", sql)
        self.assertIn("claude-code-bridge-glm-5.2-1m", sql)
        self.assertIn("claude-code-bridge-kimi-k2.7-code", sql)
        self.assertNotIn("claude-opus-4-7", sql)
        self.assertNotIn("claude-fable-5", sql)
        self.assertNotIn("claude-opus-4-5-20251101", sql)
        self.assertNotIn("claude-sonnet-4-5-20250929", sql)



    def test_apply_validates_runtime_hash_before_touching_database(self):
        import tools.claude_code_runtime_canary_config as config

        calls = []
        original_run = config.subprocess.run
        try:
            def fake_run(*args, **kwargs):
                calls.append((args, kwargs))
                raise AssertionError("apply must not touch postgres when hash validation fails")

            config.subprocess.run = fake_run
            with tempfile.TemporaryDirectory() as tmp:
                env_path = Path(tmp) / "runtime.env"
                env_path.write_text("SUB2API_CLAUDE_CODE_BRIDGE_DEEPSEEK_API_KEY=sk-existing\n", encoding="utf-8")
                with self.assertRaisesRegex(Exception, "runtime_hash"):
                    config.apply_bridge_groups(
                        postgres_container="postgres",
                        env_out=env_path,
                        target="http://127.0.0.1:3017",
                        live_bridge_models=("claude-code-bridge-deepseek-v4-pro",),
                        runtime_hash="not-a-sha256-hash",
                        overlay_hash="sha256:" + "2" * 64,
                    )
        finally:
            config.subprocess.run = original_run

        self.assertEqual([], calls)

    def test_apply_preserves_existing_native_attestation_and_runtime_hashes_when_rewriting_env(self):
        from tools.claude_code_runtime_canary_config import merge_provider_catalog_env

        existing = {
            "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID": "guard_v1",
            "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET": "secret-from-existing-env",
            "SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES": "sha256:" + "a" * 64,
            "SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES": "sha256:" + "b" * 64,
            "SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES": "sha256:" + "c" * 64,
        }

        merged = merge_provider_catalog_env(
            existing,
            target="http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            live_bridge_models=("claude-code-bridge-deepseek-v4-pro",),
            bridge_api_keys={"deepseek": "sk-deepseek"},
        )

        self.assertEqual("guard_v1", merged["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID"])
        self.assertEqual("secret-from-existing-env", merged["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET"])
        self.assertEqual("sha256:" + "a" * 64, merged["SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES"])
        self.assertEqual("sha256:" + "b" * 64, merged["SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES"])
        self.assertNotEqual("sha256:" + "1" * 64, merged["SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES"])
        self.assertIn("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", merged)

    def test_apply_can_override_stale_runtime_hashes_when_runtime_manifest_changes(self):
        from tools.claude_code_runtime_canary_config import merge_provider_catalog_env

        existing = {
            "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID": "guard_v1",
            "SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET": "secret-from-existing-env",
            "SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES": "sha256:" + "a" * 64,
            "SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES": "sha256:" + "b" * 64,
            "SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES": "sha256:" + "c" * 64,
        }
        current_runtime_hash = "sha256:" + "7" * 64
        current_overlay_hash = "sha256:" + "8" * 64

        merged = merge_provider_catalog_env(
            existing,
            target="http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
            runtime_hash=current_runtime_hash,
            overlay_hash=current_overlay_hash,
            live_bridge_models=("claude-code-bridge-deepseek-v4-pro",),
            bridge_api_keys={"deepseek": "sk-deepseek"},
        )
        catalog = json.loads(merged["SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON"])

        self.assertEqual("guard_v1", merged["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_CURRENT_KEY_ID"])
        self.assertEqual("secret-from-existing-env", merged["SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET"])
        self.assertEqual(current_runtime_hash, merged["SUB2API_CLAUDE_CODE_NATIVE_RUNTIME_HASHES"])
        self.assertEqual(current_overlay_hash, merged["SUB2API_CLAUDE_CODE_NATIVE_OVERLAY_HASHES"])
        self.assertEqual(current_runtime_hash, catalog["runtime_hash"])
        self.assertEqual(current_overlay_hash, catalog["overlay_hash"])
        self.assertEqual(merged["SUB2API_CLAUDE_CODE_NATIVE_CATALOG_HASHES"], catalog["catalog_hash"])
        self.assertNotEqual("sha256:" + "c" * 64, catalog["catalog_hash"])

    def test_apply_env_marks_native_remote_sub2api_upstream_accounts(self):
        from tools.claude_code_runtime_canary_config import merge_provider_catalog_env

        merged = merge_provider_catalog_env(
            {},
            target="http://127.0.0.1:3017",
            runtime_target="http://127.0.0.1:8080",
        )

        self.assertEqual(
            "zhumeng-claude-code-native-upstream",
            merged["SUB2API_CLAUDE_CODE_NATIVE_REMOTE_SUB2API_ACCOUNT_NAMES"],
        )

    def test_env_file_writer_outputs_docker_env_file_values_without_json_wrapping_quotes(self):
        from tools.claude_code_runtime_canary_config import _write_env_file

        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "runtime.env"
            _write_env_file(
                path,
                {
                    "SERVER_PORT": "8080",
                    "GATEWAY_CODEX_ENABLED": "true",
                    "SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON": '{"models":["claude-opus-4-8"]}',
                },
            )
            text = path.read_text(encoding="utf-8")

        self.assertIn("SERVER_PORT=8080\n", text)
        self.assertIn("GATEWAY_CODEX_ENABLED=true\n", text)
        self.assertIn('SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON={"models":["claude-opus-4-8"]}\n', text)
        self.assertNotIn('SERVER_PORT="8080"', text)
        self.assertNotIn('GATEWAY_CODEX_ENABLED="true"', text)




if __name__ == "__main__":
    unittest.main()
