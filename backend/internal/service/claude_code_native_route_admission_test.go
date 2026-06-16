package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClaudeCodeNativeCatalogAdmissionResolver struct {
	decisions map[string]claudeCodeNativeCatalogAdmissionDecision
	calls     []string
}

func (f *fakeClaudeCodeNativeCatalogAdmissionResolver) ResolveClaudeCodeNativeCatalogAdmission(model string) (claudeCodeNativeCatalogAdmissionDecision, error) {
	f.calls = append(f.calls, model)
	if decision, ok := f.decisions[model]; ok {
		return decision, nil
	}
	return claudeCodeNativeCatalogAdmissionDecision{}, nil
}

func TestClaudeCodeNativeAdmissionUsesServerCatalogNotPayloadRouteHint(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(3100, 0)
	model := "server-catalog-native-sonnet"
	body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model))
	resolver := &fakeClaudeCodeNativeCatalogAdmissionResolver{decisions: map[string]claudeCodeNativeCatalogAdmissionDecision{
		model: {
			ModelID:         model,
			Route:           ClaudeCodeNativeRoute,
			ProviderOwner:   ClaudeCodeNativeProviderOwner,
			CredentialScope: ClaudeCodeNativeCredentialScope,
			GatewayLocation: ClaudeCodeNativeGatewayLocation,
			CatalogFresh:    true,
		},
	}}
	headers := signedNativeHeadersForTest(t, body, "/v1/messages?beta=true", now, map[string]any{"nonce": "server-catalog-native"})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(resolver),
	)

	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages?beta=true", headers, body)

	require.NoError(t, err)
	require.True(t, summary.NativeAttested)
	require.Equal(t, []string{model}, resolver.calls)
}

func TestClaudeCodeNativeAdmissionRejectsSignedFormalHintWhenCatalogRouteIsBridge(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(3200, 0)
	model := "claude-server-catalog-bridge-sonnet"
	body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model))
	resolver := &fakeClaudeCodeNativeCatalogAdmissionResolver{decisions: map[string]claudeCodeNativeCatalogAdmissionDecision{
		model: {
			ModelID:         model,
			Route:           "bridge",
			ProviderOwner:   ClaudeCodeNativeProviderOwner,
			CredentialScope: "bridge_pool",
			GatewayLocation: ClaudeCodeNativeGatewayLocation,
			CatalogFresh:    true,
		},
	}}
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "signed-formal-bridge"})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(resolver),
	)

	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")
	require.Equal(t, []string{model}, resolver.calls)
}

func TestClaudeCodeNativeAdmissionRejectsUnknownOrStaleCatalogModel(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(3300, 0)
	cases := []struct {
		name     string
		model    string
		decision claudeCodeNativeCatalogAdmissionDecision
	}{
		{name: "unknown", model: "claude-server-catalog-unknown-sonnet"},
		{
			name:  "stale",
			model: "claude-server-catalog-stale-sonnet",
			decision: claudeCodeNativeCatalogAdmissionDecision{
				ModelID:         "claude-server-catalog-stale-sonnet",
				Route:           ClaudeCodeNativeRoute,
				ProviderOwner:   ClaudeCodeNativeProviderOwner,
				CredentialScope: ClaudeCodeNativeCredentialScope,
				GatewayLocation: ClaudeCodeNativeGatewayLocation,
				CatalogFresh:    false,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, tc.model))
			resolver := &fakeClaudeCodeNativeCatalogAdmissionResolver{decisions: map[string]claudeCodeNativeCatalogAdmissionDecision{tc.model: tc.decision}}
			headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "catalog-" + tc.name})
			svc := NewClaudeCodeNativeAttestationService(
				WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
				WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
				withClaudeCodeNativeCatalogAdmissionResolver(resolver),
			)

			_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

			require.Error(t, err)
			require.Contains(t, err.Error(), "catalog admission")
			require.Equal(t, []string{tc.model}, resolver.calls)
		})
	}
}

func TestClaudeCodeNativeAdmissionRejectsExternalAnthropicCompatBridge(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(3400, 0)
	model := "external-claude-compatible"
	body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model))
	resolver := &fakeClaudeCodeNativeCatalogAdmissionResolver{decisions: map[string]claudeCodeNativeCatalogAdmissionDecision{
		model: {
			ModelID:         model,
			Route:           "claude_code_bridge_anthropic_compat",
			ProviderOwner:   "user_owned",
			CredentialScope: "bridge_pool",
			GatewayLocation: ClaudeCodeNativeGatewayLocation,
			CatalogFresh:    true,
		},
	}}
	headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "external-anthropic-compat"})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
		withClaudeCodeNativeCatalogAdmissionResolver(resolver),
	)

	_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")
	require.Equal(t, []string{model}, resolver.calls)
}

func TestClaudeCodeNativeAdmissionRequiresAllFormalPoolBindings(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	now := time.Unix(3500, 0)
	base := claudeCodeNativeCatalogAdmissionDecision{
		ModelID:         "claude-formal-binding-test",
		Route:           ClaudeCodeNativeRoute,
		ProviderOwner:   ClaudeCodeNativeProviderOwner,
		CredentialScope: ClaudeCodeNativeCredentialScope,
		GatewayLocation: ClaudeCodeNativeGatewayLocation,
		CatalogFresh:    true,
	}
	cases := []struct {
		name   string
		mutate func(*claudeCodeNativeCatalogAdmissionDecision)
	}{
		{name: "provider_owner", mutate: func(d *claudeCodeNativeCatalogAdmissionDecision) { d.ProviderOwner = "external" }},
		{name: "credential_scope", mutate: func(d *claudeCodeNativeCatalogAdmissionDecision) { d.CredentialScope = "bridge_pool" }},
		{name: "gateway_location", mutate: func(d *claudeCodeNativeCatalogAdmissionDecision) { d.GatewayLocation = "local" }},
		{name: "catalog_fresh", mutate: func(d *claudeCodeNativeCatalogAdmissionDecision) { d.CatalogFresh = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := base
			tc.mutate(&decision)
			model := decision.ModelID
			body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}]}`, model))
			resolver := &fakeClaudeCodeNativeCatalogAdmissionResolver{decisions: map[string]claudeCodeNativeCatalogAdmissionDecision{model: decision}}
			headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{"nonce": "formal-binding-" + tc.name})
			svc := NewClaudeCodeNativeAttestationService(
				WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
				WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
				withClaudeCodeNativeCatalogAdmissionResolver(resolver),
			)

			_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

			require.Error(t, err)
			require.Contains(t, err.Error(), "catalog admission")
		})
	}
}

func TestClaudeCodeNativeAdmissionUsesProviderRegistryEnvCatalogSourceOfTruth(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-server-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"claude-provider-registry-sonnet","provider":"claude","route":"claude_code_native","client_type":"claude_code_native","provider_owner":"zhumeng_managed","credential_scope":"formal_pool","gateway_location":"cloud","catalog_fresh":true,"formal_pool_allowed":true,"native_attestation_allowed":true},{"model_id":"deepseek-v4-pro","provider":"deepseek","route":"deepseek_bridge","client_type":"claude_code_bridge_deepseek","provider_owner":"zhumeng_managed","credential_scope":"bridge_pool","gateway_location":"cloud","catalog_fresh":true}]}`)
	now := time.Unix(3600, 0)
	nativeBody := []byte(`{"model":"claude-provider-registry-sonnet","messages":[{"role":"user","content":"hello"}]}`)
	nativeHeaders := signedNativeHeadersForTest(t, nativeBody, "/v1/messages", now, map[string]any{
		"nonce":           "provider-registry-native",
		"model_id":        "claude-provider-registry-sonnet",
		"runtime_hash":    "sha256:" + stringOf('1', 64),
		"overlay_hash":    "sha256:" + stringOf('2', 64),
		"catalog_hash":    "sha256:" + stringOf('3', 64),
		"catalog_version": "cp5-server-catalog",
	})
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)

	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", nativeHeaders, nativeBody)
	require.NoError(t, err)
	require.True(t, summary.NativeAttested)

	bridgeBody := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`)
	bridgeHeaders := signedNativeHeadersForTest(t, bridgeBody, "/v1/messages", now, map[string]any{"nonce": "provider-registry-bridge"})
	_, err = svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", bridgeHeaders, bridgeBody)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog admission")
}

func TestClaudeCodeNativeAdmissionBindsProviderCatalogHashesAndVersion(t *testing.T) {
	t.Setenv("SUB2API_CLAUDE_CODE_NATIVE_ATTESTATION_SECRET", "native-attestation-test-secret")
	t.Setenv("SUB2API_CLAUDE_CODE_PROVIDER_CATALOG_JSON", `{"catalog_version":"cp5-server-catalog","runtime_hash":"sha256:1111111111111111111111111111111111111111111111111111111111111111","overlay_hash":"sha256:2222222222222222222222222222222222222222222222222222222222222222","catalog_hash":"sha256:3333333333333333333333333333333333333333333333333333333333333333","models":[{"model_id":"claude-provider-registry-sonnet","provider":"claude","route":"claude_code_native","client_type":"claude_code_native","provider_owner":"zhumeng_managed","credential_scope":"formal_pool","gateway_location":"cloud","catalog_fresh":true,"formal_pool_allowed":true,"native_attestation_allowed":true}]}`)
	now := time.Unix(3650, 0)
	body := []byte(`{"model":"claude-provider-registry-sonnet","messages":[{"role":"user","content":"hello"}]}`)
	svc := NewClaudeCodeNativeAttestationService(
		WithClaudeCodeNativeAttestationNowFunc(func() time.Time { return now }),
		WithClaudeCodeNativeAttestationReplayCache(NewClaudeCodeNativeNonceReplayCache(time.Minute, func() time.Time { return now })),
	)

	matching := signedNativeHeadersForTest(t, body, "/v1/messages", now, map[string]any{
		"nonce":           "provider-registry-catalog-match",
		"model_id":        "claude-provider-registry-sonnet",
		"runtime_hash":    "sha256:" + stringOf('1', 64),
		"overlay_hash":    "sha256:" + stringOf('2', 64),
		"catalog_hash":    "sha256:" + stringOf('3', 64),
		"catalog_version": "cp5-server-catalog",
	})
	matching.Set(ClaudeCodeNativeCatalogVersionHeader, "cp5-server-catalog")
	summary, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", matching, body)
	require.NoError(t, err)
	require.Equal(t, "sha256:"+stringOf('1', 64), summary.RuntimeHash)
	require.Equal(t, "cp5-server-catalog", summary.CatalogVersion)

	cases := []struct {
		name      string
		overrides map[string]any
		header    string
	}{
		{name: "runtime", overrides: map[string]any{"runtime_hash": "sha256:" + stringOf('9', 64)}, header: "cp5-server-catalog"},
		{name: "overlay", overrides: map[string]any{"overlay_hash": "sha256:" + stringOf('9', 64)}, header: "cp5-server-catalog"},
		{name: "catalog_hash", overrides: map[string]any{"catalog_hash": "sha256:" + stringOf('9', 64)}, header: "cp5-server-catalog"},
		{name: "payload_catalog_version", overrides: map[string]any{"catalog_version": "stale-catalog"}, header: "cp5-server-catalog"},
		{name: "header_catalog_version", overrides: map[string]any{"catalog_version": "cp5-server-catalog"}, header: "stale-catalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			overrides := map[string]any{
				"nonce":           "provider-registry-catalog-" + tc.name,
				"model_id":        "claude-provider-registry-sonnet",
				"runtime_hash":    "sha256:" + stringOf('1', 64),
				"overlay_hash":    "sha256:" + stringOf('2', 64),
				"catalog_hash":    "sha256:" + stringOf('3', 64),
				"catalog_version": "cp5-server-catalog",
			}
			for key, value := range tc.overrides {
				overrides[key] = value
			}
			headers := signedNativeHeadersForTest(t, body, "/v1/messages", now, overrides)
			headers.Set(ClaudeCodeNativeCatalogVersionHeader, tc.header)

			_, err := svc.VerifyMessagesRequest(http.MethodPost, "/v1/messages", headers, body)

			require.Error(t, err)
			require.Contains(t, err.Error(), "catalog admission")
		})
	}
}
