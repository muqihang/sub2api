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
