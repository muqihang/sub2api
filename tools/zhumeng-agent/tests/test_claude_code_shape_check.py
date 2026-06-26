from __future__ import annotations

import json

from zhumeng_agent.adapters.claude_code.shape_check import (
    NativeShapeEvidence,
    evaluate_native_shape_healthcheck,
    native_fixture_from_messages_body,
)


def test_native_shape_healthcheck_covers_required_fixture_matrix_without_raw_leak():
    sonnet = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"system":[{"type":"text","text":"fixture-system-marker","cache_control":{"type":"ephemeral"}}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"cache_control":{"type":"ephemeral"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"fixture-native-message-marker","cache_control":{"type":"ephemeral"}}]}],"eager_input_streaming":true}',
        name="sonnet_toolsearch",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    opus = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","stream":false,"thinking":{"type":"adaptive"},"tools":[],"messages":[{"role":"user","content":"fixture-opus-message-marker"}]}',
        name="opus_messages",
        route="/v1/messages",
        profile="real_claude_code_native_toolsearch_v1",
    )
    count_tokens = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"fixture-count-message-marker"}]}',
        name="count_tokens",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_takeover_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [sonnet, opus, count_tokens],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": True,
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": 200,
                "stores_raw": False,
                "messages_signing_reused": False,
                "response_schema_keys": ["ok"],
            },
            netwatch_summary={
                "connection_count": 1,
                "potential_guard_bypass_count": 0,
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"loopback": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
            prompt_cache_safe_usage={
                "provider_cache_mechanism": "anthropic_cache_control",
                "cache_control_present": True,
                "cache_control_locations": ["history", "system", "tools"],
                "prompt_caching_beta_present": True,
                "context_management_beta_present": True,
                "cache_usage_fields": ["cache_creation_input_tokens", "cache_read_input_tokens"],
                "cache_creation_input_tokens": 0,
                "cache_read_input_tokens": 0,
                "stores_raw": False,
                "body_omitted": True,
                "response_omitted": True,
            },
            fgts_safe_trace={
                "mode": "observe_only",
                "requested_mode": "enabled",
                "env_value": "unset",
                "eager_input_streaming_present": True,
                "direct_official_egress": False,
                "stores_raw": False,
                "body_omitted": True,
            },
        ),
    )

    assert result.status == "pass"
    assert result.passed == result.denominator
    assert "messages_fixture" in result.fields
    assert "tool_search_fixture" in result.fields
    assert "count_tokens_fixture" in result.fields
    assert "prompt_caching_fixture" in result.fields
    assert "prompt_cache_usage_fixture" in result.fields
    assert "eager_input_streaming_fixture" in result.fields
    assert "fgts_trace_fixture" in result.fields
    assert "context_management_fixture" in result.fields
    assert "output_config_fixture" in result.fields
    assert "control_plane_safe_intent_fixture" in result.fields
    assert "netwatch_fixture" in result.fields
    assert result.routes == ("/v1/messages", "/v1/messages/count_tokens")
    assert result.model_families == ("opus", "sonnet")
    safe_dict = result.to_safe_dict()
    assert safe_dict["safe_evidence"]["prompt_cache"] == "safe_usage_summary_present"
    assert safe_dict["safe_evidence"]["fgts"] == "safe_trace_summary_present"
    dumped = json.dumps(safe_dict, sort_keys=True)
    assert "fixture-native-message-marker" not in dumped
    assert "fixture-opus-message-marker" not in dumped
    assert "fixture-count-message-marker" not in dumped
    assert "fixture-system-marker" not in dumped
    assert "content" not in dumped
    assert "input_schema" not in dumped
    assert "raw_" not in dumped


def test_native_shape_healthcheck_rejects_eager_only_toolsearch_false_positive():
    eager_only = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{},"tools":[{"name":"Read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}],"eager_input_streaming":true}',
        name="eager_only",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    opus = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","messages":[{"role":"user","content":"fixture-opus"}]}',
        name="opus",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_toolsearch_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [eager_only, opus],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": True,
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": 200,
                "stores_raw": False,
                "messages_signing_reused": False,
                "response_schema_keys": ["ok"],
            },
            netwatch_summary={
                "connection_count": 1,
                "potential_guard_bypass_count": 0,
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"loopback": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
        ),
    )

    assert result.status == "fail"
    assert "tool_search_fixture" in result.failed_fields


def test_native_shape_healthcheck_fails_closed_on_non_mock_or_netwatch_bypass():
    fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"tools":[],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="sonnet_messages",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [fixture],
        NativeShapeEvidence(
            localhost_only=False,
            mock_upstream_only=False,
            control_plane_safe_intent={"safe_intent": False, "stores_raw": True},
            netwatch_summary={
                "potential_guard_bypass_count": 1,
                "official_or_public_bypass_count": 1,
                "stores_payload": False,
                "stores_headers": False,
            },
        ),
    )

    assert result.status == "fail"
    assert "localhost_only" in result.failed_fields
    assert "mock_upstream_only" in result.failed_fields
    assert "control_plane_safe_intent_fixture" in result.failed_fields
    assert "netwatch_fixture" in result.failed_fields


def test_native_shape_healthcheck_requires_explicit_toolsearch_markers():
    plain_tools = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"tool_reference":{"type":"string"},"defer_loading":{"type":"boolean"}}}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="plain_tools",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    count_tokens = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","messages":[{"role":"user","content":"fixture-count"}]}',
        name="count_tokens",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_toolsearch_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [plain_tools, count_tokens],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": True,
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": 200,
                "stores_raw": False,
                "messages_signing_reused": False,
                "response_schema_keys": ["ok"],
            },
            netwatch_summary={
                "connection_count": 1,
                "potential_guard_bypass_count": 0,
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"loopback": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
        ),
    )

    assert result.status == "fail"
    assert "tool_search_fixture" in result.failed_fields


def test_native_shape_healthcheck_rejects_sensitive_or_bypass_safe_summaries():
    fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="sonnet_toolsearch",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [fixture],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": True,
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": 200,
                "stores_raw": False,
                "messages_signing_reused": True,
                "response_schema_keys": ["ok"],
                "authorization": "present",
            },
            netwatch_summary={
                "connection_count": 1,
                "potential_guard_bypass_count": 0,
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"api.anthropic.com": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
        ),
    )

    assert result.status == "fail"
    assert "control_plane_safe_intent_fixture" in result.failed_fields
    assert "netwatch_fixture" in result.failed_fields


def test_native_shape_healthcheck_rejects_type_coercion_and_broad_sensitive_keys():
    fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="sonnet_toolsearch",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [fixture],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": "true",
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": "200",
                "stores_raw": False,
                "messages_signing_reused": False,
                "response_schema_keys": ["raw_token"],
            },
            netwatch_summary={
                "connection_count": "1",
                "potential_guard_bypass_count": "0",
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"loopback": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
        ),
    )

    assert result.status == "fail"
    assert "control_plane_safe_intent_fixture" in result.failed_fields
    assert "netwatch_fixture" in result.failed_fields


def test_native_shape_healthcheck_allows_block_and_shadow_control_plane_decisions():
    for decision in ("block_403", "shadow_stub", "shadow_block"):
        fixture = native_fixture_from_messages_body(
            b'{"model":"claude-opus-4-7","stream":true,"thinking":{},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}]}',
            name=f"fixture_{decision}",
            route="/v1/messages",
            profile="real_claude_code_native_takeover_v1",
        )
        count_tokens = native_fixture_from_messages_body(
            b'{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"fixture-count"}]}',
            name=f"count_{decision}",
            route="/v1/messages/count_tokens",
            profile="real_claude_code_native_toolsearch_v1",
        )
        result = evaluate_native_shape_healthcheck(
            [fixture, count_tokens],
            NativeShapeEvidence(
                localhost_only=True,
                mock_upstream_only=True,
                control_plane_safe_intent={
                    "safe_intent": True,
                    "method": "GET",
                    "path_template": "/api/claude_cli/bootstrap",
                    "decision": decision,
                    "status": 403,
                    "stores_raw": False,
                    "messages_signing_reused": False,
                    "response_schema_keys": ["ok"],
                },
                netwatch_summary={
                    "connection_count": 1,
                    "potential_guard_bypass_count": 0,
                    "official_or_public_bypass_count": 0,
                    "loopback_guard_connection_count": 1,
                    "remote_host_buckets": {"loopback": 1},
                    "stores_payload": False,
                    "stores_headers": False,
                },
            ),
        )

        assert "control_plane_safe_intent_fixture" not in result.failed_fields



def test_native_shape_healthcheck_requires_eager_input_streaming_trace():
    body_without_eager = b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"fixture-system","cache_control":{"type":"ephemeral"}}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"cache_control":{"type":"ephemeral"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"fixture-message","cache_control":{"type":"ephemeral"}}]}]}'
    without_eager = native_fixture_from_messages_body(
        body_without_eager,
        name="without_eager",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    with_eager = native_fixture_from_messages_body(
        body_without_eager.replace(b'"messages"', b'"eager_input_streaming":true,"messages"', 1),
        name="with_eager",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    count_tokens = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","messages":[{"role":"user","content":"fixture-count"}]}',
        name="count_tokens",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_control_plane_shadow_v1",
    )
    evidence = NativeShapeEvidence(
        localhost_only=True,
        mock_upstream_only=True,
        control_plane_safe_intent={
            "safe_intent": True,
            "method": "GET",
            "path_template": "/api/claude_cli/bootstrap",
            "decision": "stub_json",
            "status": 200,
            "stores_raw": False,
            "messages_signing_reused": False,
            "response_schema_keys": ["ok"],
        },
        netwatch_summary={
            "connection_count": 1,
            "potential_guard_bypass_count": 0,
            "official_or_public_bypass_count": 0,
            "loopback_guard_connection_count": 1,
            "remote_host_buckets": {"loopback": 1},
            "stores_payload": False,
            "stores_headers": False,
        },
        prompt_cache_safe_usage={
            "provider_cache_mechanism": "anthropic_cache_control",
            "cache_control_present": True,
            "cache_control_locations": ["history", "system", "tools"],
            "prompt_caching_beta_present": True,
            "context_management_beta_present": True,
            "cache_usage_fields": ["cache_creation_input_tokens", "cache_read_input_tokens"],
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0,
            "stores_raw": False,
            "body_omitted": True,
            "response_omitted": True,
        },
        fgts_safe_trace={
            "mode": "observe_only",
            "requested_mode": "enabled",
            "env_value": "unset",
            "eager_input_streaming_present": True,
            "direct_official_egress": False,
            "stores_raw": False,
            "body_omitted": True,
        },
    )

    missing_eager = evaluate_native_shape_healthcheck([without_eager, count_tokens], evidence)
    assert missing_eager.status == "fail"
    assert "eager_input_streaming_fixture" in missing_eager.failed_fields

    missing_trace = evaluate_native_shape_healthcheck(
        [with_eager, count_tokens],
        NativeShapeEvidence(
            localhost_only=evidence.localhost_only,
            mock_upstream_only=evidence.mock_upstream_only,
            control_plane_safe_intent=evidence.control_plane_safe_intent,
            netwatch_summary=evidence.netwatch_summary,
            prompt_cache_safe_usage=evidence.prompt_cache_safe_usage,
        ),
    )
    assert missing_trace.status == "fail"
    assert "fgts_trace_fixture" in missing_trace.failed_fields

    unsafe_trace = dict(evidence.fgts_safe_trace)
    unsafe_trace["mode"] = "enabled"
    unsafe_trace["env_value"] = "1"
    unsafe_trace["direct_official_egress"] = True
    unsafe = evaluate_native_shape_healthcheck(
        [with_eager, count_tokens],
        NativeShapeEvidence(
            localhost_only=evidence.localhost_only,
            mock_upstream_only=evidence.mock_upstream_only,
            control_plane_safe_intent=evidence.control_plane_safe_intent,
            netwatch_summary=evidence.netwatch_summary,
            prompt_cache_safe_usage=evidence.prompt_cache_safe_usage,
            fgts_safe_trace=unsafe_trace,
        ),
    )
    assert unsafe.status == "fail"
    assert "fgts_trace_fixture" in unsafe.failed_fields


def test_native_shape_healthcheck_requires_prompt_caching_shape_and_safe_usage():
    base_fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"fixture-system"}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="base_without_cache",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    cache_fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"fixture-system","cache_control":{"type":"ephemeral"}}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"cache_control":{"type":"ephemeral"},"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":[{"type":"text","text":"fixture-message","cache_control":{"type":"ephemeral"}}]}]}',
        name="with_cache",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    count_tokens = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","messages":[{"role":"user","content":"fixture-count"}]}',
        name="count_tokens",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_control_plane_shadow_v1",
    )
    evidence = NativeShapeEvidence(
        localhost_only=True,
        mock_upstream_only=True,
        control_plane_safe_intent={
            "safe_intent": True,
            "method": "GET",
            "path_template": "/api/claude_cli/bootstrap",
            "decision": "stub_json",
            "status": 200,
            "stores_raw": False,
            "messages_signing_reused": False,
            "response_schema_keys": ["ok"],
        },
        netwatch_summary={
            "connection_count": 1,
            "potential_guard_bypass_count": 0,
            "official_or_public_bypass_count": 0,
            "loopback_guard_connection_count": 1,
            "remote_host_buckets": {"loopback": 1},
            "stores_payload": False,
            "stores_headers": False,
        },
        prompt_cache_safe_usage={
            "provider_cache_mechanism": "anthropic_cache_control",
            "cache_control_present": True,
            "cache_control_locations": ["history", "system", "tools"],
            "prompt_caching_beta_present": True,
            "context_management_beta_present": True,
            "cache_usage_fields": ["cache_creation_input_tokens", "cache_read_input_tokens"],
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0,
            "stores_raw": False,
            "body_omitted": True,
            "response_omitted": True,
        },
    )

    missing_cache_shape = evaluate_native_shape_healthcheck([base_fixture, count_tokens], evidence)
    assert missing_cache_shape.status == "fail"
    assert "prompt_caching_fixture" in missing_cache_shape.failed_fields

    missing_usage = evaluate_native_shape_healthcheck(
        [cache_fixture, count_tokens],
        NativeShapeEvidence(
            localhost_only=evidence.localhost_only,
            mock_upstream_only=evidence.mock_upstream_only,
            control_plane_safe_intent=evidence.control_plane_safe_intent,
            netwatch_summary=evidence.netwatch_summary,
        ),
    )
    assert missing_usage.status == "fail"
    assert "prompt_cache_usage_fixture" in missing_usage.failed_fields

    unsafe_usage = dict(evidence.prompt_cache_safe_usage)
    unsafe_usage["cache_control_locations"] = ["history", "raw prompt body"]
    unsafe = evaluate_native_shape_healthcheck(
        [cache_fixture, count_tokens],
        NativeShapeEvidence(
            localhost_only=evidence.localhost_only,
            mock_upstream_only=evidence.mock_upstream_only,
            control_plane_safe_intent=evidence.control_plane_safe_intent,
            netwatch_summary=evidence.netwatch_summary,
            prompt_cache_safe_usage=unsafe_usage,
        ),
    )
    assert unsafe.status == "fail"
    assert "prompt_cache_usage_fixture" in unsafe.failed_fields


def test_native_shape_prompt_cache_usage_locations_must_match_observed_shape():
    cache_fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"adaptive"},"system":[{"type":"text","text":"fixture-system","cache_control":{"type":"ephemeral"}}],"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]},"output_config":{"effort":"high"},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="system_cache_only",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    count_tokens = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","messages":[{"role":"user","content":"fixture-count"}]}',
        name="count_tokens",
        route="/v1/messages/count_tokens",
        profile="real_claude_code_native_control_plane_shadow_v1",
    )

    result = evaluate_native_shape_healthcheck(
        [cache_fixture, count_tokens],
        NativeShapeEvidence(
            localhost_only=True,
            mock_upstream_only=True,
            control_plane_safe_intent={
                "safe_intent": True,
                "method": "GET",
                "path_template": "/api/claude_cli/bootstrap",
                "decision": "stub_json",
                "status": 200,
                "stores_raw": False,
                "messages_signing_reused": False,
                "response_schema_keys": ["ok"],
            },
            netwatch_summary={
                "connection_count": 1,
                "potential_guard_bypass_count": 0,
                "official_or_public_bypass_count": 0,
                "loopback_guard_connection_count": 1,
                "remote_host_buckets": {"loopback": 1},
                "stores_payload": False,
                "stores_headers": False,
            },
            prompt_cache_safe_usage={
                "provider_cache_mechanism": "anthropic_cache_control",
                "cache_control_present": True,
                "cache_control_locations": ["history", "system", "tools"],
                "prompt_caching_beta_present": True,
                "context_management_beta_present": True,
                "cache_usage_fields": ["cache_creation_input_tokens", "cache_read_input_tokens"],
                "cache_creation_input_tokens": 0,
                "cache_read_input_tokens": 0,
                "stores_raw": False,
                "body_omitted": True,
                "response_omitted": True,
            },
        ),
    )

    assert result.status == "fail"
    assert "prompt_cache_usage_fixture" in result.failed_fields


def test_native_shape_prompt_cache_detection_ignores_schema_false_positive():
    fixture = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","tools":[{"name":"Read","input_schema":{"type":"object","properties":{"cache_control":{"type":"object"}}}}],"messages":[{"role":"user","content":"fixture-message"}]}',
        name="schema_only",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )

    assert not fixture.has_prompt_caching
