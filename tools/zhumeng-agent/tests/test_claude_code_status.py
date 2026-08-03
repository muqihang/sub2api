from __future__ import annotations

import json

from zhumeng_agent.adapters.claude_code.status import derive_claude_code_operator_status


def test_operator_status_reports_ready_from_safe_healthy_state():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "profile": {"status": "ready", "profile_id": "real_claude_code_native_takeover_v1"},
                "guard": {"status": "ready", "attested": True, "mode": "production"},
                "shape_healthcheck": {"status": "pass"},
                "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                "local_session_ref": "session:opaque-1",
                "guard_summary_ref": "guard:summary-1",
                "netwatch": {
                    "status": "clean",
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "remote_host_buckets": {"loopback": 2},
                        "stores_payload": False,
                        "stores_headers": False,
                    },
                },
            }
        }
    )

    assert result.status == "ready"
    safe = result.to_safe_dict()
    assert safe["status"] == "ready"
    assert safe["guard"]["attested"] is True
    assert safe["netwatch"]["potential_guard_bypass_count"] == 0
    assert safe["evidence"]["local_session_ref"] == "session:opaque-1"


def test_operator_status_reports_running_when_process_and_guard_are_active():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "process": {"pid": 12345},
                "guard": {"status": "running", "attested": True},
                "profile": {"status": "ready"},
                "shape_healthcheck": {"status": "pass"},
                "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
            }
        },
        process_alive=lambda pid: pid == 12345,
    )

    assert result.status == "running"
    safe = result.to_safe_dict()
    assert safe["running"] is True
    assert safe["guard"]["status"] == "running"


def test_operator_status_precedence_for_required_risk_states():
    bypass = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "guard": {"status": "running", "attested": True},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 1,
                        "official_or_public_bypass_count": 1,
                        "remote_host_buckets": {"loopback": 1, "anthropic_or_claude": 1},
                    }
                },
            }
        }
    )
    assert bypass.status == "guard_bypass"
    assert "direct_egress_bypass" in bypass.reasons

    profile = derive_claude_code_operator_status(
        {"claude_code_native": {"configured": True, "profile": {"status": "profile_mismatch"}}}
    )
    assert profile.status == "profile_mismatch"

    toolsearch = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "profile": {"status": "ready"},
                "guard": {"status": "ready", "attested": True},
                "shape_healthcheck": {"status": "pass"},
                "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
                "toolsearch": {"status": "toolsearch_degraded", "reasons": ["healthcheck"]},
            }
        }
    )
    assert toolsearch.status == "toolsearch_degraded"

    quarantine = derive_claude_code_operator_status(
        {"claude_code_native": {"configured": True, "shape_healthcheck": {"status": "fail", "failed_fields": ["netwatch_fixture"]}}}
    )
    assert quarantine.status == "quarantined"
    assert "shape_healthcheck_failed" in quarantine.reasons


def test_operator_status_allows_toolsearch_degraded_when_only_toolsearch_fixture_failed():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "profile": {"status": "ready"},
                "guard": {"status": "ready", "attested": True},
                "shape_healthcheck": {"status": "fail", "failed_fields": ["tool_search_fixture"]},
                "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
                "toolsearch": {"status": "toolsearch_degraded", "reasons": ["healthcheck"]},
            }
        }
    )

    assert result.status == "toolsearch_degraded"
    assert "toolsearch_degraded" in result.reasons


def test_operator_status_quarantines_toolsearch_degraded_without_safety_evidence():
    result = derive_claude_code_operator_status(
        {"claude_code_native": {"configured": True, "toolsearch": {"status": "toolsearch_degraded", "reasons": ["healthcheck"]}}}
    )

    assert result.status == "quarantined"
    assert "toolsearch_degraded_without_native_evidence" in result.reasons
    assert "guard_not_ready" in result.reasons


def test_operator_status_uses_safe_allowlist_and_does_not_echo_sensitive_state():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "entry_api_token": "raw-token-marker",
                "raw_prompt": "raw-prompt-marker",
                "email": "operator@example.com",
                "local_session_ref": "123e4567-e89b-12d3-a456-426614174000",
                "profile": {"status": "ready", "profile_id": "prod profile with spaces and token raw-token-marker"},
                "toolsearch": {"status": "toolsearch_degraded", "reasons": ["Bearer raw-token-marker", "healthcheck"]},
                "guard": {"status": "ready", "attested": True},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
            }
        }
    )

    dumped = json.dumps(result.to_safe_dict(), sort_keys=True)
    assert "raw-token-marker" not in dumped
    assert "raw-prompt-marker" not in dumped
    assert "operator@example.com" not in dumped
    assert "123e4567-e89b-12d3-a456-426614174000" not in dumped
    assert "Bearer" not in dumped
    assert "redacted_detail" in dumped


def test_operator_status_fails_closed_when_native_evidence_is_incomplete():
    result = derive_claude_code_operator_status({"claude_code_native": {"configured": True}})

    assert result.status == "quarantined"
    assert "guard_not_ready" in result.reasons
    assert "shape_healthcheck_missing" in result.reasons
    assert "netwatch_summary_missing" in result.reasons


def test_operator_status_quarantines_unknown_control_plane_decision():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "profile": {"status": "ready"},
                "guard": {"status": "ready", "attested": True},
                "shape_healthcheck": {"status": "pass"},
                "control_plane": {
                    "safe_intent": True,
                    "messages_signing_reused": False,
                    "stores_raw": False,
                    "decision": "direct_forward",
                },
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
            }
        }
    )

    assert result.status == "quarantined"
    assert "control_plane_decision_not_allowed" in result.reasons


def test_operator_status_treats_raw_netwatch_host_keys_as_bypass_without_echoing_them():
    result = derive_claude_code_operator_status(
        {
            "claude_code_native": {
                "configured": True,
                "profile": {"status": "ready"},
                "guard": {"status": "ready", "attested": True},
                "shape_healthcheck": {"status": "pass"},
                "control_plane": {"safe_intent": True, "messages_signing_reused": False, "stores_raw": False},
                "netwatch": {
                    "summary": {
                        "potential_guard_bypass_count": 0,
                        "official_or_public_bypass_count": 0,
                        "remote_host_buckets": {"api.anthropic.com": 1},
                        "stores_payload": False,
                        "stores_headers": False,
                    }
                },
            }
        }
    )

    assert result.status == "guard_bypass"
    dumped = json.dumps(result.to_safe_dict(), sort_keys=True)
    assert "api.anthropic.com" not in dumped
    assert "anthropic_or_claude" in dumped
