import json
import os
import stat
import threading
from pathlib import Path

from zhumeng_agent.credentials import FileCredentialStore
from zhumeng_agent.security import generate_loopback_secret, redact_text
from zhumeng_agent.state import JsonStateStore, ensure_revoke_device_ready, logout_local_state


def test_generate_loopback_secret_has_entropy():
    first = generate_loopback_secret()
    second = generate_loopback_secret()

    assert first != second
    assert len(first) >= 32


def test_redact_sensitive_values():
    text = """
Authorization: Bearer managed-access-token
refresh_token=refresh-secret
device_secret=device-secret
https://example.com/setup?code=grant-code&token=query-token
sk-1234567890abcdef
"""

    redacted = redact_text(text)

    assert "managed-access-token" not in redacted
    assert "refresh-secret" not in redacted
    assert "device-secret" not in redacted
    assert "grant-code" not in redacted
    assert "query-token" not in redacted
    assert "sk-1234567890abcdef" not in redacted


def test_file_credential_store_uses_0600_permissions(tmp_path: Path):
    path = tmp_path / "credentials.json"
    store = FileCredentialStore(path)
    store.save({"device_id": 1, "refresh_token": "secret"})

    loaded = store.load()
    assert loaded["device_id"] == 1

    if os.name == "posix":
      mode = stat.S_IMODE(path.stat().st_mode)
      assert mode == 0o600


def test_state_writes_are_atomic_and_locked(tmp_path: Path):
    path = tmp_path / "state.json"
    store = JsonStateStore(path)

    def writer(value: int):
        store.write({"value": value})

    threads = [threading.Thread(target=writer, args=(1,)), threading.Thread(target=writer, args=(2,))]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    raw = path.read_text(encoding="utf-8")
    data = json.loads(raw)
    assert data["value"] in {1, 2}


def test_logout_local_only_removes_only_local_state(tmp_path: Path):
    path = tmp_path / "state.json"
    store = JsonStateStore(path)
    store.write({"device_id": 7})

    logout_local_state(store)

    assert store.read() == {}


def test_state_update_merges_without_dropping_existing_fields(tmp_path: Path):
    path = tmp_path / "state.json"
    store = JsonStateStore(path)
    store.write({"device_id": 7, "server_base_url": "https://example.com"})

    updated = store.update({"status": "reauthorization_required"})

    assert updated["device_id"] == 7
    assert updated["server_base_url"] == "https://example.com"
    assert updated["status"] == "reauthorization_required"


def test_logout_revoke_device_requires_server_and_device_info():
    try:
        ensure_revoke_device_ready({})
    except ValueError as err:
        assert "device" in str(err).lower() or "server" in str(err).lower()
    else:
        raise AssertionError("expected missing revoke context to fail")
