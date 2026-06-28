package repository

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheMetadataSerializationPreservesFormalPoolSchedulableEvidence(t *testing.T) {
	metaPayload, err := json.Marshal(buildSchedulerMetadataAccount(formalPoolSchedulerEvidenceAccount(nil)))
	require.NoError(t, err)

	got, err := decodeCachedAccount(metaPayload)

	require.NoError(t, err)
	require.True(t, got.IsSchedulable(), "complete Formal Pool evidence must survive scheduler cache metadata serialization path")
	require.Nil(t, got.Extra["access_token"])
}

func TestBuildSchedulerMetadataAccount_PreservesFormalPoolSchedulableEvidence(t *testing.T) {
	baseExtra := formalPoolSchedulerEvidenceExtra(nil)

	got := buildSchedulerMetadataAccount(formalPoolSchedulerEvidenceAccount(nil))

	require.True(t, got.IsSchedulable(), "complete Formal Pool evidence must survive scheduler metadata slimming")
	require.Equal(t, baseExtra[service.FormalPoolExtraHealthcheckRawRef], got.Extra[service.FormalPoolExtraHealthcheckRawRef])
	require.Equal(t, true, got.Extra[service.FormalPoolExtraHealthcheckCCGatewaySeen])
	require.Equal(t, false, got.Extra[service.FormalPoolExtraHealthcheckFallbackDetected])
	require.Equal(t, false, got.Extra[service.FormalPoolExtraHealthcheckProxyMismatch])
	require.Equal(t, false, got.Extra[service.FormalPoolExtraHealthcheckRiskTextDetected])
	require.Nil(t, got.Extra["access_token"])
	require.Nil(t, got.Extra["unused_large_field"])

	missingRaw := buildSchedulerMetadataAccount(formalPoolSchedulerEvidenceAccount(map[string]any{service.FormalPoolExtraHealthcheckRawRef: ""}))
	require.False(t, missingRaw.IsSchedulable(), "missing persisted raw ref must still fail closed after metadata slimming")

	missingGatewaySeen := buildSchedulerMetadataAccount(formalPoolSchedulerEvidenceAccount(map[string]any{service.FormalPoolExtraHealthcheckCCGatewaySeen: false}))
	require.False(t, missingGatewaySeen.IsSchedulable(), "missing gateway-seen evidence must still fail closed after metadata slimming")
}

func formalPoolSchedulerEvidenceAccount(extra map[string]any) service.Account {
	return service.Account{
		ID:          84,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusActive,
		Schedulable: true,
		Extra:       formalPoolSchedulerEvidenceExtra(extra),
	}
}

func formalPoolSchedulerEvidenceExtra(extra map[string]any) map[string]any {
	merged := map[string]any{
		service.FormalPoolExtraOnboardingStage:             service.FormalPoolStageWarming,
		service.FormalPoolExtraRuntimeRegistered:           true,
		service.FormalPoolExtraRuntimeRegisteredAt:         "2026-05-30T00:00:00Z",
		service.FormalPoolExtraHealthcheckStatus:           "passed",
		service.FormalPoolExtraHealthcheckStatusCodeBucket: "status_2xx",
		service.FormalPoolExtraHealthcheckRawRef:           "hmac-sha256:" + strings.Repeat("a", 64),
		service.FormalPoolExtraHealthcheckCCGatewaySeen:    true,
		service.FormalPoolExtraHealthcheckFallbackDetected: false,
		service.FormalPoolExtraHealthcheckProxyMismatch:    false,
		service.FormalPoolExtraHealthcheckRiskTextDetected: false,
		"cc_gateway_account_ref":                           "hmac-sha256:" + strings.Repeat("b", 64),
		"cc_gateway_credential_ref":                        "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_credential_binding_hmac":               "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_proxy_identity_ref":                    "hmac-sha256:" + strings.Repeat("e", 64),
		"cc_gateway_persona_profile":                       "profile:claude-code-stable",
		"claude_code_device_id":                            strings.Repeat("f", 64),
		"cc_gateway_egress_bucket_enabled":                 "true",
		"cc_gateway_egress_bucket":                         "formal-pool-bucket",
		"access_token":                                     "must-not-enter-scheduler-metadata",
		"unused_large_field":                               "drop-me",
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func TestBuildSchedulerMetadataAccount_PreservesClaudePlatformAWSFormalPoolEvidenceAndRedactsSecrets(t *testing.T) {
	account := claudePlatformAWSFormalPoolSchedulerEvidenceAccount(nil)

	got := buildSchedulerMetadataAccount(account)
	payload, err := json.Marshal(got)

	require.NoError(t, err)
	require.True(t, got.IsSchedulable(), "Claude Platform AWS formal-pool evidence must survive scheduler metadata slimming")
	for _, key := range []string{
		"cc_gateway_account_ref",
		"cc_gateway_credential_ref",
		"cc_gateway_credential_binding_hmac",
		"cc_gateway_proxy_identity_ref",
		"cc_gateway_persona_profile",
		"claude_code_device_id",
		"cc_gateway_egress_bucket_enabled",
		"cc_gateway_egress_bucket",
		service.FormalPoolExtraRuntimeRegistered,
		service.FormalPoolExtraRuntimeRegisteredAt,
		service.ClaudePlatformAWSExtraWorkspaceRef,
		service.ClaudePlatformAWSExtraWorkspaceBindingHMAC,
		service.ClaudePlatformAWSExtraEndpointRef,
		service.ClaudePlatformAWSExtraRegion,
		service.ClaudePlatformAWSExtraAuthScheme,
		service.ClaudePlatformAWSExtraRequestShapeProfileRef,
		service.ClaudePlatformAWSExtraCacheParityProfileRef,
		service.ClaudePlatformAWSExtraBetaPolicyRef,
		service.ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus,
		service.ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus,
	} {
		require.NotEmpty(t, got.Extra[key], "missing scheduler evidence key %s", key)
	}
	require.Nil(t, got.Credentials, "scheduler metadata must not carry Claude Platform AWS credentials")
	for _, forbidden := range []string{
		"secret-api-key-sentinel",
		"secret-workspace-sentinel",
		"secret-authorization-sentinel",
		"secret-hmac-input-sentinel",
		"secret-hmac-output-sentinel",
		"secret-raw-body-sentinel",
		"secret-raw-response-sentinel",
	} {
		require.NotContains(t, string(payload), forbidden)
	}

	regionMismatch := buildSchedulerMetadataAccount(claudePlatformAWSFormalPoolSchedulerEvidenceAccount(map[string]any{service.ClaudePlatformAWSExtraRegion: "eu-west-1"}))
	require.False(t, regionMismatch.IsSchedulable(), "region mismatch must still fail closed after metadata slimming")
}

func claudePlatformAWSFormalPoolSchedulerEvidenceAccount(extra map[string]any) service.Account {
	proxyID := int64(7)
	return service.Account{
		ID:          91,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeClaudePlatformAWS,
		Status:      service.StatusActive,
		Schedulable: true,
		ProxyID:     &proxyID,
		Credentials: map[string]any{
			"api_key":                "secret-api-key-sentinel",
			"anthropic_workspace_id": "secret-workspace-sentinel",
			"authorization":          "secret-authorization-sentinel",
		},
		Extra: claudePlatformAWSFormalPoolSchedulerEvidenceExtra(extra),
	}
}

func claudePlatformAWSFormalPoolSchedulerEvidenceExtra(extra map[string]any) map[string]any {
	merged := map[string]any{
		service.FormalPoolExtraRuntimeRegistered:                       true,
		service.FormalPoolExtraRuntimeRegisteredAt:                     "2026-06-28T00:00:00Z",
		"cc_gateway_account_ref":                                       "hmac-sha256:" + strings.Repeat("a", 64),
		"cc_gateway_credential_ref":                                    "hmac-sha256:" + strings.Repeat("b", 64),
		"cc_gateway_credential_binding_hmac":                           "hmac-sha256:" + strings.Repeat("c", 64),
		"cc_gateway_proxy_identity_ref":                                "hmac-sha256:" + strings.Repeat("d", 64),
		"cc_gateway_persona_profile":                                   "profile:claude-code-stable",
		"claude_code_device_id":                                        strings.Repeat("1", 64),
		"cc_gateway_egress_bucket_enabled":                             "true",
		"cc_gateway_egress_bucket":                                     "egress:formal-pool-aws-use1",
		service.ClaudePlatformAWSExtraWorkspaceRef:                     "hmac-sha256:" + strings.Repeat("e", 64),
		service.ClaudePlatformAWSExtraWorkspaceBindingHMAC:             "hmac-sha256:" + strings.Repeat("f", 64),
		service.ClaudePlatformAWSExtraEndpointRef:                      schedulerTestFormalPoolSafeRef("endpoint", service.ClaudePlatformAWSEndpointForRegion("us-east-1")),
		service.ClaudePlatformAWSExtraRegion:                           "us-east-1",
		service.ClaudePlatformAWSExtraAuthScheme:                       service.ClaudePlatformAWSAuthProfileXAPIKey,
		service.ClaudePlatformAWSExtraRequestShapeProfileRef:           "request-shape:claude-platform-aws-v1-strip",
		service.ClaudePlatformAWSExtraCacheParityProfileRef:            "cache-profile:claude-platform-aws-v1-strip",
		service.ClaudePlatformAWSExtraBetaPolicyRef:                    "beta-policy:claude-platform-aws-v1-strip",
		service.ClaudePlatformAWSExtraCP0AuthProfileEvidenceStatus:     "pass",
		service.ClaudePlatformAWSExtraCP0RegionWorkspaceEvidenceStatus: "pass",
		"raw_hmac_input":                                               "secret-hmac-input-sentinel",
		"raw_hmac_output":                                              "secret-hmac-output-sentinel",
		"raw_prompt_body":                                              "secret-raw-body-sentinel",
		"raw_upstream_response":                                        "secret-raw-response-sentinel",
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func schedulerTestFormalPoolSafeRef(scope, raw string) string {
	mac := hmac.New(sha256.New, []byte("sub2api-gateway-sticky-session-dev-key"))
	_, _ = mac.Write([]byte("formal_pool_" + scope))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte("v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(raw)))
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}
