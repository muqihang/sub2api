from __future__ import annotations

import json

from zhumeng_agent.adapters.claude_code.shape_check import (
    NativeShapeEvidence,
    evaluate_native_shape_healthcheck,
    native_fixture_from_messages_body,
)


def test_native_shape_healthcheck_covers_required_fixture_matrix_without_raw_leak():
    sonnet = native_fixture_from_messages_body(
        b'{"model":"claude-sonnet-4-6","stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"tools":[{"name":"Read","tool_reference":{"id":"ref"},"defer_loading":true,"input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"fixture-native-message-marker"}],"eager_input_streaming":true}',
        name="sonnet_toolsearch",
        route="/v1/messages",
        profile="real_claude_code_native_takeover_v1",
    )
    opus = native_fixture_from_messages_body(
        b'{"model":"claude-opus-4-7","stream":false,"tools":[],"messages":[{"role":"user","content":"fixture-opus-message-marker"}]}',
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
        ),
    )

    assert result.status == "pass"
    assert result.passed == result.denominator
    assert "messages_fixture" in result.fields
    assert "tool_search_fixture" in result.fields
    assert "count_tokens_fixture" in result.fields
    assert "control_plane_safe_intent_fixture" in result.fields
    assert "netwatch_fixture" in result.fields
    assert result.routes == ("/v1/messages", "/v1/messages/count_tokens")
    assert result.model_families == ("opus", "sonnet")
    dumped = json.dumps(result.to_safe_dict(), sort_keys=True)
    assert "fixture-native-message-marker" not in dumped
    assert "fixture-opus-message-marker" not in dumped
    assert "fixture-count-message-marker" not in dumped
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
