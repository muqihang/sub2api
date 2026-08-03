from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace
import importlib.util
import sys

import pytest

from zhumeng_agent.adapters.claude_code.model_overlay import (
    RuntimeOverlayError,
    build_cp2_model_overlay_proof,
    build_cp3a_model_overlay_contract,
    build_cp4_route_hint_contract,
    build_cp4_route_hint_headers,
    verify_cp4_route_hint_headers,
)
from zhumeng_agent.adapters.claude_code.runtime_installer import build_managed_runtime_install_plan


def _guard_route_trust_module():
    repo_root = Path(__file__).resolve().parents[3]
    module_path = repo_root / "tools" / "claude_code_route_trust.py"
    spec = importlib.util.spec_from_file_location("cp4_guard_route_trust", module_path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class VersionRunner:
    def __call__(self, command: list[str], **kwargs: object) -> SimpleNamespace:
        return SimpleNamespace(stdout="Claude Code v2.1.175", stderr="", returncode=0)


def _runtime_plan(tmp_path: Path):
    executable = tmp_path / "claude"
    executable.write_bytes(b"fake-claude-code-2.1.175")
    return build_managed_runtime_install_plan(
        executable=executable,
        runtime_root=tmp_path / ".zhumeng" / "runtimes",
        runner=VersionRunner(),
    )


def _contract(tmp_path: Path):
    cp3 = build_cp3a_model_overlay_contract(build_cp2_model_overlay_proof(_runtime_plan(tmp_path)))
    return build_cp4_route_hint_contract(
        cp3,
        catalog_hash="sha256:" + "4" * 64,
        catalog_version="cp4-test-v1",
    )


def test_cp4_overlay_signed_route_hint_binds_body_model_hashes_session_and_nonce(tmp_path: Path):
    contract = _contract(tmp_path)
    body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'

    headers = build_cp4_route_hint_headers(
        contract,
        body=body,
        request_path="/v1/messages?beta=true",
        model_id="claude-code-bridge-deepseek-v4-pro",
        session_ref="session-a",
        secret="route-hint-secret",
        now=1000,
        nonce="nonce-a",
    )
    decision = verify_cp4_route_hint_headers(
        contract,
        source_headers=headers,
        body=body,
        request_path="/v1/messages?beta=true",
        session_ref="session-a",
        secret="route-hint-secret",
        now=1000,
    )

    assert decision.route == "deepseek_bridge"
    assert decision.client_type == "claude_code_bridge_deepseek"
    assert decision.live_request_allowed is False
    assert decision.formal_pool_allowed is False
    assert decision.native_attestation_allowed is False
    assert decision.catalog_hash == "sha256:" + "4" * 64


def test_cp4_overlay_route_hint_rejects_bridge_spoofed_native_even_with_valid_key(tmp_path: Path):
    contract = _contract(tmp_path)
    body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'

    headers = build_cp4_route_hint_headers(
        contract,
        body=body,
        request_path="/v1/messages?beta=true",
        model_id="claude-code-bridge-deepseek-v4-pro",
        session_ref="session-a",
        secret="route-hint-secret",
        now=1000,
        nonce="nonce-spoof",
        route="claude_code_native",
        client_type="claude_code_native",
        formal_pool_allowed=True,
        native_attestation_allowed=True,
    )

    with pytest.raises(RuntimeOverlayError, match="catalog route binding"):
        verify_cp4_route_hint_headers(
            contract,
            source_headers=headers,
            body=body,
            request_path="/v1/messages?beta=true",
            session_ref="session-a",
            secret="route-hint-secret",
            now=1000,
        )


def test_cp4_overlay_route_hint_unknown_model_stale_catalog_and_replay_fail_closed(tmp_path: Path):
    contract = _contract(tmp_path)
    body = b'{"model":"claude-code-bridge-deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}'
    headers = build_cp4_route_hint_headers(
        contract,
        body=body,
        request_path="/v1/messages?beta=true",
        model_id="claude-code-bridge-deepseek-v4-pro",
        session_ref="session-a",
        secret="route-hint-secret",
        now=1000,
        nonce="nonce-a",
    )

    with pytest.raises(RuntimeOverlayError, match="unknown overlay model"):
        build_cp4_route_hint_headers(
            contract,
            body=b'{"model":"glm-4.6","messages":[]}',
            request_path="/v1/messages?beta=true",
            model_id="glm-4.6",
            session_ref="session-a",
            secret="route-hint-secret",
            now=1000,
            nonce="nonce-unknown",
        )
    stale_contract = build_cp4_route_hint_contract(
        build_cp3a_model_overlay_contract(build_cp2_model_overlay_proof(_runtime_plan(tmp_path))),
        catalog_hash="sha256:" + "5" * 64,
        catalog_version="cp4-test-v2",
    )
    with pytest.raises(RuntimeOverlayError, match="catalog binding"):
        verify_cp4_route_hint_headers(
            stale_contract,
            source_headers=headers,
            body=body,
            request_path="/v1/messages?beta=true",
            session_ref="session-a",
            secret="route-hint-secret",
            now=1000,
        )
    verify_cp4_route_hint_headers(
        contract,
        source_headers=headers,
        body=body,
        request_path="/v1/messages?beta=true",
        session_ref="session-a",
        secret="route-hint-secret",
        now=1000,
    )
    with pytest.raises(RuntimeOverlayError, match="replayed"):
        verify_cp4_route_hint_headers(
            contract,
            source_headers=headers,
            body=body,
            request_path="/v1/messages?beta=true",
            session_ref="session-a",
            secret="route-hint-secret",
            now=1000,
        )



def test_cp4_overlay_glm_route_hint_verifies_with_guard_catalog(tmp_path: Path):
    guard_route_trust = _guard_route_trust_module()

    contract = _contract(tmp_path)
    body = b'{"model":"claude-code-bridge-glm-5.2-1m","messages":[{"role":"user","content":"hello"}]}'
    headers = build_cp4_route_hint_headers(
        contract,
        body=body,
        request_path="/v1/messages?beta=true",
        model_id="claude-code-bridge-glm-5.2-1m",
        session_ref="session-glm",
        secret="route-hint-secret",
        now=1000,
        nonce="nonce-glm",
    )
    guard_catalog = guard_route_trust.cp4_fixture_route_catalog(
        runtime_hash=contract.overlay_contract.proof.runtime_hash,
        overlay_hash=contract.overlay_contract.proof.overlay_hash,
        catalog_hash=contract.catalog_hash,
        catalog_version=contract.catalog_version,
    )

    decision = guard_route_trust.verify_signed_route_hint_headers(
        source_headers=headers,
        body=body,
        request_path="/v1/messages?beta=true",
        catalog=guard_catalog,
        session_ref="session-glm",
        secret="route-hint-secret",
        now=1000,
        replay_cache=guard_route_trust.RouteHintReplayCache(ttl_seconds=60),
    )

    assert decision.provider == "zai_glm"
    assert decision.route == "zai_glm_bridge"
    assert decision.client_type == "claude_code_bridge_zai_glm"
    assert decision.live_request_allowed is False


def test_cp4_overlay_advertised_models_verify_with_guard_catalog(tmp_path: Path):
    guard_route_trust = _guard_route_trust_module()

    contract = _contract(tmp_path)
    guard_catalog = guard_route_trust.cp4_fixture_route_catalog(
        runtime_hash=contract.overlay_contract.proof.runtime_hash,
        overlay_hash=contract.overlay_contract.proof.overlay_hash,
        catalog_hash=contract.catalog_hash,
        catalog_version=contract.catalog_version,
    )
    for entry in contract.overlay_contract.proof.models:
        if entry.model_id not in guard_catalog.entries:
            continue
        body = (
            '{"model":' + json_escape(entry.model_id) + ',"messages":[{"role":"user","content":"hello"}]}'
        ).encode("utf-8")
        headers = build_cp4_route_hint_headers(
            contract,
            body=body,
            request_path="/v1/messages?beta=true",
            model_id=entry.model_id,
            session_ref="session-cross",
            secret="route-hint-secret",
            now=1000,
            nonce="nonce-" + entry.model_id.replace("[", "-").replace("]", "-"),
        )

        decision = guard_route_trust.verify_signed_route_hint_headers(
            source_headers=headers,
            body=body,
            request_path="/v1/messages?beta=true",
            catalog=guard_catalog,
            session_ref="session-cross",
            secret="route-hint-secret",
            now=1000,
            replay_cache=guard_route_trust.RouteHintReplayCache(ttl_seconds=60),
        )

        assert decision.route == ("claude_code_native" if entry.route == "claude_native" else entry.route)
        assert decision.client_type == entry.client_type


def test_cp4_guard_catalog_native_claude_models_are_current_curated_only(tmp_path: Path):
    guard_route_trust = _guard_route_trust_module()
    contract = _contract(tmp_path)

    guard_catalog = guard_route_trust.cp4_fixture_route_catalog(
        runtime_hash=contract.overlay_contract.proof.runtime_hash,
        overlay_hash=contract.overlay_contract.proof.overlay_hash,
        catalog_hash=contract.catalog_hash,
        catalog_version=contract.catalog_version,
    )
    native_models = {
        model_id
        for model_id, entry in guard_catalog.entries.items()
        if entry.route == "claude_code_native"
    }

    assert native_models == {
        "claude-fable-5",
        "claude-opus-4-8",
        "claude-sonnet-5",
        "claude-sonnet-4-6",
        "claude-haiku-4-5-20251001",
    }
    assert "claude-opus-4-7" not in native_models
    assert "claude-haiku-4-5" not in native_models
    assert "claude-opus-4-5-20251101" not in native_models
    assert "claude-sonnet-4-5-20250929" not in native_models


def json_escape(value: str) -> str:
    import json

    return json.dumps(value)

def test_cp4_overlay_public_api_is_exported():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        RuntimeRouteHintContract as ExportedContract,
        RouteHintReplayCache as ExportedReplayCache,
        build_cp4_route_hint_contract as exported_contract,
        build_cp4_route_hint_headers as exported_build_headers,
        verify_cp4_route_hint_headers as exported_verify,
    )

    assert ExportedContract.__name__ == "RuntimeRouteHintContract"
    assert ExportedReplayCache.__name__ == "RouteHintReplayCache"
    assert callable(exported_contract)
    assert callable(exported_build_headers)
    assert callable(exported_verify)
