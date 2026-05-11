from zhumeng_agent.deeplink import TrustedOriginPolicy, parse_zhumeng_deeplink


def test_reject_non_https_server():
    policy = TrustedOriginPolicy({"https://example.com"})
    try:
        policy.validate("http://example.com")
    except ValueError as err:
        assert "https" in str(err).lower()
    else:
        raise AssertionError("expected non-https origin to be rejected")


def test_reject_loopback_and_private_ip_literals():
    policy = TrustedOriginPolicy({"https://example.com"})

    for origin in (
        "https://127.0.0.1",
        "https://localhost",
        "https://10.0.0.8",
        "https://192.168.1.20",
    ):
        try:
            policy.validate(origin)
        except ValueError:
            pass
        else:
            raise AssertionError(f"expected {origin} to be rejected")


def test_allow_configured_trusted_origin():
    policy = TrustedOriginPolicy({"https://sub2api.example.com"})
    assert policy.validate("https://sub2api.example.com") == "https://sub2api.example.com"


def test_dev_mode_allows_local_origin():
    policy = TrustedOriginPolicy({"https://sub2api.example.com"}, dev_mode=True)
    assert policy.validate("http://127.0.0.1:8080") == "http://127.0.0.1:8080"


def test_parse_setup_deeplink():
    parsed = parse_zhumeng_deeplink("zhumeng-agent://setup?client=codex&code=abc&server=https%3A%2F%2Fexample.com")
    assert parsed == {
        "client": "codex",
        "code": "abc",
        "server": "https://example.com",
    }
