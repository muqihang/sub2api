package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testOpenAIEgressConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICore.Enabled = true
	cfg.Gateway.OpenAICore.DefaultEgressBucket = "default"
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = true
	cfg.Gateway.OpenAICore.AllowDirectFallback = true
	cfg.Gateway.OpenAICore.EgressBuckets = []config.OpenAIGatewayEgressBucketConfig{
		{Name: "default", Enabled: true},
		{Name: "bucket-a", Enabled: true, ProxyURL: "socks5h://user:pass@127.0.0.1:9001"},
		{Name: "bucket-disabled", Enabled: false, ProxyURL: "http://127.0.0.1:8080"},
	}
	return cfg
}

func TestOpenAIGatewayEgressSelectsEnabledBucketProxy(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"openai_gateway_egress_bucket": "bucket-a"},
	}

	resolution, err := svc.ResolveEgress(context.Background(), account, "")
	require.NoError(t, err)
	require.Equal(t, "bucket-a", resolution.BucketName)
	require.Equal(t, "socks5h://user:pass@127.0.0.1:9001", resolution.ProxyURL)
	require.True(t, resolution.ProxySelected)
	require.NotContains(t, resolution.ProxyLabel, "user")
	require.NotContains(t, resolution.ProxyLabel, "pass")
	require.NotEmpty(t, resolution.ProxyHash)
}

func TestOpenAIGatewayEgressAllowsDirectBucketInLocalMode(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = true
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)

	resolution, err := svc.ResolveEgress(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "")
	require.NoError(t, err)
	require.Equal(t, "default", resolution.BucketName)
	require.False(t, resolution.ProxySelected)
	require.Equal(t, "direct_fallback", resolution.Source)
}

func TestOpenAIGatewayEgressMissingBucketFailsClosed(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_egress_bucket": "missing"}}

	resolution, err := svc.ResolveEgress(context.Background(), account, "http://account-proxy.local:8080")
	require.Nil(t, resolution)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.True(t, errors.As(err, &policyErr))
	require.Equal(t, "missing_bucket", policyErr.Code)
	require.Equal(t, "missing", policyErr.BucketName)
}

func TestOpenAIGatewayEgressDisabledBucketFailsClosed(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-disabled"}}

	resolution, err := svc.ResolveEgress(context.Background(), account, "http://account-proxy.local:8080")
	require.Nil(t, resolution)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.True(t, errors.As(err, &policyErr))
	require.Equal(t, "disabled_bucket", policyErr.Code)
}

func TestOpenAIGatewayEgressDisabledBucketFallbackRequiresFlag(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = false
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = true
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_egress_bucket": "bucket-disabled"}}

	resolution, err := svc.ResolveEgress(context.Background(), account, "http://account-proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, "bucket-disabled", resolution.BucketName)
	require.Equal(t, "http://account-proxy.local:8080", resolution.ProxyURL)
	require.Equal(t, "account_fallback", resolution.Source)
}

func TestOpenAIGatewayEgressFallbacksFailWhenDisabled(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)

	resolution, err := svc.ResolveEgress(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "http://account-proxy.local:8080")
	require.Nil(t, resolution)
	require.Error(t, err)
	var policyErr *OpenAIEgressPolicyError
	require.True(t, errors.As(err, &policyErr))
	require.Equal(t, "direct_fallback_disabled", policyErr.Code)
}

func TestOpenAIGatewayLegacyProxyWrapperDoesNotMaskFailClosedPolicyError(t *testing.T) {
	cfg := testOpenAIEgressConfig()
	cfg.Gateway.OpenAICore.EgressFailClosed = true
	cfg.Gateway.OpenAICore.AllowAccountProxyFallback = false
	cfg.Gateway.OpenAICore.AllowDirectFallback = false
	svc := NewOpenAIGatewayCoreService(nil, cfg, nil)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"openai_gateway_egress_bucket": "missing"}}

	require.Empty(t, svc.ResolveEgressProxyURL(account, "http://account-proxy.local:8080"))
}

func TestOpenAIGatewayEgressProxyMaskAndHash(t *testing.T) {
	first := "socks5h://user:pass@127.0.0.1:9001"
	second := "socks5h://user:pass@127.0.0.1:9002"

	require.NotContains(t, MaskOpenAIProxyURL(first), "user")
	require.NotContains(t, MaskOpenAIProxyURL(first), "pass")
	require.NotEmpty(t, HashOpenAIProxyURL(first))
	require.NotEqual(t, HashOpenAIProxyURL(first), HashOpenAIProxyURL(second))
	require.Equal(t, HashOpenAIProxyURL(first), HashOpenAIProxyURL(first))
}

func TestOpenAIGatewayRuntimeSnapshotDoesNotExposeRawProxyURL(t *testing.T) {
	runtime := OpenAIGatewayAccountRuntime{
		EgressBucket:  "bucket-a",
		ProxySelected: true,
		ProxyLabel:    "socks5h://127.0.0.1:9001",
		ProxyHash:     "hash",
	}

	raw, err := json.Marshal(runtime)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "egress_proxy_url")
}

func TestOpenAIGatewayAdminBucketSnapshotDoesNotExposeRawProxyURL(t *testing.T) {
	bucket := OpenAIGatewayAdminBucketSnapshot{
		Name:          "bucket-a",
		Enabled:       true,
		ProxySelected: true,
		ProxyLabel:    "socks5h://127.0.0.1:9001",
		ProxyHash:     "hash",
		AccountCount:  1,
	}

	raw, err := json.Marshal(bucket)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "proxy_url")
}
