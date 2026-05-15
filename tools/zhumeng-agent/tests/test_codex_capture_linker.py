import json
from pathlib import Path

from zhumeng_agent.adapters.codex.capture_config import CorrelationHasher
from zhumeng_agent.adapters.codex.capture_linker import build_correlation_hashes, link_traces, load_jsonl, write_trace_links


def test_shared_correlation_hashes_match_with_same_key_and_not_different_key(tmp_path: Path):
    key_a = tmp_path / "a.key"
    key_b = tmp_path / "b.key"
    key_a.write_bytes(b"shared")
    key_b.write_bytes(b"different")
    identifiers = {
        "session_id": "session-1",
        "thread_id": "thread-1",
        "turn_id": "turn-1",
        "x_client_request_id": "request-1",
        "window_id": "window-1",
    }

    first = build_correlation_hashes(identifiers, CorrelationHasher.from_key_file(key_a))
    second = build_correlation_hashes(identifiers, CorrelationHasher.from_key_file(key_a))
    different = build_correlation_hashes(identifiers, CorrelationHasher.from_key_file(key_b))

    assert first == second
    assert first["x_client_request_id_hash"] != different["x_client_request_id_hash"]
    assert "request-1" not in json.dumps(first)


def test_correlation_hashes_accept_hyphenated_header_aliases(tmp_path: Path):
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    hashes = build_correlation_hashes({
        "x-client-request-id": "request-1",
        "x-codex-window-id": "window-1",
        "x-codex-thread-id": "thread-1",
    }, CorrelationHasher.from_key_file(key))

    assert hashes["x_client_request_id_hash"].startswith("hmac-sha256:")
    assert hashes["window_id_hash"].startswith("hmac-sha256:")
    assert hashes["thread_id_hash"].startswith("hmac-sha256:")
    assert "request-1" not in json.dumps(hashes)


def test_linker_writes_trace_link_without_raw_identifiers(tmp_path: Path):
    key_file = tmp_path / "correlation.key"
    key_file.write_bytes(b"shared")
    hasher = CorrelationHasher.from_key_file(key_file)
    hashes = build_correlation_hashes({"x_client_request_id": "request-1", "thread_id": "thread-1"}, hasher)
    desktop = [{
        "desktop_trace_id": "cd_1",
        "ts": "2026-05-14T00:00:00.000Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": hashes,
    }]
    gateway = [{
        "gateway_trace_id": "trace_1",
        "ts": "2026-05-14T00:00:00.042Z",
        "model": "deepseek-v4-pro",
        "request_path": "/codex/v1/responses",
        "correlation_hashes": hashes,
    }]

    links = link_traces(desktop, gateway)
    write_trace_links(tmp_path / "trace_link.jsonl", links)

    assert links[0]["linked_by"] == "x_client_request_id_hash"
    assert links[0]["confidence"] == "high"
    content = (tmp_path / "trace_link.jsonl").read_text(encoding="utf-8")
    assert "request-1" not in content
    assert "thread-1" not in content
    assert "trace_1" in content


def test_linker_missing_key_uses_low_confidence_time_window():
    desktop = [{"desktop_trace_id": "cd_1", "ts": "2026-05-14T00:00:00.000Z", "model": "m", "request_path": "/codex/v1/responses"}]
    gateway = [{"gateway_trace_id": "trace_1", "ts": "2026-05-14T00:00:01.000Z", "model": "m", "request_path": "/codex/v1/responses"}]

    links = link_traces(desktop, gateway)

    assert links[0]["confidence"] == "low"
    assert links[0]["linked_by"] == "time_model_path"


def test_missing_key_hashes_are_not_used_for_high_confidence_links():
    hasher = CorrelationHasher.from_key_file(None)
    hashes = build_correlation_hashes({"x_client_request_id": "request-1"}, hasher)
    desktop = [{"desktop_trace_id": "cd_1", "ts": "2026-05-14T00:00:00.000Z", "model": "m", "request_path": "/codex/v1/responses", "correlation_hashes": hashes}]
    gateway = [{"gateway_trace_id": "trace_1", "ts": "2026-05-14T00:00:00.010Z", "model": "m", "request_path": "/codex/v1/responses", "correlation_hashes": hashes}]

    links = link_traces(desktop, gateway)

    assert links[0]["confidence"] == "low"
    assert links[0]["linked_by"] == "time_model_path"


def test_sha256_hashes_loaded_from_jsonl_are_not_high_confidence_links():
    shared = {"x_client_request_id_hash": "sha256:abc"}
    desktop = [{"desktop_trace_id": "cd_1", "ts": "2026-05-14T00:00:00.000Z", "model": "m", "request_path": "/codex/v1/responses", "correlation_hashes": shared}]
    gateway = [{"gateway_trace_id": "trace_1", "ts": "2026-05-14T00:00:00.010Z", "model": "m", "request_path": "/codex/v1/responses", "correlation_hashes": shared}]

    links = link_traces(desktop, gateway)

    assert links[0]["confidence"] == "low"
    assert links[0]["linked_by"] == "time_model_path"


def test_load_jsonl_skips_bad_lines(tmp_path: Path):
    path = tmp_path / "events.jsonl"
    path.write_text('{"ok": 1}\nnot-json\n{"ok": 2}\n', encoding="utf-8")

    assert load_jsonl(path) == [{"ok": 1}, {"ok": 2}]
