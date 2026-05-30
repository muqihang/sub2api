package repository

import (
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
		"cc_gateway_egress_bucket":                         "formal-pool-bucket",
		"access_token":                                     "must-not-enter-scheduler-metadata",
		"unused_large_field":                               "drop-me",
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
