package service

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodeRuntimeMappingArtifactCheckerRequiresPrivatePermissionsAndIgnoresInvalidShape(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime-mapping")
	path := filepath.Join(dir, "mapping.json")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"account_ref":"opaque:acct:runtime","egress_bucket":"bucket-a","device_ref":"opaque:device:runtime","session_ref":"hmac-sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`), 0o600))

	result := CheckClaudeCodeRuntimeMappingArtifact(path)
	require.True(t, result.OK)
	require.Equal(t, "pass", result.Status)
	require.NoError(t, ValidateNoRawSensitiveLedger(result))

	require.NoError(t, os.Chmod(path, 0o644))
	result = CheckClaudeCodeRuntimeMappingArtifact(path)
	require.False(t, result.OK)
	require.Equal(t, "unsafe_file_permissions", result.Code)

	require.NoError(t, os.Chmod(path, 0o600))
	rawAccountUUID := strings.Join([]string{"123e4567-e89b", "-42d3-a456-", "426614174999"}, "")
	require.NoError(t, os.WriteFile(path, []byte(`{"account_ref":"`+rawAccountUUID+`","egress_bucket":"bucket-a"}`), 0o600))
	result = CheckClaudeCodeRuntimeMappingArtifact(path)
	require.False(t, result.OK)
	require.Equal(t, "invalid_shape", result.Code)
	dumped, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(dumped), rawAccountUUID)
}

func TestFormalPoolNativeHardGateRiskRecordUsesSafeRefsOnly(t *testing.T) {
	account := newCCGatewayBoundaryAccount()
	formalPoolApplyCompleteSchedulingEvidenceForTest(account)
	account.Extra[FormalPoolExtraOnboardingStage] = FormalPoolStageProduction
	account.Extra[FormalPoolExtraPoolProfileEffective] = PoolProfileNormal
	account.Extra[ccGatewayExtraAccountRef] = "opaque:acct:formal-hardgate"
	rawSessionID := strings.Join([]string{"11111111-2222", "-4333-8444-", "555555555555"}, "")
	rawUserID := strings.Join([]string{"forged", "@", "example.invalid"}, "")
	rawAccountUUID := strings.Join([]string{"123e4567-e89b", "-42d3-a456-", "426614174999"}, "")
	authWord := strings.Join([]string{"auth", "orization"}, "")
	rawPromptWord := strings.Join([]string{"raw", " prompt"}, "")
	unsafeReason := strings.Join([]string{
		authWord,
		": Bearer fixture-token ",
		rawPromptWord,
		` {"account_uuid":"`,
		rawAccountUUID,
		`"}`,
	}, "")

	for _, tc := range []struct {
		name       string
		gate       string
		status     int
		wantAction string
	}{
		{name: "401", gate: "401", status: http.StatusUnauthorized, wantAction: BudgetActionP0Block},
		{name: "403", gate: "403", status: http.StatusForbidden, wantAction: BudgetActionP0Block},
		{name: "429", gate: "429_retry_loop", status: http.StatusTooManyRequests, wantAction: BudgetActionCooldown},
		{name: "proxy", gate: "proxy_account_mismatch", status: http.StatusBadGateway, wantAction: BudgetActionP0Block},
		{name: "runtime", gate: "missing_runtime_mapping", status: http.StatusBadGateway, wantAction: BudgetActionP0Block},
		{name: "healthcheck", gate: "healthcheck_failure", status: http.StatusBadGateway, wantAction: BudgetActionP0Block},
		{name: "identity", gate: "identity_boundary_failure", status: http.StatusBadGateway, wantAction: BudgetActionP0Block},
		{name: "shape", gate: "final_shape_verifier_failure", status: http.StatusBadGateway, wantAction: BudgetActionP0Block},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := BuildFormalPoolNativeHardGateRiskRecord(FormalPoolNativeHardGateInput{
				Account:         account,
				RawSessionID:    rawSessionID,
				RawUserID:       rawUserID,
				Gate:            tc.gate,
				Status:          tc.status,
				UnsafeRawReason: unsafeReason,
			})
			require.NotEmpty(t, record.RiskEvents)
			require.Equal(t, tc.wantAction, record.Decision.Action)
			require.NoError(t, ValidateNoRawSensitiveLedger(record))
			dumped, err := json.Marshal(record)
			require.NoError(t, err)
			text := string(dumped)
			require.NotContains(t, text, rawSessionID)
			require.NotContains(t, text, rawUserID)
			require.NotContains(t, text, rawAccountUUID)
			require.NotContains(t, strings.ToLower(text), rawPromptWord)
			require.NotContains(t, strings.ToLower(text), authWord)
		})
	}
}
