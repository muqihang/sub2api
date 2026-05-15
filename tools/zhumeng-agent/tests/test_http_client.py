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
