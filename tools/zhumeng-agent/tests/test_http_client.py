from zhumeng_agent.http_client import AgentHTTPClient


class FakeResponse:
    def __init__(self, payload):
        self._payload = payload

    def raise_for_status(self):
        return None

    def json(self):
        return self._payload


def test_exchange_setup_grant_unwraps_success_envelope(monkeypatch):
    monkeypatch.setattr("zhumeng_agent.http_client.httpx.post", lambda *args, **kwargs: FakeResponse({
        "code": 0,
        "message": "success",
        "data": {
            "device_id": 9,
            "config_profile": {"model_provider": "zhumeng-managed"},
        },
    }))

    client = AgentHTTPClient("https://example.com")
    data = client.exchange_setup_grant(code="abc", server_origin="https://example.com")
    assert data["device_id"] == 9
    assert data["config_profile"]["model_provider"] == "zhumeng-managed"


def test_list_codex_models_uses_managed_device_headers(monkeypatch):
    captured = {}

    def fake_get(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return FakeResponse({
            "models": [
                {"slug": "deepseek-v4-pro", "display_name": "DeepSeek V4 Pro"},
            ],
        })

    monkeypatch.setattr("zhumeng_agent.http_client.httpx.get", fake_get)

    client = AgentHTTPClient("https://example.com")
    data = client.list_codex_models(
        gateway_base_url="https://gateway.example.com",
        access_token="access-token",
        managed_session_id="sess-1",
        device_id=9,
    )

    assert captured["args"][0] == "https://gateway.example.com/codex/v1/models"
    assert captured["kwargs"]["headers"]["Authorization"] == "Bearer access-token"
    assert captured["kwargs"]["headers"]["X-Zhumeng-Managed-Session"] == "sess-1"
    assert captured["kwargs"]["headers"]["X-Zhumeng-Device-ID"] == "9"
    assert data["models"][0]["slug"] == "deepseek-v4-pro"
